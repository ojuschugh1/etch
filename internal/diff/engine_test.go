package diff

import (
	"testing"

	"github.com/ojuschugh1/etch/internal/snapshot"
)

func TestCompare_IdenticalEntries(t *testing.T) {
	engine := NewDiffEngine()
	entry := &snapshot.SnapshotEntry{
		Method:     "GET",
		URL:        "https://api.example.com/users/1",
		StatusCode: 200,
		Headers:    map[string][]string{"Content-Type": {"application/json"}},
		Body:       `{"id":1,"name":"Alice"}`,
	}
	result := engine.Compare(entry, entry)
	if !result.Matched {
		t.Errorf("expected Matched=true for identical entries, got false")
	}
	if len(result.Fields) != 0 {
		t.Errorf("expected 0 fields, got %d: %+v", len(result.Fields), result.Fields)
	}
}

func TestCompare_StatusCodeDiff(t *testing.T) {
	engine := NewDiffEngine()
	stored := &snapshot.SnapshotEntry{StatusCode: 200, Headers: map[string][]string{}, Body: ""}
	live := &snapshot.SnapshotEntry{StatusCode: 404, Headers: map[string][]string{}, Body: ""}

	result := engine.Compare(stored, live)
	if result.Matched {
		t.Fatal("expected Matched=false")
	}
	found := false
	for _, f := range result.Fields {
		if f.Path == "status_code" {
			found = true
			if f.Expected != "200" || f.Actual != "404" {
				t.Errorf("expected 200/404, got %s/%s", f.Expected, f.Actual)
			}
		}
	}
	if !found {
		t.Error("expected a FieldDiff with Path 'status_code'")
	}
}

func TestCompare_HeaderDiff(t *testing.T) {
	engine := NewDiffEngine()
	stored := &snapshot.SnapshotEntry{
		StatusCode: 200,
		Headers:    map[string][]string{"Content-Type": {"application/json"}, "X-Old": {"val"}},
		Body:       "",
	}
	live := &snapshot.SnapshotEntry{
		StatusCode: 200,
		Headers:    map[string][]string{"Content-Type": {"text/plain"}, "X-New": {"val"}},
		Body:       "",
	}

	result := engine.Compare(stored, live)
	if result.Matched {
		t.Fatal("expected Matched=false")
	}

	paths := make(map[string]bool)
	for _, f := range result.Fields {
		paths[f.Path] = true
	}
	if !paths["headers.Content-Type"] {
		t.Error("expected diff for headers.Content-Type")
	}
	if !paths["headers.X-Old"] {
		t.Error("expected diff for headers.X-Old (removed)")
	}
	if !paths["headers.X-New"] {
		t.Error("expected diff for headers.X-New (added)")
	}
}

func TestCompare_JSONBodyFieldLevelDiff(t *testing.T) {
	engine := NewDiffEngine()
	stored := &snapshot.SnapshotEntry{
		StatusCode: 200,
		Headers:    map[string][]string{},
		Body:       `{"user":{"name":"Alice","age":30},"active":true}`,
	}
	live := &snapshot.SnapshotEntry{
		StatusCode: 200,
		Headers:    map[string][]string{},
		Body:       `{"user":{"name":"Bob","age":30},"active":false}`,
	}

	result := engine.Compare(stored, live)
	if result.Matched {
		t.Fatal("expected Matched=false")
	}

	paths := make(map[string]bool)
	for _, f := range result.Fields {
		paths[f.Path] = true
	}
	if !paths["body.user.name"] {
		t.Error("expected diff at body.user.name")
	}
	if !paths["body.active"] {
		t.Error("expected diff at body.active")
	}
	if paths["body.user.age"] {
		t.Error("did not expect diff at body.user.age")
	}
}

func TestCompare_JSONArrayDiff(t *testing.T) {
	engine := NewDiffEngine()
	stored := &snapshot.SnapshotEntry{
		StatusCode: 200,
		Headers:    map[string][]string{},
		Body:       `{"items":[{"id":1},{"id":2}]}`,
	}
	live := &snapshot.SnapshotEntry{
		StatusCode: 200,
		Headers:    map[string][]string{},
		Body:       `{"items":[{"id":1},{"id":3}]}`,
	}

	result := engine.Compare(stored, live)
	if result.Matched {
		t.Fatal("expected Matched=false")
	}

	found := false
	for _, f := range result.Fields {
		if f.Path == "body.items[1].id" {
			found = true
			if f.Expected != "2" || f.Actual != "3" {
				t.Errorf("expected 2/3, got %s/%s", f.Expected, f.Actual)
			}
		}
	}
	if !found {
		t.Error("expected diff at body.items[1].id")
	}
}

func TestCompare_NonJSONBodyDiff(t *testing.T) {
	engine := NewDiffEngine()
	stored := &snapshot.SnapshotEntry{
		StatusCode: 200,
		Headers:    map[string][]string{},
		Body:       "Hello World",
	}
	live := &snapshot.SnapshotEntry{
		StatusCode: 200,
		Headers:    map[string][]string{},
		Body:       "Hello Changed",
	}

	result := engine.Compare(stored, live)
	if result.Matched {
		t.Fatal("expected Matched=false")
	}
	if len(result.Fields) != 1 {
		t.Fatalf("expected 1 field diff, got %d", len(result.Fields))
	}
	if result.Fields[0].Path != "body" {
		t.Errorf("expected path 'body', got '%s'", result.Fields[0].Path)
	}
}

