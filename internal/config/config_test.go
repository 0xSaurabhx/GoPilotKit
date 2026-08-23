package config

import (
	"os"
	"testing"
)

func TestLoadValidPersonalConfig(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "sk-test-12345")
	defer os.Unsetenv("OPENAI_API_KEY")

	cfg, err := Load("../../configs/personal.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Version != "v1alpha" {
		t.Errorf("expected version v1alpha, got %s", cfg.Version)
	}
	if cfg.Profile != ProfilePersonal {
		t.Errorf("expected profile personal, got %s", cfg.Profile)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Server.Port)
	}
	if cfg.Storage.Driver != "sqlite" {
		t.Errorf("expected storage driver sqlite, got %s", cfg.Storage.Driver)
	}
	if len(cfg.Providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(cfg.Providers))
	}
	if cfg.Providers[0].APIKey != "sk-test-12345" {
		t.Errorf("expected api key to be expanded, got %s", cfg.Providers[0].APIKey)
	}
}

func TestLoadValidTeamConfig(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "sk-test")
	os.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	defer os.Unsetenv("OPENAI_API_KEY")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	cfg, err := Load("../../configs/team.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Profile != ProfileTeam {
		t.Errorf("expected profile team, got %s", cfg.Profile)
	}
	if len(cfg.Providers) != 2 {
		t.Errorf("expected 2 providers, got %d", len(cfg.Providers))
	}
	if len(cfg.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(cfg.Tools))
	}
}

