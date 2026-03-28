package constraint

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/ojuschugh1/etch/internal/snapshot"
)

// Rule is a learned invariant about a field across multiple recordings.
type Rule struct {
	Path       string // e.g. "body.user.name"
	Type       string // "string", "number", "boolean", "null", "array", "object"
	AlwaysSet  bool   // field was present in every recording
	NeverNull  bool   // field was never null
	Values     []string // unique values seen (capped at 10)
	Endpoint   string // e.g. "GET /users"
}

// Report holds all learned constraints grouped by endpoint.
type Report struct {
	Endpoints map[string][]Rule
}

// Learn analyzes all recorded snapshots and extracts field-level constraints.
func Learn(store *snapshot.SnapshotStore) (*Report, error) {
	all, err := store.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("loading snapshots: %w", err)
	}

	// group entries by endpoint (method + path)
	type endpointKey struct{ method, path string }
	grouped := make(map[endpointKey][]snapshot.SnapshotEntry)

	for _, sf := range all {
		for _, entry := range sf {
			parsed, err := url.Parse(entry.URL)
			if err != nil {
				continue
			}
			key := endpointKey{entry.Method, parsed.Path}
			grouped[key] = append(grouped[key], entry)
		}
	}

	report := &Report{Endpoints: make(map[string][]Rule)}

	for key, entries := range grouped {
		epName := key.method + " " + key.path
		rules := learnFromEntries(entries, epName)
		if len(rules) > 0 {
			report.Endpoints[epName] = rules
		}
	}

	return report, nil
}

func learnFromEntries(entries []snapshot.SnapshotEntry, endpoint string) []Rule {
	// collect all field observations across recordings
	type fieldObs struct {
		types    map[string]int
		present  int
		nonNull  int
		values   map[string]bool
	}

	fields := make(map[string]*fieldObs)
	total := 0

	for _, entry := range entries {
		if entry.Body == "" {
			continue
		}

		var parsed interface{}
		if err := json.Unmarshal([]byte(entry.Body), &parsed); err != nil {
			continue
		}

		total++
		walkJSON("body", parsed, func(path string, val interface{}) {
			obs, ok := fields[path]
			if !ok {
				obs = &fieldObs{types: make(map[string]int), values: make(map[string]bool)}
				fields[path] = obs
			}
			obs.present++

			typeName := jsonTypeName(val)
			obs.types[typeName]++

			if val != nil {
				obs.nonNull++
				// track unique scalar values (cap at 10 to avoid bloat)
				if typeName == "string" || typeName == "number" || typeName == "boolean" {
					if len(obs.values) < 10 {
						obs.values[fmt.Sprintf("%v", val)] = true
					}
				}
			}
		})
	}

	if total == 0 {
		return nil
	}

	var rules []Rule
	for path, obs := range fields {
		// determine dominant type
		bestType := ""
		bestCount := 0
		for t, c := range obs.types {
			if c > bestCount {
				bestType = t
				bestCount = c
			}
		}

		vals := make([]string, 0, len(obs.values))
		for v := range obs.values {
			vals = append(vals, v)
		}
		sort.Strings(vals)

		rules = append(rules, Rule{
			Path:      path,
			Type:      bestType,
			AlwaysSet: obs.present == total,
			NeverNull: obs.nonNull == obs.present,
			Values:    vals,
			Endpoint:  endpoint,
		})
	}

	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Path < rules[j].Path
	})

	return rules
}

func walkJSON(prefix string, val interface{}, visit func(string, interface{})) {
	visit(prefix, val)

	switch v := val.(type) {
	case map[string]interface{}:
		for key, child := range v {
			walkJSON(prefix+"."+key, child, visit)
		}
	case []interface{}:
		for i, child := range v {
			walkJSON(fmt.Sprintf("%s[%d]", prefix, i), child, visit)
		}
	}
}

func jsonTypeName(v interface{}) string {
	switch v.(type) {
	case map[string]interface{}:
		return "object"
	case []interface{}:
		return "array"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case nil:
		return "null"
	default:
		return "unknown"
	}
}

// Format produces a readable summary of learned constraints.
func (r *Report) Format() string {
	if len(r.Endpoints) == 0 {
		return "No constraints learned. Record more traffic for better results.\n"
	}

	var b strings.Builder

	eps := make([]string, 0, len(r.Endpoints))
	for ep := range r.Endpoints {
		eps = append(eps, ep)
	}
	sort.Strings(eps)

	for _, ep := range eps {
		rules := r.Endpoints[ep]
		b.WriteString(fmt.Sprintf("%s (%d fields)\n", ep, len(rules)))

		for _, rule := range rules {
			flags := []string{rule.Type}
			if rule.AlwaysSet {
				flags = append(flags, "always present")
			}
			if rule.NeverNull {
				flags = append(flags, "never null")
			}
			b.WriteString(fmt.Sprintf("  %-40s %s\n", rule.Path, strings.Join(flags, ", ")))

			if len(rule.Values) > 0 && len(rule.Values) <= 5 {
				b.WriteString(fmt.Sprintf("  %-40s values: %s\n", "", strings.Join(rule.Values, ", ")))
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}
