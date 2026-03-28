package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ojuschugh1/etch/internal/approval"
	"github.com/ojuschugh1/etch/internal/audit"
	"github.com/ojuschugh1/etch/internal/ca"
	"github.com/ojuschugh1/etch/internal/config"
	"github.com/ojuschugh1/etch/internal/constraint"
	"github.com/ojuschugh1/etch/internal/coverage"
	"github.com/ojuschugh1/etch/internal/diff"
	"github.com/ojuschugh1/etch/internal/envvar"
	"github.com/ojuschugh1/etch/internal/hash"
	"github.com/ojuschugh1/etch/internal/history"
	"github.com/ojuschugh1/etch/internal/ignore"
	"github.com/ojuschugh1/etch/internal/llm"
	"github.com/ojuschugh1/etch/internal/mock"
	"github.com/ojuschugh1/etch/internal/noise"
	"github.com/ojuschugh1/etch/internal/openapi"
	"github.com/ojuschugh1/etch/internal/proxy"
	"github.com/ojuschugh1/etch/internal/redact"
	"github.com/ojuschugh1/etch/internal/report"
	"github.com/ojuschugh1/etch/internal/schema"
	"github.com/ojuschugh1/etch/internal/snapshot"
	"github.com/ojuschugh1/etch/internal/verify"
	"github.com/ojuschugh1/etch/internal/watch"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(0)
	}

	cmd := os.Args[1]

	// Handle --help and -h at top level
	if cmd == "--help" || cmd == "-h" {
		printHelp()
		os.Exit(0)
	}

	switch cmd {
	case "record":
		os.Exit(runRecord(os.Args[2:]))
	case "test":
		os.Exit(runTest(os.Args[2:]))
	case "diff":
		os.Exit(runDiff(os.Args[2:]))
	case "approve":
		os.Exit(runApprove(os.Args[2:]))
	case "ca-cert":
		os.Exit(runCACert(os.Args[2:]))
	case "version":
		fmt.Println(version)
		os.Exit(0)
	case "openapi":
		os.Exit(runOpenAPI(os.Args[2:]))
	case "mock":
		os.Exit(runMock(os.Args[2:]))
	case "coverage":
		os.Exit(runCoverage(os.Args[2:]))
	case "learn":
		os.Exit(runLearn(os.Args[2:]))
	case "noise":
		os.Exit(runNoise(os.Args[2:]))
	case "validate":
		os.Exit(runValidate(os.Args[2:]))
	case "verify":
		os.Exit(runVerify(os.Args[2:]))
	case "report":
		os.Exit(runReport(os.Args[2:]))
	case "watch":
		os.Exit(runWatch(os.Args[2:]))
	case "history":
		os.Exit(runHistory(os.Args[2:]))
	case "init":
		os.Exit(runInit(os.Args[2:]))
	case "audit":
		os.Exit(runAudit(os.Args[2:]))
	case "prune":
		os.Exit(runPrune(os.Args[2:]))
	default:
		fmt.Fprintf(os.Stderr, "etch: unknown command %q\n", cmd)
		fmt.Fprintln(os.Stderr, "Run 'etch --help' for usage.")
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Println("etch - API snapshot testing tool")
	fmt.Println()
	fmt.Println("Usage: etch <command> [flags]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  record    Start proxy in record mode")
	fmt.Println("  test      Start proxy in test mode")
	fmt.Println("  diff      Show pending diffs")
	fmt.Println("  approve   Approve snapshot changes")
	fmt.Println("  ca-cert   Print CA certificate path")
	fmt.Println("  version   Print version")
	fmt.Println("  openapi   Generate OpenAPI spec")
	fmt.Println("  mock      Serve recorded snapshots as a mock server")
	fmt.Println("  coverage  Show recorded endpoint coverage")
	fmt.Println("  learn     Show learned field constraints from recordings")
	fmt.Println("  noise     Detect noisy fields and suggest .etchignore rules")
	fmt.Println("  validate  Validate snapshots against an OpenAPI spec")
	fmt.Println("  verify    Run metamorphic relation checks")
	fmt.Println("  report    Generate HTML diff report")
	fmt.Println("  watch     Continuously monitor for snapshot changes")
	fmt.Println("  history   Show schema evolution timeline")
	fmt.Println("  init      Initialize etch in the current directory")
	fmt.Println("  audit     Scan snapshots for sensitive data (PII, secrets, tokens)")
	fmt.Println("  prune     Remove stale snapshots not hit during last test run")
	fmt.Println()
	fmt.Println("Global Flags:")
	fmt.Println("  --port       Proxy listen port (default: 8080)")
	fmt.Println("  --snap-dir   Snapshot directory")
	fmt.Println("  --config     Config file path")
	fmt.Println("  --ci         Enable CI mode (no color, non-interactive)")
}

// parseGlobalFlags registers and parses the standard global flags on the given FlagSet.
// Returns the populated CLIFlags.
func parseGlobalFlags(fs *flag.FlagSet, args []string) config.CLIFlags {
	var flags config.CLIFlags

	port := fs.Int("port", 0, "Proxy listen port")
	snapDir := fs.String("snap-dir", "", "Snapshot directory")
	configPath := fs.String("config", "", "Config file path")
	ci := fs.Bool("ci", false, "Enable CI mode (no color, non-interactive)")

	_ = fs.Parse(args)

	if isFlagSet(fs, "port") {
		flags.Port = port
	}
	if isFlagSet(fs, "snap-dir") {
		flags.SnapDir = snapDir
	}
	if isFlagSet(fs, "config") {
		flags.ConfigPath = configPath
	}
	if isFlagSet(fs, "ci") {
		flags.CI = ci
	}

	return flags
}

