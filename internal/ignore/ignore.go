package ignore

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// DefaultFile is the standard name for the ignore file.
const DefaultFile = ".etchignore"

// Rules holds a set of field path patterns to ignore during diff comparison.
// Patterns can be exact paths like "headers.Date" or "body.timestamp",
// or prefix patterns with a trailing wildcard like "body.meta.*".
type Rules struct {
	exact    map[string]bool
	prefixes []string
}

// Empty returns a Rules with nothing ignored.
func Empty() *Rules {
	return &Rules{exact: make(map[string]bool)}
}

// Load reads an .etchignore file and returns the parsed rules.
// If the file doesn't exist, returns empty rules (not an error).
// Lines starting with # are comments. Blank lines are skipped.
//
// Supported patterns:
//
//	headers.Date          - ignore exact field path
//	body.timestamp        - ignore exact field path
//	body.meta.*           - ignore anything under body.meta
//	status_code           - ignore status code changes entirely
func Load(dir string) (*Rules, error) {
	path := filepath.Join(dir, DefaultFile)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Empty(), nil
		}
		return nil, err
	}
	defer f.Close()

	return Parse(f)
}

// Parse reads ignore rules from a reader (one pattern per line).
func Parse(r *os.File) (*Rules, error) {
	rules := Empty()
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// skip blanks and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasSuffix(line, ".*") {
			// prefix wildcard: "body.meta.*" matches "body.meta.anything"
			prefix := strings.TrimSuffix(line, "*")
			rules.prefixes = append(rules.prefixes, prefix)
		} else {
			rules.exact[line] = true
		}
	}

	return rules, scanner.Err()
}

// ShouldIgnore returns true if the given field path matches any ignore rule.
func (r *Rules) ShouldIgnore(path string) bool {
	if r == nil {
		return false
	}

	if r.exact[path] {
		return true
	}

	for _, prefix := range r.prefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}

	return false
}

// IsEmpty returns true if no rules are loaded.
func (r *Rules) IsEmpty() bool {
	return r == nil || (len(r.exact) == 0 && len(r.prefixes) == 0)
}

// Count returns the total number of rules.
func (r *Rules) Count() int {
	if r == nil {
		return 0
	}
	return len(r.exact) + len(r.prefixes)
}
