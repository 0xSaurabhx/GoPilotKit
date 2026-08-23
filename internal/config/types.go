// Package config implements YAML configuration parsing, validation,
// and environment variable expansion for GoPilotKit.
//
// The configuration schema is defined in intelligence.yaml and supports
// three deployment profiles: personal, team, and enterprise.
package config

import (
	"fmt"
	"time"
)

// Profile represents the deployment profile which controls feature availability.
type Profile string

const (
	ProfilePersonal  Profile = "personal"
	ProfileTeam      Profile = "team"
	ProfileEnterprise Profile = "enterprise"
)

// ValidProfiles lists all accepted profile values.
var ValidProfiles = map[Profile]bool{
	ProfilePersonal:  true,
	ProfileTeam:      true,
	ProfileEnterprise: true,
}

// IntelligenceConfig is the top-level configuration struct.
type IntelligenceConfig struct {
	Version      string            `yaml:"version"`
	Profile      Profile           `yaml:"profile"`
	Server       ServerConfig      `yaml:"server"`
	Storage      StorageConfig     `yaml:"storage"`
	Telemetry    TelemetryConfig   `yaml:"telemetry"`
	Learning     LearningConfig    `yaml:"learning"`
	Analytics    AnalyticsConfig   `yaml:"analytics"`
	Channels     ChannelsConfig    `yaml:"channels"`
	Enterprise   EnterpriseConfig  `yaml:"enterprise"`
	Providers    []ProviderConfig  `yaml:"providers"`
	Agents       []AgentConfig     `yaml:"agents"`
	Tools        []ToolConfig      `yaml:"tools"`
}

// ServerConfig defines the HTTP/gRPC server settings.
type ServerConfig struct {
	Port              int      `yaml:"port"`
	Transport         []string `yaml:"transport"`
	CORSAllowedOrigins []string `yaml:"cors_allowed_origins"`
}

// StorageConfig defines the persistence layer settings.
type StorageConfig struct {
	Driver        string          `yaml:"driver"`
	ConnectionURI string          `yaml:"connection_uri"`
	VectorStore   VectorStoreConfig `yaml:"vector_store"`
}

// VectorStoreConfig defines the vector store settings.
type VectorStoreConfig struct {
	Driver    string `yaml:"driver"`
	Dimension int    `yaml:"dimension"`
}

// TelemetryConfig defines observability settings.
type TelemetryConfig struct {
	Provider string `yaml:"provider"`
	Endpoint string `yaml:"endpoint"`
	LogLevel string `yaml:"log_level"`
}

// LearningConfig defines the autonomous self-improvement engine.
type LearningConfig struct {
	Enabled      bool                    `yaml:"enabled"`
	Containers   []LearningContainerConfig `yaml:"containers"`
	Capabilities LearningCapabilitiesConfig `yaml:"capabilities"`
	Evaluation   EvaluationConfig         `yaml:"evaluation"`
	Export       ExportConfig             `yaml:"export"`
}

// LearningContainerConfig defines scoping for learning.
type LearningContainerConfig struct {
	Scope               string  `yaml:"scope"`
	MinInteractions     int     `yaml:"min_interactions"`
	ImprovementThreshold float64 `yaml:"improvement_threshold"`
}

// LearningCapabilitiesConfig defines what the system learns autonomously.
type LearningCapabilitiesConfig struct {
	PromptEvolution   bool `yaml:"prompt_evolution"`
	FewShotCuration   bool `yaml:"few_shot_curation"`
	SkillRecording    bool `yaml:"skill_recording"`
	MemoryOptimization bool `yaml:"memory_optimization"`
}

// EvaluationConfig defines the automated evaluation pipeline.
type EvaluationConfig struct {
	LLMAsJudge        bool    `yaml:"llm_as_judge"`
	RegressionTesting bool    `yaml:"regression_testing"`
	ABTesting         bool    `yaml:"ab_testing"`
	QualityThreshold  float64 `yaml:"quality_threshold"`
}

// ExportConfig defines dataset export settings.
type ExportConfig struct {
	Enabled bool   `yaml:"enabled"`
	Format  string `yaml:"format"`
}

// AnalyticsConfig defines product analytics and insights.
type AnalyticsConfig struct {
	Enabled     bool `yaml:"enabled"`
	Dashboard   bool `yaml:"dashboard"`
	OTLPExport  bool `yaml:"otlp_export"`
	CostTracking bool `yaml:"cost_tracking"`
}

// ChannelsConfig defines multi-surface adapters.
type ChannelsConfig struct {
	Web    bool `yaml:"web"`
	Mobile bool `yaml:"mobile"`
	Slack  bool `yaml:"slack"`
	Teams  bool `yaml:"teams"`
}

// EnterpriseConfig defines enterprise security features.
type EnterpriseConfig struct {
	SSO          SSOConfig          `yaml:"sso"`
	RBAC         RBACConfig         `yaml:"rbac"`
	AuditLog     AuditLogConfig     `yaml:"audit_log"`
	DataRetention DataRetentionConfig `yaml:"data_retention"`
}

