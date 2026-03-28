package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_MissingDefaultPath_ReturnsDefaults(t *testing.T) {
	_ = os.Remove(DefaultConfigPath)

	cfg, err := LoadConfig(DefaultConfigPath)
	if err != nil {
		t.Fatalf("shouldn't error on missing default config: %v", err)
	}

	want := DefaultConfig()
	if cfg.Port != want.Port {
		t.Errorf("port: got %d, want %d", cfg.Port, want.Port)
	}
	if cfg.SnapDir != want.SnapDir {
		t.Errorf("snap_dir: got %q, want %q", cfg.SnapDir, want.SnapDir)
	}
	if cfg.CADir != want.CADir {
		t.Errorf("ca_dir: got %q, want %q", cfg.CADir, want.CADir)
	}
	if len(cfg.ExcludedHeaders) != len(want.ExcludedHeaders) {
		t.Errorf("excluded headers count: got %d, want %d", len(cfg.ExcludedHeaders), len(want.ExcludedHeaders))
	}
	if len(cfg.IncludedHeaders) != len(want.IncludedHeaders) {
		t.Errorf("included headers count: got %d, want %d", len(cfg.IncludedHeaders), len(want.IncludedHeaders))
	}
	if cfg.LLM != nil {
		t.Errorf("LLM should be nil by default, got %+v", cfg.LLM)
	}
}

func TestLoadConfig_MissingExplicitPath_Errors(t *testing.T) {
	_, err := LoadConfig("/tmp/etch_nonexistent_config_12345.json")
	if err == nil {
		t.Fatal("should error when explicit config path doesn't exist")
	}
}

func TestLoadConfig_BadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")

	os.WriteFile(path, []byte(`{not valid json`), 0644)

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("should error on malformed JSON")
	}
}

// If the config file only sets port, everything else should fall back to defaults.
func TestLoadConfig_PartialConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "partial.json")
	os.WriteFile(path, []byte(`{"port": 9090}`), 0644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if cfg.Port != 9090 {
		t.Errorf("port: got %d, want 9090", cfg.Port)
	}
	want := DefaultConfig()
	if cfg.SnapDir != want.SnapDir {
		t.Errorf("snap_dir should default to %q, got %q", want.SnapDir, cfg.SnapDir)
	}
	if cfg.CADir != want.CADir {
		t.Errorf("ca_dir should default to %q, got %q", want.CADir, cfg.CADir)
	}
}

func TestMergeFlags_PortOnly(t *testing.T) {
	cfg := &Config{
		Port:            8080,
		SnapDir:         ".etch/snapshots",
		CADir:           "~/.etch",
		ExcludedHeaders: []string{"Date"},
		IncludedHeaders: []string{},
	}

	port := 3000
	merged := cfg.MergeFlags(CLIFlags{Port: &port})

	if merged.Port != 3000 {
		t.Errorf("port: got %d, want 3000", merged.Port)
	}
	if merged.SnapDir != cfg.SnapDir {
		t.Errorf("snap_dir shouldn't change: got %q", merged.SnapDir)
	}
}

func TestMergeFlags_SnapDirOnly(t *testing.T) {
	cfg := &Config{
		Port:            8080,
		SnapDir:         ".etch/snapshots",
		CADir:           "~/.etch",
		ExcludedHeaders: []string{"Date"},
		IncludedHeaders: []string{},
	}

	snapDir := "/custom/snaps"
	merged := cfg.MergeFlags(CLIFlags{SnapDir: &snapDir})

	if merged.SnapDir != "/custom/snaps" {
		t.Errorf("snap_dir: got %q, want /custom/snaps", merged.SnapDir)
	}
	if merged.Port != cfg.Port {
		t.Errorf("port shouldn't change: got %d", merged.Port)
	}
}

func TestMergeFlags_Both(t *testing.T) {
	cfg := DefaultConfig()

	port := 4444
	snapDir := "/other/dir"
	merged := cfg.MergeFlags(CLIFlags{Port: &port, SnapDir: &snapDir})

	if merged.Port != 4444 {
		t.Errorf("port: got %d, want 4444", merged.Port)
	}
	if merged.SnapDir != "/other/dir" {
		t.Errorf("snap_dir: got %q", merged.SnapDir)
	}
	if merged.CADir != cfg.CADir {
		t.Errorf("ca_dir shouldn't change")
	}
}

func TestMergeFlags_NoFlags(t *testing.T) {
	cfg := DefaultConfig()
	merged := cfg.MergeFlags(CLIFlags{})

	if merged.Port != cfg.Port || merged.SnapDir != cfg.SnapDir || merged.CADir != cfg.CADir {
		t.Error("empty flags shouldn't change anything")
	}
}

// MergeFlags should return a copy, not mutate the original.
func TestMergeFlags_NoMutation(t *testing.T) {
	cfg := DefaultConfig()
	origPort := cfg.Port

	port := 9999
	_ = cfg.MergeFlags(CLIFlags{Port: &port})

	if cfg.Port != origPort {
		t.Errorf("original config got mutated: port went from %d to %d", origPort, cfg.Port)
	}
}
