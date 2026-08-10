package llm

import (
	"fmt"

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
	opts := ProviderOptions{APIKey: p.APIKey, Model: p.Model, MaxTokens: p.MaxTokens}
	if model != "" {
		opts.Model = model
	}
	switch name {
	case "openai":
		return NewOpenAIProvider(opts), nil
	case "anthropic":
		return NewAnthropicProvider(opts), nil
	default:
		return nil, fmt.Errorf("config: unsupported llm provider %q", name)
	}
}
