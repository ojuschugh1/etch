package coverage

import (
	"strings"
	"testing"

	"github.com/ojuschugh1/etch/internal/snapshot"
)

func TestGenerate_EmptyStore(t *testing.T) {
	store := snapshot.NewSnapshotStore(t.TempDir())
	report, err := Generate(store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Total != 0 {
		t.Errorf("expected 0 endpoints, got %d", report.Total)
	}
}

func TestGenerate_MultipleEndpoints(t *testing.T) {
	store := snapshot.NewSnapshotStore(t.TempDir())

	store.Record("api.example.com", "h1", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://api.example.com/users", StatusCode: 200,
		Headers: map[string][]string{}, Body: "[]",
	})
	store.Record("api.example.com", "h2", snapshot.SnapshotEntry{
		Method: "POST", URL: "http://api.example.com/users", StatusCode: 201,
		Headers: map[string][]string{}, Body: "{}",
	})
	store.Record("api.example.com", "h3", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://api.example.com/users/1", StatusCode: 200,
		Headers: map[string][]string{}, Body: "{}",
	})
	store.Record("cdn.example.com", "h4", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://cdn.example.com/assets/logo.png", StatusCode: 200,
		Headers: map[string][]string{}, Body: "<binary>",
	})

	report, err := Generate(store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Total != 4 {
		t.Errorf("expected 4 endpoints, got %d", report.Total)
	}
}

func TestGenerate_DeduplicatesSamePath(t *testing.T) {
	store := snapshot.NewSnapshotStore(t.TempDir())

	// two different hashes but same method+path = one endpoint
	store.Record("api.example.com", "h1", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://api.example.com/users?page=1", StatusCode: 200,
		Headers: map[string][]string{}, Body: "[]",
	})
	store.Record("api.example.com", "h2", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://api.example.com/users?page=2", StatusCode: 200,
		Headers: map[string][]string{}, Body: "[]",
	})

	report, _ := Generate(store)
	if report.Total != 1 {
		t.Errorf("expected 1 unique endpoint, got %d", report.Total)
	}
}

func TestReport_Format(t *testing.T) {
	store := snapshot.NewSnapshotStore(t.TempDir())
	store.Record("api.example.com", "h1", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://api.example.com/users", StatusCode: 200,
		Headers: map[string][]string{}, Body: "[]",
	})

	report, _ := Generate(store)
	output := report.Format()

	if !strings.Contains(output, "api.example.com") {
		t.Error("format should include host")
	}
	if !strings.Contains(output, "GET") {
		t.Error("format should include method")
	}
	if !strings.Contains(output, "/users") {
		t.Error("format should include path")
	}
}

func TestReport_Format_Empty(t *testing.T) {
	report := &Report{Total: 0}
	output := report.Format()
	if !strings.Contains(output, "No recorded") {
		t.Error("empty report should say no recorded endpoints")
	}
}
