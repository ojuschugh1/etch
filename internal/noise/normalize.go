package noise

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
)

// Normalizer replaces dynamic values (UUIDs, timestamps, JWTs) with stable
// placeholders to prevent false diffs without needing .etchignore rules.
type Normalizer struct {
	rules   []normRule
	Explain bool // when true, log each normalization to stderr
}

type normRule struct {
	pattern     *regexp.Regexp
	replacement string
}

// NewNormalizer creates a normalizer with the standard set of replacement rules.
func NewNormalizer() *Normalizer {
	return &Normalizer{
		rules: []normRule{
			{regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`), "<uuid>"},
			{regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[^\s"]*`), "<timestamp>"},
			{regexp.MustCompile(`(Mon|Tue|Wed|Thu|Fri|Sat|Sun), \d{2} (Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) \d{4} \d{2}:\d{2}:\d{2} [A-Z]+`), "<http-date>"},
			{regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`), "<jwt>"},
			{regexp.MustCompile(`Root=1-[0-9a-f]{8}-[0-9a-f]{24}`), "<aws-trace-id>"},
		},
	}
}

// NormalizeBody normalizes a response body. Walks JSON values if valid JSON, otherwise normalizes raw string.
func (n *Normalizer) NormalizeBody(body string) string {
	var parsed interface{}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		// not JSON, normalize the raw string
		return n.normalizeString(body)
	}

	normalized := n.normalizeValue(parsed)
	// use an encoder that doesn't escape HTML characters
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(normalized); err != nil {
		return n.normalizeString(body)
	}
	// Encode adds a trailing newline, strip it
	return strings.TrimSpace(buf.String())
}

// NormalizeHeaders applies normalization rules to header values.
func (n *Normalizer) NormalizeHeaders(headers map[string][]string) map[string][]string {
	result := make(map[string][]string, len(headers))
	for k, vals := range headers {
		normalized := make([]string, len(vals))
		for i, v := range vals {
			normalized[i] = n.normalizeString(v)
		}
		result[k] = normalized
	}
	return result
}

func (n *Normalizer) normalizeValue(v interface{}) interface{} {
	switch val := v.(type) {
	case string:
		return n.normalizeString(val)
	case map[string]interface{}:
		result := make(map[string]interface{}, len(val))
		for k, child := range val {
			result[k] = n.normalizeValue(child)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, child := range val {
			result[i] = n.normalizeValue(child)
		}
		return result
	default:
		return v
	}
}

func (n *Normalizer) normalizeString(s string) string {
	for _, rule := range n.rules {
		if rule.pattern.MatchString(s) {
			result := rule.pattern.ReplaceAllString(s, rule.replacement)
			if n.Explain && result != s {
				log.Printf("[explain] normalized %q -> %q (matched %s)", s, result, rule.replacement)
			}
			s = result
		}
	}
	return s
}

// HasDynamicContent returns true if the string contains dynamic patterns (UUIDs, timestamps, etc.).
func (n *Normalizer) HasDynamicContent(s string) bool {
	for _, rule := range n.rules {
		if rule.pattern.MatchString(s) {
			return true
		}
	}
	return false
}

// StripDynamic replaces dynamic patterns with placeholders, returns whether anything changed.
func (n *Normalizer) StripDynamic(s string) (string, bool) {
	original := s
	result := n.normalizeString(s)
	return result, result != original
}

// StructuralHash returns a type-only representation of a JSON body.
// Two responses with the same shape but different values produce the same hash.
func StructuralHash(body string) string {
	var parsed interface{}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return "<non-json>"
	}
	return structuralString(parsed)
}

func structuralString(v interface{}) string {
	switch val := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			parts = append(parts, k+":"+structuralString(val[k]))
		}
		return "{" + strings.Join(parts, ",") + "}"
	case []interface{}:
		if len(val) == 0 {
			return "[]"
		}
		return "[" + structuralString(val[0]) + "]"
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

// NewNormalizerForMode returns a normalizer for the given mode.
// "raw" = nil (no normalization), "strict" = timestamps/traces only, "smart" = full.
func NewNormalizerForMode(mode string) *Normalizer {
	switch mode {
	case "raw":
		return nil
	case "strict":
		return &Normalizer{
			rules: []normRule{
				{regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[^\s"]*`), "<timestamp>"},
				{regexp.MustCompile(`(Mon|Tue|Wed|Thu|Fri|Sat|Sun), \d{2} (Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) \d{4} \d{2}:\d{2}:\d{2} [A-Z]+`), "<http-date>"},
				{regexp.MustCompile(`Root=1-[0-9a-f]{8}-[0-9a-f]{24}`), "<aws-trace-id>"},
			},
		}
	default: // "smart"
		return NewNormalizer()
	}
}

// AddRule adds a custom regex normalization rule.
func (n *Normalizer) AddRule(pattern, replacement string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid normalizer pattern %q: %w", pattern, err)
	}
	n.rules = append(n.rules, normRule{pattern: re, replacement: replacement})
	return nil
}
