package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// OpenAICompatibleProvider implements Provider against any server exposing the
// OpenAI chat-completions API — e.g. Ollama, vLLM, LM Studio, Together, Groq.
// It shares the OpenAI adapter's wire format but, unlike the certified openai
// adapter, it is keyless by default (requests authenticate only when api_key is
// set) and reports capabilities per instance instead of hardcoding them, since
// compatible servers vary in tool and structured-output support.
type OpenAICompatibleProvider struct {
	inner *OpenAIProvider
}

// NewOpenAICompatibleProvider constructs an OpenAI-compatible adapter. BaseURL
// is required — there is no official endpoint to fall back to. The adapter
// defaults to AuthBearerIfKey: requests carry an Authorization: Bearer header
// only when opts.APIKey is non-empty. Per-instance capability overrides can be
// supplied via opts.ToolCalling and opts.StructuredOutput.
func NewOpenAICompatibleProvider(opts ProviderOptions) (*OpenAICompatibleProvider, error) {
	baseURL := strings.TrimSuffix(opts.BaseURL, "/")
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("llm: openai_compatible requires base_url to be a valid http(s) endpoint, got %q", opts.BaseURL)
	}
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	auth := opts.Auth
	if auth == "" {
		auth = AuthBearerIfKey
	}
	return &OpenAICompatibleProvider{inner: &OpenAIProvider{
		apiKey:           opts.APIKey,
		model:            opts.Model,
		baseURL:          baseURL,
		maxTokens:        opts.MaxTokens,
		client:           client,
		auth:             auth,
		toolCalling:      boolOr(opts.ToolCalling, true),
		structuredOutput: boolOr(opts.StructuredOutput, true),
	}}, nil
}

func (p *OpenAICompatibleProvider) SupportsToolCalling() bool {
	return p.inner.SupportsToolCalling()
}

func (p *OpenAICompatibleProvider) SupportsStructuredOutput() bool {
	return p.inner.SupportsStructuredOutput()
}

func (p *OpenAICompatibleProvider) ModelInfo() ModelInfo {
	return p.inner.ModelInfo()
}

func (p *OpenAICompatibleProvider) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	return p.inner.ChatCompletion(ctx, req)
}

func (p *OpenAICompatibleProvider) StreamChatCompletion(ctx context.Context, req ChatRequest) (<-chan ChatChunk, error) {
	return p.inner.StreamChatCompletion(ctx, req)
}
