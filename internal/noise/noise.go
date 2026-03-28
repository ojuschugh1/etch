package noise

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/ojuschugh1/etch/internal/snapshot"
)

// known patterns that indicate dynamic/noisy values
var patterns = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{"uuid", regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)},
	{"iso-timestamp", regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)},
	{"unix-timestamp", regexp.MustCompile(`^1[0-9]{9}$`)},                                                           // 10-digit unix ts
	{"unix-timestamp-ms", regexp.MustCompile(`^1[0-9]{12}$`)},                                                       // 13-digit unix ts ms
	{"http-date", regexp.MustCompile(`^(Mon|Tue|Wed|Thu|Fri|Sat|Sun), \d{2} (Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)`)},
	{"jwt", regexp.MustCompile(`^eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`)},
	{"hex-id", regexp.MustCompile(`^[0-9a-fA-F]{24,64}$`)},
	{"aws-trace", regexp.MustCompile(`^Root=1-[0-9a-f]{8}-[0-9a-f]{24}$`)},
	{"request-id-like", regexp.MustCompile(`^[0-9a-f]{8,}(-[0-9a-f]{4,}){2,}`)},
}

// known noisy field name patterns
var noisyNames = []string{
	"timestamp", "created_at", "updated_at", "modified_at", "deleted_at",
	"date", "time", "expires", "expires_at", "not_before",
	"request_id", "requestid", "trace_id", "traceid", "correlation_id",
	"x-request-id", "x-amzn-trace-id", "x-amzn-requestid",
	"etag", "last-modified", "x-cache", "cf-ray", "x-served-by",
	"nonce", "token", "session_id", "csrf",
}

// NoisyField represents a field that was detected as likely noisy.
type NoisyField struct {
	Path         string
	Reason       string
	Confidence   float64 // 0.0 to 1.0
	UniqueValues int
	TotalSeen    int
	Pattern      string // which pattern matched, if any
}

// DetectResult holds the output of noise analysis.
type DetectResult struct {
	Fields          []NoisyField
	SuggestedIgnore []string // lines to add to .etchignore
}

// Detect identifies noisy fields across all recorded snapshots.
func Detect(store *snapshot.SnapshotStore) (*DetectResult, error) {
	all, err := store.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("loading snapshots: %w", err)
	}

	// group entries by endpoint
	type epKey struct{ method, path string }
	grouped := make(map[epKey][]snapshot.SnapshotEntry)

	for _, sf := range all {
		for _, entry := range sf {
			parsed, _ := url.Parse(entry.URL)
			if parsed == nil {
				continue
			}
			key := epKey{entry.Method, parsed.Path}
			grouped[key] = append(grouped[key], entry)
		}
	}

	var allNoisy []NoisyField
	seen := make(map[string]bool)

	for _, entries := range grouped {
		noisy := detectNoisyInEntries(entries)
		for _, n := range noisy {
			if !seen[n.Path] {
				seen[n.Path] = true
				allNoisy = append(allNoisy, n)
			}
		}
	}

	// also check headers across all entries
	headerNoisy := detectNoisyHeaders(all)
	for _, n := range headerNoisy {
		if !seen[n.Path] {
			seen[n.Path] = true
			allNoisy = append(allNoisy, n)
		}
	}

	sort.Slice(allNoisy, func(i, j int) bool {
		return allNoisy[i].Path < allNoisy[j].Path
	})

	// build suggested .etchignore lines
	var suggestions []string
	for _, n := range allNoisy {
		suggestions = append(suggestions, n.Path)
	}

	return &DetectResult{
		Fields:          allNoisy,
		SuggestedIgnore: suggestions,
	}, nil
}

func detectNoisyInEntries(entries []snapshot.SnapshotEntry) []NoisyField {
	if len(entries) < 2 {
		return nil
	}

	// collect values per field path across all entries
	type fieldValues struct {
		values map[string]bool
		count  int
	}
	fields := make(map[string]*fieldValues)

	for _, entry := range entries {
		if entry.Body == "" {
			continue
		}
		var parsed interface{}
		if json.Unmarshal([]byte(entry.Body), &parsed) != nil {
			continue
		}
		walkJSON("body", parsed, func(path string, val interface{}) {
			fv, ok := fields[path]
			if !ok {
				fv = &fieldValues{values: make(map[string]bool)}
				fields[path] = fv
			}
			fv.count++
			if val != nil {
				fv.values[fmt.Sprintf("%v", val)] = true
			}
		})
	}

	var noisy []NoisyField
	for path, fv := range fields {
		if fv.count < 2 {
			continue
		}

		reason := ""

		// check 1: field name matches known noisy patterns
		if isNoisyName(path) {
			reason = "field name suggests dynamic value"
		}

		// check 2: every recording has a different value (high cardinality)
		if len(fv.values) == fv.count && fv.count >= 2 {
			if reason == "" {
				reason = "unique value in every recording"
			}
		}

		// check 3: values match known patterns (UUID, timestamp, etc.)
		if reason == "" {
			for v := range fv.values {
				if p := matchPattern(v); p != "" {
					reason = fmt.Sprintf("values match %s pattern", p)
					break
				}
			}
		}

		if reason != "" {
			patternName := ""
			for v := range fv.values {
				patternName = matchPattern(v)
				if patternName != "" {
					break
				}
			}

			// compute confidence based on evidence strength
			conf := 0.0
			if isNoisyName(path) {
				conf += 0.3
			}
			if len(fv.values) == fv.count && fv.count >= 2 {
				conf += 0.4 // every recording has unique value
			} else if float64(len(fv.values))/float64(fv.count) > 0.8 {
				conf += 0.2 // most recordings have unique values
			}
			if patternName != "" {
				conf += 0.3 // matches a known dynamic pattern
			}
			if conf > 1.0 {
				conf = 1.0
			}

			// lower confidence for fields that look important
			if looksImportant(path) {
				conf *= 0.5
			}

			// fewer than 3 recordings = less reliable signal
			if fv.count < 3 {
				conf *= 0.6
			}

			noisy = append(noisy, NoisyField{
				Path:         path,
				Reason:       reason,
				Confidence:   conf,
				UniqueValues: len(fv.values),
				TotalSeen:    fv.count,
				Pattern:      patternName,
			})
		}
	}

	return noisy
}

