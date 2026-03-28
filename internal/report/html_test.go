package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ojuschugh1/etch/internal/diff"
)

func TestGenerateHTML_WithDiffs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.html")

	results := []diff.DiffResult{
		{
			URL:     "http://api.example.com/users",
			Matched: false,
			Fields: []diff.FieldDiff{
				{Path: "status_code", Expected: "200", Actual: "500"},
				{Path: "body.name", Expected: `"Alice"`, Actual: `"Bob"`},
			},
		},
	}

	err := GenerateHTML(results, path)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}

	data, _ := os.ReadFile(path)
	html := string(data)

	if !strings.Contains(html, "Etch Diff Report") {
		t.Error("should contain title")
	}
	if !strings.Contains(html, "api.example.com/users") {
		t.Error("should contain endpoint URL")
	}
	if !strings.Contains(html, "status_code") {
		t.Error("should contain field path")
	}
	if !strings.Contains(html, "critical") {
		t.Error("should contain severity")
	}
}

func TestGenerateHTML_NoDiffs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.html")

	err := GenerateHTML(nil, path)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "All responses match") {
		t.Error("empty report should say all match")
	}
}

func TestCollectDiffs(t *testing.T) {
	raw := []interface{}{
		diff.DiffResult{Matched: true},
		diff.DiffResult{Matched: false, URL: "http://test.com", Fields: []diff.FieldDiff{{Path: "x"}}},
		diff.DiffResult{Matched: false, URL: "http://test2.com", Fields: []diff.FieldDiff{{Path: "y"}}},
	}

	results := CollectDiffs(raw)
	if len(results) != 2 {
		t.Errorf("expected 2 unmatched results, got %d", len(results))
	}
}

func TestFormatSummary(t *testing.T) {
	results := []diff.DiffResult{
		{Matched: false, Fields: []diff.FieldDiff{{}, {}}},
		{Matched: false, Fields: []diff.FieldDiff{{}}},
	}

	got := FormatSummary(results)
	if !strings.Contains(got, "2 endpoint") {
		t.Errorf("should mention 2 endpoints, got %q", got)
	}
	if !strings.Contains(got, "3 field") {
		t.Errorf("should mention 3 fields, got %q", got)
	}
}
