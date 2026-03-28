package hash

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// DefaultExcludedHeaders are headers excluded from hash computation by default.
var DefaultExcludedHeaders = []string{
	"Date",
	"Authorization",
	"X-Request-Id",
	"X-Amzn-Trace-Id",
	"X-Amzn-Requestid",
}

// HashComputer computes deterministic hashes from HTTP requests.
type HashComputer struct {
	ExcludedHeaders map[string]bool
	IncludedHeaders map[string]bool
}

// NewHashComputer creates a HashComputer. Header names are canonicalized.
func NewHashComputer(excluded, included []string) *HashComputer {
	exc := make(map[string]bool, len(excluded))
	for _, h := range excluded {
		exc[http.CanonicalHeaderKey(h)] = true
	}
	inc := make(map[string]bool, len(included))
	for _, h := range included {
		inc[http.CanonicalHeaderKey(h)] = true
	}
	return &HashComputer{
		ExcludedHeaders: exc,
		IncludedHeaders: inc,
	}
}

// ComputeHash returns a SHA-256 hash of method + canonical URL + headers + body.
// Query params are sorted so ?a=1&b=2 and ?b=2&a=1 produce the same hash.
func (h *HashComputer) ComputeHash(method, rawURL string, headers http.Header, body ...string) (string, error) {
	// normalize method
	method = strings.ToUpper(method)

	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("hash: invalid URL %q: %w", rawURL, err)
	}

	canonicalURL := buildCanonicalURL(u)
	headerLines := h.collectHeaders(headers)

	var b strings.Builder
	b.WriteString(method)
	b.WriteByte('\n')
	b.WriteString(canonicalURL)
	for _, line := range headerLines {
		b.WriteByte('\n')
		b.WriteString(line)
	}

	if len(body) > 0 && body[0] != "" {
		b.WriteByte('\n')
		b.WriteString(body[0])
	}

	sum := sha256.Sum256([]byte(b.String()))
	return fmt.Sprintf("%x", sum), nil
}

// buildCanonicalURL returns the URL with query params sorted alphabetically.
func buildCanonicalURL(u *url.URL) string {
	params := u.Query()
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sortedParams []string
	for _, k := range keys {
		vals := make([]string, len(params[k]))
		copy(vals, params[k])
		sort.Strings(vals)
		for _, v := range vals {
			sortedParams = append(sortedParams, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}

	var b strings.Builder
	if u.Scheme != "" {
		b.WriteString(u.Scheme)
		b.WriteString("://")
	}
	b.WriteString(u.Host)
	if u.Path != "" {
		b.WriteString(u.Path)
	} else {
		b.WriteByte('/')
	}
	if len(sortedParams) > 0 {
		b.WriteByte('?')
		b.WriteString(strings.Join(sortedParams, "&"))
	}
	return b.String()
}

// collectHeaders picks and sorts headers per inclusion/exclusion rules.
func (h *HashComputer) collectHeaders(headers http.Header) []string {
	selected := make(http.Header)

	if len(h.IncludedHeaders) > 0 {
		for key, vals := range headers {
			canonical := http.CanonicalHeaderKey(key)
			if h.IncludedHeaders[canonical] {
				selected[canonical] = vals
			}
		}
	} else {
		for key, vals := range headers {
			canonical := http.CanonicalHeaderKey(key)
			if !h.ExcludedHeaders[canonical] {
				selected[canonical] = vals
			}
		}
	}

	keys := make([]string, 0, len(selected))
	for k := range selected {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		vals := make([]string, len(selected[k]))
		copy(vals, selected[k])
		sort.Strings(vals)
		for _, v := range vals {
			lines = append(lines, k+":"+v)
		}
	}
	return lines
}