// isFlagSet returns true if the named flag was explicitly set on the command line.
func isFlagSet(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

// loadMergedConfig loads the config file and merges CLI flags.
func loadMergedConfig(flags config.CLIFlags) (*config.Config, error) {
	cfgPath := config.DefaultConfigPath
	if flags.ConfigPath != nil {
		cfgPath = *flags.ConfigPath
	}

	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		return nil, err
	}

	return cfg.MergeFlags(flags), nil
}

// isCI returns true if CI mode is active via flag or environment variable.
func isCI(flags config.CLIFlags) bool {
	if flags.CI != nil && *flags.CI {
		return true
	}
	return os.Getenv("CI") == "true"
}


// --- record command ---

func runRecord(args []string) int {
	fs := flag.NewFlagSet("record", flag.ExitOnError)
	redactSecrets := fs.Bool("redact", false, "Scrub PII and secrets from snapshots before saving")
	fs.Usage = func() {
		fmt.Println("Usage: etch record [flags]")
		fmt.Println()
		fmt.Println("Start the proxy in record mode. Intercepted API responses are")
		fmt.Println("saved as snapshots for later comparison.")
		fmt.Println()
		fmt.Println("Flags:")
		fs.PrintDefaults()
	}
	flags := parseGlobalFlags(fs, args)

	cfg, err := loadMergedConfig(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch record: %v\n", err)
		return 1
	}

	hc := hash.NewHashComputer(cfg.ExcludedHeaders, cfg.IncludedHeaders)
	ss := snapshot.NewSnapshotStore(cfg.SnapDir)

	cam, err := ca.NewCAManager(cfg.CADir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch record: %v\n", err)
		return 1
	}

	handler := proxy.NewRecordHandler(hc, ss)

	// wire env var support if configured
	if len(cfg.Env) > 0 {
		handler.EnvExpander = envvar.NewExpander(cfg.Env)
	}

	// wire secret redaction if requested
	if *redactSecrets {
		handler.Redactor = redact.New()
		fmt.Println("Secret redaction enabled - PII and tokens will be scrubbed from snapshots")
	}

	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := proxy.NewProxyServer(addr, proxy.ModeRecord, cam, handler)
	srv.MaxBodySize = cfg.MaxBodySize

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := srv.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "etch record: %v\n", err)
		return 1
	}

	fmt.Println("Snapshots saved. Exiting.")
	return 0
}

// --- test command ---

