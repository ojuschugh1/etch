package noise

import (
	"strings"
	"testing"

	"github.com/ojuschugh1/etch/internal/snapshot"
)

func TestDetect_EmptyStore(t *testing.T) {
	store := snapshot.NewSnapshotStore(t.TempDir())
	result, err := Detect(store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Fields) != 0 {
		t.Errorf("expected 0 noisy fields, got %d", len(result.Fields))
	}
}

func TestDetect_FindsTimestamps(t *testing.T) {
	store := snapshot.NewSnapshotStore(t.TempDir())

	store.Record("api.test.com", "h1", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://api.test.com/data", StatusCode: 200,
		Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body:    `{"name":"Alice","created_at":"2024-01-01T10:00:00Z","id":"abc"}`,
	})
	store.Record("api.test.com", "h2", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://api.test.com/data", StatusCode: 200,
		Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body:    `{"name":"Alice","created_at":"2024-01-02T11:00:00Z","id":"def"}`,
	})

	result, _ := Detect(store)

	found := make(map[string]bool)
	for _, f := range result.Fields {
		found[f.Path] = true
	}

	if !found["body.created_at"] {
		t.Error("should detect body.created_at as noisy")
	}
	// "name" is the same in both recordings, should NOT be flagged
	if found["body.name"] {
		t.Error("body.name should not be flagged (same value)")
	}
}

func TestDetect_FindsUUIDs(t *testing.T) {
	store := snapshot.NewSnapshotStore(t.TempDir())

	store.Record("api.test.com", "h1", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://api.test.com/item", StatusCode: 200,
		Headers: map[string][]string{},
		Body:    `{"request_id":"550e8400-e29b-41d4-a716-446655440000","value":42}`,
	})
	store.Record("api.test.com", "h2", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://api.test.com/item", StatusCode: 200,
		Headers: map[string][]string{},
		Body:    `{"request_id":"6ba7b810-9dad-11d1-80b4-00c04fd430c8","value":42}`,
	})

	result, _ := Detect(store)

	found := false
	for _, f := range result.Fields {
		if f.Path == "body.request_id" {
			found = true
			if f.Pattern != "uuid" {
				t.Errorf("expected uuid pattern, got %q", f.Pattern)
			}
		}
	}
	if !found {
		t.Error("should detect body.request_id as noisy (UUID pattern)")
	}
}

func TestDetect_FindsNoisyHeaders(t *testing.T) {
	store := snapshot.NewSnapshotStore(t.TempDir())

	store.Record("api.test.com", "h1", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://api.test.com/data", StatusCode: 200,
		Headers: map[string][]string{
			"Date":            {"Mon, 01 Jan 2024 00:00:00 GMT"},
			"X-Request-Id":    {"abc-123"},
			"Content-Type":    {"application/json"},
		},
		Body: `{}`,
	})
	store.Record("api.test.com", "h2", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://api.test.com/data", StatusCode: 200,
		Headers: map[string][]string{
			"Date":            {"Tue, 02 Jan 2024 00:00:00 GMT"},
			"X-Request-Id":    {"def-456"},
			"Content-Type":    {"application/json"},
		},
		Body: `{}`,
	})

	result, _ := Detect(store)

	found := make(map[string]bool)
	for _, f := range result.Fields {
		found[f.Path] = true
	}

	if !found["headers.Date"] {
		t.Error("should detect headers.Date as noisy")
	}
	if !found["headers.X-Request-Id"] {
		t.Error("should detect headers.X-Request-Id as noisy")
	}
	// Content-Type is the same in both, should not be flagged
	if found["headers.Content-Type"] {
		t.Error("headers.Content-Type should not be flagged")
	}
}

func TestDetect_SuggestsIgnoreLines(t *testing.T) {
	store := snapshot.NewSnapshotStore(t.TempDir())

	store.Record("api.test.com", "h1", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://api.test.com/data", StatusCode: 200,
		Headers: map[string][]string{"Date": {"Mon, 01 Jan 2024"}},
		Body:    `{"ts":"2024-01-01T00:00:00Z"}`,
	})
	store.Record("api.test.com", "h2", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://api.test.com/data", StatusCode: 200,
		Headers: map[string][]string{"Date": {"Tue, 02 Jan 2024"}},
		Body:    `{"ts":"2024-01-02T00:00:00Z"}`,
	})

	result, _ := Detect(store)

	if len(result.SuggestedIgnore) == 0 {
		t.Fatal("expected suggested ignore lines")
	}

	etchignore := result.FormatEtchignore()
	if !strings.Contains(etchignore, "headers.Date") {
		t.Error("suggested .etchignore should include headers.Date")
	}
}

func TestDetect_SingleRecording_NoNoise(t *testing.T) {
	store := snapshot.NewSnapshotStore(t.TempDir())

	// only one recording - can't detect noise without comparison
	store.Record("api.test.com", "h1", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://api.test.com/data", StatusCode: 200,
		Headers: map[string][]string{},
		Body:    `{"id":"550e8400-e29b-41d4-a716-446655440000"}`,
	})

	result, _ := Detect(store)
	if len(result.Fields) != 0 {
		t.Errorf("single recording shouldn't detect noise, got %d fields", len(result.Fields))
	}
}
