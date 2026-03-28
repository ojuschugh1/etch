package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"pgregory.net/rapid"
)

// Make sure we can write a config to disk and read it back without losing anything.
// This catches serialization bugs, field omission, type coercion issues, etc.
func TestConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(rt *rapid.T) {
		cfg := genConfig(rt)
		path := filepath.Join(dir, "config.json")

		if err := WriteConfig(path, cfg); err != nil {
			rt.Fatalf("write failed: %v", err)
		}

		loaded, err := LoadConfig(path)
		if err != nil {
			rt.Fatalf("load failed: %v", err)
		}

		if !reflect.DeepEqual(cfg, loaded) {
			rt.Fatalf("config changed after round-trip:\nbefore: %+v\nafter:  %+v", cfg, loaded)
		}
	})
}

func genConfig(t *rapid.T) *Config {
	cfg := &Config{
		Port:            rapid.IntRange(1, 65535).Draw(t, "port"),
		SnapDir:         rapid.StringMatching(`[a-zA-Z0-9._/\-]{1,50}`).Draw(t, "snap_dir"),
		CADir:           rapid.StringMatching(`[a-zA-Z0-9._/\-]{1,50}`).Draw(t, "ca_dir"),
		ExcludedHeaders: genStringSlice(t, "excluded_headers"),
		IncludedHeaders: genStringSlice(t, "included_headers"),
		MaxBodySize:     int64(rapid.IntRange(0, 100*1024*1024).Draw(t, "max_body_size")),
	}

	if rapid.Bool().Draw(t, "has_llm") {
		cfg.LLM = &LLMConfig{
			Endpoint: rapid.StringMatching(`https?://[a-z0-9.]{1,30}/[a-z0-9/]{0,30}`).Draw(t, "llm_endpoint"),
			APIKey:   rapid.StringMatching(`[a-zA-Z0-9]{0,40}`).Draw(t, "llm_api_key"),
			Model:    rapid.StringMatching(`[a-zA-Z0-9\-]{1,20}`).Draw(t, "llm_model"),
		}
	}

	return cfg
}

func genStringSlice(t *rapid.T, label string) []string {
	return rapid.SliceOf(
		rapid.StringMatching(`[A-Za-z][A-Za-z0-9\-]{0,29}`),
	).Draw(t, label)
}

// CLI flags should always win over whatever's in the config file.
// Generate random configs + random flag values and check that the flag
// values end up in the merged result while everything else stays put.
func TestCLIFlagsOverrideConfig(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		cfg := genConfig(rt)

		flagPort := rapid.IntRange(1, 65535).Draw(rt, "flag_port")
		flagSnapDir := rapid.StringMatching(`[a-zA-Z0-9._/\-]{1,50}`).Draw(rt, "flag_snap_dir")

		flags := CLIFlags{
			Port:    &flagPort,
			SnapDir: &flagSnapDir,
		}

		merged := cfg.MergeFlags(flags)

		if merged.Port != flagPort {
			rt.Fatalf("port should be %d from flag, got %d", flagPort, merged.Port)
		}
		if merged.SnapDir != flagSnapDir {
			rt.Fatalf("snap_dir should be %q from flag, got %q", flagSnapDir, merged.SnapDir)
		}

		// everything else should be untouched
		if merged.CADir != cfg.CADir {
			rt.Fatalf("ca_dir got clobbered: %q -> %q", cfg.CADir, merged.CADir)
		}
		if !reflect.DeepEqual(merged.ExcludedHeaders, cfg.ExcludedHeaders) {
			rt.Fatalf("excluded headers changed unexpectedly")
		}
		if !reflect.DeepEqual(merged.IncludedHeaders, cfg.IncludedHeaders) {
			rt.Fatalf("included headers changed unexpectedly")
		}
		if !reflect.DeepEqual(merged.LLM, cfg.LLM) {
			rt.Fatalf("LLM config changed unexpectedly")
		}
	})
}

// Feed garbage into LoadConfig and make sure it always errors out.
// We skip the rare case where random bytes happen to be valid JSON.
func TestInvalidJSONConfigErrors(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		raw := rapid.StringMatching(`[^\x00]{1,200}`).Draw(rt, "raw_content")

		if json.Valid([]byte(raw)) {
			rt.Skip("got lucky - random string is valid JSON")
		}

		dir := t.TempDir()
		path := filepath.Join(dir, "bad_config.json")
		if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
			rt.Fatalf("couldn't write temp file: %v", err)
		}

		_, err := LoadConfig(path)
		if err == nil {
			rt.Fatalf("expected an error for %q but got nil", raw)
		}
	})
}
