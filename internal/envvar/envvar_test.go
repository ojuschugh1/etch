package envvar

import (
	"os"
	"testing"
)

func TestExpand_ConfigVars(t *testing.T) {
	e := NewExpander(map[string]string{
		"BASE_URL": "https://api.staging.example.com",
	})

	got := e.Expand("{{BASE_URL}}/users")
	want := "https://api.staging.example.com/users"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExpand_OSEnvFallback(t *testing.T) {
	os.Setenv("ETCH_API_HOST", "https://api.test.com")
	defer os.Unsetenv("ETCH_API_HOST")

	e := NewExpander(nil)
	got := e.Expand("{{API_HOST}}/data")
	want := "https://api.test.com/data"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExpand_ConfigOverridesOS(t *testing.T) {
	os.Setenv("ETCH_BASE_URL", "https://from-os.com")
	defer os.Unsetenv("ETCH_BASE_URL")

	e := NewExpander(map[string]string{
		"BASE_URL": "https://from-config.com",
	})

	got := e.Expand("{{BASE_URL}}/api")
	if got != "https://from-config.com/api" {
		t.Errorf("config should override OS env, got %q", got)
	}
}

func TestExpand_UnresolvedLeftAsIs(t *testing.T) {
	e := NewExpander(nil)
	got := e.Expand("{{UNKNOWN_VAR}}/path")
	if got != "{{UNKNOWN_VAR}}/path" {
		t.Errorf("unresolved should stay, got %q", got)
	}
}

func TestExpand_MultipleVars(t *testing.T) {
	e := NewExpander(map[string]string{
		"HOST": "api.example.com",
		"VER":  "v2",
	})

	got := e.Expand("https://{{HOST}}/{{VER}}/users")
	if got != "https://api.example.com/v2/users" {
		t.Errorf("got %q", got)
	}
}

func TestCollapse(t *testing.T) {
	e := NewExpander(map[string]string{
		"BASE_URL": "https://api.prod.example.com",
	})

	got := e.Collapse("https://api.prod.example.com/users/42")
	want := "{{BASE_URL}}/users/42"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCollapse_NoMatch(t *testing.T) {
	e := NewExpander(map[string]string{
		"BASE_URL": "https://api.prod.example.com",
	})

	got := e.Collapse("https://other.api.com/data")
	if got != "https://other.api.com/data" {
		t.Errorf("should be unchanged, got %q", got)
	}
}

func TestHasPlaceholders(t *testing.T) {
	if !HasPlaceholders("{{BASE_URL}}/users") {
		t.Error("should detect placeholder")
	}
	if HasPlaceholders("https://api.example.com/users") {
		t.Error("should not detect placeholder in plain URL")
	}
}