// SSOConfig defines single sign-on settings.
type SSOConfig struct {
	Enabled bool   `yaml:"enabled"`
	Provider string `yaml:"provider"`
	Issuer  string `yaml:"issuer"`
}

// RBACConfig defines role-based access control.
type RBACConfig struct {
	Enabled bool     `yaml:"enabled"`
	Roles   []string `yaml:"roles"`
}

// AuditLogConfig defines audit logging.
type AuditLogConfig struct {
	Enabled bool `yaml:"enabled"`
}

// DataRetentionConfig defines data retention policies.
type DataRetentionConfig struct {
	Enabled bool `yaml:"enabled"`
	Days    int  `yaml:"days"`
}

// ProviderConfig defines an LLM provider.
type ProviderConfig struct {
	ID     string `yaml:"id"`
	Type   string `yaml:"type"`
	Model  string `yaml:"model"`
	APIKey string `yaml:"api_key"`
	BaseURL string `yaml:"base_url,omitempty"`
}

// ValidProviderTypes lists all accepted provider types.
var ValidProviderTypes = map[string]bool{
	"openai":    true,
	"anthropic": true,
	"ollama":    true,
	"google":    true,
	"mistral":   true,
	"cohere":    true,
}

// AgentConfig defines an agent configuration.
type AgentConfig struct {
	Name             string                    `yaml:"name"`
	Provider         string                    `yaml:"provider"`
	FallbackProvider string                    `yaml:"fallback_provider,omitempty"`
	SystemPrompt     string                    `yaml:"system_prompt"`
	Memory           MemoryConfig              `yaml:"memory"`
	Tools            []string                  `yaml:"tools"`
}

// MemoryConfig defines agent memory settings.
type MemoryConfig struct {
	SlidingWindow  int  `yaml:"sliding_window"`
	SemanticRecall bool `yaml:"semantic_recall"`
}

// ToolConfig defines a tool configuration.
type ToolConfig struct {
	ID         string   `yaml:"id"`
	Type       string   `yaml:"type"`
	BinaryPath string   `yaml:"binary_path,omitempty"`
	Transport  string   `yaml:"transport,omitempty"`
	Command    string   `yaml:"command,omitempty"`
	Args       []string `yaml:"args,omitempty"`
}

// ValidToolTypes lists all accepted tool types.
var ValidToolTypes = map[string]bool{
	"wasm": true,
	"mcp":  true,
}

// ValidStorageDrivers lists all accepted storage drivers.
var ValidStorageDrivers = map[string]bool{
	"sqlite":   true,
	"postgres": true,
}

// ValidVectorStoreDrivers lists all accepted vector store drivers.
var ValidVectorStoreDrivers = map[string]bool{
	"embedded": true,
	"qdrant":   true,
	"pgvector": true,
}

// ValidTransports lists all accepted transport types.
var ValidTransports = map[string]bool{
	"sse":  true,
	"grpc": true,
	"ws":   true,
}

// DefaultConfig returns a minimal personal-profile configuration.
func DefaultConfig() *IntelligenceConfig {
	return &IntelligenceConfig{
		Version: "v1alpha",
		Profile: ProfilePersonal,
		Server: ServerConfig{
			Port:              8080,
			Transport:         []string{"sse"},
			CORSAllowedOrigins: []string{"*"},
		},
		Storage: StorageConfig{
			Driver:        "sqlite",
			ConnectionURI: "./data/gopilotkit.db",
			VectorStore: VectorStoreConfig{
				Driver:    "embedded",
				Dimension: 1536,
			},
		},
		Telemetry: TelemetryConfig{
			Provider: "opentelemetry",
			LogLevel: "info",
		},
	}
}

// Validate performs comprehensive validation on the configuration.
func (c *IntelligenceConfig) Validate() error {
	var errs []error

	// Version
	if c.Version == "" {
		errs = append(errs, fmt.Errorf("version is required"))
	}

	// Profile
	if c.Profile == "" {
		errs = append(errs, fmt.Errorf("profile is required (personal, team, enterprise)"))
	} else if !ValidProfiles[c.Profile] {
		errs = append(errs, fmt.Errorf("profile %q is not valid; must be one of: personal, team, enterprise", c.Profile))
	}

	// Server
	if err := c.Server.validate(); err != nil {
		errs = append(errs, fmt.Errorf("server: %w", err))
	}

	// Storage
	if err := c.Storage.validate(); err != nil {
		errs = append(errs, fmt.Errorf("storage: %w", err))
	}

	// Providers
	if len(c.Providers) == 0 {
		errs = append(errs, fmt.Errorf("at least one provider is required"))
	}
	providerIDs := make(map[string]bool)
	for i, p := range c.Providers {
		if err := p.validate(i); err != nil {
			errs = append(errs, err)
		}
		if providerIDs[p.ID] {
			errs = append(errs, fmt.Errorf("provider %q is duplicated", p.ID))
		}
		providerIDs[p.ID] = true
	}

	// Agents
	for i, a := range c.Agents {
		if err := a.validate(i, providerIDs); err != nil {
			errs = append(errs, err)
		}
	}

	// Tools
	toolIDs := make(map[string]bool)
	for i, t := range c.Tools {
		if err := t.validate(i); err != nil {
			errs = append(errs, err)
		}
		if toolIDs[t.ID] {
			errs = append(errs, fmt.Errorf("tool %q is duplicated", t.ID))
		}
		toolIDs[t.ID] = true
	}

	// Cross-reference: agent tools must exist in tools list
	for _, a := range c.Agents {
		for _, toolRef := range a.Tools {
			if !toolIDs[toolRef] {
				errs = append(errs, fmt.Errorf("agent %q references tool %q which does not exist in tools list", a.Name, toolRef))
			}
		}
	}

	if len(errs) > 0 {
		return &ConfigErrors{Errors: errs}
	}
	return nil
}