func detectNoisyHeaders(all map[string]snapshot.SnapshotFile) []NoisyField {
	// collect header values across all entries
	type headerVals struct {
		values map[string]bool
		count  int
	}
	headers := make(map[string]*headerVals)

	for _, sf := range all {
		for _, entry := range sf {
			for key, vals := range entry.Headers {
				path := "headers." + key
				hv, ok := headers[path]
				if !ok {
					hv = &headerVals{values: make(map[string]bool)}
					headers[path] = hv
				}
				hv.count++
				for _, v := range vals {
					hv.values[v] = true
				}
			}
		}
	}

	var noisy []NoisyField
	for path, hv := range headers {
		if hv.count < 2 {
			continue
		}

		reason := ""
		if isNoisyName(path) {
			reason = "header name suggests dynamic value"
		}
		if len(hv.values) == hv.count && hv.count >= 2 {
			if reason == "" {
				reason = "unique value in every recording"
			}
		}
		if reason == "" {
			for v := range hv.values {
				if p := matchPattern(v); p != "" {
					reason = fmt.Sprintf("values match %s pattern", p)
					break
				}
			}
		}

		if reason != "" {
			conf := 0.0
			if isNoisyName(path) {
				conf += 0.4
			}
			if len(hv.values) == hv.count && hv.count >= 2 {
				conf += 0.4
			}
			patternName := ""
			for v := range hv.values {
				if p := matchPattern(v); p != "" {
					patternName = p
					conf += 0.2
					break
				}
			}
			if conf > 1.0 {
				conf = 1.0
			}

			noisy = append(noisy, NoisyField{
				Path:         path,
				Reason:       reason,
				Confidence:   conf,
				UniqueValues: len(hv.values),
				TotalSeen:    hv.count,
				Pattern:      patternName,
			})
		}
	}

	return noisy
}

func isNoisyName(path string) bool {
	lower := strings.ToLower(path)
	for _, name := range noisyNames {
		if strings.HasSuffix(lower, name) || strings.Contains(lower, "."+name) {
			return true
		}
	}
	return false
}

func matchPattern(value string) string {
	for _, p := range patterns {
		if p.pattern.MatchString(value) {
			return p.name
		}
	}
	return ""
}

// looksImportant returns true for fields that are likely business-critical,
// even if their values change frequently.
func looksImportant(path string) bool {
	important := []string{
		"price", "amount", "total", "cost", "balance", "fee",
		"status", "state", "role", "permission", "enabled", "active",
		"email", "name", "username", "count", "quantity",
	}
	lower := strings.ToLower(path)
	for _, kw := range important {
		if strings.HasSuffix(lower, kw) || strings.Contains(lower, "."+kw) {
			return true
		}
	}
	return false
}

func walkJSON(prefix string, val interface{}, visit func(string, interface{})) {
	switch v := val.(type) {
	case map[string]interface{}:
		for key, child := range v {
			walkJSON(prefix+"."+key, child, visit)
		}
	case []interface{}:
		for i, child := range v {
			walkJSON(fmt.Sprintf("%s[%d]", prefix, i), child, visit)
		}
	default:
		visit(prefix, val)
	}
}

// Format produces a readable report of detected noisy fields.
func (r *DetectResult) Format() string {
	if len(r.Fields) == 0 {
		return "No noisy fields detected. Your snapshots look clean.\n"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Detected %d likely noisy field(s):\n\n", len(r.Fields)))

	for _, f := range r.Fields {
		confPct := int(f.Confidence * 100)
		b.WriteString(fmt.Sprintf("  %-40s %3d%%  %s\n", f.Path, confPct, f.Reason))
		if f.Pattern != "" {
			b.WriteString(fmt.Sprintf("  %-40s       pattern: %s\n", "", f.Pattern))
		}
	}

	// only suggest fields with confidence >= 60%
	var highConf []string
	for _, f := range r.Fields {
		if f.Confidence >= 0.6 {
			highConf = append(highConf, f.Path)
		}
	}

	if len(highConf) > 0 {
		b.WriteString("\nSuggested .etchignore additions (confidence >= 60%):\n\n")
		for _, line := range highConf {
			b.WriteString("  " + line + "\n")
		}
	}

	return b.String()
}

// FormatEtchignore returns the suggested rules as a ready-to-use .etchignore file.
func (r *DetectResult) FormatEtchignore() string {
	if len(r.SuggestedIgnore) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("# auto-detected noisy fields\n")
	for _, line := range r.SuggestedIgnore {
		b.WriteString(line + "\n")
	}
	return b.String()
}
