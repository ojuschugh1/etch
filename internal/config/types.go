package config

// Config holds the application configuration.
type Config struct {
	Port            int              `json:"port"`
	SnapDir         string           `json:"snap_dir"`
	CADir           string           `json:"ca_dir"`
	ExcludedHeaders []string         `json:"excluded_headers"`
	IncludedHeaders []string         `json:"included_headers"`
	LLM             *LLMConfig       `json:"llm,omitempty"`
	Normalizers     []NormalizerRule `json:"normalizers,omitempty"`
	Env             map[string]string `json:"env,omitempty"`
	Hooks           *HooksConfig      `json:"hooks,omitempty"`
	MaxBodySize     int64            `json:"max_body_size"` // max response body size in bytes (0 = unlimited, default 50MB)
}

// HooksConfig defines commands to run before/after etch operations.
type HooksConfig struct {
	BeforeTest  string `json:"before_test,omitempty"`
	AfterRecord string `json:"after_record,omitempty"`
	BeforeRecord string `json:"before_record,omitempty"`
}

// LLMConfig holds the BYOK LLM integration settings.
type LLMConfig struct {
	Endpoint string `json:"endpoint"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
}

// CLIFlags holds parsed command-line flags. Pointer fields distinguish "not set" from zero.
type CLIFlags struct {
	Port       *int
	SnapDir    *string
	ConfigPath *string
	CI         *bool
}

// NormalizerRule is a custom regex-based value normalizer in config.
type NormalizerRule struct {
	Pattern string `json:"pattern"` // regex pattern to match
	Replace string `json:"replace"` // replacement string, e.g. "<order-id>"
}