func runTest(args []string) int {
	fs := flag.NewFlagSet("test", flag.ExitOnError)
	mode := fs.String("mode", "smart", "Comparison mode: raw, smart, strict, schema")
	explain := fs.Bool("explain", false, "Show why fields were normalized or ignored")
	specPath := fs.String("spec", "", "OpenAPI spec to validate responses against (optional)")
	injectHeader := fs.String("inject-header", "", "Inject a header into replayed requests (e.g. 'Authorization: Bearer $TOKEN')")
	jsonOutput := fs.Bool("json", false, "Output test results as JSON (for CI pipelines)")
	fs.Usage = func() {
		fmt.Println("Usage: etch test [flags]")
		fmt.Println()
		fmt.Println("Start the proxy in test mode. Live API responses are compared")
		fmt.Println("against stored snapshots. Exit code 1 if mismatches are found.")
		fmt.Println()
		fmt.Println("If --spec is provided, responses are also validated against the")
		fmt.Println("OpenAPI specification for missing fields, type mismatches, etc.")
		fmt.Println()
		fmt.Println("Modes:")
		fmt.Println("  raw      No normalization - compare values exactly as-is")
		fmt.Println("  smart    Normalize common dynamic patterns (UUIDs, timestamps, JWTs)")
		fmt.Println("  strict   Only normalize the most obvious patterns (timestamps, trace IDs)")
		fmt.Println()
		fmt.Println("Flags:")
		fs.PrintDefaults()
	}
	flags := parseGlobalFlags(fs, args)

	cfg, err := loadMergedConfig(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch test: %v\n", err)
		return 1
	}

	ciMode := isCI(flags)
	useColor := !ciMode

	// run before_test hook if configured
	if cfg.Hooks != nil && cfg.Hooks.BeforeTest != "" {
		fmt.Printf("Running before_test hook: %s\n", cfg.Hooks.BeforeTest)
		cmd := exec.Command("sh", "-c", cfg.Hooks.BeforeTest)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "etch test: before_test hook failed: %v\n", err)
			return 1
		}
	}

	hc := hash.NewHashComputer(cfg.ExcludedHeaders, cfg.IncludedHeaders)
	ss := snapshot.NewSnapshotStore(cfg.SnapDir)

	// show active mode
	modeDesc := map[string]string{
		"raw":    "no normalization - exact comparison",
		"smart":  "normalizing UUIDs, timestamps, JWTs, trace IDs",
		"strict": "normalizing timestamps and trace IDs only",
		"schema": "comparing types and structure only, ignoring values",
	}
	fmt.Printf("Mode: %s (%s)\n", *mode, modeDesc[*mode])

	// check that snapshots exist before starting the proxy
	if _, err := os.Stat(cfg.SnapDir); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "etch test: no snapshots found at %s\n", cfg.SnapDir)
		fmt.Fprintln(os.Stderr, "Run 'etch record' first to capture baseline responses,")
		fmt.Fprintln(os.Stderr, "then commit the snapshot directory to version control.")
		return 1
	}

	// Load .etchignore rules from the working directory
	ignoreRules, err := ignore.Load(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch test: loading .etchignore: %v\n", err)
		return 1
	}
	if !ignoreRules.IsEmpty() {
		fmt.Printf("Loaded %d ignore rule(s) from .etchignore\n", ignoreRules.Count())
	}

	normalizer := noise.NewNormalizerForMode(*mode)
	if normalizer != nil && *explain {
		normalizer.Explain = true
	}
	// add custom normalizers from config
	if normalizer != nil && len(cfg.Normalizers) > 0 {
		for _, nr := range cfg.Normalizers {
			if err := normalizer.AddRule(nr.Pattern, nr.Replace); err != nil {
				fmt.Fprintf(os.Stderr, "etch test: %v\n", err)
				return 1
			}
		}
	}
	de := diff.NewDiffEngineFull(ignoreRules, normalizer)
	if *mode == "schema" {
		de.SchemaMode = true
	}
	pendingDir := filepath.Join(cfg.SnapDir, ".pending")
	am := approval.NewApprovalManager(ss, pendingDir)

	cam, err := ca.NewCAManager(cfg.CADir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch test: %v\n", err)
		return 1
	}

	handler := proxy.NewTestHandler(hc, ss, de, am)

	// wire env var support for cross-environment matching
	if len(cfg.Env) > 0 {
		handler.EnvExpander = envvar.NewExpander(cfg.Env)
	}

	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := proxy.NewProxyServer(addr, proxy.ModeTest, cam, handler)
	srv.MaxBodySize = cfg.MaxBodySize

	// parse and inject headers if provided
	if *injectHeader != "" {
		parts := strings.SplitN(*injectHeader, ":", 2)
		if len(parts) == 2 {
			srv.InjectHeaders = map[string]string{
				strings.TrimSpace(parts[0]): strings.TrimSpace(parts[1]),
			}
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := srv.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "etch test: %v\n", err)
		return 1
	}

	// Print summary after proxy shuts down.
	summary := handler.GetSummary()

	// Save hit hashes for the prune command.
	hitHashes := handler.GetHitHashes()
	if len(hitHashes) > 0 {
		hitData, _ := json.Marshal(hitHashes)
		hitPath := filepath.Join(cfg.SnapDir, ".last-test-hits.json")
		_ = os.WriteFile(hitPath, hitData, 0644)
	}

	fmt.Println()
	fmt.Printf("Test Summary (mode: %s):\n", *mode)
	fmt.Printf("  Total:      %d\n", summary.TotalRequests)
	fmt.Printf("  Matches:    %d\n", summary.Matches)
	fmt.Printf("  Mismatches: %d\n", summary.Mismatches)
	fmt.Printf("  Unrecorded: %d\n", summary.Unrecorded)

	// show severity breakdown for mismatches
	if len(summary.Diffs) > 0 {
		var allFields []diff.FieldDiff
		for _, raw := range summary.Diffs {
			if d, ok := raw.(diff.DiffResult); ok {
				allFields = append(allFields, d.Fields...)
			}
		}
		if len(allFields) > 0 {
			fmt.Printf("  Breakdown:  %s\n", diff.FormatSummary(allFields, useColor))
		}
	}

	// Optionally invoke LLM for diff summaries.
	llmClient := llm.NewLLMClient(cfg.LLM)
	if llmClient != nil && len(summary.Diffs) > 0 {
		for _, raw := range summary.Diffs {
			d, ok := raw.(diff.DiffResult)
			if !ok {
				continue
			}
			formatted := de.FormatDiff(d, false)
			llmSummary, err := llmClient.Summarize(formatted)
			if err != nil {
				log.Printf("LLM summary failed: %v", err)
				fmt.Print(de.FormatDiff(d, useColor))
			} else {
				fmt.Printf("\n  LLM Summary for %s:\n  %s\n", d.URL, llmSummary)
			}
		}
	} else if len(summary.Diffs) > 0 {
		for _, raw := range summary.Diffs {
			d, ok := raw.(diff.DiffResult)
			if !ok {
				continue
			}
			fmt.Print(de.FormatDiff(d, useColor))
		}
	}

	hasFailures := summary.Mismatches > 0 || summary.Unrecorded > 0
	schemaErrors := 0

	// if --spec was provided, also validate snapshots against the schema
	if *specPath != "" {
		fmt.Println()
		fmt.Println("Schema Validation:")

		spec, err := schema.LoadSpec(*specPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "etch test: loading spec: %v\n", err)
			return 1
		}

		all, err := ss.LoadAll()
		if err != nil {
			fmt.Fprintf(os.Stderr, "etch test: loading snapshots: %v\n", err)
			return 1
		}

		schemaWarnings := 0
		for _, sf := range all {
			for _, entry := range sf {
				p, _ := parseURL(entry.URL)
				result := spec.ValidateResponse(entry.Method, p.Path, entry.StatusCode, entry.Body)
				if len(result.Violations) > 0 {
					fmt.Print(result.Format(useColor))
					for _, v := range result.Violations {
						if v.Severity == "error" {
							schemaErrors++
						} else {
							schemaWarnings++
						}
					}
				}
			}
		}

		fmt.Printf("  Schema: %d error(s), %d warning(s)\n", schemaErrors, schemaWarnings)
	}

	// final verdict
	fmt.Println()
	red, green, reset := "", "", ""
	if useColor {
		red = "\033[31m"
		green = "\033[32m"
		reset = "\033[0m"
	}

	passed := !hasFailures && schemaErrors == 0

	// JSON output for CI pipelines
	if *jsonOutput {
		type jsonDiff struct {
			URL    string `json:"url"`
			Method string `json:"method,omitempty"`
			Fields []struct {
				Path     string `json:"path"`
				Expected string `json:"expected"`
				Actual   string `json:"actual"`
			} `json:"fields"`
		}
		type jsonResult struct {
			Passed       bool       `json:"passed"`
			Mode         string     `json:"mode"`
			Total        int        `json:"total"`
			Matches      int        `json:"matches"`
			Mismatches   int        `json:"mismatches"`
			Unrecorded   int        `json:"unrecorded"`
			SchemaErrors int        `json:"schema_errors"`
			Diffs        []jsonDiff `json:"diffs,omitempty"`
		}
		jr := jsonResult{
			Passed:       passed,
			Mode:         *mode,
			Total:        summary.TotalRequests,
			Matches:      summary.Matches,
			Mismatches:   summary.Mismatches,
			Unrecorded:   summary.Unrecorded,
			SchemaErrors: schemaErrors,
		}
		for _, raw := range summary.Diffs {
			d, ok := raw.(diff.DiffResult)
			if !ok {
				continue
			}
			jd := jsonDiff{URL: d.URL}
			for _, f := range d.Fields {
				jd.Fields = append(jd.Fields, struct {
					Path     string `json:"path"`
					Expected string `json:"expected"`
					Actual   string `json:"actual"`
				}{Path: f.Path, Expected: f.Expected, Actual: f.Actual})
			}
			jr.Diffs = append(jr.Diffs, jd)
		}
		out, _ := json.MarshalIndent(jr, "", "  ")
		fmt.Println(string(out))
		if !passed {
			return 1
		}
		return 0
	}

	if !passed {
		fmt.Printf("%s✗ Unsafe changes detected%s\n", red, reset)
		if summary.Mismatches > 0 {
			fmt.Printf("  %d mismatch(es)\n", summary.Mismatches)
		}
		if summary.Unrecorded > 0 {
			fmt.Printf("  %d unrecorded request(s)\n", summary.Unrecorded)
		}
		if schemaErrors > 0 {
			fmt.Printf("  %d schema violation(s)\n", schemaErrors)
		}
		return 1
	}

	fmt.Printf("%s✓ No breaking changes detected%s\n", green, reset)
	return 0
}

