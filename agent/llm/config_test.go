package llm_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/orjanda-framework/orjanda/agent/llm"
	"github.com/orjanda-framework/orjanda/config"
)

func TestProviderFromConfigOpenAICompatible(t *testing.T) {
	cfg := &config.Config{LLM: config.LLMConfig{
		DefaultProvider: "openai_compatible",
		Providers: map[string]config.LLMProviderConfig{
			"openai_compatible": {
				APIKey:    "test-key",
				Model:     "test-model",
				BaseURL:   "http://localhost:11434/v1",
				MaxTokens: 2048,
			},
		},
	}}
	p, err := llm.ProviderFromConfig(cfg, "")
	if err != nil {
		t.Fatalf("ProviderFromConfig: %v", err)
	}
	oc, ok := p.(*llm.OpenAICompatibleProvider)
	if !ok {
		t.Fatalf("provider = %T, want *llm.OpenAICompatibleProvider", p)
	}
	if mi := oc.ModelInfo(); mi.Name != "test-model" {
		t.Errorf("ModelInfo().Name = %q, want test-model", mi.Name)
	}
	if !oc.SupportsToolCalling() || !oc.SupportsStructuredOutput() {
		t.Error("default capabilities must be true/true")
	}
}

// TestProviderFromConfigOpenAICompatibleBaseURLPlumbed proves base_url reaches
// the adapter: a round trip through an httptest.Server succeeds end to end.
func TestProviderFromConfigOpenAICompatibleBaseURLPlumbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"total_tokens":1}}`))
	}))
	defer srv.Close()

	cfg := &config.Config{LLM: config.LLMConfig{
		DefaultProvider: "openai_compatible",
		Providers: map[string]config.LLMProviderConfig{
			"openai_compatible": {Model: "test-model", BaseURL: srv.URL},
		},
	}}
	p, err := llm.ProviderFromConfig(cfg, "")
	if err != nil {
		t.Fatalf("ProviderFromConfig: %v", err)
	}
	resp, err := p.ChatCompletion(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("Content = %q, want ok", resp.Content)
	}
}

func TestProviderFromConfigOpenAICompatibleMissingBaseURL(t *testing.T) {
	cfg := &config.Config{LLM: config.LLMConfig{
		DefaultProvider: "openai_compatible",
		Providers: map[string]config.LLMProviderConfig{
			"openai_compatible": {Model: "test-model"},
		},
	}}
	if _, err := llm.ProviderFromConfig(cfg, ""); err == nil {
		t.Fatal("expected error for missing base_url, got nil")
	}
}

func TestProviderFromConfigOpenAICompatibleBadAuth(t *testing.T) {
	cfg := &config.Config{LLM: config.LLMConfig{
		DefaultProvider: "openai_compatible",
		Providers: map[string]config.LLMProviderConfig{
			"openai_compatible": {
				Model:   "test-model",
				BaseURL: "http://localhost:11434/v1",
				Auth:    "digest",
			},
		},
	}}
	if _, err := llm.ProviderFromConfig(cfg, ""); err == nil {
		t.Fatal("expected error for unsupported auth mode, got nil")
	}
}

func TestProviderFromConfigOpenAICompatibleCapabilityOverrides(t *testing.T) {
	f := false
	cfg := &config.Config{LLM: config.LLMConfig{
		DefaultProvider: "openai_compatible",
		Providers: map[string]config.LLMProviderConfig{
			"openai_compatible": {
				Model:            "test-model",
				BaseURL:          "http://localhost:11434/v1",
				ToolCalling:      &f,
				StructuredOutput: &f,
			},
		},
	}}
	p, err := llm.ProviderFromConfig(cfg, "")
	if err != nil {
		t.Fatalf("ProviderFromConfig: %v", err)
	}
	if p.SupportsToolCalling() || p.SupportsStructuredOutput() {
		t.Error("capability overrides from config not applied")
	}
}

func TestProviderFromConfigModelOverride(t *testing.T) {
	cfg := &config.Config{LLM: config.LLMConfig{
		DefaultProvider: "openai",
		Providers: map[string]config.LLMProviderConfig{
			"openai": {Model: "gpt-4o"},
		},
	}}
	p, err := llm.ProviderFromConfig(cfg, "gpt-4o-mini")
	if err != nil {
		t.Fatalf("ProviderFromConfig: %v", err)
	}
	if mi := p.ModelInfo(); mi.Name != "gpt-4o-mini" {
		t.Errorf("ModelInfo().Name = %q, want gpt-4o-mini", mi.Name)
	}
}

func TestProviderFromConfigUnsupported(t *testing.T) {
	cfg := &config.Config{LLM: config.LLMConfig{
		DefaultProvider: "gemini",
		Providers:       map[string]config.LLMProviderConfig{"gemini": {}},
	}}
	_, err := llm.ProviderFromConfig(cfg, "")
	if err == nil || !strings.Contains(err.Error(), "unsupported llm provider") {
		t.Fatalf("err = %v, want unsupported llm provider", err)
	}
}
