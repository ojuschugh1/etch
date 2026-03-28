package envvar

import (
	"os"
	"regexp"
	"strings"
)

var placeholder = regexp.MustCompile(`\{\{([A-Z_][A-Z0-9_]*)\}\}`)

// Expander replaces {{VAR}} placeholders in strings with values from
// config env map or OS environment variables. Config values take precedence.
type Expander struct {
	vars map[string]string
}

// NewExpander creates an expander from a config env map.
// OS environment variables are used as fallback for any key not in the map.
func NewExpander(configEnv map[string]string) *Expander {
	vars := make(map[string]string)
	for k, v := range configEnv {
		vars[k] = v
	}
	return &Expander{vars: vars}
}

// Expand replaces all {{VAR}} placeholders in s with their values.
// Looks up config env first, then OS env. Unresolved placeholders are left as-is.
func (e *Expander) Expand(s string) string {
	return placeholder.ReplaceAllStringFunc(s, func(match string) string {
		name := match[2 : len(match)-2] // strip {{ and }}

		// config env takes precedence
		if val, ok := e.vars[name]; ok {
			return val
		}

		// fall back to OS env
		if val := os.Getenv("ETCH_" + name); val != "" {
			return val
		}
		if val := os.Getenv(name); val != "" {
			return val
		}

		return match // leave unresolved
	})
}

// ExpandURL replaces placeholders in a URL string.
func (e *Expander) ExpandURL(url string) string {
	return e.Expand(url)
}

// HasPlaceholders returns true if s contains any {{VAR}} patterns.
func HasPlaceholders(s string) bool {
	return placeholder.MatchString(s)
}

// Collapse replaces a known base URL with a {{VAR}} placeholder.
// Used during recording to make snapshots environment-independent.
func (e *Expander) Collapse(url string) string {
	for name, val := range e.vars {
		if val != "" && strings.HasPrefix(url, val) {
			return "{{" + name + "}}" + url[len(val):]
		}
	}
	return url
}
