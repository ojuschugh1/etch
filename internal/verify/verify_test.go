package verify

import (
	"strings"
	"testing"

	"github.com/ojuschugh1/etch/internal/snapshot"
)

func setupTestStore(t *testing.T) *snapshot.SnapshotStore {
	t.Helper()
	store := snapshot.NewSnapshotStore(t.TempDir())

	store.Record("api.test.com", "h1", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://api.test.com/users", StatusCode: 200,
		Headers: map[string][]string{},
		Body:    `{"users":[{"id":1,"name":"Alice"},{"id":2,"name":"Bob"}],"total":2}`,
	})
	store.Record("api.test.com", "h2", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://api.test.com/users?role=admin", StatusCode: 200,
		Headers: map[string][]string{},
		Body:    `{"users":[{"id":1,"name":"Alice"}],"total":1}`,
	})
	store.Record("api.test.com", "h3", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://api.test.com/users/count", StatusCode: 200,
		Headers: map[string][]string{},
		Body:    `{"count":2}`,
	})

	return store
}

func TestEquivalence_SameTotal(t *testing.T) {
	store := setupTestStore(t)

	relations := []Relation{{
		Name:      "total is consistent across pages",
		Type:      "equivalence",
		Endpoints: []string{"GET /users", "GET /users"},
		Field:     "total",
	}}

	report, err := RunAll(store, relations)
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	if report.Failed != 0 {
		t.Errorf("expected 0 failures, got %d: %s", report.Failed, report.Results[0].Message)
	}
}

func TestSubset_FilteredIsSubset(t *testing.T) {
	store := setupTestStore(t)

	relations := []Relation{{
		Name:      "admin users are subset of all users",
		Type:      "subset",
		Endpoints: []string{"GET /users?role=admin", "GET /users"},
		Field:     "users",
	}}

	report, _ := RunAll(store, relations)
	if report.Failed != 0 {
		t.Errorf("expected pass, got: %s", report.Results[0].Message)
	}
}

func TestConsistency_CountMatchesLength(t *testing.T) {
	store := setupTestStore(t)

	relations := []Relation{{
		Name:       "user count matches list length",
		Type:       "consistency",
		Endpoints:  []string{"GET /users", "GET /users/count"},
		Field:      "users",
		CountField: "count",
	}}

	report, _ := RunAll(store, relations)
	if report.Failed != 0 {
		t.Errorf("expected pass, got: %s", report.Results[0].Message)
	}
}

func TestConsistency_Mismatch(t *testing.T) {
	store := snapshot.NewSnapshotStore(t.TempDir())
	store.Record("api.test.com", "h1", snapshot.SnapshotEntry{
		Method: "GET", URL: "http://api.test.com/items", StatusCode: 200,
		Headers: map[string][]string{},
		Body:    `{"items":[1,2,3],"total":5}`,
	})

	relations := []Relation{{
		Name:       "items count matches total",
		Type:       "consistency",
		Endpoints:  []string{"GET /items", "GET /items"},
		Field:      "items",
		CountField: "total",
	}}

	report, _ := RunAll(store, relations)
	if report.Passed != 0 {
		t.Error("should fail - array has 3 items but total says 5")
	}
}

func TestMissingEndpoint(t *testing.T) {
	store := snapshot.NewSnapshotStore(t.TempDir())

	relations := []Relation{{
		Name:      "check missing",
		Type:      "equivalence",
		Endpoints: []string{"GET /nonexistent", "GET /also-missing"},
		Field:     "total",
	}}

	report, _ := RunAll(store, relations)
	if report.Passed != 0 {
		t.Error("should fail for missing endpoints")
	}
}

func TestReport_Format(t *testing.T) {
	report := &Report{
		Passed: 1,
		Failed: 1,
		Results: []Result{
			{Name: "check A", Passed: true, Message: "ok"},
			{Name: "check B", Passed: false, Message: "mismatch"},
		},
	}

	out := report.Format(false)
	if !strings.Contains(out, "1 passed") {
		t.Error("should show passed count")
	}
	if !strings.Contains(out, "1 failed") {
		t.Error("should show failed count")
	}
	if !strings.Contains(out, "check B") {
		t.Error("should show failed check name")
	}
}

func TestEmptyRelations(t *testing.T) {
	store := snapshot.NewSnapshotStore(t.TempDir())
	report, _ := RunAll(store, nil)
	out := report.Format(false)
	if !strings.Contains(out, "No relations") {
		t.Error("should say no relations defined")
	}
}
