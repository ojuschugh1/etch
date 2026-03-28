package constraint

import (
	"strings"
	"testing"

	"github.com/ojuschugh1/etch/internal/snapshot"
)

func TestLearn_EmptyStore(t *testing.T) {
	store := snapshot.NewSnapshotStore(t.TempDir())
	report, err := Learn(store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Endpoints) != 0 {
		t.Errorf("expected 0 endpoints, got %d", len(report.Endpoints))
	}
}

func TestLearn_DetectsTypes(t *testing.T) {
	store := snapshot.NewSnapshotStore(t.TempDir())
	store.Record("api.test.com", "h1", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://api.test.com/user", StatusCode: 200,
		Headers: map[string][]string{}, Body: `{"name":"Alice","age":30,"active":true}`,
	})

	report, _ := Learn(store)
	rules := report.Endpoints["GET /user"]

	typeMap := make(map[string]string)
	for _, r := range rules {
		typeMap[r.Path] = r.Type
	}

	if typeMap["body.name"] != "string" {
		t.Errorf("body.name type: got %q, want string", typeMap["body.name"])
	}
	if typeMap["body.age"] != "number" {
		t.Errorf("body.age type: got %q, want number", typeMap["body.age"])
	}
	if typeMap["body.active"] != "boolean" {
		t.Errorf("body.active type: got %q, want boolean", typeMap["body.active"])
	}
}

func TestLearn_AlwaysSetAndNeverNull(t *testing.T) {
	store := snapshot.NewSnapshotStore(t.TempDir())

	// two recordings of the same endpoint, both have "name" but only one has "nickname"
	store.Record("api.test.com", "h1", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://api.test.com/user", StatusCode: 200,
		Headers: map[string][]string{}, Body: `{"name":"Alice","nickname":"Ali"}`,
	})
	store.Record("api.test.com", "h2", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://api.test.com/user", StatusCode: 200,
		Headers: map[string][]string{}, Body: `{"name":"Bob"}`,
	})

	report, _ := Learn(store)
	rules := report.Endpoints["GET /user"]

	ruleMap := make(map[string]Rule)
	for _, r := range rules {
		ruleMap[r.Path] = r
	}

	if !ruleMap["body.name"].AlwaysSet {
		t.Error("body.name should be always set")
	}
	if ruleMap["body.nickname"].AlwaysSet {
		t.Error("body.nickname should NOT be always set (missing in second recording)")
	}
}

func TestLearn_TracksUniqueValues(t *testing.T) {
	store := snapshot.NewSnapshotStore(t.TempDir())
	store.Record("api.test.com", "h1", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://api.test.com/status", StatusCode: 200,
		Headers: map[string][]string{}, Body: `{"status":"active"}`,
	})
	store.Record("api.test.com", "h2", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://api.test.com/status", StatusCode: 200,
		Headers: map[string][]string{}, Body: `{"status":"inactive"}`,
	})

	report, _ := Learn(store)
	rules := report.Endpoints["GET /status"]

	for _, r := range rules {
		if r.Path == "body.status" {
			if len(r.Values) != 2 {
				t.Errorf("expected 2 unique values, got %d", len(r.Values))
			}
			return
		}
	}
	t.Error("body.status rule not found")
}

func TestReport_Format(t *testing.T) {
	store := snapshot.NewSnapshotStore(t.TempDir())
	store.Record("api.test.com", "h1", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://api.test.com/user", StatusCode: 200,
		Headers: map[string][]string{}, Body: `{"name":"Alice"}`,
	})

	report, _ := Learn(store)
	output := report.Format()

	if !strings.Contains(output, "GET /user") {
		t.Error("format should include endpoint")
	}
	if !strings.Contains(output, "body.name") {
		t.Error("format should include field path")
	}
	if !strings.Contains(output, "string") {
		t.Error("format should include type")
	}
}

func TestReport_Format_Empty(t *testing.T) {
	report := &Report{Endpoints: make(map[string][]Rule)}
	output := report.Format()
	if !strings.Contains(output, "No constraints") {
		t.Error("empty report should say no constraints")
	}
}