// --- diff command ---

func runDiff(args []string) int {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Println("Usage: etch diff [flags]")
		fmt.Println()
		fmt.Println("Display pending diffs from the last test run.")
		fmt.Println()
		fmt.Println("Flags:")
		fs.PrintDefaults()
	}
	flags := parseGlobalFlags(fs, args)

	cfg, err := loadMergedConfig(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch diff: %v\n", err)
		return 1
	}

	ciMode := isCI(flags)
	useColor := !ciMode

	ss := snapshot.NewSnapshotStore(cfg.SnapDir)
	pendingDir := filepath.Join(cfg.SnapDir, ".pending")
	am := approval.NewApprovalManager(ss, pendingDir)

	ignoreRules, err := ignore.Load(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch diff: loading .etchignore: %v\n", err)
		return 1
	}
	de := diff.NewDiffEngineFull(ignoreRules, noise.NewNormalizer())

	pending, err := am.ListPending()
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch diff: %v\n", err)
		return 1
	}

	if len(pending) == 0 {
		fmt.Println("No pending diffs.")
		return 0
	}

	for _, p := range pending {
		// Look up the stored snapshot to compare against.
		stored, err := ss.Lookup(p.Host, p.RequestHash)
		if err != nil {
			log.Printf("etch diff: lookup %s: %v", p.RequestHash, err)
			continue
		}
		if stored == nil {
			fmt.Printf("  %s (no stored snapshot)\n", p.RequestHash)
			continue
		}

		result := de.Compare(stored, &p.Live)
		result.RequestHash = p.RequestHash
		result.URL = p.Live.URL
		fmt.Print(de.FormatDiff(result, useColor))
	}

	return 0
}

// --- approve command ---

func runApprove(args []string) int {
	fs := flag.NewFlagSet("approve", flag.ExitOnError)
	requestHash := fs.String("request", "", "Approve a single pending diff by request hash")
	pattern := fs.String("pattern", "", "Approve all diffs matching a field name pattern")
	dryRun := fs.Bool("dry-run", false, "Show what would be approved without actually approving")
	fs.Usage = func() {
		fmt.Println("Usage: etch approve [flags]")
		fmt.Println()
		fmt.Println("Approve pending snapshot changes. Without flags, approves all.")
		fmt.Println("Use --request <hash> to approve a single diff.")
		fmt.Println("Use --pattern <field> to approve all diffs matching a field name.")
		fmt.Println("Use --dry-run to preview without changing anything.")
		fmt.Println()
		fmt.Println("Flags:")
		fs.PrintDefaults()
	}
	flags := parseGlobalFlags(fs, args)

	cfg, err := loadMergedConfig(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch approve: %v\n", err)
		return 1
	}

	ss := snapshot.NewSnapshotStore(cfg.SnapDir)
	pendingDir := filepath.Join(cfg.SnapDir, ".pending")
	am := approval.NewApprovalManager(ss, pendingDir)

	// dry-run: just show what would be approved
	if *dryRun {
		pending, err := am.ListPending()
		if err != nil {
			fmt.Fprintf(os.Stderr, "etch approve: %v\n", err)
			return 1
		}
		if len(pending) == 0 {
			fmt.Println("No pending diffs to approve.")
			return 0
		}
		fmt.Printf("Would approve %d pending diff(s):\n", len(pending))
		for _, p := range pending {
			fmt.Printf("  %s %s (%s)\n", p.Live.Method, p.Live.URL, p.RequestHash[:12]+"...")
		}
		return 0
	}

	if *requestHash != "" {
		if err := am.ApproveOne(*requestHash); err != nil {
			fmt.Fprintf(os.Stderr, "etch approve: %v\n", err)
			return 1
		}
		fmt.Printf("Approved diff for request %s.\n", *requestHash)
		return 0
	}

	if *pattern != "" {
		count, err := am.ApproveByPattern(*pattern)
		if err != nil {
			fmt.Fprintf(os.Stderr, "etch approve: %v\n", err)
			return 1
		}
		if count == 0 {
			fmt.Printf("No pending diffs match pattern %q.\n", *pattern)
		} else {
			fmt.Printf("Approved %d diff(s) matching pattern %q.\n", count, *pattern)
		}
		return 0
	}

	hasPending, err := am.HasPendingDiffs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch approve: %v\n", err)
		return 1
	}
	if !hasPending {
		fmt.Println("No pending diffs to approve.")
		return 0
	}

	count, err := am.ApproveAll()
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch approve: %v\n", err)
		return 1
	}
	fmt.Printf("Approved %d pending diff(s).\n", count)
	return 0
}

