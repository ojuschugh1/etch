package openapi

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ojuschugh1/etch/internal/snapshot"
	"gopkg.in/yaml.v3"
)

func TestEmptySnapshots_ProducesMessage(t *testing.T) {
	dir := t.TempDir()
	store := snapshot.NewSnapshotStore(dir)
	gen := NewOpenAPIGenerator(store)

	spec, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() returned error: %v", err)
	}

	paths, ok := spec["paths"].(map[string]interface{})
	if !ok {
		t.Fatal("spec missing 'paths' field or not a map")
	}
	if len(paths) != 0 {
		t.Fatalf("expected empty paths for empty snapshots, got %d entries", len(paths))
	}

	// WriteYAML should still produce valid YAML with no path content.
	var buf bytes.Buffer
	if err := gen.WriteYAML(&buf); err != nil {
		t.Fatalf("WriteYAML() returned error: %v", err)
	}

	var parsed map[string]interface{}
	if err := yaml.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("WriteYAML output is not valid YAML: %v", err)
	}
}

func TestSingleEndpoint_GeneratesCorrectPathAndSchema(t *testing.T) {
	dir := t.TempDir()
	store := snapshot.NewSnapshotStore(dir)

	entry := snapshot.SnapshotEntry{
		Method:     "GET",
		URL:        "https://api.example.com/users",
		StatusCode: 200,
		Headers:    map[string][]string{"Content-Type": {"application/json"}},
		Body:       `{"id":1,"name":"Alice"}`,
	}
	if err := store.Record("api.example.com", "abc123", entry); err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	gen := NewOpenAPIGenerator(store)
	spec, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() returned error: %v", err)
	}

	// Verify openapi version.
	if v, _ := spec["openapi"].(string); v != "3.0.0" {
		t.Fatalf("expected openapi '3.0.0', got %q", v)
	}

	// Verify paths contains /users.
	paths, _ := spec["paths"].(map[string]interface{})
	pathItem, ok := paths["/users"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected path '/users' in spec, got paths: %v", paths)
	}

	// Verify GET operation exists.
	getOp, ok := pathItem["get"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'get' operation under /users, got: %v", pathItem)
	}

	// Verify responses contain status 200.
	responses, ok := getOp["responses"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'responses' in GET operation")
	}
	resp200, ok := responses["200"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected '200' response, got responses: %v", responses)
	}

	// Verify response has content with application/json schema.
	content, ok := resp200["content"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'content' in 200 response")
	}
	jsonContent, ok := content["application/json"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'application/json' in content")
	}
	schema, ok := jsonContent["schema"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'schema' in application/json content")
	}

	// Schema should be an object with "id" and "name" properties.
	if schema["type"] != "object" {
		t.Fatalf("expected schema type 'object', got %v", schema["type"])
	}
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'properties' in schema")
	}
	if _, ok := props["id"]; !ok {
		t.Fatal("schema missing 'id' property")
	}
	if _, ok := props["name"]; !ok {
		t.Fatal("schema missing 'name' property")
	}
}

func TestPathParamInference_UsersWithIDs(t *testing.T) {
	gen := &OpenAPIGenerator{}

	urls := []string{"/users/123", "/users/456"}
	templates := gen.InferPathParams(urls)

	if len(templates) != 2 {
		t.Fatalf("expected 2 templates, got %d", len(templates))
	}

	expected := "/users/{userId}"
	for i, tmpl := range templates {
		if tmpl.Template != expected {
			t.Fatalf("template[%d] = %q, expected %q", i, tmpl.Template, expected)
		}
		if len(tmpl.Params) != 1 || tmpl.Params[0] != "userId" {
			t.Fatalf("template[%d] params = %v, expected [\"userId\"]", i, tmpl.Params)
		}
	}
}

