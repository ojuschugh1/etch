package history

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Entry represents a single schema change event.
type Entry struct {
	Timestamp string `json:"timestamp"`
	Endpoint  string `json:"endpoint"`
	Field     string `json:"field"`
	Change    string `json:"change"` // "added", "removed", "type_changed"
	OldValue  string `json:"old_value,omitempty"`
	NewValue  string `json:"new_value,omitempty"`
}

// Store manages the history log file.
type Store struct {
	path string
}

// NewStore creates a history store at the given directory.
func NewStore(dir string) *Store {
	return &Store{path: filepath.Join(dir, "history.jsonl")}
}

// Append adds entries to the history log.
func (s *Store) Append(entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, e := range entries {
		data, err := json.Marshal(e)
		if err != nil {
			continue
		}
		f.Write(data)
		f.Write([]byte("\n"))
	}
	return nil
}

// Load reads all history entries from the log file.
func (s *Store) Load() ([]Entry, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []Entry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var e Entry
		if json.Unmarshal(scanner.Bytes(), &e) == nil {
			entries = append(entries, e)
		}
	}
	return entries, scanner.Err()
}

// ForEndpoint returns entries filtered by endpoint, sorted by time.
func (s *Store) ForEndpoint(endpoint string) ([]Entry, error) {
	all, err := s.Load()
	if err != nil {
		return nil, err
	}

	var filtered []Entry
	for _, e := range all {
		if e.Endpoint == endpoint || endpoint == "" {
			filtered = append(filtered, e)
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Timestamp < filtered[j].Timestamp
	})

	return filtered, nil
}

// RecordApproval creates history entries from approved diffs.
func RecordApproval(endpoint string, fields []FieldChange) []Entry {
	ts := time.Now().Format("2006-01-02 15:04:05")
	var entries []Entry
	for _, f := range fields {
		entries = append(entries, Entry{
			Timestamp: ts,
			Endpoint:  endpoint,
			Field:     f.Path,
			Change:    f.ChangeType,
			OldValue:  f.OldValue,
			NewValue:  f.NewValue,
		})
	}
	return entries
}

// FieldChange describes a single field-level change for history recording.
type FieldChange struct {
	Path       string
	ChangeType string // "added", "removed", "type_changed", "value_changed"
	OldValue   string
	NewValue   string
}

// Format produces a readable timeline.
func Format(entries []Entry, color bool) string {
	if len(entries) == 0 {
		return "No history recorded yet. Changes are logged when you run 'etch approve'.\n"
	}

	green, red, yellow, gray, reset := "", "", "", "", ""
	if color {
		green = "\033[32m"
		red = "\033[31m"
		yellow = "\033[33m"
		gray = "\033[90m"
		reset = "\033[0m"
	}

	var b strings.Builder

	// group by date
	type dayGroup struct {
		date    string
		entries []Entry
	}
	groups := make(map[string]*dayGroup)
	var dates []string

	for _, e := range entries {
		date := e.Timestamp[:10] // YYYY-MM-DD
		if _, ok := groups[date]; !ok {
			groups[date] = &dayGroup{date: date}
			dates = append(dates, date)
		}
		groups[date].entries = append(groups[date].entries, e)
	}

	sort.Strings(dates)

	for _, date := range dates {
		g := groups[date]
		b.WriteString(fmt.Sprintf("%s%s%s\n", gray, date, reset))

		for _, e := range g.entries {
			sym := yellow + "!" + reset
			switch e.Change {
			case "added":
				sym = green + "+" + reset
			case "removed":
				sym = red + "-" + reset
			case "type_changed":
				sym = red + "✗" + reset
			}

			ts := e.Timestamp[11:] // HH:MM:SS
			b.WriteString(fmt.Sprintf("  %s%s%s %s %s %s", gray, ts, reset, sym, e.Endpoint, e.Field))

			if e.OldValue != "" && e.NewValue != "" {
				b.WriteString(fmt.Sprintf(" %s%s%s -> %s%s%s", red, e.OldValue, reset, green, e.NewValue, reset))
			} else if e.NewValue != "" {
				b.WriteString(fmt.Sprintf(" %s%s%s", green, e.NewValue, reset))
			} else if e.OldValue != "" {
				b.WriteString(fmt.Sprintf(" %s%s%s", red, e.OldValue, reset))
			}

			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	return b.String()
}