// --- ca-cert command ---

func runCACert(args []string) int {
	fs := flag.NewFlagSet("ca-cert", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Println("Usage: etch ca-cert [flags]")
		fmt.Println()
		fmt.Println("Print the path to the Etch CA certificate.")
		fmt.Println()
		fmt.Println("Flags:")
		fs.PrintDefaults()
	}
	flags := parseGlobalFlags(fs, args)

	cfg, err := loadMergedConfig(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch ca-cert: %v\n", err)
		return 1
	}

	cam, err := ca.NewCAManager(cfg.CADir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch ca-cert: %v\n", err)
		return 1
	}

	fmt.Println(cam.CertPath())
	return 0
}

// --- openapi command ---

func runOpenAPI(args []string) int {
	fs := flag.NewFlagSet("openapi", flag.ExitOnError)
	output := fs.String("output", "", "Write OpenAPI spec to file instead of stdout")
	fs.Usage = func() {
		fmt.Println("Usage: etch openapi [flags]")
		fmt.Println()
		fmt.Println("Generate an OpenAPI 3.0 spec from recorded snapshots.")
		fmt.Println()
		fmt.Println("Flags:")
		fs.PrintDefaults()
	}
	flags := parseGlobalFlags(fs, args)

	cfg, err := loadMergedConfig(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch openapi: %v\n", err)
		return 1
	}

	ss := snapshot.NewSnapshotStore(cfg.SnapDir)
	gen := openapi.NewOpenAPIGenerator(ss)

	var w *os.File
	if *output != "" {
		w, err = os.Create(*output)
		if err != nil {
			fmt.Fprintf(os.Stderr, "etch openapi: %v\n", err)
			return 1
		}
		defer w.Close()
	} else {
		w = os.Stdout
	}

	if err := gen.WriteYAML(w); err != nil {
		fmt.Fprintf(os.Stderr, "etch openapi: %v\n", err)
		return 1
	}

	if *output != "" {
		fmt.Fprintf(os.Stderr, "OpenAPI spec written to %s\n", *output)
	}

	return 0
}

// --- mock command ---

func runMock(args []string) int {
	fs := flag.NewFlagSet("mock", flag.ExitOnError)
	port := fs.Int("port", 9090, "Mock server listen port")
	fs.Usage = func() {
		fmt.Println("Usage: etch mock [flags]")
		fmt.Println()
		fmt.Println("Serve recorded snapshots as a local mock HTTP server.")
		fmt.Println("Point your app at this instead of the real API.")
		fmt.Println()
		fmt.Println("Flags:")
		fs.PrintDefaults()
	}
	flags := parseGlobalFlags(fs, args)

	cfg, err := loadMergedConfig(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch mock: %v\n", err)
		return 1
	}

	// use --port from mock flags, not the global proxy port
	if isFlagSet(fs, "port") {
		cfg.Port = *port
	} else {
		cfg.Port = 9090
	}

	ss := snapshot.NewSnapshotStore(cfg.SnapDir)
	addr := fmt.Sprintf(":%d", cfg.Port)

	srv, err := mock.NewServer(ss, addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch mock: %v\n", err)
		return 1
	}

	if err := srv.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "etch mock: %v\n", err)
		return 1
	}

	return 0
}

// --- coverage command ---

func runCoverage(args []string) int {
	fs := flag.NewFlagSet("coverage", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Println("Usage: etch coverage [flags]")
		fmt.Println()
		fmt.Println("Show which API endpoints have recorded snapshots.")
		fmt.Println()
		fmt.Println("Flags:")
		fs.PrintDefaults()
	}
	flags := parseGlobalFlags(fs, args)

	cfg, err := loadMergedConfig(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch coverage: %v\n", err)
		return 1
	}

	ss := snapshot.NewSnapshotStore(cfg.SnapDir)
	report, err := coverage.Generate(ss)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch coverage: %v\n", err)
		return 1
	}

	fmt.Print(report.Format())
	return 0
}

// --- learn command ---

func runLearn(args []string) int {
	fs := flag.NewFlagSet("learn", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Println("Usage: etch learn [flags]")
		fmt.Println()
		fmt.Println("Analyze recorded snapshots and show learned field constraints.")
		fmt.Println("Detects types, required fields, and value patterns.")
		fmt.Println()
		fmt.Println("Flags:")
		fs.PrintDefaults()
	}
	flags := parseGlobalFlags(fs, args)

	cfg, err := loadMergedConfig(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch learn: %v\n", err)
		return 1
	}

	ss := snapshot.NewSnapshotStore(cfg.SnapDir)
	report, err := constraint.Learn(ss)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch learn: %v\n", err)
		return 1
	}

	fmt.Print(report.Format())
	return 0
}

// --- noise command ---

