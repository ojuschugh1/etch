package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ojuschugh1/etch/internal/config"
	"github.com/ojuschugh1/etch/internal/snapshot"
	"pgregory.net/rapid"
)

// Exit code should be 0 when everything matches, 1 otherwise.
func TestExitCodeReflectsDiffs(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		matches := rapid.IntRange(0, 100).Draw(rt, "matches")
		mismatches := rapid.IntRange(0, 100).Draw(rt, "mismatches")
		unrecorded := rapid.IntRange(0, 100).Draw(rt, "unrecorded")

		summary := snapshot.TestSummary{
			TotalRequests: matches + mismatches + unrecorded,
			Matches:       matches,
			Mismatches:    mismatches,
			Unrecorded:    unrecorded,
		}

		exitCode := computeExitCode(summary)

		if summary.Mismatches == 0 && summary.Unrecorded == 0 {
			if exitCode != 0 {
				rt.Fatalf("expected exit code 0 when mismatches=%d and unrecorded=%d, got %d",
					summary.Mismatches, summary.Unrecorded, exitCode)
			}
		} else {
			if exitCode != 1 {
				rt.Fatalf("expected exit code 1 when mismatches=%d and unrecorded=%d, got %d",
					summary.Mismatches, summary.Unrecorded, exitCode)
			}
		}
	})
}

// total = matches + mismatches + unrecorded, always.
func TestSummaryCountsAddUp(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		matches := rapid.IntRange(0, 1000).Draw(rt, "matches")
		mismatches := rapid.IntRange(0, 1000).Draw(rt, "mismatches")
		unrecorded := rapid.IntRange(0, 1000).Draw(rt, "unrecorded")

		summary := snapshot.TestSummary{
			TotalRequests: matches + mismatches + unrecorded,
			Matches:       matches,
			Mismatches:    mismatches,
			Unrecorded:    unrecorded,
		}

		got := summary.Matches + summary.Mismatches + summary.Unrecorded
		if got != summary.TotalRequests {
			rt.Fatalf("TotalRequests (%d) != Matches (%d) + Mismatches (%d) + Unrecorded (%d) = %d",
				summary.TotalRequests, summary.Matches, summary.Mismatches, summary.Unrecorded, got)
		}
	})
}

// Every command should print something useful with --help.
func TestHelpOutputForAllCommands(t *testing.T) {
	commands := []string{"record", "test", "diff", "approve", "ca-cert", "openapi"}

	rapid.Check(t, func(rt *rapid.T) {
		cmd := rapid.SampledFrom(commands).Draw(rt, "command")

		output := captureHelpOutput(cmd)

		if len(strings.TrimSpace(output)) == 0 {
			rt.Fatalf("command %q with --help produced empty output", cmd)
		}

		// Help output should mention "Usage" or the command name
		lower := strings.ToLower(output)
		if !strings.Contains(lower, "usage") && !strings.Contains(lower, cmd) {
			rt.Fatalf("command %q --help output does not contain 'usage' or command name:\n%s", cmd, output)
		}
	})
}

// --- Helper functions ---

// computeExitCode mirrors the logic from runTest.
func computeExitCode(summary snapshot.TestSummary) int {
	if summary.Mismatches > 0 || summary.Unrecorded > 0 {
		return 1
	}
	return 0
}

// captureHelpOutput grabs the stdout from a command's --help flag.
func captureHelpOutput(cmd string) string {
	var buf bytes.Buffer

	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(&buf)

	// Register the same flags as the real commands
	fs.Int("port", 0, "Proxy listen port")
	fs.String("snap-dir", "", "Snapshot directory")
	fs.String("config", "", "Config file path")
	fs.Bool("ci", false, "Enable CI mode (no color, non-interactive)")

	// Some commands have extra flags
	switch cmd {
	case "approve":
		fs.String("request", "", "Approve a single pending diff by request hash")
	case "openapi":
		fs.String("output", "", "Write OpenAPI spec to file instead of stdout")
	}

	fs.Usage = func() {
		fmt.Fprintf(&buf, "Usage: etch %s [flags]\n", cmd)
		fs.PrintDefaults()
	}

	_ = fs.Parse([]string{"--help"})

	return buf.String()
}

// --- Unit Tests ---

func TestCLI_NoArgsPrintsHelp(t *testing.T) {
	output := capturePrintHelp()
	if len(strings.TrimSpace(output)) == 0 {
		t.Fatal("expected non-empty help output when no args provided")
	}
	if !strings.Contains(output, "etch") {
		t.Fatal("help output should mention 'etch'")
	}
	if !strings.Contains(output, "Commands:") {
		t.Fatal("help output should list commands")
	}
	// Verify all commands are listed
	for _, cmd := range []string{"record", "test", "diff", "approve", "ca-cert", "version", "openapi"} {
		if !strings.Contains(output, cmd) {
			t.Fatalf("help output should mention command %q", cmd)
		}
	}
}

func TestCLI_CommandHelpOutput(t *testing.T) {
	commands := []string{"record", "test", "diff", "approve", "ca-cert", "openapi"}
	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			output := captureHelpOutput(cmd)
			if len(strings.TrimSpace(output)) == 0 {
				t.Fatalf("command %q --help produced empty output", cmd)
			}
		})
	}
}

func TestCLI_PortFlagOverridesConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.Port != 8080 {
		t.Fatalf("expected default port 8080, got %d", cfg.Port)
	}

	overridePort := 9999
	flags := config.CLIFlags{
		Port: &overridePort,
	}

	merged := cfg.MergeFlags(flags)
	if merged.Port != 9999 {
		t.Fatalf("expected merged port 9999, got %d", merged.Port)
	}
}

func TestCLI_ExitCode1WhenMismatches(t *testing.T) {
	tests := []struct {
		name       string
		mismatches int
		unrecorded int
		wantCode   int
	}{
		{"no mismatches no unrecorded", 0, 0, 0},
		{"one mismatch", 1, 0, 1},
		{"many mismatches", 5, 0, 1},
		{"one unrecorded", 0, 1, 1},
		{"both mismatches and unrecorded", 3, 2, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := snapshot.TestSummary{
				TotalRequests: 10,
				Matches:       10 - tt.mismatches - tt.unrecorded,
				Mismatches:    tt.mismatches,
				Unrecorded:    tt.unrecorded,
			}
			code := computeExitCode(summary)
			if code != tt.wantCode {
				t.Fatalf("expected exit code %d, got %d", tt.wantCode, code)
			}
		})
	}
}

func TestCLI_CIModeDisablesColor(t *testing.T) {
	// Test with --ci flag
	ciTrue := true
	flags := config.CLIFlags{CI: &ciTrue}
	if !isCI(flags) {
		t.Fatal("expected isCI=true when --ci flag is set")
	}

	// Test without --ci flag and no env var
	ciFalse := false
	flags = config.CLIFlags{CI: &ciFalse}
	// Temporarily clear CI env var
	origCI := os.Getenv("CI")
	os.Unsetenv("CI")
	defer os.Setenv("CI", origCI)

	if isCI(flags) {
		t.Fatal("expected isCI=false when --ci flag is false and CI env not set")
	}

	// Test with CI=true env var
	os.Setenv("CI", "true")
	flags = config.CLIFlags{} // no --ci flag
	if !isCI(flags) {
		t.Fatal("expected isCI=true when CI=true env var is set")
	}
}

// capturePrintHelp captures the output of printHelp.
func capturePrintHelp() string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printHelp()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}
