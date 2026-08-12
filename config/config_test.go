package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orjanda-framework/orjanda/config"
)

// writeYAML writes content to a temp file and returns its path.
func writeYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "orjanda.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeYAML: %v", err)
	}
	return path
}

// exampleYAML is identical to the TAD §1.3 example config, plus the required
// auth.jwt_secret block (REVIEW-2026-08-12 finding 1).
const exampleYAML = `
server:
  port: 8080
  host: "0.0.0.0"
  cors_origins: ["*"]
database:
  driver: "postgres"
  dsn: "postgres://user:pass@localhost:5432/orjanda?sslmode=disable"
  max_open_conns: 25
  max_idle_conns: 5
auth:
  jwt_secret: "example-jwt-secret-0123456789-0123456789"
llm:
  default_provider: "openai"
  providers:
    openai:
      api_key: "sk-test"
      model: "gpt-4o"
      max_tokens: 4096
    anthropic:
      api_key: "ant-test"
      model: "claude-3-5-sonnet-20240620"
      max_tokens: 4096
    openai_compatible:
      base_url: "http://localhost:11434/v1"
      model: "llama3.1"
      max_tokens: 4096
  safety:
    max_bulk_operations: 5
`

// TestLoadExampleYAML verifies that config.Load correctly parses the TAD §1.3
// example orjanda.yaml — a Phase 0 completion criterion.
func TestLoadExampleYAML(t *testing.T) {
	// Isolate from any ambient ORJANDA_AUTH_JWT_SECRET (empty env is ignored).
	t.Setenv("ORJANDA_AUTH_JWT_SECRET", "")
	path := writeYAML(t, exampleYAML)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Server
	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Server.Host = %q, want %q", cfg.Server.Host, "0.0.0.0")
	}

	// Database
	if cfg.Database.Driver != "postgres" {
		t.Errorf("Database.Driver = %q, want %q", cfg.Database.Driver, "postgres")
	}
	if cfg.Database.MaxOpenConns != 25 {
		t.Errorf("Database.MaxOpenConns = %d, want 25", cfg.Database.MaxOpenConns)
	}
	if cfg.Database.MaxIdleConns != 5 {
		t.Errorf("Database.MaxIdleConns = %d, want 5", cfg.Database.MaxIdleConns)
	}

	// Auth
	if cfg.Auth.JWTSecret != "example-jwt-secret-0123456789-0123456789" {
		t.Errorf("Auth.JWTSecret = %q, want %q", cfg.Auth.JWTSecret, "example-jwt-secret-0123456789-0123456789")
	}

	// LLM
	if cfg.LLM.DefaultProvider != "openai" {
		t.Errorf("LLM.DefaultProvider = %q, want %q", cfg.LLM.DefaultProvider, "openai")
	}
	openai, ok := cfg.LLM.Providers["openai"]
	if !ok {
		t.Fatal("LLM.Providers[openai] missing")
	}
	if openai.Model != "gpt-4o" {
		t.Errorf("openai.Model = %q, want %q", openai.Model, "gpt-4o")
	}
	if openai.MaxTokens != 4096 {
		t.Errorf("openai.MaxTokens = %d, want 4096", openai.MaxTokens)
	}
	oc, ok := cfg.LLM.Providers["openai_compatible"]
	if !ok {
		t.Fatal("LLM.Providers[openai_compatible] missing")
	}
	if oc.BaseURL != "http://localhost:11434/v1" {
		t.Errorf("openai_compatible.BaseURL = %q, want %q", oc.BaseURL, "http://localhost:11434/v1")
	}
	if cfg.LLM.Safety.MaxBulkOperations != 5 {
		t.Errorf("LLM.Safety.MaxBulkOperations = %d, want 5", cfg.LLM.Safety.MaxBulkOperations)
	}
}

// TestEnvVarOverride verifies that ORJANDA_OPENAI_API_KEY overrides
// llm.providers.openai.api_key — an explicit Phase 0 completion criterion.
func TestEnvVarOverride(t *testing.T) {
	const envKey = "ORJANDA_OPENAI_API_KEY"
	const want = "sk-from-env-override"

	t.Setenv(envKey, want)
	t.Setenv("ORJANDA_AUTH_JWT_SECRET", "")

	path := writeYAML(t, exampleYAML)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	openai, ok := cfg.LLM.Providers["openai"]
	if !ok {
		t.Fatal("LLM.Providers[openai] missing")
	}
	if openai.APIKey != want {
		t.Errorf("openai.APIKey = %q, want %q (env override not applied)", openai.APIKey, want)
	}
}

// TestAnthropicEnvOverride mirrors the above for ORJANDA_ANTHROPIC_API_KEY.
func TestAnthropicEnvOverride(t *testing.T) {
	t.Setenv("ORJANDA_ANTHROPIC_API_KEY", "ant-from-env")
	t.Setenv("ORJANDA_AUTH_JWT_SECRET", "")

	path := writeYAML(t, exampleYAML)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	ant, ok := cfg.LLM.Providers["anthropic"]
	if !ok {
		t.Fatal("LLM.Providers[anthropic] missing")
	}
	if ant.APIKey != "ant-from-env" {
		t.Errorf("anthropic.APIKey = %q, want %q", ant.APIKey, "ant-from-env")
	}
}

