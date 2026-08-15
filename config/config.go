package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Deployment environments. Only these two values are accepted for the "env"
// config key / ORJANDA_ENV environment variable (TAD §16).
const (
	// EnvDevelopment is the default environment. It is the local-first
	// configuration: forgiving Registry compile (warn-and-continue), missing
	// tables auto-created, an ephemeral JWT secret generated when none is
	// configured, and an admin bootstrapped on first run.
	EnvDevelopment = "development"

	// EnvProduction is the fail-fast environment: any Registry error, pending
	// schema migration, or stale committed frontend codegen aborts startup,
	// and a valid persistent auth.jwt_secret is mandatory.
	EnvProduction = "production"

	// EnvDefault is the value used when neither the env config key nor
	// ORJANDA_ENV is set. Development matches the framework's dev-first model:
	// the former `orjanda serve` command was always a dev server, a bare
	// application binary still defaults to serving (cli/main.go), and the
	// scaffolded orjanda.yaml ships dev defaults. Production is explicit
	// opt-in via ORJANDA_ENV=production, exactly as it used to be explicit
	// opt-in via the former `orjanda bench` command.
	EnvDefault = EnvDevelopment
)

// Config is the root configuration object. It is populated by Load() from
// orjanda.yaml and/or ORJANDA_-prefixed environment variables.
// Struct tags bind the fields to Viper keys; env-var names are derived by
// uppercasing the key and replacing dots/underscores as needed.
//
// See TAD §1.3 for the authoritative schema.
type Config struct {
	// Env selects the deployment environment: EnvDevelopment or EnvProduction.
	// Default: EnvDevelopment. Set via orjanda.yaml (env) or ORJANDA_ENV.
	Env      string         `mapstructure:"env"`
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Auth     AuthConfig     `mapstructure:"auth"`
	LLM      LLMConfig      `mapstructure:"llm"`
}

// MinJWTSecretLength is the minimum accepted length for auth.jwt_secret.
// HS256 signing keys shorter than this offer negligible forgery resistance.
const MinJWTSecretLength = 32

