package llm

import (
	"fmt"
	"strings"

	"github.com/orjanda-framework/orjanda/config"
)

// ProviderFromConfig builds the default configured provider from a config.
// It is the single place a site/CLI resolves site.Config.LLM into a Provider
// (TAD §9.3: llm.Provider | site.Config.LLM.Providers[name]). model overrides
// the provider's configured model when non-empty.
func ProviderFromConfig(cfg *config.Config, model string) (Provider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("llm: nil config")
	}
	name := cfg.LLM.DefaultProvider
	if name == "" {
		name = "openai"
	}
	p, ok := cfg.LLM.Providers[name]
	if !ok {
		return nil, fmt.Errorf("config: llm.providers.%s is not configured", name)
	}
	opts := ProviderOptions{
		APIKey:           p.APIKey,
		Model:            p.Model,
		BaseURL:          p.BaseURL,
		MaxTokens:        p.MaxTokens,
		ToolCalling:      p.ToolCalling,
		StructuredOutput: p.StructuredOutput,
	}
	if model != "" {
		opts.Model = model
	}
	auth, err := authModeFromConfig(name, p.Auth)
	if err != nil {
		return nil, err
	}
	opts.Auth = auth
	switch name {
	case "openai":
		return NewOpenAIProvider(opts), nil
	case "anthropic":
		return NewAnthropicProvider(opts), nil
	case "openai_compatible":
		return NewOpenAICompatibleProvider(opts)
	default:
		return nil, fmt.Errorf("config: unsupported llm provider %q", name)
	}
}

// authModeFromConfig maps the llm.providers.<name>.auth config value onto an
// AuthMode. An empty value means "provider default" (represented by "").
func authModeFromConfig(name, value string) (AuthMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", nil
	case string(AuthBearer):
		return AuthBearer, nil
	case string(AuthBearerIfKey):
		return AuthBearerIfKey, nil
	case string(AuthNone):
		return AuthNone, nil
	default:
		return "", fmt.Errorf("config: llm.providers.%s.auth %q is not supported; choose %q, %q, or %q",
			name, value, AuthBearer, AuthBearerIfKey, AuthNone)
	}
}
