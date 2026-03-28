package noise

import (
	"strings"
	"testing"
)

func TestNormalizer_UUID(t *testing.T) {
	n := NewNormalizer()
	input := `{"id":"550e8400-e29b-41d4-a716-446655440000","name":"Alice"}`
	got := n.NormalizeBody(input)

	if !strings.Contains(got, `"<uuid>"`) {
		t.Errorf("UUID should be replaced, got %s", got)
	}
	if !strings.Contains(got, `"Alice"`) {
		t.Errorf("name should be preserved, got %s", got)
	}
}

func TestNormalizer_Timestamp(t *testing.T) {
	n := NewNormalizer()
	input := `{"created":"2024-01-15T10:30:00Z","value":42}`
	got := n.NormalizeBody(input)

	if !strings.Contains(got, `"<timestamp>"`) {
		t.Errorf("timestamp should be replaced, got %s", got)
	}
	if !strings.Contains(got, "42") {
		t.Errorf("value should be preserved, got %s", got)
	}
}

func TestNormalizer_JWT(t *testing.T) {
	n := NewNormalizer()
	token := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	input := `{"token":"` + token + `"}`
	got := n.NormalizeBody(input)

	if !strings.Contains(got, `"<jwt>"`) {
		t.Errorf("JWT should be replaced, got %s", got)
	}
}

func TestNormalizer_AWSTrace(t *testing.T) {
	n := NewNormalizer()
	input := `{"trace":"Root=1-69c8013b-4e7c685f31dbe84664fe39de"}`
	got := n.NormalizeBody(input)

	if !strings.Contains(got, `"<aws-trace-id>"`) {
		t.Errorf("AWS trace should be replaced, got %s", got)
	}
}

func TestNormalizer_Headers(t *testing.T) {
	n := NewNormalizer()
	headers := map[string][]string{
		"Date":             {"Mon, 01 Jan 2024 00:00:00 GMT"},
		"X-Amzn-Trace-Id": {"Root=1-69c8013b-4e7c685f31dbe84664fe39de"},
		"Content-Type":     {"application/json"},
	}

	normalized := n.NormalizeHeaders(headers)

	if normalized["Date"][0] != "<http-date>" {
		t.Errorf("Date: got %q", normalized["Date"][0])
	}
	if normalized["X-Amzn-Trace-Id"][0] != "<aws-trace-id>" {
		t.Errorf("Trace: got %q", normalized["X-Amzn-Trace-Id"][0])
	}
	if normalized["Content-Type"][0] != "application/json" {
		t.Errorf("Content-Type should be unchanged: got %q", normalized["Content-Type"][0])
	}
}

func TestNormalizer_NonJSON(t *testing.T) {
	n := NewNormalizer()
	input := "Request ID: 550e8400-e29b-41d4-a716-446655440000"
	got := n.NormalizeBody(input)

	if got != "Request ID: <uuid>" {
		t.Errorf("got %q", got)
	}
}

func TestNormalizer_NoChanges(t *testing.T) {
	n := NewNormalizer()
	input := `{"name":"Alice","age":30}`
	got := n.NormalizeBody(input)

	// json re-marshaling may reorder keys, so just check the values are preserved
	if !strings.Contains(got, `"Alice"`) || !strings.Contains(got, "30") {
		t.Errorf("values should be preserved, got %s", got)
	}
}

func TestHasDynamicContent(t *testing.T) {
	n := NewNormalizer()

	if !n.HasDynamicContent("550e8400-e29b-41d4-a716-446655440000") {
		t.Error("UUID should be detected as dynamic")
	}
	if !n.HasDynamicContent("2024-01-15T10:30:00Z") {
		t.Error("timestamp should be detected as dynamic")
	}
	if n.HasDynamicContent("Alice") {
		t.Error("plain string should not be dynamic")
	}
}

func TestStructuralHash_SameStructure(t *testing.T) {
	body1 := `{"name":"Alice","age":30,"active":true}`
	body2 := `{"name":"Bob","age":25,"active":false}`

	h1 := StructuralHash(body1)
	h2 := StructuralHash(body2)

	if h1 != h2 {
		t.Errorf("same structure should produce same hash:\n  %s\n  %s", h1, h2)
	}
}

func TestStructuralHash_DifferentStructure(t *testing.T) {
	body1 := `{"name":"Alice","age":30}`
	body2 := `{"name":"Alice","age":30,"email":"alice@test.com"}`

	h1 := StructuralHash(body1)
	h2 := StructuralHash(body2)

	if h1 == h2 {
		t.Error("different structure should produce different hash")
	}
}

func TestStructuralHash_NonJSON(t *testing.T) {
	got := StructuralHash("not json")
	if got != "<non-json>" {
		t.Errorf("non-JSON should return <non-json>, got %q", got)
	}
}
