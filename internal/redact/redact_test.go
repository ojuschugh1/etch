package redact

import (
	"strings"
	"testing"
)

func TestRedactBody_Email(t *testing.T) {
	r := New()
	got := r.RedactBody(`{"email":"alice@example.com","name":"Alice"}`)
	if strings.Contains(got, "alice@example.com") {
		t.Errorf("email not redacted: %s", got)
	}
	if !strings.Contains(got, "[EMAIL]") {
		t.Errorf("expected [EMAIL] placeholder: %s", got)
	}
	if !strings.Contains(got, "Alice") {
		t.Errorf("name should be preserved: %s", got)
	}
}

func TestRedactBody_CreditCard(t *testing.T) {
	r := New()
	got := r.RedactBody(`{"card":"4111-1111-1111-1111","amount":99}`)
	if strings.Contains(got, "4111") {
		t.Errorf("card not redacted: %s", got)
	}
	if !strings.Contains(got, "[CARD]") {
		t.Errorf("expected [CARD]: %s", got)
	}
}

func TestRedactBody_BearerToken(t *testing.T) {
	r := New()
	got := r.RedactBody(`{"auth":"Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abc123"}`)
	if strings.Contains(got, "eyJhbGci") {
		t.Errorf("bearer token not redacted: %s", got)
	}
}

func TestRedactBody_AWSKey(t *testing.T) {
	r := New()
	got := r.RedactBody(`{"key":"AKIAIOSFODNN7EXAMPLE"}`)
	if strings.Contains(got, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("AWS key not redacted: %s", got)
	}
}

func TestRedactBody_NoSecrets(t *testing.T) {
	r := New()
	input := `{"name":"Alice","age":30,"active":true}`
	got := r.RedactBody(input)
	if got != input {
		t.Errorf("clean body should be unchanged: %s", got)
	}
}

func TestRedactHeaders_Authorization(t *testing.T) {
	r := New()
	headers := map[string][]string{
		"Authorization": {"Bearer sk-abc123xyz"},
		"Content-Type":  {"application/json"},
	}
	got := r.RedactHeaders(headers)
	if got["Authorization"][0] != "[REDACTED]" {
		t.Errorf("auth header not redacted: %s", got["Authorization"][0])
	}
	if got["Content-Type"][0] != "application/json" {
		t.Errorf("content-type should be unchanged: %s", got["Content-Type"][0])
	}
}

func TestRedactHeaders_Cookie(t *testing.T) {
	r := New()
	headers := map[string][]string{
		"Cookie":     {"session=abc123; token=xyz789"},
		"Set-Cookie": {"session=new123; Path=/"},
	}
	got := r.RedactHeaders(headers)
	if got["Cookie"][0] != "[REDACTED]" {
		t.Errorf("cookie not redacted: %s", got["Cookie"][0])
	}
	if got["Set-Cookie"][0] != "[REDACTED]" {
		t.Errorf("set-cookie not redacted: %s", got["Set-Cookie"][0])
	}
}

func TestHasSensitiveContent(t *testing.T) {
	r := New()
	if !r.HasSensitiveContent("alice@test.com") {
		t.Error("should detect email")
	}
	if !r.HasSensitiveContent("AKIAIOSFODNN7EXAMPLE") {
		t.Error("should detect AWS key")
	}
	if r.HasSensitiveContent("just a normal string") {
		t.Error("should not flag normal text")
	}
}

func TestRedactBody_SSN(t *testing.T) {
	r := New()
	got := r.RedactBody(`{"ssn":"123-45-6789"}`)
	if strings.Contains(got, "123-45-6789") {
		t.Errorf("SSN not redacted: %s", got)
	}
}
