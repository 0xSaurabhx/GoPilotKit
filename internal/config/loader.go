package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// DefaultConfigPath is the default configuration file path.
const DefaultConfigPath = "intelligence.yaml"

// Load reads a YAML configuration file, expands environment variables,
// unmarshals into IntelligenceConfig, and validates the result.
//
// Returns a detailed error if the file cannot be read, parsed, or validated.
func Load(path string) (*IntelligenceConfig, error) {
	// Read file
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %q: %w", path, err)
	}

	// Expand environment variables (${VAR} and ${VAR|default} patterns)
	expanded := ExpandEnvVars(raw)

	// Unmarshal YAML
	var cfg IntelligenceConfig
	if err := yaml.Unmarshal(expanded, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %q: %w", path, err)
	}

	// Apply defaults for zero-value fields
	applyDefaults(&cfg)

	// Validate
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config file %q: %w", path, err)
	}

	return &cfg, nil
}

// LoadBytes parses configuration from raw bytes (useful for testing).
func LoadBytes(data []byte) (*IntelligenceConfig, error) {
	expanded := ExpandEnvVars(data)

	var cfg IntelligenceConfig
	if err := yaml.Unmarshal(expanded, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	applyDefaults(&cfg)

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &cfg, nil
}

// LoadRaw reads a YAML file and returns the parsed config without validation.
// Useful for inspection and debugging.
func LoadRaw(path string) (*IntelligenceConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %q: %w", path, err)
	}

	expanded := ExpandEnvVars(raw)

	var cfg IntelligenceConfig
	if err := yaml.Unmarshal(expanded, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %q: %w", path, err)
	}

	applyDefaults(&cfg)

	return &cfg, nil
}

// applyDefaults sets default values for fields that have zero values.
func applyDefaults(cfg *IntelligenceConfig) {
	if cfg.Version == "" {
		cfg.Version = "v1alpha"
	}
	if cfg.Profile == "" {
		cfg.Profile = ProfilePersonal
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if len(cfg.Server.Transport) == 0 {
		cfg.Server.Transport = []string{"sse"}
	}
	if len(cfg.Server.CORSAllowedOrigins) == 0 {
		cfg.Server.CORSAllowedOrigins = []string{"*"}
	}
	if cfg.Storage.Driver == "" {
		cfg.Storage.Driver = "sqlite"
	}
	if cfg.Storage.ConnectionURI == "" {
		cfg.Storage.ConnectionURI = "./data/gopilotkit.db"
	}
	if cfg.Storage.VectorStore.Driver == "" {
		cfg.Storage.VectorStore.Driver = "embedded"
	}
	if cfg.Storage.VectorStore.Dimension == 0 {
		cfg.Storage.VectorStore.Dimension = 1536
	}
	if cfg.Telemetry.LogLevel == "" {
		cfg.Telemetry.LogLevel = "info"
	}
}

// FindConfigPath returns the config path from flag, env, or default.
//
// Priority: flag value > GOPilotKit_CONFIG env var > DefaultConfigPath.
func FindConfigPath(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if envPath := os.Getenv("GOPILOTKIT_CONFIG"); envPath != "" {
		return envPath
	}
	return DefaultConfigPath
}