func runNoise(args []string) int {
	fs := flag.NewFlagSet("noise", flag.ExitOnError)
	writeIgnore := fs.Bool("write", false, "Write suggested rules to .etchignore")
	fs.Usage = func() {
		fmt.Println("Usage: etch noise [flags]")
		fmt.Println()
		fmt.Println("Analyze recorded snapshots and detect fields that are likely noisy")
		fmt.Println("(timestamps, UUIDs, request IDs, etc.). Suggests .etchignore rules.")
		fmt.Println()
		fmt.Println("Flags:")
		fs.PrintDefaults()
	}
	flags := parseGlobalFlags(fs, args)

	cfg, err := loadMergedConfig(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch noise: %v\n", err)
		return 1
	}

	ss := snapshot.NewSnapshotStore(cfg.SnapDir)
	result, err := noise.Detect(ss)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch noise: %v\n", err)
		return 1
	}

	fmt.Print(result.Format())

	if *writeIgnore && len(result.SuggestedIgnore) > 0 {
		content := result.FormatEtchignore()
		if err := os.WriteFile(".etchignore", []byte(content), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "etch noise: writing .etchignore: %v\n", err)
			return 1
		}
		fmt.Println("\nWrote .etchignore with suggested rules.")
	}

	return 0
}

// --- validate command ---

func runValidate(args []string) int {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	specPath := fs.String("spec", "", "Path to OpenAPI spec (YAML or JSON)")
	fs.Usage = func() {
		fmt.Println("Usage: etch validate --spec <path> [flags]")
		fmt.Println()
		fmt.Println("Validate recorded snapshots against an OpenAPI specification.")
		fmt.Println("Detects missing fields, type mismatches, and undocumented fields.")
		fmt.Println()
		fmt.Println("Flags:")
		fs.PrintDefaults()
	}
	flags := parseGlobalFlags(fs, args)

	if *specPath == "" {
		fmt.Fprintln(os.Stderr, "etch validate: --spec flag is required")
		return 1
	}

	cfg, err := loadMergedConfig(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch validate: %v\n", err)
		return 1
	}

	ciMode := isCI(flags)
	useColor := !ciMode

	spec, err := schema.LoadSpec(*specPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch validate: %v\n", err)
		return 1
	}

	ss := snapshot.NewSnapshotStore(cfg.SnapDir)
	all, err := ss.LoadAll()
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch validate: %v\n", err)
		return 1
	}

	if len(all) == 0 {
		fmt.Println("No snapshots found. Run 'etch record' first.")
		return 0
	}

	totalErrors := 0
	totalWarnings := 0
	totalEndpoints := 0

	for _, sf := range all {
		for _, entry := range sf {
			parsed, err := parseURL(entry.URL)
			if err != nil {
				continue
			}

			result := spec.ValidateResponse(entry.Method, parsed.Path, entry.StatusCode, entry.Body)
			totalEndpoints++

			if len(result.Violations) > 0 {
				fmt.Print(result.Format(useColor))
				for _, v := range result.Violations {
					if v.Severity == "error" {
						totalErrors++
					} else {
						totalWarnings++
					}
				}
			}
		}
	}

	fmt.Printf("\nValidated %d endpoint(s): %d error(s), %d warning(s)\n",
		totalEndpoints, totalErrors, totalWarnings)

	if totalErrors > 0 {
		return 1
	}
	return 0
}

func parseURL(rawURL string) (*struct{ Path string }, error) {
	// simple URL path extraction
	idx := strings.Index(rawURL, "://")
	if idx == -1 {
		return &struct{ Path string }{rawURL}, nil
	}
	rest := rawURL[idx+3:]
	slashIdx := strings.Index(rest, "/")
	if slashIdx == -1 {
		return &struct{ Path string }{"/"}, nil
	}
	path := rest[slashIdx:]
	// strip query string
	if qIdx := strings.Index(path, "?"); qIdx != -1 {
		path = path[:qIdx]
	}
	return &struct{ Path string }{path}, nil
}

// --- verify command ---

func runVerify(args []string) int {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	relationsPath := fs.String("relations", ".etch/relations.json", "Path to relations file")
	fs.Usage = func() {
		fmt.Println("Usage: etch verify [flags]")
		fmt.Println()
		fmt.Println("Run metamorphic relation checks against recorded snapshots.")
		fmt.Println("Relations are defined in .etch/relations.json.")
		fmt.Println()
		fmt.Println("Relation types:")
		fmt.Println("  equivalence  - two endpoints return the same value for a field")
		fmt.Println("  subset       - filtered results are a subset of unfiltered")
		fmt.Println("  consistency  - array length matches a count field")
		fmt.Println("  idempotent   - same request produces same result")
		fmt.Println()
		fmt.Println("Flags:")
		fs.PrintDefaults()
	}
	flags := parseGlobalFlags(fs, args)

	cfg, err := loadMergedConfig(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch verify: %v\n", err)
		return 1
	}

	ciMode := isCI(flags)
	useColor := !ciMode

	// load relations
	data, err := os.ReadFile(*relationsPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No relations file found. Create .etch/relations.json to define consistency checks.")
			fmt.Println()
			fmt.Println("Example:")
			fmt.Println(`  {`)
			fmt.Println(`    "relations": [`)
			fmt.Println(`      {`)
			fmt.Println(`        "name": "user count matches list",`)
			fmt.Println(`        "type": "consistency",`)
			fmt.Println(`        "endpoints": ["GET /users", "GET /users/count"],`)
			fmt.Println(`        "field": "users",`)
			fmt.Println(`        "count_field": "count"`)
			fmt.Println(`      }`)
			fmt.Println(`    ]`)
			fmt.Println(`  }`)
			return 0
		}
		fmt.Fprintf(os.Stderr, "etch verify: %v\n", err)
		return 1
	}

	var relFile struct {
		Relations []verify.Relation `json:"relations"`
	}
	if err := json.Unmarshal(data, &relFile); err != nil {
		fmt.Fprintf(os.Stderr, "etch verify: parsing relations file: %v\n", err)
		return 1
	}

	ss := snapshot.NewSnapshotStore(cfg.SnapDir)
	report, err := verify.RunAll(ss, relFile.Relations)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch verify: %v\n", err)
		return 1
	}

	fmt.Print(report.Format(useColor))

	if report.Failed > 0 {
		return 1
	}
	return 0
}