func TestCompare_EmptyBodiesMatch(t *testing.T) {
	engine := NewDiffEngine()
	stored := &snapshot.SnapshotEntry{StatusCode: 200, Headers: map[string][]string{}, Body: ""}
	live := &snapshot.SnapshotEntry{StatusCode: 200, Headers: map[string][]string{}, Body: ""}

	result := engine.Compare(stored, live)
	if !result.Matched {
		t.Error("expected Matched=true for identical empty entries")
	}
}

func TestFormatDiff_MatchedReturnsEmpty(t *testing.T) {
	engine := NewDiffEngine()
	result := DiffResult{Matched: true, URL: "https://example.com"}
	out := engine.FormatDiff(result, true)
	if out != "" {
		t.Errorf("expected empty string for matched result, got %q", out)
	}
}

func TestFormatDiff_WithColor(t *testing.T) {
	engine := NewDiffEngine()
	result := DiffResult{
		URL:     "https://api.example.com/users/1",
		Matched: false,
		Fields: []FieldDiff{
			{Path: "status_code", Expected: "200", Actual: "404"},
			{Path: "body.user.name", Expected: `"Alice"`, Actual: `"Bob"`},
		},
	}
	out := engine.FormatDiff(result, true)

	// Should contain ANSI codes
	if !containsANSI(out) {
		t.Error("expected ANSI codes in colored output")
	}
	// Should contain the URL
	if !contains(out, "https://api.example.com/users/1") {
		t.Error("expected URL in output")
	}
	// Should contain field paths
	if !contains(out, "status_code") {
		t.Error("expected status_code path in output")
	}
	if !contains(out, "body.user.name") {
		t.Error("expected body.user.name path in output")
	}
	// Should contain the arrow separator
	if !contains(out, "->") {
		t.Error("expected arrow separator in output")
	}
}

func TestFormatDiff_WithoutColor(t *testing.T) {
	engine := NewDiffEngine()
	result := DiffResult{
		URL:     "https://api.example.com/users/1",
		Matched: false,
		Fields: []FieldDiff{
			{Path: "status_code", Expected: "200", Actual: "404"},
		},
	}
	out := engine.FormatDiff(result, false)

	// Should NOT contain ANSI codes
	if containsANSI(out) {
		t.Error("expected no ANSI codes when color=false")
	}
	// Should still contain the content
	if !contains(out, "200") || !contains(out, "404") {
		t.Error("expected values in output")
	}
}

func containsANSI(s string) bool {
	return contains(s, "\033[")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Array reordering should not produce diffs - same elements, different order.
func TestCompare_ArrayReorderingIsNotADiff(t *testing.T) {
	engine := NewDiffEngine()
	stored := &snapshot.SnapshotEntry{
		StatusCode: 200,
		Headers:    map[string][]string{},
		Body:       `{"users":[{"id":1,"name":"Alice"},{"id":2,"name":"Bob"}]}`,
	}
	live := &snapshot.SnapshotEntry{
		StatusCode: 200,
		Headers:    map[string][]string{},
		Body:       `{"users":[{"id":2,"name":"Bob"},{"id":1,"name":"Alice"}]}`,
	}

	result := engine.Compare(stored, live)
	if !result.Matched {
		t.Errorf("reordered array should match, got %d diffs:", len(result.Fields))
		for _, f := range result.Fields {
			t.Errorf("  %s: %s -> %s", f.Path, f.Expected, f.Actual)
		}
	}
}

// Array with different content should still produce diffs.
func TestCompare_ArrayDifferentContent(t *testing.T) {
	engine := NewDiffEngine()
	stored := &snapshot.SnapshotEntry{
		StatusCode: 200,
		Headers:    map[string][]string{},
		Body:       `{"items":[1,2,3]}`,
	}
	live := &snapshot.SnapshotEntry{
		StatusCode: 200,
		Headers:    map[string][]string{},
		Body:       `{"items":[1,2,4]}`,
	}

	result := engine.Compare(stored, live)
	if result.Matched {
		t.Error("different array content should not match")
	}
}

// Array with added element should produce a diff.
func TestCompare_ArrayLengthChange(t *testing.T) {
	engine := NewDiffEngine()
	stored := &snapshot.SnapshotEntry{
		StatusCode: 200,
		Headers:    map[string][]string{},
		Body:       `{"items":[1,2]}`,
	}
	live := &snapshot.SnapshotEntry{
		StatusCode: 200,
		Headers:    map[string][]string{},
		Body:       `{"items":[1,2,3]}`,
	}

	result := engine.Compare(stored, live)
	if result.Matched {
		t.Error("different array length should not match")
	}
}

// Simple scalar array reordering should also be handled.
func TestCompare_ScalarArrayReordering(t *testing.T) {
	engine := NewDiffEngine()
	stored := &snapshot.SnapshotEntry{
		StatusCode: 200,
		Headers:    map[string][]string{},
		Body:       `{"tags":["beta","alpha","gamma"]}`,
	}
	live := &snapshot.SnapshotEntry{
		StatusCode: 200,
		Headers:    map[string][]string{},
		Body:       `{"tags":["gamma","beta","alpha"]}`,
	}

	result := engine.Compare(stored, live)
	if !result.Matched {
		t.Errorf("reordered scalar array should match, got %d diffs", len(result.Fields))
	}
}