func (s *ServerConfig) validate() error {
	if s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", s.Port)
	}
	if len(s.Transport) == 0 {
		return fmt.Errorf("at least one transport is required")
	}
	for _, t := range s.Transport {
		if !ValidTransports[t] {
			return fmt.Errorf("transport %q is not valid; must be one of: sse, grpc, ws", t)
		}
	}
	return nil
}

func (s *StorageConfig) validate() error {
	if !ValidStorageDrivers[s.Driver] {
		return fmt.Errorf("driver %q is not valid; must be one of: sqlite, postgres", s.Driver)
	}
	if s.ConnectionURI == "" {
		return fmt.Errorf("connection_uri is required")
	}
	if !ValidVectorStoreDrivers[s.VectorStore.Driver] {
		return fmt.Errorf("vector_store.driver %q is not valid; must be one of: embedded, qdrant, pgvector", s.VectorStore.Driver)
	}
	if s.VectorStore.Dimension < 1 {
		return fmt.Errorf("vector_store.dimension must be >= 1, got %d", s.VectorStore.Dimension)
	}
	return nil
}

func (p *ProviderConfig) validate(index int) error {
	prefix := fmt.Errorf("providers[%d]", index)
	if p.ID == "" {
		return fmt.Errorf("%w: id is required", prefix)
	}
	if p.Type == "" {
		return fmt.Errorf("%w: type is required", prefix)
	}
	if !ValidProviderTypes[p.Type] {
		return fmt.Errorf("%w: type %q is not valid", prefix, p.Type)
	}
	if p.Model == "" {
		return fmt.Errorf("%w: model is required", prefix)
	}
	if p.APIKey == "" {
		return fmt.Errorf("%w: api_key is required (use ${ENV_VAR} syntax)", prefix)
	}
	return nil
}

func (a *AgentConfig) validate(index int, providerIDs map[string]bool) error {
	prefix := fmt.Errorf("agents[%d]", index)
	if a.Name == "" {
		return fmt.Errorf("%w: name is required", prefix)
	}
	if a.Provider == "" {
		return fmt.Errorf("%w: provider is required", prefix)
	}
	if !providerIDs[a.Provider] {
		return fmt.Errorf("%w: provider %q not found in providers list", prefix, a.Provider)
	}
	if a.FallbackProvider != "" && !providerIDs[a.FallbackProvider] {
		return fmt.Errorf("%w: fallback_provider %q not found in providers list", prefix, a.FallbackProvider)
	}
	if a.SystemPrompt == "" {
		return fmt.Errorf("%w: system_prompt is required", prefix)
	}
	return nil
}

func (t *ToolConfig) validate(index int) error {
	prefix := fmt.Errorf("tools[%d]", index)
	if t.ID == "" {
		return fmt.Errorf("%w: id is required", prefix)
	}
	if !ValidToolTypes[t.Type] {
		return fmt.Errorf("%w: type %q is not valid; must be one of: wasm, mcp", prefix, t.Type)
	}
	if t.Type == "wasm" && t.BinaryPath == "" {
		return fmt.Errorf("%w: binary_path is required for wasm tools", prefix)
	}
	if t.Type == "mcp" {
		if t.Transport == "" {
			return fmt.Errorf("%w: transport is required for mcp tools", prefix)
		}
		if t.Transport != "stdio" && t.Transport != "sse" && t.Transport != "grpc" {
			return fmt.Errorf("%w: transport %q is not valid for mcp tools; must be one of: stdio, sse, grpc", prefix, t.Transport)
		}
		if t.Command == "" {
			return fmt.Errorf("%w: command is required for mcp tools", prefix)
		}
	}
	return nil
}

// ConfigErrors holds multiple validation errors.
type ConfigErrors struct {
	Errors []error
}

func (e *ConfigErrors) Error() string {
	msg := "config validation failed:"
	for _, err := range e.Errors {
		msg += "\n  - " + err.Error()
	}
	return msg
}

// Unwrap returns the individual errors for errors.Is/errors.As.
func (e *ConfigErrors) Unwrap() []error {
	return e.Errors
}

// TimeoutOrDefault returns the given duration or a default if zero.
func TimeoutOrDefault(d, defaultVal time.Duration) time.Duration {
	if d == 0 {
		return defaultVal
	}
	return d
}