// --- report command ---

func runReport(args []string) int {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	output := fs.String("output", report.DefaultPath(), "Output HTML file path")
	fs.Usage = func() {
		fmt.Println("Usage: etch report [flags]")
		fmt.Println()
		fmt.Println("Generate a self-contained HTML diff report from pending diffs.")
		fmt.Println("Share it with your team without everyone needing the CLI.")
		fmt.Println()
		fmt.Println("Flags:")
		fs.PrintDefaults()
	}
	flags := parseGlobalFlags(fs, args)

	cfg, err := loadMergedConfig(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch report: %v\n", err)
		return 1
	}

	ss := snapshot.NewSnapshotStore(cfg.SnapDir)
	pendingDir := filepath.Join(cfg.SnapDir, ".pending")
	am := approval.NewApprovalManager(ss, pendingDir)

	ignoreRules, _ := ignore.Load(".")
	normalizer := noise.NewNormalizer()
	de := diff.NewDiffEngineFull(ignoreRules, normalizer)

	pending, err := am.ListPending()
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch report: %v\n", err)
		return 1
	}

	if len(pending) == 0 {
		fmt.Println("No pending diffs. Run 'etch test' first.")
		return 0
	}

	var results []diff.DiffResult
	for _, p := range pending {
		stored, err := ss.Lookup(p.Host, p.RequestHash)
		if err != nil || stored == nil {
			continue
		}
		result := de.Compare(stored, &p.Live)
		result.RequestHash = p.RequestHash
		result.URL = p.Live.URL
		if !result.Matched {
			results = append(results, result)
		}
	}

	if err := report.WriteReport(results, *output); err != nil {
		fmt.Fprintf(os.Stderr, "etch report: %v\n", err)
		return 1
	}

	return 0
}

// --- watch command ---

func runWatch(args []string) int {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	interval := fs.Duration("interval", 5*time.Second, "Check interval")
	injectHeader := fs.String("inject-header", "", "Inject a header into replayed requests (e.g. 'Authorization: Bearer $TOKEN')")
	fs.Usage = func() {
		fmt.Println("Usage: etch watch [flags]")
		fmt.Println()
		fmt.Println("Continuously monitor snapshot files for changes.")
		fmt.Println("Alerts when any recorded response differs from baseline.")
		fmt.Println()
		fmt.Println("Flags:")
		fs.PrintDefaults()
	}
	flags := parseGlobalFlags(fs, args)

	cfg, err := loadMergedConfig(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch watch: %v\n", err)
		return 1
	}

	ss := snapshot.NewSnapshotStore(cfg.SnapDir)
	ignoreRules, _ := ignore.Load(".")
	normalizer := noise.NewNormalizer()
	de := diff.NewDiffEngineFull(ignoreRules, normalizer)

	w := watch.NewWatcher(ss, de, *interval)

	// parse and inject headers if provided
	if *injectHeader != "" {
		parts := strings.SplitN(*injectHeader, ":", 2)
		if len(parts) == 2 {
			w.InjectHeaders = map[string]string{
				strings.TrimSpace(parts[0]): os.ExpandEnv(strings.TrimSpace(parts[1])),
			}
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := w.Watch(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "etch watch: %v\n", err)
		return 1
	}

	return 0
}

// --- history command ---

func runHistory(args []string) int {
	fs := flag.NewFlagSet("history", flag.ExitOnError)
	endpoint := fs.String("endpoint", "", "Filter by endpoint (e.g. 'GET /users')")
	fs.Usage = func() {
		fmt.Println("Usage: etch history [flags]")
		fmt.Println()
		fmt.Println("Show the schema evolution timeline - when fields were added,")
		fmt.Println("removed, or changed type. History is recorded on each 'etch approve'.")
		fmt.Println()
		fmt.Println("Flags:")
		fs.PrintDefaults()
	}
	flags := parseGlobalFlags(fs, args)

	cfg, err := loadMergedConfig(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch history: %v\n", err)
		return 1
	}

	ciMode := isCI(flags)
	useColor := !ciMode

	store := history.NewStore(cfg.SnapDir)
	entries, err := store.ForEndpoint(*endpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch history: %v\n", err)
		return 1
	}

	fmt.Print(history.Format(entries, useColor))
	return 0
}

// --- init command ---

func runInit(args []string) int {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Println("Usage: etch init")
		fmt.Println()
		fmt.Println("Initialize etch in the current directory. Creates the config")
		fmt.Println("directory, a default config file, generates the CA certificate,")
		fmt.Println("and prints next steps.")
	}
	_ = fs.Parse(args)

	// 1. Create .etch directory
	if err := os.MkdirAll(".etch/snapshots", 0755); err != nil {
		fmt.Fprintf(os.Stderr, "etch init: creating directories: %v\n", err)
		return 1
	}

	// 2. Write default config if it doesn't exist
	cfgPath := config.DefaultConfigPath
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		cfg := config.DefaultConfig()
		if err := config.WriteConfig(cfgPath, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "etch init: writing config: %v\n", err)
			return 1
		}
		fmt.Printf("Created %s\n", cfgPath)
	} else {
		fmt.Printf("Config already exists at %s (skipped)\n", cfgPath)
	}

	// 3. Create .etchignore if it doesn't exist
	ignorePath := ".etchignore"
	if _, err := os.Stat(ignorePath); os.IsNotExist(err) {
		defaultIgnore := "# Fields to ignore during comparison\n" +
			"# One path per line. Wildcards supported (e.g. body.meta.*)\n" +
			"#\n" +
			"# headers.Date\n" +
			"# headers.X-Request-Id\n" +
			"# body.created_at\n" +
			"# body.updated_at\n"
		if err := os.WriteFile(ignorePath, []byte(defaultIgnore), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "etch init: writing .etchignore: %v\n", err)
			return 1
		}
		fmt.Printf("Created %s\n", ignorePath)
	}

	// 4. Generate CA certificate
	cfg := config.DefaultConfig()
	cam, err := ca.NewCAManager(cfg.CADir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch init: generating CA cert: %v\n", err)
		return 1
	}
	fmt.Printf("CA certificate ready at %s\n", cam.CertPath())

	// 5. Print next steps
	fmt.Println()
	fmt.Println("Etch initialized. Next steps:")
	fmt.Println()
	fmt.Println("  1. Trust the CA certificate (for HTTPS interception):")
	fmt.Printf("     %s\n", cam.CertPath())
	fmt.Println()
	fmt.Println("  2. Record your first baseline:")
	fmt.Println("     etch record --port 8080")
	fmt.Println("     # In another terminal:")
	fmt.Println("     http_proxy=http://localhost:8080 <your-app-or-curl>")
	fmt.Println()
	fmt.Println("  3. Auto-suppress noisy fields:")
	fmt.Println("     etch noise --write")
	fmt.Println()
	fmt.Println("  4. Test for changes:")
	fmt.Println("     etch test --port 8080")
	fmt.Println()
	fmt.Println("  5. Add .etch/ to version control (snapshots are deterministic JSON)")

	return 0
}