// TestDefaults verifies that Load("") returns sensible defaults when no file
// or environment overrides are present.
func TestDefaults(t *testing.T) {
	// Unset env vars that other tests might have set (t.Setenv is cleaned up
	// automatically by the testing package, but be explicit here).
	t.Setenv("ORJANDA_OPENAI_API_KEY", "")
	t.Setenv("ORJANDA_ANTHROPIC_API_KEY", "")
	// The JWT secret is required (no default), so supply it via env.
	t.Setenv("ORJANDA_AUTH_JWT_SECRET", "defaults-test-jwt-secret-0123456789")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load(\"\") error = %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("default Server.Port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.LLM.Safety.MaxBulkOperations != 5 {
		t.Errorf("default MaxBulkOperations = %d, want 5", cfg.LLM.Safety.MaxBulkOperations)
	}
}

// TestLoadOpenAICompatibleConfig verifies parsing of the openai_compatible
// provider block (base_url, auth, capability overrides).
func TestLoadOpenAICompatibleConfig(t *testing.T) {
	t.Setenv("ORJANDA_AUTH_JWT_SECRET", "")
	const yaml = `
auth:
  jwt_secret: "openai-compatible-test-jwt-secret-0123"
llm:
  default_provider: "openai_compatible"
  providers:
    openai_compatible:
      api_key: "local-key"
      model: "llama3.1"
      base_url: "http://localhost:11434/v1"
      max_tokens: 1024
      auth: "none"
      tool_calling: false
      structured_output: false
`
	path := writeYAML(t, yaml)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	oc, ok := cfg.LLM.Providers["openai_compatible"]
	if !ok {
		t.Fatal("LLM.Providers[openai_compatible] missing")
	}
	if oc.BaseURL != "http://localhost:11434/v1" {
		t.Errorf("BaseURL = %q, want %q", oc.BaseURL, "http://localhost:11434/v1")
	}
	if oc.Auth != "none" {
		t.Errorf("Auth = %q, want none", oc.Auth)
	}
	if oc.APIKey != "local-key" {
		t.Errorf("APIKey = %q, want local-key", oc.APIKey)
	}
	if oc.Model != "llama3.1" {
		t.Errorf("Model = %q, want llama3.1", oc.Model)
	}
	if oc.MaxTokens != 1024 {
		t.Errorf("MaxTokens = %d, want 1024", oc.MaxTokens)
	}
	if oc.ToolCalling == nil || *oc.ToolCalling {
		t.Errorf("ToolCalling = %v, want false", oc.ToolCalling)
	}
	if oc.StructuredOutput == nil || *oc.StructuredOutput {
		t.Errorf("StructuredOutput = %v, want false", oc.StructuredOutput)
	}
}

// TestInvalidDriver verifies that an unsupported driver returns an error.
func TestInvalidDriver(t *testing.T) {
	const badYAML = `
database:
  driver: "mysql"
  dsn: "mysql://..."
`
	path := writeYAML(t, badYAML)
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("Load() with driver=mysql expected error, got nil")
	}
}

// TestInvalidPort verifies that an out-of-range port returns an error.
func TestInvalidPort(t *testing.T) {
	const badYAML = `
server:
  port: 99999
database:
  driver: "sqlite"
`
	path := writeYAML(t, badYAML)
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("Load() with port=99999 expected error, got nil")
	}
}

// TestMissingJWTSecret verifies that config.Load fails fast when the JWT
// signing secret is not configured — regression for REVIEW-2026-08-12 finding
// 1, where a missing secret silently fell back to a guessable host-derived key.
func TestMissingJWTSecret(t *testing.T) {
	t.Setenv("ORJANDA_AUTH_JWT_SECRET", "")
	path := writeYAML(t, `server:
  port: 8080
  host: "0.0.0.0"
database:
  driver: "sqlite"
`)
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("Load() without auth.jwt_secret expected error, got nil")
	}
	if !strings.Contains(err.Error(), "auth.jwt_secret") {
		t.Errorf("Load() error = %q, want it to mention auth.jwt_secret", err)
	}
}

// TestWeakJWTSecret verifies that short/guessable secrets are rejected.
func TestWeakJWTSecret(t *testing.T) {
	t.Setenv("ORJANDA_AUTH_JWT_SECRET", "")
	const yaml = `
auth:
  jwt_secret: "short"
database:
  driver: "sqlite"
`
	path := writeYAML(t, yaml)
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("Load() with a weak auth.jwt_secret expected error, got nil")
	}
}

// TestJWTSecretEnvOverride verifies that ORJANDA_AUTH_JWT_SECRET overrides the
// auth.jwt_secret from the config file, mirroring the existing env-override
// pattern for LLM API keys.
func TestJWTSecretEnvOverride(t *testing.T) {
	const want = "env-secret-0123456789-0123456789-0123456789"
	t.Setenv("ORJANDA_AUTH_JWT_SECRET", want)

	path := writeYAML(t, exampleYAML)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Auth.JWTSecret != want {
		t.Errorf("Auth.JWTSecret = %q, want %q (env override not applied)", cfg.Auth.JWTSecret, want)
	}
}