// AuthConfig holds authentication and token signing settings.
type AuthConfig struct {
	// JWTSecret is the HMAC-SHA256 signing key for access and refresh JWTs.
	// It is required: at least MinJWTSecretLength characters, supplied via
	// orjanda.yaml (auth.jwt_secret) or the ORJANDA_AUTH_JWT_SECRET
	// environment variable. There is deliberately no default — a derived or
	// hardcoded default key would let anyone forge administrator tokens
	// (REVIEW-2026-08-12 finding 1).
	JWTSecret string `mapstructure:"jwt_secret"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	// Port is the TCP port the HTTP server listens on. Default: 8080.
	Port int `mapstructure:"port"`

	// Host is the bind address. Default: "0.0.0.0".
	Host string `mapstructure:"host"`

	// CORSOrigins is the list of allowed CORS origin patterns.
	// Default: ["*"].
	CORSOrigins []string `mapstructure:"cors_origins"`
}

// DatabaseConfig holds database connection settings.
type DatabaseConfig struct {
	// Driver selects the database dialect. Must be "postgres" or "sqlite".
	Driver string `mapstructure:"driver"`

	// DSN is the connection string passed to the underlying driver.
	DSN string `mapstructure:"dsn"`

	// MaxOpenConns is the maximum number of open connections in the pool.
	// Default: 25.
	MaxOpenConns int `mapstructure:"max_open_conns"`

	// MaxIdleConns is the maximum number of idle connections in the pool.
	// Default: 5.
	MaxIdleConns int `mapstructure:"max_idle_conns"`
}

// LLMConfig holds large-language-model provider settings.
type LLMConfig struct {
	// DefaultProvider is the key into Providers to use when no explicit
	// provider is specified. Default: "openai".
	DefaultProvider string `mapstructure:"default_provider"`

	// Providers maps provider names (e.g. "openai", "anthropic") to their
	// individual settings.
	Providers map[string]LLMProviderConfig `mapstructure:"providers"`

	// Safety holds agent safety-related limits.
	Safety LLMSafetyConfig `mapstructure:"safety"`
}

// LLMProviderConfig holds the per-provider LLM settings.
type LLMProviderConfig struct {
	// APIKey is the provider's secret API key.
	// Loaded from the environment via e.g. ORJANDA_LLM_PROVIDERS_OPENAI_API_KEY
	// or the ${ORJANDA_OPENAI_API_KEY} interpolation in orjanda.yaml.
	APIKey string `mapstructure:"api_key"`

	// Model is the model identifier to use (e.g. "gpt-4o").
	Model string `mapstructure:"model"`

	// MaxTokens caps the number of tokens per LLM completion request.
	// Default: 4096.
	MaxTokens int `mapstructure:"max_tokens"`

	// BaseURL overrides the provider endpoint. Required for
	// "openai_compatible"; empty means the provider's official default
	// endpoint (TAD §1.3).
	BaseURL string `mapstructure:"base_url"`

	// Auth selects the provider authentication mode: "bearer" (always send
	// the Authorization: Bearer header), "bearer_if_key" (only when api_key
	// is set), or "none" (never). Empty means the provider default.
	// Honored by the OpenAI and openai_compatible adapters (TAD §1.3).
	Auth string `mapstructure:"auth"`

	// ToolCalling and StructuredOutput override the adapter's capability
	// report; OpenAI-compatible servers vary in support, so a self-hosted
	// endpoint can disable a feature it lacks. nil = adapter default.
	ToolCalling      *bool `mapstructure:"tool_calling"`
	StructuredOutput *bool `mapstructure:"structured_output"`
}

// LLMSafetyConfig holds safety-related knobs for the agent runtime.
type LLMSafetyConfig struct {
	// MaxBulkOperations is the number of records above which a bulk agent
	// operation always requires human approval (TAD §12.1 step 2).
	// Default: 5. This default is non-configurable to "always" by PRD §28.1.
	MaxBulkOperations int `mapstructure:"max_bulk_operations"`
}

// defaults sets the Viper defaults that match the TAD §1.3 example config.
// These are applied before any file or environment override.
func setDefaults(v *viper.Viper) {
	v.SetDefault("env", EnvDefault)

	v.SetDefault("server.port", 8080)
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.cors_origins", []string{"*"})

	v.SetDefault("database.driver", "sqlite")
	v.SetDefault("database.dsn", "orjanda.db")
	v.SetDefault("database.max_open_conns", 25)
	v.SetDefault("database.max_idle_conns", 5)

	v.SetDefault("llm.default_provider", "openai")
	v.SetDefault("llm.providers.openai.model", "gpt-4o")
	v.SetDefault("llm.providers.openai.max_tokens", 4096)
	v.SetDefault("llm.providers.anthropic.model", "claude-3-5-sonnet-20240620")
	v.SetDefault("llm.providers.anthropic.max_tokens", 4096)
	v.SetDefault("llm.providers.openai_compatible.max_tokens", 4096)
	v.SetDefault("llm.safety.max_bulk_operations", 5)
}

// Load reads configuration from the supplied file path (pass "" to skip the
// file and rely on defaults + environment variables) and from ORJANDA_-prefixed
// environment variables, which override file values.
//
// Environment variable names are derived from Viper keys by uppercasing and
// replacing dots with underscores, then prepending "ORJANDA_". For example,
// the key "llm.providers.openai.api_key" maps to
// ORJANDA_LLM_PROVIDERS_OPENAI_API_KEY. The env key maps to ORJANDA_ENV.
//
// The deployment environment (env / ORJANDA_ENV) decides how the JWT signing
// secret is handled (TAD §16):
//   - development: a missing or too-short auth.jwt_secret is tolerated — a
//     fresh random secret is generated in its place and returned as the second
//     result. The generated secret is ephemeral; tokens signed with it are
//     invalidated on restart, which is acceptable for local development but
//     never for production. When the configured secret is already valid, the
//     returned string is "".
//   - production: a missing or too-short auth.jwt_secret is a hard error and
//     no secret is generated.
//
// Load satisfies the Phase 0 completion criterion:
//   - Correctly parses the example orjanda.yaml from TAD §1.3.
//   - Env-var override verified for at least one nested key
//     (ORJANDA_OPENAI_API_KEY maps to llm.providers.openai.api_key).
func Load(cfgFile string) (*Config, string, error) {
	v := viper.New()

	setDefaults(v)

	// Bind environment variables with the ORJANDA_ prefix.
	v.SetEnvPrefix("ORJANDA")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// The deployment environment is explicitly bound to ORJANDA_ENV so the
	// value reaches Unmarshal even before any config file is read.
	_ = v.BindEnv("env", "ORJANDA_ENV")

	// Convenience alias: ORJANDA_OPENAI_API_KEY → llm.providers.openai.api_key
	// This matches the ${ORJANDA_OPENAI_API_KEY} interpolation in TAD §1.3.
	_ = v.BindEnv("llm.providers.openai.api_key", "ORJANDA_OPENAI_API_KEY")
	_ = v.BindEnv("llm.providers.anthropic.api_key", "ORJANDA_ANTHROPIC_API_KEY")

	// The JWT signing secret is required and has no default; it must be
	// explicitly bound so the environment value reaches Unmarshal.
	_ = v.BindEnv("auth.jwt_secret", "ORJANDA_AUTH_JWT_SECRET")

	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
		if err := v.ReadInConfig(); err != nil {
			return nil, "", fmt.Errorf("config: reading %q: %w", cfgFile, err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, "", fmt.Errorf("config: unmarshal: %w", err)
	}

	if err := validate(&cfg); err != nil {
		return nil, "", err
	}

	generated := ""
	switch cfg.Env {
	case EnvDevelopment:
		if err := ValidateJWTSecret(cfg.Auth.JWTSecret); err != nil {
			generated = GenerateDevJWTSecret()
			cfg.Auth.JWTSecret = generated
		}
	case EnvProduction:
		if err := ValidateJWTSecret(cfg.Auth.JWTSecret); err != nil {
			return nil, "", fmt.Errorf("config: %w", err)
		}
	default:
		// Unreachable: validate() rejects any other env value.
		return nil, "", fmt.Errorf("config: unsupported ORJANDA_ENV %q", cfg.Env)
	}

	return &cfg, generated, nil
}

// GenerateDevJWTSecret returns a cryptographically random signing key of at
// least MinJWTSecretLength bytes (base64-URL encoded), intended only for
// ephemeral local development secrets.
func GenerateDevJWTSecret() string {
	b := make([]byte, MinJWTSecretLength)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("dev-only-%d-bytes", MinJWTSecretLength)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// ValidateJWTSecret returns an error unless secret is a strong JWT signing key.
// It rejects empty and short values so a misconfigured site fails fast instead
// of silently operating with a guessable key (see TAD §1.3, PRD §15.1).
func ValidateJWTSecret(secret string) error {
	if secret == "" {
		return fmt.Errorf("auth.jwt_secret must be set (orjanda.yaml: auth.jwt_secret, or env ORJANDA_AUTH_JWT_SECRET)")
	}
	if len(secret) < MinJWTSecretLength {
		return fmt.Errorf("auth.jwt_secret must be at least %d characters", MinJWTSecretLength)
	}
	return nil
}

// validate checks that the config values are self-consistent after loading.
// The env key must be one of the two documented environments (TAD §16), and
// the auth.jwt_secret rule is enforced separately by Load so development and
// production can differ on exactly that one point.
func validate(cfg *Config) error {
	switch cfg.Env {
	case EnvDevelopment, EnvProduction:
		// valid
	case "":
		// default was not applied — should not happen (setDefaults sets env)
		return fmt.Errorf("config: env must not be empty")
	default:
		return fmt.Errorf("config: ORJANDA_ENV %q is not supported; choose %q or %q", cfg.Env, EnvDevelopment, EnvProduction)
	}
	switch cfg.Database.Driver {
	case "postgres", "sqlite":
		// valid
	case "":
		// default was not applied — should not happen
		return fmt.Errorf("config: database.driver must not be empty")
	default:
		return fmt.Errorf("config: database.driver %q is not supported; choose postgres or sqlite", cfg.Database.Driver)
	}
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return fmt.Errorf("config: server.port %d is out of range [1, 65535]", cfg.Server.Port)
	}
	return nil
}
