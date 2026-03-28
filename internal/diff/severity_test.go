package diff

import (
	"testing"
)

func TestClassify_StatusCodeChange(t *testing.T) {
	diffs := ClassifyDiffs([]FieldDiff{
		{Path: "status_code", Expected: "200", Actual: "500"},
	})
	if diffs[0].Severity != SeverityCritical {
		t.Errorf("status code change should be critical, got %s", diffs[0].Severity)
	}
}

func TestClassify_FieldRemoved(t *testing.T) {
	diffs := ClassifyDiffs([]FieldDiff{
		{Path: "body.name", Expected: `"Alice"`, Actual: ""},
	})
	if diffs[0].Severity != SeverityCritical {
		t.Errorf("field removal should be critical, got %s", diffs[0].Severity)
	}
}

func TestClassify_FieldAdded(t *testing.T) {
	diffs := ClassifyDiffs([]FieldDiff{
		{Path: "body.new_field", Expected: "", Actual: `"value"`},
	})
	if diffs[0].Severity != SeverityCritical {
		t.Errorf("field addition should be critical, got %s", diffs[0].Severity)
	}
}

func TestClassify_TypeChange(t *testing.T) {
	diffs := ClassifyDiffs([]FieldDiff{
		{Path: "body.name", Expected: `"Alice"`, Actual: "null"},
	})
	if diffs[0].Severity != SeverityCritical {
		t.Errorf("type change (string->null) should be critical, got %s", diffs[0].Severity)
	}
}

func TestClassify_NoisyField(t *testing.T) {
	diffs := ClassifyDiffs([]FieldDiff{
		{Path: "body.created_at", Expected: `"2024-01-01"`, Actual: `"2024-01-02"`},
		{Path: "headers.Date", Expected: "Mon, 01 Jan", Actual: "Tue, 02 Jan"},
		{Path: "body.meta.request_id", Expected: `"abc"`, Actual: `"def"`},
	})
	for _, d := range diffs {
		if d.Severity != SeverityInfo {
			t.Errorf("%s should be info (noisy), got %s", d.Path, d.Severity)
		}
	}
}

func TestClassify_PriceChange(t *testing.T) {
	diffs := ClassifyDiffs([]FieldDiff{
		{Path: "body.total_price", Expected: "29.99", Actual: "39.99"},
	})
	if diffs[0].Severity != SeverityCritical {
		t.Errorf("price change should be critical, got %s", diffs[0].Severity)
	}
}

func TestClassify_HeaderChange(t *testing.T) {
	diffs := ClassifyDiffs([]FieldDiff{
		{Path: "headers.X-RateLimit-Remaining", Expected: "99", Actual: "42"},
	})
	if diffs[0].Severity != SeverityWarning {
		t.Errorf("header change should be warning, got %s", diffs[0].Severity)
	}
}

func TestClassify_RegularValueChange(t *testing.T) {
	diffs := ClassifyDiffs([]FieldDiff{
		{Path: "body.user.name", Expected: `"Alice"`, Actual: `"Bob"`},
	})
	if diffs[0].Severity != SeverityWarning {
		t.Errorf("regular value change should be warning, got %s", diffs[0].Severity)
	}
}

func TestSeverity_String(t *testing.T) {
	tests := []struct {
		s    Severity
		want string
	}{
		{SeverityInfo, "info"},
		{SeverityWarning, "warning"},
		{SeverityCritical, "critical"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("Severity(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestSeverity_Symbol_NoColor(t *testing.T) {
	if SeverityInfo.Symbol(false) != "~" {
		t.Error("info symbol should be ~")
	}
	if SeverityWarning.Symbol(false) != "!" {
		t.Error("warning symbol should be !")
	}
	if SeverityCritical.Symbol(false) != "✗" {
		t.Error("critical symbol should be ✗")
	}
}
