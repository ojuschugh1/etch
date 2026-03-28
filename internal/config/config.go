package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// DefaultConfigPath is the default location for the config file.
const DefaultConfigPath = ".etch/config.json"

// DefaultConfig returns a Config with sensible default values.
func DefaultConfig() *Config {
	return &Config{
		Port:    8080,
		SnapDir: ".etch/snapshots",
		CADir:   "~/.etch",
		ExcludedHeaders: []string{
			"Date",
			"Authorization",
			"X-Request-Id",
			"X-Amzn-Trace-Id",
			"X-Amzn-RequestId",
		},
		IncludedHeaders: []string{},
		LLM:             nil,
		MaxBodySize:     50 * 1024 * 1024, // 50MB default
	}
}

// LoadConfig loads config from path. Returns defaults if path is the default and doesn't exist.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && path == DefaultConfigPath {
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %s: %w", path, err)
	}
	return cfg, nil
}

// WriteConfig writes cfg to path as pretty-printed JSON.
func WriteConfig(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

// MergeFlags applies CLI flag overrides on top of the config. Returns a new Config.
func (c *Config) MergeFlags(flags CLIFlags) *Config {
	merged := *c

	// Deep-copy slices so the original is not shared.
	merged.ExcludedHeaders = make([]string, len(c.ExcludedHeaders))
	copy(merged.ExcludedHeaders, c.ExcludedHeaders)
	merged.IncludedHeaders = make([]string, len(c.IncludedHeaders))
	copy(merged.IncludedHeaders, c.IncludedHeaders)

	if c.LLM != nil {
		llmCopy := *c.LLM
		merged.LLM = &llmCopy
	}

	if flags.Port != nil {
		merged.Port = *flags.Port
	}
	if flags.SnapDir != nil {
		merged.SnapDir = *flags.SnapDir
	}

	return &merged
}