// --- audit command ---

func runAudit(args []string) int {
	fs := flag.NewFlagSet("audit", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Println("Usage: etch audit [flags]")
		fmt.Println()
		fmt.Println("Scan recorded snapshots for sensitive data exposure.")
		fmt.Println("Detects PII (emails, SSNs, credit cards, phone numbers),")
		fmt.Println("secrets (API keys, bearer tokens, AWS keys), and sensitive")
		fmt.Println("data leaked in URL query parameters.")
		fmt.Println()
		fmt.Println("Flags:")
		fs.PrintDefaults()
	}
	flags := parseGlobalFlags(fs, args)

	cfg, err := loadMergedConfig(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch audit: %v\n", err)
		return 1
	}

	ciMode := isCI(flags)
	useColor := !ciMode

	ss := snapshot.NewSnapshotStore(cfg.SnapDir)
	report, err := audit.Scan(ss)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch audit: %v\n", err)
		return 1
	}

	fmt.Print(report.Format(useColor))

	if report.HasCritical() {
		return 1
	}
	return 0
}

// --- prune command ---

func runPrune(args []string) int {
	fs := flag.NewFlagSet("prune", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "Show what would be pruned without deleting")
	fs.Usage = func() {
		fmt.Println("Usage: etch prune [flags]")
		fmt.Println()
		fmt.Println("Remove stale snapshot entries that were not matched during")
		fmt.Println("the last test run. This keeps the snapshot directory clean")
		fmt.Println("and prevents it from growing indefinitely.")
		fmt.Println()
		fmt.Println("How it works:")
		fmt.Println("  1. Run 'etch test' to record which snapshots were hit")
		fmt.Println("  2. Run 'etch prune' to remove entries that weren't matched")
		fmt.Println()
		fmt.Println("Flags:")
		fs.PrintDefaults()
	}
	flags := parseGlobalFlags(fs, args)

	cfg, err := loadMergedConfig(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch prune: %v\n", err)
		return 1
	}

	ss := snapshot.NewSnapshotStore(cfg.SnapDir)

	// Load the hit set from the last test run
	hitSetPath := filepath.Join(cfg.SnapDir, ".last-test-hits.json")
	data, err := os.ReadFile(hitSetPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "etch prune: no test run data found.")
			fmt.Fprintln(os.Stderr, "Run 'etch test' first, then 'etch prune' to remove stale snapshots.")
			return 1
		}
		fmt.Fprintf(os.Stderr, "etch prune: reading hit set: %v\n", err)
		return 1
	}

	var hitHashes []string
	if err := json.Unmarshal(data, &hitHashes); err != nil {
		fmt.Fprintf(os.Stderr, "etch prune: parsing hit set: %v\n", err)
		return 1
	}

	hitSet := make(map[string]bool, len(hitHashes))
	for _, h := range hitHashes {
		hitSet[h] = true
	}

	result, err := ss.Prune(hitSet, *dryRun)
	if err != nil {
		fmt.Fprintf(os.Stderr, "etch prune: %v\n", err)
		return 1
	}

	removed := result.TotalBefore - result.TotalAfter
	if removed == 0 {
		fmt.Println("No stale snapshots found. All entries were hit during the last test run.")
		return 0
	}

	if *dryRun {
		fmt.Printf("Would remove %d stale snapshot(s) (%d → %d entries)\n",
			removed, result.TotalBefore, result.TotalAfter)
		for _, h := range result.RemovedHashes {
			fmt.Printf("  - %s\n", h[:16]+"...")
		}
	} else {
		fmt.Printf("Pruned %d stale snapshot(s) (%d → %d entries)\n",
			removed, result.TotalBefore, result.TotalAfter)
		if len(result.RemovedHosts) > 0 {
			fmt.Printf("Removed %d empty snapshot file(s)\n", len(result.RemovedHosts))
		}
	}

	return 0
}
