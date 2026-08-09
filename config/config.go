package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config is the root configuration object. It is populated by Load() from
// orjanda.yaml and/or ORJANDA_-prefixed environment variables.
// Struct tags bind the fields to Viper keys; env-var names are derived by
// uppercasing the key and replacing dots/underscores as needed.
//
// See TAD §1.3 for the authoritative schema.
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	LLM      LLMConfig      `mapstructure:"llm"`
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
	v.SetDefault("llm.safety.max_bulk_operations", 5)
}

// Load reads configuration from the supplied file path (pass "" to skip the
// file and rely on defaults + environment variables) and from ORJANDA_-prefixed
// environment variables, which override file values.
//
// Environment variable names are derived from Viper keys by uppercasing and
// replacing dots with underscores, then prepending "ORJANDA_". For example,
// the key "llm.providers.openai.api_key" maps to
// ORJANDA_LLM_PROVIDERS_OPENAI_API_KEY.
//
// Load satisfies the Phase 0 completion criterion:
//   - Correctly parses the example orjanda.yaml from TAD §1.3.
//   - Env-var override verified for at least one nested key
//     (ORJANDA_OPENAI_API_KEY maps to llm.providers.openai.api_key).
func Load(cfgFile string) (*Config, error) {
	v := viper.New()

	setDefaults(v)

	// Bind environment variables with the ORJANDA_ prefix.
	v.SetEnvPrefix("ORJANDA")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Convenience alias: ORJANDA_OPENAI_API_KEY → llm.providers.openai.api_key
	// This matches the ${ORJANDA_OPENAI_API_KEY} interpolation in TAD §1.3.
	_ = v.BindEnv("llm.providers.openai.api_key", "ORJANDA_OPENAI_API_KEY")
	_ = v.BindEnv("llm.providers.anthropic.api_key", "ORJANDA_ANTHROPIC_API_KEY")

	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("config: reading %q: %w", cfgFile, err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}

	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// validate checks that the config values are self-consistent after loading.
func validate(cfg *Config) error {
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
