// Package chain handles request chaining for dependent API flows (e.g. create then get).
package chain

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// Rule defines how to extract a value from a response and store it as a named variable.
type Rule struct {
	Name     string `json:"name"`     // variable name, e.g. "user_id"
	Source   string `json:"source"`   // "body" or "header"
	Path     string `json:"path"`     // JSON path for body (e.g. "id") or header name
	Pattern  string `json:"pattern"`  // optional regex to extract a substring
}

// Store holds extracted variables from a session. Thread-safe.
type Store struct {
	vars map[string]string
	mu   sync.RWMutex
}

// NewStore creates an empty variable store.
func NewStore() *Store {
	return &Store{vars: make(map[string]string)}
}

// Set stores a named variable.
func (s *Store) Set(name, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vars[name] = value
}

// Get returns the value for name, or empty string if not set.
func (s *Store) Get(name string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.vars[name]
}

// All returns a copy of all stored variables.
func (s *Store) All() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make(map[string]string, len(s.vars))
	for k, v := range s.vars {
		cp[k] = v
	}
	return cp
}

// Expand replaces {{variable}} placeholders with stored values. Unknown vars are left as-is.
func (s *Store) Expand(input string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for name, val := range s.vars {
		input = strings.ReplaceAll(input, "{{"+name+"}}", val)
	}
	return input
}

// Extract applies chain rules to a response, storing extracted values.
func (s *Store) Extract(rules []Rule, body string, headers map[string][]string) {
	for _, rule := range rules {
		var raw string

		switch rule.Source {
		case "body":
			raw = extractJSONPath(body, rule.Path)
		case "header":
			if vals, ok := headers[rule.Path]; ok && len(vals) > 0 {
				raw = vals[0]
			}
		}

		if raw == "" {
			continue
		}

		// Apply optional regex pattern to extract a substring
		if rule.Pattern != "" {
			re, err := regexp.Compile(rule.Pattern)
			if err != nil {
				continue
			}
			matches := re.FindStringSubmatch(raw)
			if len(matches) > 1 {
				raw = matches[1] // first capture group
			} else if len(matches) == 1 {
				raw = matches[0]
			}
		}

		if raw != "" {
			s.Set(rule.Name, raw)
		}
	}
}

// extractJSONPath extracts a value from JSON using a dot-separated path (e.g. "user.id").
func extractJSONPath(body, path string) string {
	if body == "" || path == "" {
		return ""
	}

	var parsed interface{}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return ""
	}

	parts := strings.Split(path, ".")
	current := parsed

	for _, part := range parts {
		switch v := current.(type) {
		case map[string]interface{}:
			current = v[part]
		default:
			return ""
		}
	}

	if current == nil {
		return ""
	}

	return fmt.Sprintf("%v", current)
}
