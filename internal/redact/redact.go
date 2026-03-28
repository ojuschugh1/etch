package redact

import (
	"regexp"
	"strings"
)

// patterns that look like secrets or PII
var defaultPatterns = []struct {
	name    string
	pattern *regexp.Regexp
	replace string
}{
	// auth tokens
	{"bearer-token", regexp.MustCompile(`(?i)(bearer\s+)[a-zA-Z0-9._\-]+`), "${1}[REDACTED]"},
	{"api-key-value", regexp.MustCompile(`(?i)(api[_-]?key[":\s]+)[a-zA-Z0-9._\-]{16,}`), "${1}[REDACTED]"},
	{"secret-value", regexp.MustCompile(`(?i)(secret[":\s]+)[a-zA-Z0-9._\-]{16,}`), "${1}[REDACTED]"},

	// emails
	{"email", regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`), "[EMAIL]"},

	// credit cards (basic patterns)
	{"credit-card", regexp.MustCompile(`\b\d{4}[\s\-]?\d{4}[\s\-]?\d{4}[\s\-]?\d{4}\b`), "[CARD]"},

	// SSN
	{"ssn", regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`), "[SSN]"},

	// phone numbers (US format)
	{"phone", regexp.MustCompile(`\b\+?1?[\s\-]?\(?\d{3}\)?[\s\-]?\d{3}[\s\-]?\d{4}\b`), "[PHONE]"},

	// AWS keys
	{"aws-access-key", regexp.MustCompile(`AKIA[0-9A-Z]{16}`), "[AWS_KEY]"},
	{"aws-secret-key", regexp.MustCompile(`(?i)(aws_secret_access_key[":\s=]+)[a-zA-Z0-9/+=]{40}`), "${1}[REDACTED]"},

	// generic long hex strings that look like tokens (32+ chars)
	{"hex-token", regexp.MustCompile(`\b[a-f0-9]{40,}\b`), "[TOKEN]"},
}

// Redactor scrubs sensitive data from strings before they get saved to snapshots.
type Redactor struct {
	patterns []struct {
		name    string
		pattern *regexp.Regexp
		replace string
	}
}

// New creates a redactor with the default set of PII/secret patterns.
func New() *Redactor {
	return &Redactor{patterns: defaultPatterns}
}

// RedactBody scrubs sensitive patterns from a response body string.
func (r *Redactor) RedactBody(body string) string {
	for _, p := range r.patterns {
		body = p.pattern.ReplaceAllString(body, p.replace)
	}
	return body
}

// RedactHeaders scrubs sensitive header values.
func (r *Redactor) RedactHeaders(headers map[string][]string) map[string][]string {
	result := make(map[string][]string, len(headers))
	sensitiveHeaders := map[string]bool{
		"authorization":    true,
		"x-api-key":        true,
		"cookie":           true,
		"set-cookie":       true,
		"x-csrf-token":     true,
		"x-auth-token":     true,
	}

	for k, vals := range headers {
		if sensitiveHeaders[strings.ToLower(k)] {
			result[k] = []string{"[REDACTED]"}
		} else {
			newVals := make([]string, len(vals))
			for i, v := range vals {
				newVals[i] = r.RedactBody(v)
			}
			result[k] = newVals
		}
	}
	return result
}

// HasSensitiveContent returns true if the string contains secrets or PII patterns.
func (r *Redactor) HasSensitiveContent(s string) bool {
	for _, p := range r.patterns {
		if p.pattern.MatchString(s) {
			return true
		}
	}
	return false
}
