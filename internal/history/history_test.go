package history

import (
	"strings"
	"testing"
)

func TestAppendAndLoad(t *testing.T) {
	store := NewStore(t.TempDir())

	entries := []Entry{
		{Timestamp: "2026-03-01 10:00:00", Endpoint: "GET /users", Field: "body.role", Change: "added", NewValue: "string"},
		{Timestamp: "2026-03-15 14:30:00", Endpoint: "GET /users", Field: "body.id", Change: "type_changed", OldValue: "integer", NewValue: "string"},
	}

	if err := store.Append(entries); err != nil {
		t.Fatalf("Append: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(loaded) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(loaded))
	}
	if loaded[0].Field != "body.role" {
		t.Errorf("first entry field: got %q", loaded[0].Field)
	}
	if loaded[1].Change != "type_changed" {
		t.Errorf("second entry change: got %q", loaded[1].Change)
	}
}

func TestForEndpoint(t *testing.T) {
	store := NewStore(t.TempDir())
	store.Append([]Entry{
		{Timestamp: "2026-03-01 10:00:00", Endpoint: "GET /users", Field: "body.name", Change: "value_changed"},
		{Timestamp: "2026-03-02 11:00:00", Endpoint: "GET /orders", Field: "body.total", Change: "type_changed"},
		{Timestamp: "2026-03-03 12:00:00", Endpoint: "GET /users", Field: "body.role", Change: "added"},
	})

	filtered, _ := store.ForEndpoint("GET /users")
	if len(filtered) != 2 {
		t.Fatalf("expected 2 entries for GET /users, got %d", len(filtered))
	}
}

func TestForEndpoint_Empty(t *testing.T) {
	store := NewStore(t.TempDir())
	entries, _ := store.ForEndpoint("GET /anything")
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestFormat(t *testing.T) {
	entries := []Entry{
		{Timestamp: "2026-03-01 10:00:00", Endpoint: "GET /users", Field: "body.role", Change: "added", NewValue: "string"},
		{Timestamp: "2026-03-15 14:30:00", Endpoint: "GET /users", Field: "body.id", Change: "type_changed", OldValue: "integer", NewValue: "string"},
	}

	out := Format(entries, false)
	if !strings.Contains(out, "2026-03-01") {
		t.Error("should contain date")
	}
	if !strings.Contains(out, "body.role") {
		t.Error("should contain field")
	}
	if !strings.Contains(out, "integer") {
		t.Error("should contain old type")
	}
}

func TestFormat_Empty(t *testing.T) {
	out := Format(nil, false)
	if !strings.Contains(out, "No history") {
		t.Error("should say no history")
	}
}

func TestRecordApproval(t *testing.T) {
	fields := []FieldChange{
		{Path: "body.name", ChangeType: "value_changed", OldValue: "Alice", NewValue: "Bob"},
		{Path: "body.role", ChangeType: "added", NewValue: "admin"},
	}

	entries := RecordApproval("GET /users", fields)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Endpoint != "GET /users" {
		t.Errorf("endpoint: got %q", entries[0].Endpoint)
	}
}