func TestLoadValidEnterpriseConfig(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "sk-test")
	os.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	os.Setenv("OIDC_ISSUER", "https://auth.example.com")
	os.Setenv("DATABASE_URL", "postgres://localhost:5432/gopilotkit")
	defer os.Unsetenv("OPENAI_API_KEY")
	defer os.Unsetenv("ANTHROPIC_API_KEY")
	defer os.Unsetenv("OIDC_ISSUER")
	defer os.Unsetenv("DATABASE_URL")

	cfg, err := Load("../../configs/enterprise.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Profile != ProfileEnterprise {
		t.Errorf("expected profile enterprise, got %s", cfg.Profile)
	}
	if cfg.Storage.Driver != "postgres" {
		t.Errorf("expected storage driver postgres, got %s", cfg.Storage.Driver)
	}
	if len(cfg.Providers) != 3 {
		t.Errorf("expected 3 providers, got %d", len(cfg.Providers))
	}
	if !cfg.Enterprise.SSO.Enabled {
		t.Error("expected SSO enabled for enterprise")
	}
	if !cfg.Enterprise.RBAC.Enabled {
		t.Error("expected RBAC enabled for enterprise")
	}
	if !cfg.Enterprise.AuditLog.Enabled {
		t.Error("expected audit log enabled for enterprise")
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("nonexistent.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	data := []byte(`version: "v1alpha"
profile: "personal"
server:
  port: not-a-number
`)
	_, err := LoadBytes(data)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestValidateMissingVersion(t *testing.T) {
	cfg := &IntelligenceConfig{
		Profile: ProfilePersonal,
		Server:  ServerConfig{Port: 8080, Transport: []string{"sse"}},
		Storage: StorageConfig{Driver: "sqlite", ConnectionURI: "./data/test.db", VectorStore: VectorStoreConfig{Driver: "embedded", Dimension: 1536}},
		Providers: []ProviderConfig{{ID: "llm", Type: "openai", Model: "gpt-4o", APIKey: "test"}},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing version")
	}
}

func TestValidateInvalidProfile(t *testing.T) {
	cfg := &IntelligenceConfig{
		Version: "v1alpha",
		Profile: "invalid",
		Server:  ServerConfig{Port: 8080, Transport: []string{"sse"}},
		Storage: StorageConfig{Driver: "sqlite", ConnectionURI: "./data/test.db", VectorStore: VectorStoreConfig{Driver: "embedded", Dimension: 1536}},
		Providers: []ProviderConfig{{ID: "llm", Type: "openai", Model: "gpt-4o", APIKey: "test"}},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid profile")
	}
}

func TestValidateInvalidPort(t *testing.T) {
	cfg := &IntelligenceConfig{
		Version: "v1alpha",
		Profile: ProfilePersonal,
		Server:  ServerConfig{Port: 99999, Transport: []string{"sse"}},
		Storage: StorageConfig{Driver: "sqlite", ConnectionURI: "./data/test.db", VectorStore: VectorStoreConfig{Driver: "embedded", Dimension: 1536}},
		Providers: []ProviderConfig{{ID: "llm", Type: "openai", Model: "gpt-4o", APIKey: "test"}},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid port")
	}
}

func TestValidateInvalidTransport(t *testing.T) {
	cfg := &IntelligenceConfig{
		Version: "v1alpha",
		Profile: ProfilePersonal,
		Server:  ServerConfig{Port: 8080, Transport: []string{"http"}},
		Storage: StorageConfig{Driver: "sqlite", ConnectionURI: "./data/test.db", VectorStore: VectorStoreConfig{Driver: "embedded", Dimension: 1536}},
		Providers: []ProviderConfig{{ID: "llm", Type: "openai", Model: "gpt-4o", APIKey: "test"}},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid transport")
	}
}

func TestValidateInvalidStorageDriver(t *testing.T) {
	cfg := &IntelligenceConfig{
		Version: "v1alpha",
		Profile: ProfilePersonal,
		Server:  ServerConfig{Port: 8080, Transport: []string{"sse"}},
		Storage: StorageConfig{Driver: "mysql", ConnectionURI: "test", VectorStore: VectorStoreConfig{Driver: "embedded", Dimension: 1536}},
		Providers: []ProviderConfig{{ID: "llm", Type: "openai", Model: "gpt-4o", APIKey: "test"}},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid storage driver")
	}
}

func TestValidateNoProviders(t *testing.T) {
	cfg := &IntelligenceConfig{
		Version:   "v1alpha",
		Profile:   ProfilePersonal,
		Server:    ServerConfig{Port: 8080, Transport: []string{"sse"}},
		Storage:   StorageConfig{Driver: "sqlite", ConnectionURI: "./data/test.db", VectorStore: VectorStoreConfig{Driver: "embedded", Dimension: 1536}},
		Providers: []ProviderConfig{},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for no providers")
	}
}

func TestValidateDuplicateProviderIDs(t *testing.T) {
	cfg := &IntelligenceConfig{
		Version: "v1alpha",
		Profile: ProfilePersonal,
		Server:  ServerConfig{Port: 8080, Transport: []string{"sse"}},
		Storage: StorageConfig{Driver: "sqlite", ConnectionURI: "./data/test.db", VectorStore: VectorStoreConfig{Driver: "embedded", Dimension: 1536}},
		Providers: []ProviderConfig{
			{ID: "llm", Type: "openai", Model: "gpt-4o", APIKey: "test"},
			{ID: "llm", Type: "anthropic", Model: "claude-3", APIKey: "test"},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for duplicate provider IDs")
	}
}

func TestValidateAgentReferencingNonexistentProvider(t *testing.T) {
	cfg := &IntelligenceConfig{
		Version: "v1alpha",
		Profile: ProfilePersonal,
		Server:  ServerConfig{Port: 8080, Transport: []string{"sse"}},
		Storage: StorageConfig{Driver: "sqlite", ConnectionURI: "./data/test.db", VectorStore: VectorStoreConfig{Driver: "embedded", Dimension: 1536}},
		Providers: []ProviderConfig{
			{ID: "llm", Type: "openai", Model: "gpt-4o", APIKey: "test"},
		},
		Agents: []AgentConfig{
			{Name: "test", Provider: "nonexistent", SystemPrompt: "test"},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for agent referencing nonexistent provider")
	}
}

func TestValidateAgentReferencingNonexistentTool(t *testing.T) {
	cfg := &IntelligenceConfig{
		Version: "v1alpha",
		Profile: ProfilePersonal,
		Server:  ServerConfig{Port: 8080, Transport: []string{"sse"}},
		Storage: StorageConfig{Driver: "sqlite", ConnectionURI: "./data/test.db", VectorStore: VectorStoreConfig{Driver: "embedded", Dimension: 1536}},
		Providers: []ProviderConfig{
			{ID: "llm", Type: "openai", Model: "gpt-4o", APIKey: "test"},
		},
		Agents: []AgentConfig{
			{Name: "test", Provider: "llm", SystemPrompt: "test", Tools: []string{"nonexistent-tool"}},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for agent referencing nonexistent tool")
	}
}

func TestValidateInvalidToolType(t *testing.T) {
	cfg := &IntelligenceConfig{
		Version: "v1alpha",
		Profile: ProfilePersonal,
		Server:  ServerConfig{Port: 8080, Transport: []string{"sse"}},
		Storage: StorageConfig{Driver: "sqlite", ConnectionURI: "./data/test.db", VectorStore: VectorStoreConfig{Driver: "embedded", Dimension: 1536}},
		Providers: []ProviderConfig{
			{ID: "llm", Type: "openai", Model: "gpt-4o", APIKey: "test"},
		},
		Tools: []ToolConfig{
			{ID: "bad-tool", Type: "python"},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid tool type")
	}
}

func TestValidateWASMToolMissingBinaryPath(t *testing.T) {
	cfg := &IntelligenceConfig{
		Version: "v1alpha",
		Profile: ProfilePersonal,
		Server:  ServerConfig{Port: 8080, Transport: []string{"sse"}},
		Storage: StorageConfig{Driver: "sqlite", ConnectionURI: "./data/test.db", VectorStore: VectorStoreConfig{Driver: "embedded", Dimension: 1536}},
		Providers: []ProviderConfig{
			{ID: "llm", Type: "openai", Model: "gpt-4o", APIKey: "test"},
		},
		Tools: []ToolConfig{
			{ID: "wasm-tool", Type: "wasm"},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for WASM tool missing binary_path")
	}
}

func TestValidateMCPToolMissingTransport(t *testing.T) {
	cfg := &IntelligenceConfig{
		Version: "v1alpha",
		Profile: ProfilePersonal,
		Server:  ServerConfig{Port: 8080, Transport: []string{"sse"}},
		Storage: StorageConfig{Driver: "sqlite", ConnectionURI: "./data/test.db", VectorStore: VectorStoreConfig{Driver: "embedded", Dimension: 1536}},
		Providers: []ProviderConfig{
			{ID: "llm", Type: "openai", Model: "gpt-4o", APIKey: "test"},
		},
		Tools: []ToolConfig{
			{ID: "mcp-tool", Type: "mcp", Command: "npx"},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for MCP tool missing transport")
	}
}

func TestValidateMCPToolMissingCommand(t *testing.T) {
	cfg := &IntelligenceConfig{
		Version: "v1alpha",
		Profile: ProfilePersonal,
		Server:  ServerConfig{Port: 8080, Transport: []string{"sse"}},
		Storage: StorageConfig{Driver: "sqlite", ConnectionURI: "./data/test.db", VectorStore: VectorStoreConfig{Driver: "embedded", Dimension: 1536}},
		Providers: []ProviderConfig{
			{ID: "llm", Type: "openai", Model: "gpt-4o", APIKey: "test"},
		},
		Tools: []ToolConfig{
			{ID: "mcp-tool", Type: "mcp", Transport: "stdio"},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for MCP tool missing command")
	}
}

func TestValidateDuplicateTools(t *testing.T) {
	cfg := &IntelligenceConfig{
		Version: "v1alpha",
		Profile: ProfilePersonal,
		Server:  ServerConfig{Port: 8080, Transport: []string{"sse"}},
		Storage: StorageConfig{Driver: "sqlite", ConnectionURI: "./data/test.db", VectorStore: VectorStoreConfig{Driver: "embedded", Dimension: 1536}},
		Providers: []ProviderConfig{
			{ID: "llm", Type: "openai", Model: "gpt-4o", APIKey: "test"},
		},
		Tools: []ToolConfig{
			{ID: "tool-a", Type: "wasm", BinaryPath: "./a.wasm"},
			{ID: "tool-a", Type: "wasm", BinaryPath: "./b.wasm"},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for duplicate tool IDs")
	}
}

// --- Env Var Expansion Tests ---

func TestExpandEnvVarsBasic(t *testing.T) {
	os.Setenv("TEST_VAR_HELLO", "world")
	defer os.Unsetenv("TEST_VAR_HELLO")

	input := []byte(`key: ${TEST_VAR_HELLO}`)
	result := ExpandEnvVars(input)
	expected := []byte(`key: world`)
	if string(result) != string(expected) {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestExpandEnvVarsWithDefault(t *testing.T) {
	os.Unsetenv("TEST_VAR_MISSING")

	input := []byte(`key: ${TEST_VAR_MISSING|fallback_value}`)
	result := ExpandEnvVars(input)
	expected := []byte(`key: fallback_value`)
	if string(result) != string(expected) {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestExpandEnvVarsTakesEnvOverDefault(t *testing.T) {
	os.Setenv("TEST_VAR_OVERRIDE", "actual")
	defer os.Unsetenv("TEST_VAR_OVERRIDE")

	input := []byte(`key: ${TEST_VAR_OVERRIDE|fallback}`)
	result := ExpandEnvVars(input)
	expected := []byte(`key: actual`)
	if string(result) != string(expected) {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestExpandEnvVarsMultiple(t *testing.T) {
	os.Setenv("TEST_DB_HOST", "localhost")
	os.Setenv("TEST_DB_PORT", "5432")
	defer os.Unsetenv("TEST_DB_HOST")
	defer os.Unsetenv("TEST_DB_PORT")

	input := []byte(`uri: postgres://${TEST_DB_HOST}:${TEST_DB_PORT}/mydb`)
	result := ExpandEnvVars(input)
	expected := []byte(`uri: postgres://localhost:5432/mydb`)
	if string(result) != string(expected) {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestExpandEnvVarsNoVars(t *testing.T) {
	input := []byte(`key: plain_value`)
	result := ExpandEnvVars(input)
	if string(result) != string(input) {
		t.Errorf("expected unchanged input, got %q", result)
	}
}

func TestHasUnexpandedVars(t *testing.T) {
	if !HasUnexpandedVars([]byte(`${SOME_VAR}`)) {
		t.Error("expected true for input with vars")
	}
	if HasUnexpandedVars([]byte(`plain text`)) {
		t.Error("expected false for plain text")
	}
}

func TestListEnvVars(t *testing.T) {
	input := []byte(`${VAR_A} and ${VAR_B} and ${VAR_A} again`)
	vars := ListEnvVars(input)
	if len(vars) != 2 {
		t.Errorf("expected 2 unique vars, got %d: %v", len(vars), vars)
	}
}

// --- ConfigErrors Tests ---

func TestConfigErrorsUnwrap(t *testing.T) {
	errs := &ConfigErrors{
		Errors: []error{
			os.ErrNotExist,
			os.ErrPermission,
		},
	}
	unwrapped := errs.Unwrap()
	if len(unwrapped) != 2 {
		t.Errorf("expected 2 unwrapped errors, got %d", len(unwrapped))
	}
}

func TestConfigErrorsError(t *testing.T) {
	errs := &ConfigErrors{
		Errors: []error{
			os.ErrNotExist,
			os.ErrPermission,
		},
	}
	msg := errs.Error()
	if msg == "" {
		t.Error("expected non-empty error string")
	}
}

// --- DefaultConfig Tests ---

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Version != "v1alpha" {
		t.Errorf("expected default version v1alpha, got %s", cfg.Version)
	}
	if cfg.Profile != ProfilePersonal {
		t.Errorf("expected default profile personal, got %s", cfg.Profile)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Server.Port)
	}
}

// --- FindConfigPath Tests ---

func TestFindConfigPathFlagPriority(t *testing.T) {
	os.Setenv("GOPILOTKIT_CONFIG", "/env/config.yaml")
	defer os.Unsetenv("GOPILOTKIT_CONFIG")

	path := FindConfigPath("/flag/config.yaml")
	if path != "/flag/config.yaml" {
		t.Errorf("expected flag path, got %s", path)
	}
}

func TestFindConfigPathEnvPriority(t *testing.T) {
	os.Setenv("GOPILOTKIT_CONFIG", "/env/config.yaml")
	defer os.Unsetenv("GOPILOTKIT_CONFIG")

	path := FindConfigPath("")
	if path != "/env/config.yaml" {
		t.Errorf("expected env path, got %s", path)
	}
}

func TestFindConfigPathDefault(t *testing.T) {
	os.Unsetenv("GOPILOTKIT_CONFIG")

	path := FindConfigPath("")
	if path != DefaultConfigPath {
		t.Errorf("expected default path %s, got %s", DefaultConfigPath, path)
	}
}

// --- LoadBytes Tests ---

func TestLoadBytesValidConfig(t *testing.T) {
	os.Setenv("TEST_API_KEY", "test-key-123")
	defer os.Unsetenv("TEST_API_KEY")

	data := []byte(`version: "v1alpha"
profile: "personal"
server:
  port: 9090
  transport: ["sse"]
storage:
  driver: "sqlite"
  connection_uri: "./test.db"
  vector_store:
    driver: "embedded"
    dimension: 1536
providers:
  - id: "test-llm"
    type: "openai"
    model: "gpt-4o"
    api_key: "${TEST_API_KEY}"
agents:
  - name: "test-agent"
    provider: "test-llm"
    system_prompt: "You are a test agent."
`)

	cfg, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Server.Port)
	}
	if cfg.Providers[0].APIKey != "test-key-123" {
		t.Errorf("expected expanded API key, got %s", cfg.Providers[0].APIKey)
	}
}

