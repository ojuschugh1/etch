package audit

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/ojuschugh1/etch/internal/redact"
	"github.com/ojuschugh1/etch/internal/snapshot"
)

// Finding represents a single security issue found in a snapshot.
type Finding struct {
	Host     string
	Hash     string
	Method   string
	URL      string
	Location string // "body", "headers", "url", "request_body"
	Type     string // "pii", "secret", "sensitive-url-param"
	Detail   string
	Severity string // "critical", "warning"
}

// Report holds all findings from an audit scan.
type Report struct {
	Findings  []Finding
	Scanned   int
	Clean     int
	WithIssue int
}

// urlParamPatterns detects sensitive data leaked in URL query parameters.
var urlParamPatterns = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{"password-in-url", regexp.MustCompile(`(?i)(password|passwd|pwd)=`)},
	{"token-in-url", regexp.MustCompile(`(?i)(token|api_key|apikey|secret|access_key)=`)},
	{"ssn-in-url", regexp.MustCompile(`\d{3}-\d{2}-\d{4}`)},
	{"email-in-url", regexp.MustCompile(`[a-zA-Z0-9._%+\-]+%40[a-zA-Z0-9.\-]+`)}, // URL-encoded @
}

// Scan examines all recorded snapshots for sensitive data exposure.
func Scan(store *snapshot.SnapshotStore) (*Report, error) {
	all, err := store.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("loading snapshots: %w", err)
	}

	r := redact.New()
	report := &Report{}

	for host, sf := range all {
		for hash, entry := range sf {
			report.Scanned++
			found := false

			// Check response body
			if r.HasSensitiveContent(entry.Body) {
				findings := detectPatterns(entry.Body, "body")
				for _, f := range findings {
					f.Host = host
					f.Hash = hash
					f.Method = entry.Method
					f.URL = entry.URL
					report.Findings = append(report.Findings, f)
				}
				found = true
			}

			// Check request body
			if entry.RequestBody != "" && r.HasSensitiveContent(entry.RequestBody) {
				findings := detectPatterns(entry.RequestBody, "request_body")
				for _, f := range findings {
					f.Host = host
					f.Hash = hash
					f.Method = entry.Method
					f.URL = entry.URL
					report.Findings = append(report.Findings, f)
				}
				found = true
			}

			// Check headers for sensitive values
			for hdr, vals := range entry.Headers {
				for _, v := range vals {
					if r.HasSensitiveContent(v) {
						report.Findings = append(report.Findings, Finding{
							Host:     host,
							Hash:     hash,
							Method:   entry.Method,
							URL:      entry.URL,
							Location: "headers." + hdr,
							Type:     "secret",
							Detail:   "sensitive value in header",
							Severity: "critical",
						})
						found = true
					}
				}
			}

			// Check URL for sensitive query params
			parsed, err := url.Parse(entry.URL)
			if err == nil && parsed.RawQuery != "" {
				for _, p := range urlParamPatterns {
					if p.pattern.MatchString(parsed.RawQuery) {
						report.Findings = append(report.Findings, Finding{
							Host:     host,
							Hash:     hash,
							Method:   entry.Method,
							URL:      entry.URL,
							Location: "url",
							Type:     "sensitive-url-param",
							Detail:   p.name + " detected in query string",
							Severity: "critical",
						})
						found = true
					}
				}
			}

			if found {
				report.WithIssue++
			} else {
				report.Clean++
			}
		}
	}

	// Sort findings by severity (critical first), then by URL
	sort.Slice(report.Findings, func(i, j int) bool {
		if report.Findings[i].Severity != report.Findings[j].Severity {
			return report.Findings[i].Severity == "critical"
		}
		return report.Findings[i].URL < report.Findings[j].URL
	})

	return report, nil
}

// detectPatterns identifies specific types of sensitive data in a string.
func detectPatterns(content, location string) []Finding {
	var findings []Finding
	patterns := []struct {
		name     string
		pattern  *regexp.Regexp
		typ      string
		severity string
	}{
		{"bearer-token", regexp.MustCompile(`(?i)bearer\s+[a-zA-Z0-9._\-]+`), "secret", "critical"},
		{"api-key", regexp.MustCompile(`(?i)(api[_-]?key[":\s]+)[a-zA-Z0-9._\-]{16,}`), "secret", "critical"},
		{"aws-access-key", regexp.MustCompile(`AKIA[0-9A-Z]{16}`), "secret", "critical"},
		{"email", regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`), "pii", "warning"},
		{"credit-card", regexp.MustCompile(`\b\d{4}[\s\-]?\d{4}[\s\-]?\d{4}[\s\-]?\d{4}\b`), "pii", "critical"},
		{"ssn", regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`), "pii", "critical"},
		{"phone", regexp.MustCompile(`\b\+?1?[\s\-]?\(?\d{3}\)?[\s\-]?\d{3}[\s\-]?\d{4}\b`), "pii", "warning"},
	}

	for _, p := range patterns {
		if p.pattern.MatchString(content) {
			findings = append(findings, Finding{
				Location: location,
				Type:     p.typ,
				Detail:   p.name + " detected",
				Severity: p.severity,
			})
		}
	}
	return findings
}

// Format produces a human-readable audit report.
func (r *Report) Format(color bool) string {
	red, yellow, green, gray, reset := "", "", "", "", ""
	if color {
		red = "\033[31m"
		yellow = "\033[33m"
		green = "\033[32m"
		gray = "\033[90m"
		reset = "\033[0m"
	}

	var b strings.Builder

	b.WriteString(fmt.Sprintf("Scanned %d snapshot(s): %d clean, %d with issues\n\n",
		r.Scanned, r.Clean, r.WithIssue))

	if len(r.Findings) == 0 {
		b.WriteString(fmt.Sprintf("%s✓ No sensitive data found in snapshots%s\n", green, reset))
		return b.String()
	}

	// Group by URL
	byURL := make(map[string][]Finding)
	var urls []string
	for _, f := range r.Findings {
		key := f.Method + " " + f.URL
		if _, seen := byURL[key]; !seen {
			urls = append(urls, key)
		}
		byURL[key] = append(byURL[key], f)
	}

	for _, u := range urls {
		findings := byURL[u]
		b.WriteString(fmt.Sprintf("  %s\n", u))
		for _, f := range findings {
			sym := yellow + "!" + reset
			if f.Severity == "critical" {
				sym = red + "✗" + reset
			}
			b.WriteString(fmt.Sprintf("    %s [%s] %s %s(%s)%s\n",
				sym, f.Type, f.Detail, gray, f.Location, reset))
		}
		b.WriteString("\n")
	}

	critical := 0
	warnings := 0
	for _, f := range r.Findings {
		if f.Severity == "critical" {
			critical++
		} else {
			warnings++
		}
	}

	b.WriteString(fmt.Sprintf("%s✗ %d critical, %d warning(s)%s\n", red, critical, warnings, reset))
	b.WriteString("\nRun 'etch record --redact' to scrub sensitive data from future recordings.\n")

	return b.String()
}

// HasCritical returns true if any finding is critical severity.
func (r *Report) HasCritical() bool {
	for _, f := range r.Findings {
		if f.Severity == "critical" {
			return true
		}
	}
	return false
}
