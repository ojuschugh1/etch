package diff

import (
	"testing"

	"github.com/ojuschugh1/etch/internal/snapshot"
)

// simpleIgnore is a test helper that ignores exact paths.
type simpleIgnore struct {
	paths map[string]bool
}

func (s *simpleIgnore) ShouldIgnore(path string) bool {
	return s.paths[path]
}

// Two entries that differ only in ignored fields should be reported as matching.
func TestCompareWithIgnoreRules_IgnoredFieldsOnly(t *testing.T) {
	rules := &simpleIgnore{paths: map[string]bool{
		"headers.Date":                    true,
		"headers.X-Request-Id":            true,
		"body.headers.X-Amzn-Trace-Id":   true,
	}}

	engine := NewDiffEngineWithIgnore(rules)

	stored := &snapshot.SnapshotEntry{
		StatusCode: 200,
		Headers: map[string][]string{
			"Content-Type": {"application/json"},
			"Date":         {"Mon, 01 Jan 2024 00:00:00 GMT"},
			"X-Request-Id": {"abc-123"},
		},
		Body: `{"name":"Alice","headers":{"X-Amzn-Trace-Id":"Root=1-old"}}`,
	}

	live := &snapshot.SnapshotEntry{
		StatusCode: 200,
		Headers: map[string][]string{
			"Content-Type": {"application/json"},
			"Date":         {"Tue, 02 Jan 2024 00:00:00 GMT"},
			"X-Request-Id": {"def-456"},
		},
		Body: `{"name":"Alice","headers":{"X-Amzn-Trace-Id":"Root=1-new"}}`,
	}

	result := engine.Compare(stored, live)

	if !result.Matched {
		t.Errorf("expected match when only ignored fields differ, got %d diffs:", len(result.Fields))
		for _, f := range result.Fields {
			t.Errorf("  %s: %q -> %q", f.Path, f.Expected, f.Actual)
		}
	}
}

// Real changes should still be caught even when some fields are ignored.
func TestCompareWithIgnoreRules_MixedChanges(t *testing.T) {
	rules := &simpleIgnore{paths: map[string]bool{
		"headers.Date": true,
	}}

	engine := NewDiffEngineWithIgnore(rules)

	stored := &snapshot.SnapshotEntry{
		StatusCode: 200,
		Headers: map[string][]string{
			"Content-Type": {"application/json"},
			"Date":         {"Mon, 01 Jan 2024 00:00:00 GMT"},
		},
		Body: `{"name":"Alice"}`,
	}

	live := &snapshot.SnapshotEntry{
		StatusCode: 200,
		Headers: map[string][]string{
			"Content-Type": {"application/json"},
			"Date":         {"Tue, 02 Jan 2024 00:00:00 GMT"},
		},
		Body: `{"name":"Bob"}`,
	}

	result := engine.Compare(stored, live)

	if result.Matched {
		t.Fatal("expected mismatch - body.name changed")
	}
	if len(result.Fields) != 1 {
		t.Fatalf("expected 1 diff (body.name), got %d", len(result.Fields))
	}
	if result.Fields[0].Path != "body.name" {
		t.Errorf("expected diff at body.name, got %s", result.Fields[0].Path)
	}
}

// No ignore rules = same behavior as before.
func TestCompareWithoutIgnoreRules(t *testing.T) {
	engine := NewDiffEngine()

	stored := &snapshot.SnapshotEntry{
		StatusCode: 200,
		Headers:    map[string][]string{"Date": {"old"}},
		Body:       `{"name":"Alice"}`,
	}
	live := &snapshot.SnapshotEntry{
		StatusCode: 200,
		Headers:    map[string][]string{"Date": {"new"}},
		Body:       `{"name":"Alice"}`,
	}

	result := engine.Compare(stored, live)
	if result.Matched {
		t.Fatal("without ignore rules, Date header change should be a mismatch")
	}
}
