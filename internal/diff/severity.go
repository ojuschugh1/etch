package diff

import (
	"strconv"
	"strings"
)

// Severity classifies how important a diff is.
type Severity int

const (
	SeverityInfo     Severity = iota // cosmetic, probably ignorable
	SeverityWarning                  // worth looking at
	SeverityCritical                 // likely a breaking change
)

func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "info"
	case SeverityWarning:
		return "warning"
	case SeverityCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// Symbol returns a colored indicator for terminal output.
func (s Severity) Symbol(color bool) string {
	if !color {
		switch s {
		case SeverityInfo:
			return "~"
		case SeverityWarning:
			return "!"
		case SeverityCritical:
			return "✗"
		}
		return "?"
	}
	switch s {
	case SeverityInfo:
		return "\033[90m~\033[0m" // gray
	case SeverityWarning:
		return "\033[33m!\033[0m" // yellow
	case SeverityCritical:
		return "\033[31m✗\033[0m" // red
	}
	return "?"
}

// ClassifiedDiff wraps a FieldDiff with a severity classification.
type ClassifiedDiff struct {
	FieldDiff
	Severity Severity
	Reason   string
}

// ClassifyDiffs takes raw field diffs and assigns a severity to each one
// based on what changed and where.
func ClassifyDiffs(fields []FieldDiff) []ClassifiedDiff {
	result := make([]ClassifiedDiff, len(fields))
	for i, f := range fields {
		result[i] = ClassifiedDiff{
			FieldDiff: f,
			Severity:  classifyField(f),
			Reason:    reasonForField(f),
		}
	}
	return result
}

func classifyField(f FieldDiff) Severity {
	path := strings.ToLower(f.Path)

	// status code changes are always critical
	if path == "status_code" {
		return SeverityCritical
	}

	// field added or removed = critical
	if f.Expected == "" || f.Actual == "" {
		return SeverityCritical
	}

	// type changes (e.g. string -> null, number -> string) = critical
	if isTypeChange(f.Expected, f.Actual) {
		return SeverityCritical
	}

	// known noisy fields = info
	if isNoisyField(path) {
		return SeverityInfo
	}

	// numeric value changes in price/amount/total fields = critical
	if isPriceField(path) && isNumericChange(f.Expected, f.Actual) {
		return SeverityCritical
	}

	// header changes are usually warnings
	if strings.HasPrefix(path, "headers.") {
		return SeverityWarning
	}

	// everything else is a warning
	return SeverityWarning
}

func reasonForField(f FieldDiff) string {
	path := strings.ToLower(f.Path)

	if path == "status_code" {
		return "HTTP status code changed"
	}
	if f.Expected == "" {
		return "field added"
	}
	if f.Actual == "" {
		return "field removed"
	}
	if isTypeChange(f.Expected, f.Actual) {
		return "value type changed"
	}
	if isNoisyField(path) {
		return "dynamic field (likely noise)"
	}
	if isPriceField(path) {
		return "monetary/quantity value changed"
	}
	if strings.HasPrefix(path, "headers.") {
		return "response header changed"
	}
	return "value changed"
}

func isNoisyField(path string) bool {
	noisy := []string{
		"timestamp", "created_at", "updated_at", "modified_at",
		"date", "time", "request_id", "trace_id", "correlation_id",
		"x-request-id", "x-amzn-trace-id", "x-amzn-requestid",
		"etag", "last-modified", "x-cache",
	}
	lower := strings.ToLower(path)
	for _, n := range noisy {
		if strings.HasSuffix(lower, n) || strings.Contains(lower, "."+n) {
			return true
		}
	}
	return false
}

func isPriceField(path string) bool {
	keywords := []string{".price", ".amount", ".total", ".cost", ".balance", ".fee", ".quantity", ".count"}
	lower := strings.ToLower(path)
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	// also match if the field name ends with these
	suffixes := []string{"_price", "_amount", "_total", "_cost", "_balance", "_fee", "_rate", "_quantity", "_count"}
	for _, s := range suffixes {
		if strings.HasSuffix(lower, s) {
			return true
		}
	}
	return false
}

func isNumericChange(expected, actual string) bool {
	_, err1 := strconv.ParseFloat(expected, 64)
	_, err2 := strconv.ParseFloat(actual, 64)
	return err1 == nil && err2 == nil
}

func isTypeChange(expected, actual string) bool {
	return jsonValueType(expected) != jsonValueType(actual)
}

func jsonValueType(s string) string {
	if s == "null" {
		return "null"
	}
	if s == "true" || s == "false" {
		return "boolean"
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return "number"
	}
	if strings.HasPrefix(s, "\"") {
		return "string"
	}
	if strings.HasPrefix(s, "{") {
		return "object"
	}
	if strings.HasPrefix(s, "[") {
		return "array"
	}
	return "string" // fallback
}
