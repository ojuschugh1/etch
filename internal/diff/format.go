package diff

import "fmt"

const (
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiGray   = "\033[90m"
	ansiReset  = "\033[0m"
)

// FormatDiff formats a DiffResult as a human-readable string with severity
// indicators. When color is true, ANSI escape codes are used.
func (d *DiffEngine) FormatDiff(result DiffResult, color bool) string {
	if result.Matched {
		return ""
	}

	classified := ClassifyDiffs(result.Fields)

	red, green, yellow, gray, reset := "", "", "", "", ""
	if color {
		red = ansiRed
		green = ansiGreen
		yellow = ansiYellow
		gray = ansiGray
		reset = ansiReset
	}

	out := fmt.Sprintf("  ✗ %s\n", result.URL)
	for _, c := range classified {
		sym := c.Severity.Symbol(color)

		// pick color based on severity
		valColor := yellow
		switch c.Severity {
		case SeverityCritical:
			valColor = red
		case SeverityInfo:
			valColor = gray
		}

		if c.Expected == "" {
			out += fmt.Sprintf("    %s %s: %s(added)%s %s%s%s\n",
				sym, c.Path, valColor, reset, green, c.Actual, reset)
		} else if c.Actual == "" {
			out += fmt.Sprintf("    %s %s: %s%s%s %s(removed)%s\n",
				sym, c.Path, red, c.Expected, reset, valColor, reset)
		} else {
			out += fmt.Sprintf("    %s %s: %s%s%s -> %s%s%s  %s[%s]%s\n",
				sym, c.Path,
				red, c.Expected, reset,
				green, c.Actual, reset,
				gray, c.Reason, reset,
			)
		}
	}
	return out
}

// FormatSummary produces a one-line summary of classified diffs.
// Example: "2 critical, 1 warning, 3 info"
func FormatSummary(fields []FieldDiff, color bool) string {
	classified := ClassifyDiffs(fields)

	counts := map[Severity]int{}
	for _, c := range classified {
		counts[c.Severity]++
	}

	red, yellow, gray, reset := "", "", "", ""
	if color {
		red = ansiRed
		yellow = ansiYellow
		gray = ansiGray
		reset = ansiReset
	}

	parts := []string{}
	if n := counts[SeverityCritical]; n > 0 {
		parts = append(parts, fmt.Sprintf("%s%d critical%s", red, n, reset))
	}
	if n := counts[SeverityWarning]; n > 0 {
		parts = append(parts, fmt.Sprintf("%s%d warning%s", yellow, n, reset))
	}
	if n := counts[SeverityInfo]; n > 0 {
		parts = append(parts, fmt.Sprintf("%s%d info%s", gray, n, reset))
	}

	if len(parts) == 0 {
		return "no changes"
	}

	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ", "
		}
		result += p
	}
	return result
}
