package diff

import (
	"testing"

	"github.com/ojuschugh1/etch/internal/snapshot"
)

func TestSchemaMode_ValueChangeIgnored(t *testing.T) {
	de := &DiffEngine{SchemaMode: true}

	stored := &snapshot.SnapshotEntry{
		StatusCode: 200, Headers: map[string][]string{},
		Body: `{"name":"Alice","age":30,"active":true}`,
	}
	live := &snapshot.SnapshotEntry{
		StatusCode: 200, Headers: map[string][]string{},
		Body: `{"name":"Bob","age":25,"active":false}`,
	}

	result := de.Compare(stored, live)
	if !result.Matched {
		t.Errorf("schema mode should ignore value changes, got %d diffs:", len(result.Fields))
		for _, f := range result.Fields {
			t.Errorf("  %s: %s -> %s", f.Path, f.Expected, f.Actual)
		}
	}
}

func TestSchemaMode_TypeChangeCaught(t *testing.T) {
	de := &DiffEngine{SchemaMode: true}

	// id changes from integer to string - this is the critical case
	stored := &snapshot.SnapshotEntry{
		StatusCode: 200, Headers: map[string][]string{},
		Body: `{"user_id":4521,"name":"Alice"}`,
	}
	live := &snapshot.SnapshotEntry{
		StatusCode: 200, Headers: map[string][]string{},
		Body: `{"user_id":"4521","name":"Bob"}`,
	}

	result := de.Compare(stored, live)
	if result.Matched {
		t.Fatal("should catch integer -> string type change")
	}

	found := false
	for _, f := range result.Fields {
		if f.Path == "body.user_id" && f.Expected == "integer" && f.Actual == "string" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected body.user_id: integer -> string, got: %+v", result.Fields)
	}
}

func TestSchemaMode_FieldRemoved(t *testing.T) {
	de := &DiffEngine{SchemaMode: true}

	stored := &snapshot.SnapshotEntry{
		StatusCode: 200, Headers: map[string][]string{},
		Body: `{"name":"Alice","email":"alice@test.com"}`,
	}
	live := &snapshot.SnapshotEntry{
		StatusCode: 200, Headers: map[string][]string{},
		Body: `{"name":"Bob"}`,
	}

	result := de.Compare(stored, live)
	if result.Matched {
		t.Fatal("should catch removed field")
	}

	found := false
	for _, f := range result.Fields {
		if f.Path == "body.email" && f.Actual == "" {
			found = true
		}
	}
	if !found {
		t.Error("expected body.email removal diff")
	}
}

func TestSchemaMode_FieldAdded(t *testing.T) {
	de := &DiffEngine{SchemaMode: true}

	stored := &snapshot.SnapshotEntry{
		StatusCode: 200, Headers: map[string][]string{},
		Body: `{"name":"Alice"}`,
	}
	live := &snapshot.SnapshotEntry{
		StatusCode: 200, Headers: map[string][]string{},
		Body: `{"name":"Bob","role":"admin"}`,
	}

	result := de.Compare(stored, live)
	if result.Matched {
		t.Fatal("should catch added field")
	}

	found := false
	for _, f := range result.Fields {
		if f.Path == "body.role" && f.Expected == "" {
			found = true
		}
	}
	if !found {
		t.Error("expected body.role addition diff")
	}
}

func TestSchemaMode_NestedTypeChange(t *testing.T) {
	de := &DiffEngine{SchemaMode: true}

	stored := &snapshot.SnapshotEntry{
		StatusCode: 200, Headers: map[string][]string{},
		Body: `{"user":{"id":1,"name":"Alice"}}`,
	}
	live := &snapshot.SnapshotEntry{
		StatusCode: 200, Headers: map[string][]string{},
		Body: `{"user":{"id":"one","name":"Bob"}}`,
	}

	result := de.Compare(stored, live)
	if result.Matched {
		t.Fatal("should catch nested type change")
	}

	found := false
	for _, f := range result.Fields {
		if f.Path == "body.user.id" {
			found = true
		}
	}
	if !found {
		t.Error("expected body.user.id type change diff")
	}
}

func TestSchemaMode_ArrayElementTypeChange(t *testing.T) {
	de := &DiffEngine{SchemaMode: true}

	stored := &snapshot.SnapshotEntry{
		StatusCode: 200, Headers: map[string][]string{},
		Body: `{"items":[{"id":1,"name":"A"}]}`,
	}
	live := &snapshot.SnapshotEntry{
		StatusCode: 200, Headers: map[string][]string{},
		Body: `{"items":[{"id":"one","name":"B"}]}`,
	}

	result := de.Compare(stored, live)
	if result.Matched {
		t.Fatal("should catch array element type change")
	}
}

func TestSchemaMode_StatusCodeStillCaught(t *testing.T) {
	de := &DiffEngine{SchemaMode: true}

	stored := &snapshot.SnapshotEntry{
		StatusCode: 200, Headers: map[string][]string{}, Body: `{}`,
	}
	live := &snapshot.SnapshotEntry{
		StatusCode: 500, Headers: map[string][]string{}, Body: `{}`,
	}

	result := de.Compare(stored, live)
	if result.Matched {
		t.Fatal("schema mode should still catch status code changes")
	}
}

func TestInferSchema(t *testing.T) {
	schema := InferSchema(`{"id":42,"name":"Alice","active":true,"score":3.14,"tags":["a","b"]}`)
	if schema == nil {
		t.Fatal("should infer schema")
	}
	if schema["id"] != "integer" {
		t.Errorf("id: got %v, want integer", schema["id"])
	}
	if schema["name"] != "string" {
		t.Errorf("name: got %v, want string", schema["name"])
	}
	if schema["active"] != "boolean" {
		t.Errorf("active: got %v, want boolean", schema["active"])
	}
	if schema["score"] != "number" {
		t.Errorf("score: got %v, want number", schema["score"])
	}
}
