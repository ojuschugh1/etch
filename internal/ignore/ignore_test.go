package ignore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_NoFile(t *testing.T) {
	rules, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rules.IsEmpty() {
		t.Error("expected empty rules when no .etchignore exists")
	}
}

func TestLoad_BasicRules(t *testing.T) {
	dir := t.TempDir()
	content := `# ignore noisy headers
headers.Date
headers.X-Request-Id

# ignore timestamps in body
body.timestamp
body.meta.*
`
	os.WriteFile(filepath.Join(dir, DefaultFile), []byte(content), 0644)

	rules, err := Load(dir)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if rules.Count() != 4 {
		t.Fatalf("expected 4 rules, got %d", rules.Count())
	}

	tests := []struct {
		path   string
		ignore bool
	}{
		{"headers.Date", true},
		{"headers.X-Request-Id", true},
		{"headers.Content-Type", false},
		{"body.timestamp", true},
		{"body.meta.created_at", true},
		{"body.meta.updated_at", true},
		{"body.meta", false},       // exact "body.meta" is NOT matched by "body.meta.*"
		{"body.name", false},
		{"status_code", false},
	}

	for _, tt := range tests {
		got := rules.ShouldIgnore(tt.path)
		if got != tt.ignore {
			t.Errorf("ShouldIgnore(%q) = %v, want %v", tt.path, got, tt.ignore)
		}
	}
}

func TestLoad_StatusCode(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, DefaultFile), []byte("status_code\n"), 0644)

	rules, _ := Load(dir)

	if !rules.ShouldIgnore("status_code") {
		t.Error("should ignore status_code")
	}
	if rules.ShouldIgnore("body.name") {
		t.Error("should not ignore body.name")
	}
}

func TestLoad_CommentsAndBlanks(t *testing.T) {
	dir := t.TempDir()
	content := `
# this is a comment
   # indented comment

headers.Date

   body.id   
`
	os.WriteFile(filepath.Join(dir, DefaultFile), []byte(content), 0644)

	rules, _ := Load(dir)

	if rules.Count() != 2 {
		t.Fatalf("expected 2 rules, got %d", rules.Count())
	}
	if !rules.ShouldIgnore("headers.Date") {
		t.Error("should ignore headers.Date")
	}
	if !rules.ShouldIgnore("body.id") {
		t.Error("should ignore body.id")
	}
}

func TestShouldIgnore_NilRules(t *testing.T) {
	var rules *Rules
	if rules.ShouldIgnore("anything") {
		t.Error("nil rules should never ignore")
	}
}

func TestLoad_WildcardPatterns(t *testing.T) {
	dir := t.TempDir()
	content := `body.users.*
headers.*
`
	os.WriteFile(filepath.Join(dir, DefaultFile), []byte(content), 0644)

	rules, _ := Load(dir)

	tests := []struct {
		path   string
		ignore bool
	}{
		{"body.users.0.name", true},
		{"body.users.1.email", true},
		{"body.count", false},
		{"headers.Date", true},
		{"headers.Content-Type", true},
		{"status_code", false},
	}

	for _, tt := range tests {
		got := rules.ShouldIgnore(tt.path)
		if got != tt.ignore {
			t.Errorf("ShouldIgnore(%q) = %v, want %v", tt.path, got, tt.ignore)
		}
	}
}
