package schema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSpec(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.yaml")
	os.WriteFile(path, []byte(content), 0644)
	return path
}

const testSpec = `
openapi: "3.0.0"
info:
  title: Test API
  version: "1.0"
paths:
  /users:
    get:
      responses:
        "200":
          description: user list
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    id:
                      type: number
                    name:
                      type: string
                    email:
                      type: string
  /users/{userId}:
    get:
      responses:
        "200":
          description: single user
          content:
            application/json:
              schema:
                type: object
                properties:
                  id:
                    type: number
                  name:
                    type: string
                  email:
                    type: string
`

func TestValidateResponse_ValidBody(t *testing.T) {
	path := writeSpec(t, testSpec)
	spec, err := LoadSpec(path)
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}

	result := spec.ValidateResponse("GET", "/users/42", 200, `{"id":42,"name":"Alice","email":"alice@test.com"}`)

	if len(result.Violations) != 0 {
		t.Errorf("expected no violations, got %d:", len(result.Violations))
		for _, v := range result.Violations {
			t.Errorf("  %s: %s (%s)", v.Path, v.Message, v.Severity)
		}
	}
}

func TestValidateResponse_MissingField(t *testing.T) {
	path := writeSpec(t, testSpec)
	spec, _ := LoadSpec(path)

	// missing "email" field
	result := spec.ValidateResponse("GET", "/users/42", 200, `{"id":42,"name":"Alice"}`)

	found := false
	for _, v := range result.Violations {
		if v.Path == "body.email" && v.Severity == "error" {
			found = true
		}
	}
	if !found {
		t.Error("expected error for missing email field")
	}
}

func TestValidateResponse_UndocumentedField(t *testing.T) {
	path := writeSpec(t, testSpec)
	spec, _ := LoadSpec(path)

	// "role" is not in the spec
	result := spec.ValidateResponse("GET", "/users/42", 200, `{"id":42,"name":"Alice","email":"a@b.com","role":"admin"}`)

	found := false
	for _, v := range result.Violations {
		if v.Path == "body.role" && strings.Contains(v.Message, "undocumented") {
			found = true
		}
	}
	if !found {
		t.Error("expected warning for undocumented 'role' field")
	}
}

func TestValidateResponse_TypeMismatch(t *testing.T) {
	path := writeSpec(t, testSpec)
	spec, _ := LoadSpec(path)

	// "id" should be number but got string
	result := spec.ValidateResponse("GET", "/users/42", 200, `{"id":"not-a-number","name":"Alice","email":"a@b.com"}`)

	found := false
	for _, v := range result.Violations {
		if v.Path == "body.id" && strings.Contains(v.Message, "expected number") {
			found = true
		}
	}
	if !found {
		t.Error("expected error for type mismatch on id")
	}
}

func TestValidateResponse_UnknownEndpoint(t *testing.T) {
	path := writeSpec(t, testSpec)
	spec, _ := LoadSpec(path)

	result := spec.ValidateResponse("GET", "/unknown", 200, `{}`)

	if len(result.Violations) == 0 {
		t.Error("expected warning for unknown endpoint")
	}
}

func TestValidateResponse_ArrayBody(t *testing.T) {
	path := writeSpec(t, testSpec)
	spec, _ := LoadSpec(path)

	result := spec.ValidateResponse("GET", "/users", 200, `[{"id":1,"name":"Alice","email":"a@b.com"}]`)

	errors := 0
	for _, v := range result.Violations {
		if v.Severity == "error" {
			errors++
		}
	}
	if errors != 0 {
		t.Errorf("expected 0 errors for valid array body, got %d", errors)
	}
}

func TestValidateResponse_ParameterizedPath(t *testing.T) {
	path := writeSpec(t, testSpec)
	spec, _ := LoadSpec(path)

	// /users/123 should match /users/{userId}
	result := spec.ValidateResponse("GET", "/users/123", 200, `{"id":123,"name":"Bob","email":"b@c.com"}`)

	errors := 0
	for _, v := range result.Violations {
		if v.Severity == "error" {
			errors++
		}
	}
	if errors != 0 {
		t.Errorf("expected 0 errors, got %d", errors)
	}
}

func TestResult_HasErrors(t *testing.T) {
	r := &Result{Violations: []Violation{
		{Severity: "warning"},
		{Severity: "error"},
	}}
	if !r.HasErrors() {
		t.Error("should have errors")
	}

	r2 := &Result{Violations: []Violation{
		{Severity: "warning"},
	}}
	if r2.HasErrors() {
		t.Error("should not have errors (only warnings)")
	}
}

func TestResult_Format(t *testing.T) {
	r := &Result{
		Endpoint:   "GET /users",
		StatusCode: 200,
		Violations: []Violation{
			{Path: "body.email", Message: "field missing", Severity: "error"},
			{Path: "body.role", Message: "undocumented", Severity: "warning"},
		},
	}

	out := r.Format(false)
	if !strings.Contains(out, "body.email") {
		t.Error("should contain field path")
	}
	if !strings.Contains(out, "field missing") {
		t.Error("should contain message")
	}
}