// End-to-end: record two URLs that differ only in the last segment,
// run Generate, and check that the output has a parameterized path.
func TestPathParamInference_IntegrationWithGenerate(t *testing.T) {
	dir := t.TempDir()
	store := snapshot.NewSnapshotStore(dir)

	for _, id := range []string{"123", "456"} {
		entry := snapshot.SnapshotEntry{
			Method:     "GET",
			URL:        "https://api.example.com/users/" + id,
			StatusCode: 200,
			Headers:    map[string][]string{"Content-Type": {"application/json"}},
			Body:       `{"id":` + id + `}`,
		}
		if err := store.Record("api.example.com", "hash-"+id, entry); err != nil {
			t.Fatalf("Record failed: %v", err)
		}
	}

	gen := NewOpenAPIGenerator(store)
	spec, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() returned error: %v", err)
	}

	paths, _ := spec["paths"].(map[string]interface{})

	// Should have /users/{userId} instead of /users/123 and /users/456.
	if _, ok := paths["/users/{userId}"]; !ok {
		t.Fatalf("expected parameterized path '/users/{userId}', got paths: %v", keysOf(paths))
	}
	if _, ok := paths["/users/123"]; ok {
		t.Fatal("did not expect literal path '/users/123'")
	}
	if _, ok := paths["/users/456"]; ok {
		t.Fatal("did not expect literal path '/users/456'")
	}

	// Verify the GET operation has a path parameter.
	pathItem, _ := paths["/users/{userId}"].(map[string]interface{})
	getOp, _ := pathItem["get"].(map[string]interface{})
	params, ok := getOp["parameters"].([]interface{})
	if !ok || len(params) == 0 {
		t.Fatal("expected path parameters in GET /users/{userId}")
	}
	param0, _ := params[0].(map[string]interface{})
	if param0["name"] != "userId" {
		t.Fatalf("expected parameter name 'userId', got %v", param0["name"])
	}
	if param0["in"] != "path" {
		t.Fatalf("expected parameter in 'path', got %v", param0["in"])
	}
}

// Plain text responses shouldn't get a JSON schema in the output.
func TestNonJSONBody_SkipsSchemaGeneration(t *testing.T) {
	dir := t.TempDir()
	store := snapshot.NewSnapshotStore(dir)

	entry := snapshot.SnapshotEntry{
		Method:     "GET",
		URL:        "https://api.example.com/health",
		StatusCode: 200,
		Headers:    map[string][]string{"Content-Type": {"text/plain"}},
		Body:       "OK - server is healthy",
	}
	if err := store.Record("api.example.com", "health-hash", entry); err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	gen := NewOpenAPIGenerator(store)
	spec, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() returned error: %v", err)
	}

	paths, _ := spec["paths"].(map[string]interface{})
	pathItem, ok := paths["/health"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected path '/health', got paths: %v", keysOf(paths))
	}

	getOp, ok := pathItem["get"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'get' operation under /health")
	}

	responses, _ := getOp["responses"].(map[string]interface{})
	resp200, _ := responses["200"].(map[string]interface{})

	// Non-JSON body should not produce a content/schema entry.
	if content, ok := resp200["content"]; ok {
		t.Fatalf("expected no 'content' for non-JSON body, got: %v", content)
	}
}

// TestInferPathParams_SingleURL verifies that a single URL is returned as-is
// with no parameters.
func TestInferPathParams_SingleURL(t *testing.T) {
	gen := &OpenAPIGenerator{}
	templates := gen.InferPathParams([]string{"/users/123"})

	if len(templates) != 1 {
		t.Fatalf("expected 1 template, got %d", len(templates))
	}
	if templates[0].Template != "/users/123" {
		t.Fatalf("expected '/users/123', got %q", templates[0].Template)
	}
	if len(templates[0].Params) != 0 {
		t.Fatalf("expected no params for single URL, got %v", templates[0].Params)
	}
}

// TestInferPathParams_EmptyInput verifies that empty input returns nil.
func TestInferPathParams_EmptyInput(t *testing.T) {
	gen := &OpenAPIGenerator{}
	templates := gen.InferPathParams(nil)

	if templates != nil {
		t.Fatalf("expected nil for empty input, got %v", templates)
	}
}

// TestWriteYAML_ProducesValidYAML verifies that WriteYAML output is parseable YAML
// containing the expected OpenAPI structure.
func TestWriteYAML_ProducesValidYAML(t *testing.T) {
	dir := t.TempDir()
	store := snapshot.NewSnapshotStore(dir)

	entry := snapshot.SnapshotEntry{
		Method:     "POST",
		URL:        "https://api.example.com/items",
		StatusCode: 201,
		Headers:    map[string][]string{"Content-Type": {"application/json"}},
		Body:       `{"id":42,"created":true}`,
	}
	if err := store.Record("api.example.com", "items-hash", entry); err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	gen := NewOpenAPIGenerator(store)
	var buf bytes.Buffer
	if err := gen.WriteYAML(&buf); err != nil {
		t.Fatalf("WriteYAML() returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "openapi") {
		t.Fatal("YAML output missing 'openapi' field")
	}
	if !strings.Contains(output, "/items") {
		t.Fatal("YAML output missing '/items' path")
	}

	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("YAML output is not parseable: %v", err)
	}
}

// keysOf returns the keys of a map for diagnostic output.
func keysOf(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
