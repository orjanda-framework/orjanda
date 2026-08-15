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

	cfg, _, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Env != config.EnvDevelopment {
		t.Errorf("Env = %q, want default %q", cfg.Env, config.EnvDevelopment)
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
	cfg, _, err := config.Load(path)
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
	cfg, _, err := config.Load(path)
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
// or environment overrides are present, including the default environment.
func TestDefaults(t *testing.T) {
	// Unset env vars that other tests might have set (t.Setenv is cleaned up
	// automatically by the testing package, but be explicit here).
	t.Setenv("ORJANDA_ENV", "")
	t.Setenv("ORJANDA_OPENAI_API_KEY", "")
	t.Setenv("ORJANDA_ANTHROPIC_API_KEY", "")
	// The JWT secret is required (no default), so supply it via env.
	t.Setenv("ORJANDA_AUTH_JWT_SECRET", "defaults-test-jwt-secret-0123456789")

	cfg, _, err := config.Load("")
	if err != nil {
		t.Fatalf("Load(\"\") error = %v", err)
	}
	if cfg.Env != config.EnvDevelopment {
		t.Errorf("default Env = %q, want %q", cfg.Env, config.EnvDevelopment)
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
	cfg, _, err := config.Load(path)
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
	_, _, err := config.Load(path)
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
	_, _, err := config.Load(path)
	if err == nil {
		t.Fatal("Load() with port=99999 expected error, got nil")
	}
}

// TestMissingJWTSecretProduction verifies that config.Load fails fast in
// production when the JWT signing secret is not configured — regression for
// REVIEW-2026-08-12 finding 1, where a missing secret silently fell back to a
// guessable host-derived key. Production never generates a secret.
func TestMissingJWTSecretProduction(t *testing.T) {
	t.Setenv("ORJANDA_ENV", config.EnvProduction)
	t.Setenv("ORJANDA_AUTH_JWT_SECRET", "")
	path := writeYAML(t, `server:
  port: 8080
  host: "0.0.0.0"
database:
  driver: "sqlite"
`)
	_, _, err := config.Load(path)
	if err == nil {
		t.Fatal("Load() without auth.jwt_secret in production expected error, got nil")
	}
	if !strings.Contains(err.Error(), "auth.jwt_secret") {
		t.Errorf("Load() error = %q, want it to mention auth.jwt_secret", err)
	}
}

// TestWeakJWTSecretProduction verifies that short/guessable secrets are
// rejected in production.
func TestWeakJWTSecretProduction(t *testing.T) {
	t.Setenv("ORJANDA_ENV", config.EnvProduction)
	t.Setenv("ORJANDA_AUTH_JWT_SECRET", "")
	const yaml = `
auth:
  jwt_secret: "short"
database:
  driver: "sqlite"
`
	path := writeYAML(t, yaml)
	_, _, err := config.Load(path)
	if err == nil {
		t.Fatal("Load() with a weak auth.jwt_secret in production expected error, got nil")
	}
}

// TestJWTSecretEnvOverride verifies that ORJANDA_AUTH_JWT_SECRET overrides the
// auth.jwt_secret from the config file, mirroring the existing env-override
// pattern for LLM API keys.
func TestJWTSecretEnvOverride(t *testing.T) {
	const want = "env-secret-0123456789-0123456789-0123456789"
	t.Setenv("ORJANDA_AUTH_JWT_SECRET", want)

	path := writeYAML(t, exampleYAML)
	cfg, _, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Auth.JWTSecret != want {
		t.Errorf("Auth.JWTSecret = %q, want %q (env override not applied)", cfg.Auth.JWTSecret, want)
	}
}

// TestDevelopmentGeneratesEphemeralSecret proves the development fallback: with
// no auth.jwt_secret configured, Load in the development environment succeeds,
// substitutes a cryptographically strong generated secret, and reports it via
// the second return value so the caller can warn the operator.
func TestDevelopmentGeneratesEphemeralSecret(t *testing.T) {
	t.Setenv("ORJANDA_ENV", config.EnvDevelopment)
	t.Setenv("ORJANDA_AUTH_JWT_SECRET", "")
	path := writeYAML(t, `server:
  port: 8080
database:
  driver: "sqlite"
`)

	// Production must never silently boot without a secret.
	t.Setenv("ORJANDA_ENV", config.EnvProduction)
	if _, _, err := config.Load(path); err == nil {
		t.Fatal("Load() without auth.jwt_secret in production expected error, got nil")
	}

	t.Setenv("ORJANDA_ENV", config.EnvDevelopment)
	cfg, generated, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if generated == "" {
		t.Fatal("Load() in development must generate a non-empty secret")
	}
	if cfg.Auth.JWTSecret != generated {
		t.Errorf("cfg.Auth.JWTSecret = %q, want generated %q", cfg.Auth.JWTSecret, generated)
	}
	if err := config.ValidateJWTSecret(cfg.Auth.JWTSecret); err != nil {
		t.Errorf("generated secret must be valid: %v", err)
	}
}

// TestDevelopmentKeepsConfiguredSecret verifies that Load returns "" in
// development when a valid secret is already configured, so serve never
// double-substitutes.
func TestDevelopmentKeepsConfiguredSecret(t *testing.T) {
	t.Setenv("ORJANDA_ENV", config.EnvDevelopment)
	t.Setenv("ORJANDA_AUTH_JWT_SECRET", "")
	path := writeYAML(t, exampleYAML)

	cfg, generated, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if generated != "" {
		t.Errorf("Load() generated = %q, want \"\" for a configured secret", generated)
	}
	if cfg.Auth.JWTSecret != "example-jwt-secret-0123456789-0123456789" {
		t.Errorf("configured secret was not preserved: %q", cfg.Auth.JWTSecret)
	}
}

// TestDevelopmentShortSecretReplaced verifies that too-short secrets are
// replaced with a generated one in the development environment.
func TestDevelopmentShortSecretReplaced(t *testing.T) {
	t.Setenv("ORJANDA_ENV", config.EnvDevelopment)
	t.Setenv("ORJANDA_AUTH_JWT_SECRET", "")
	path := writeYAML(t, `auth:
  jwt_secret: "short"
database:
  driver: "sqlite"
`)

	cfg, generated, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if generated == "" {
		t.Fatal("Load() in development should replace a too-short secret")
	}
	if err := config.ValidateJWTSecret(cfg.Auth.JWTSecret); err != nil {
		t.Errorf("replaced secret must be valid: %v", err)
	}
}

// TestGenerateDevJWTSecret verifies the generator yields strong, unique keys.
func TestGenerateDevJWTSecret(t *testing.T) {
	a := config.GenerateDevJWTSecret()
	b := config.GenerateDevJWTSecret()
	if a == b {
		t.Error("GenerateDevJWTSecret() returned the same value twice")
	}
	for _, s := range []string{a, b} {
		if len(s) < config.MinJWTSecretLength {
			t.Errorf("generated secret length %d < %d", len(s), config.MinJWTSecretLength)
		}
		if err := config.ValidateJWTSecret(s); err != nil {
			t.Errorf("generated secret invalid: %v", err)
		}
	}
}

// TestDevelopmentKeepsNonSecretErrors verifies the development environment does
// not mask unrelated config errors (e.g. an unsupported driver) just because
// the JWT check is tolerated.
func TestDevelopmentKeepsNonSecretErrors(t *testing.T) {
	t.Setenv("ORJANDA_ENV", config.EnvDevelopment)
	t.Setenv("ORJANDA_AUTH_JWT_SECRET", "")
	path := writeYAML(t, `server:
  port: 8080
database:
  driver: "mysql"
`)
	if _, _, err := config.Load(path); err == nil {
		t.Fatal("Load() with driver=mysql in development expected error, got nil")
	}
}

// TestProductionWithSecret verifies production accepts a valid configured
// secret and never generates one.
func TestProductionWithSecret(t *testing.T) {
	t.Setenv("ORJANDA_ENV", config.EnvProduction)
	t.Setenv("ORJANDA_AUTH_JWT_SECRET", "")
	path := writeYAML(t, exampleYAML)

	cfg, generated, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if generated != "" {
		t.Errorf("production generated = %q, want \"\" — production must never generate a secret", generated)
	}
	if cfg.Env != config.EnvProduction {
		t.Errorf("Env = %q, want %q", cfg.Env, config.EnvProduction)
	}
}

// TestInvalidEnvFailsFast verifies that any ORJANDA_ENV other than
// development/production is a hard config error before anything else runs.
func TestInvalidEnvFailsFast(t *testing.T) {
	t.Setenv("ORJANDA_AUTH_JWT_SECRET", "")
	for _, env := range []string{"staging", "test", "prod", "PRODUCTION"} {
		t.Setenv("ORJANDA_ENV", env)
		path := writeYAML(t, exampleYAML)
		_, _, err := config.Load(path)
		if err == nil {
			t.Fatalf("Load() with ORJANDA_ENV=%q expected error, got nil", env)
		}
		if !strings.Contains(err.Error(), "ORJANDA_ENV") {
			t.Errorf("Load() error = %q, want it to mention ORJANDA_ENV", err)
		}
		if !strings.Contains(err.Error(), config.EnvDevelopment) || !strings.Contains(err.Error(), config.EnvProduction) {
			t.Errorf("Load() error = %q, want it to list supported values", err)
		}
	}
}

// TestEnvVarOverridesYamlEnv verifies ORJANDA_ENV beats the env key in the
// config file, and that the yaml env key alone selects the environment.
func TestEnvVarOverridesYamlEnv(t *testing.T) {
	t.Setenv("ORJANDA_AUTH_JWT_SECRET", "")

	// yaml says production, env says development → env wins.
	t.Setenv("ORJANDA_ENV", config.EnvDevelopment)
	yamlProd := `env: production
auth:
  jwt_secret: "production-yaml-secret-0123456789-0123456789"
database:
  driver: "sqlite"
`
	cfg, _, err := config.Load(writeYAML(t, yamlProd))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Env != config.EnvDevelopment {
		t.Errorf("Env = %q, want %q (env var must override yaml)", cfg.Env, config.EnvDevelopment)
	}

	// No env var → yaml env key applies.
	t.Setenv("ORJANDA_ENV", "")
	cfg, _, err = config.Load(writeYAML(t, yamlProd))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Env != config.EnvProduction {
		t.Errorf("Env = %q, want %q from yaml", cfg.Env, config.EnvProduction)
	}
}
