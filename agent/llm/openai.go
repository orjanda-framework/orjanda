package llm

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	orjerrors "github.com/orjanda-framework/orjanda/errors"
)

// defaultOpenAIBaseURL is the OpenAI chat-completions endpoint (PRD §26.2).
const defaultOpenAIBaseURL = "https://api.openai.com/v1"

// OpenAIProvider implements Provider against the OpenAI chat-completions API.
// See TAD §2.7 and PRD §26.
type OpenAIProvider struct {
	apiKey    string
	model     string
	baseURL   string
	maxTokens int
	client    *http.Client
}

// NewOpenAIProvider constructs an OpenAI adapter. BaseURL and HTTPClient in
// opts default to the official endpoint and http.DefaultClient respectively.
func NewOpenAIProvider(opts ProviderOptions) *OpenAIProvider {
	baseURL := strings.TrimSuffix(opts.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &OpenAIProvider{
		apiKey:    opts.APIKey,
		model:     opts.Model,
		baseURL:   baseURL,
		maxTokens: opts.MaxTokens,
		client:    client,
	}
}

func (p *OpenAIProvider) SupportsToolCalling() bool { return true }

func (p *OpenAIProvider) SupportsStructuredOutput() bool { return true }

func (p *OpenAIProvider) ModelInfo() ModelInfo {
	return ModelInfo{
		Name:                     p.model,
		SupportsTools:            true,
		SupportsStructuredOutput: true,
	}
}

// --- Wire format ------------------------------------------------------------

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	Name       string           `json:"name,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string         `json:"name"`
	Arguments   string         `json:"arguments,omitempty"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIResponseFormat struct {
	Type       string                 `json:"type"`
	JSONSchema openAIJSONSchemaFormat `json:"json_schema,omitempty"`
}

type openAIJSONSchemaFormat struct {
	Name   string         `json:"name"`
	Schema map[string]any `json:"schema"`
	Strict bool           `json:"strict"`
}

type openAIRequest struct {
	Model          string                `json:"model"`
	Messages       []openAIMessage       `json:"messages"`
	Tools          []openAITool          `json:"tools,omitempty"`
	Temperature    *float64              `json:"temperature,omitempty"`
	MaxTokens      *int                  `json:"max_tokens,omitempty"`
	Stream         bool                  `json:"stream"`
	ResponseFormat *openAIResponseFormat `json:"response_format,omitempty"`
}

type openAIChoice struct {
	Message      openAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openAIResponse struct {
	ID      string         `json:"id"`
	Choices []openAIChoice `json:"choices"`
	Usage   struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// --- Translation helpers ----------------------------------------------------

func (p *OpenAIProvider) buildRequest(req ChatRequest, stream bool) openAIRequest {
	model := req.Model
	if model == "" {
		model = p.model
	}

	out := openAIRequest{
		Model:    model,
		Messages: make([]openAIMessage, 0, len(req.Messages)),
		Stream:   stream,
	}
	for _, m := range req.Messages {
		om := openAIMessage{
			Role:       m.Role,
			Content:    m.Content,
			Name:       m.Name,
			ToolCallID: m.ToolCallID,
		}
		for _, tc := range m.ToolCalls {
			om.ToolCalls = append(om.ToolCalls, openAIToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: openAIFunction{
					Name:      tc.Name,
					Arguments: tc.Arguments,
				},
			})
		}
		out.Messages = append(out.Messages, om)
	}

	for _, t := range req.Tools {
		out.Tools = append(out.Tools, openAITool{
			Type: "function",
			Function: openAIFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}

	if req.MaxTokens > 0 {
		out.MaxTokens = &req.MaxTokens
	} else if p.maxTokens > 0 {
		out.MaxTokens = &p.maxTokens
	}
	temp := req.Temperature
	out.Temperature = &temp

	if req.ResponseFormat != nil {
		out.ResponseFormat = &openAIResponseFormat{
			Type: "json_schema",
			JSONSchema: openAIJSONSchemaFormat{
				Name:   req.ResponseFormat.Name,
				Schema: req.ResponseFormat.Schema,
				Strict: true,
			},
		}
	}
	return out
}

func translateOpenAIResponse(resp *openAIResponse) *ChatResponse {
	out := &ChatResponse{}
	if len(resp.Choices) > 0 {
		ch := resp.Choices[0]
		out.Content = ch.Message.Content
		out.FinishReason = ch.FinishReason
		for _, tc := range ch.Message.ToolCalls {
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
	}
	out.Usage = TokenUsage{
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.TotalTokens,
	}
	return out
}

// --- Provider implementation -----------------------------------------------

func (p *OpenAIProvider) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	wire := p.buildRequest(req, false)

	var resp openAIResponse
	if err := doJSON(ctx, p.client, "POST", p.baseURL+"/chat/completions", p.headers(), wire, &resp); err != nil {
		return nil, orjerrors.Internal("openai: chat completion request failed", err)
	}
	if resp.Error != nil {
		return nil, orjerrors.Internal("openai: "+resp.Error.Message, nil)
	}
	return translateOpenAIResponse(&resp), nil
}

func (p *OpenAIProvider) StreamChatCompletion(ctx context.Context, req ChatRequest) (<-chan ChatChunk, error) {
	wire := p.buildRequest(req, true)

	// Open the stream synchronously so connection/5xx failures surface as the
	// returned error (and the Gateway can fail over) before any chunk is sent.
	//nolint:bodyclose // ownership transfers to consumeStream, which closes resp.Body (goroutine-stream false positive)
	resp, err := openStream(ctx, p.client, "POST", p.baseURL+"/chat/completions", p.headers(), wire)
	if err != nil {
		return nil, orjerrors.Internal("openai: stream request failed", err)
	}

	ch := make(chan ChatChunk)
	go func() {
		defer close(ch)
		if err := p.consumeStream(ctx, resp, ch); err != nil {
			// TAD §2.7's ChatChunk carries no error field: a mid-stream failure
			// surfaces as a short stream that closes without a finish chunk.
			slog.Warn("openai.stream_interrupted", "err", err)
		}
	}()
	return ch, nil
}

// consumeStream drains resp, forwarding SSE deltas to ch. It owns resp.Body
// and closes it on every exit path (TAD §2.7 streaming contract).
func (p *OpenAIProvider) consumeStream(ctx context.Context, resp *http.Response, ch chan<- ChatChunk) error {
	defer func() { _ = resp.Body.Close() }()
	return streamEvents(ctx, resp, func(data []byte) error {
		var ev openAIStreamEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			return orjerrors.Internal("openai: malformed stream event", err)
		}
		if ev.Error != nil {
			return orjerrors.Internal("openai: "+ev.Error.Message, nil)
		}
		if len(ev.Choices) == 0 {
			return nil
		}
		chunk := ChatChunk{FinishReason: ev.Choices[0].FinishReason}
		delta := ev.Choices[0].Delta
		chunk.Content = delta.Content
		for _, tc := range delta.ToolCalls {
			chunk.ToolCalls = append(chunk.ToolCalls, ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
		select {
		case ch <- chunk:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})
}

type openAIDelta struct {
	Content   string           `json:"content"`
	ToolCalls []openAIToolCall `json:"tool_calls"`
}

type openAIStreamEvent struct {
	Choices []struct {
		Index        int         `json:"index"`
		Delta        openAIDelta `json:"delta"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (p *OpenAIProvider) headers() map[string]string {
	return map[string]string{
		"Authorization": "Bearer " + p.apiKey,
	}
}