func TestLoadBytesNoProviders(t *testing.T) {
	data := []byte(`version: "v1alpha"
profile: "personal"
server:
  port: 8080
  transport: ["sse"]
storage:
  driver: "sqlite"
  connection_uri: "./test.db"
  vector_store:
    driver: "embedded"
    dimension: 1536
providers: []
`)
	_, err := LoadBytes(data)
	if err == nil {
		t.Fatal("expected error for empty providers")
	}
}

// --- ValidateAllProfilesAcceptValidConfig ---

func TestValidateAllProfilesAcceptValidConfig(t *testing.T) {
	profiles := []Profile{ProfilePersonal, ProfileTeam, ProfileEnterprise}
	for _, p := range profiles {
		t.Run(string(p), func(t *testing.T) {
			cfg := &IntelligenceConfig{
				Version: "v1alpha",
				Profile: p,
				Server:  ServerConfig{Port: 8080, Transport: []string{"sse"}},
				Storage: StorageConfig{Driver: "sqlite", ConnectionURI: "./test.db", VectorStore: VectorStoreConfig{Driver: "embedded", Dimension: 1536}},
				Providers: []ProviderConfig{
					{ID: "llm", Type: "openai", Model: "gpt-4o", APIKey: "test"},
				},
			}
			if err := cfg.Validate(); err != nil {
				t.Errorf("profile %s should be valid: %v", p, err)
			}
		})
	}
}
