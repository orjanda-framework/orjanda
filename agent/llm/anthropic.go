package llm

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	orjerrors "github.com/orjanda-framework/orjanda/errors"
)

// defaultAnthropicBaseURL is the Anthropic Messages endpoint (PRD §26.2).
const defaultAnthropicBaseURL = "https://api.anthropic.com"

// anthropicAPIVersion is the Messages API version header required by Anthropic.
const anthropicAPIVersion = "2023-06-01"

// AnthropicProvider implements Provider against the Anthropic Messages API.
// Structured output (ResponseFormat) is not natively supported by the Messages
// API, so SupportsStructuredOutput returns false and ResponseFormat is ignored
// (TAD §11.3 plans only use ResponseFormat when the selected provider supports
// it). See TAD §2.7 and PRD §26.
type AnthropicProvider struct {
	apiKey    string
	model     string
	baseURL   string
	maxTokens int
	client    *http.Client
}

// NewAnthropicProvider constructs an Anthropic adapter. BaseURL and HTTPClient
// in opts default to the official endpoint and http.DefaultClient respectively.
func NewAnthropicProvider(opts ProviderOptions) *AnthropicProvider {
	baseURL := strings.TrimSuffix(opts.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultAnthropicBaseURL
	}
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &AnthropicProvider{
		apiKey:    opts.APIKey,
		model:     opts.Model,
		baseURL:   baseURL,
		maxTokens: opts.MaxTokens,
		client:    client,
	}
}

func (p *AnthropicProvider) SupportsToolCalling() bool { return true }

func (p *AnthropicProvider) SupportsStructuredOutput() bool { return false }

func (p *AnthropicProvider) ModelInfo() ModelInfo {
	return ModelInfo{
		Name:                     p.model,
		SupportsTools:            true,
		SupportsStructuredOutput: false,
	}
}

// --- Wire format ------------------------------------------------------------

type anthropicContentBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   string         `json:"content,omitempty"`
}

type anthropicMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicRequest struct {
	Model       string               `json:"model"`
	MaxTokens   int                  `json:"max_tokens"`
	Temperature *float64             `json:"temperature,omitempty"`
	System      string               `json:"system,omitempty"`
	Messages    []anthropicMessage   `json:"messages"`
	Tools       []anthropicTool      `json:"tools,omitempty"`
	ToolChoice  *anthropicToolChoice `json:"tool_choice,omitempty"`
	Stream      bool                 `json:"stream"`
}

type anthropicToolChoice struct {
	Type string `json:"type"`
}

type anthropicResponse struct {
	ID         string                  `json:"id"`
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// --- Translation helpers ----------------------------------------------------

// buildRequest translates the unified ChatRequest into Anthropic's wire shape.
// System messages move to the top-level "system" field; consecutive "tool"
// role messages are merged into a single "user" message of tool_result blocks
// because Anthropic requires alternating user/assistant turns.
func (p *AnthropicProvider) buildRequest(req ChatRequest, stream bool) anthropicRequest {
	model := req.Model
	if model == "" {
		model = p.model
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = p.maxTokens
	}
	if maxTokens == 0 {
		maxTokens = 4096
	}

	out := anthropicRequest{
		Model:     model,
		MaxTokens: maxTokens,
		Stream:    stream,
	}

	var systemParts []string
	var messages []anthropicMessage
	var pendingToolResult []anthropicContentBlock

	flushToolResults := func() {
		if len(pendingToolResult) == 0 {
			return
		}
		messages = append(messages, anthropicMessage{
			Role:    "user",
			Content: pendingToolResult,
		})
		pendingToolResult = nil
	}

	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			if m.Content != "" {
				systemParts = append(systemParts, m.Content)
			}
		case "tool":
			pendingToolResult = append(pendingToolResult, anthropicContentBlock{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   m.Content,
			})
		default:
			flushToolResults()
			blocks := make([]anthropicContentBlock, 0, 1+len(m.ToolCalls))
			if m.Content != "" {
				blocks = append(blocks, anthropicContentBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				input := make(map[string]any)
				if tc.Arguments != "" {
					_ = json.Unmarshal([]byte(tc.Arguments), &input)
				}
				blocks = append(blocks, anthropicContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: input,
				})
			}
			role := m.Role
			if role != "assistant" {
				role = "user"
			}
			messages = append(messages, anthropicMessage{Role: role, Content: blocks})
		}
	}
	flushToolResults()
	out.System = strings.Join(systemParts, "\n\n")
	out.Messages = messages

	for _, t := range req.Tools {
		out.Tools = append(out.Tools, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.Parameters,
		})
	}
	if len(out.Tools) > 0 {
		out.ToolChoice = &anthropicToolChoice{Type: "auto"}
	}
	if req.Temperature != 0 {
		temp := req.Temperature
		out.Temperature = &temp
	}
	return out
}

func translateAnthropicResponse(resp *anthropicResponse) *ChatResponse {
	out := &ChatResponse{}
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			out.Content += block.Text
		case "tool_use":
			args := "{}"
			if block.Input != nil {
				if raw, err := json.Marshal(block.Input); err == nil {
					args = string(raw)
				}
			}
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: args,
			})
		}
	}
	out.FinishReason = mapStopReason(resp.StopReason)
	out.Usage = TokenUsage{
		PromptTokens:     resp.Usage.InputTokens,
		CompletionTokens: resp.Usage.OutputTokens,
		TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
	}
	return out
}

// mapStopReason normalizes Anthropic stop reasons to the provider-agnostic
// set used across adapters.
func mapStopReason(reason string) string {
	switch reason {
	case "end_turn":
		return "stop"
	case "tool_use":
		return "tool_calls"
	case "max_tokens":
		return "length"
	default:
		return reason
	}
}

// --- Provider implementation -----------------------------------------------

func (p *AnthropicProvider) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	wire := p.buildRequest(req, false)

	var resp anthropicResponse
	if err := doJSON(ctx, p.client, "POST", p.baseURL+"/v1/messages", p.headers(), wire, &resp); err != nil {
		return nil, orjerrors.Internal("anthropic: message request failed", err)
	}
	if resp.Error != nil {
		return nil, orjerrors.Internal("anthropic: "+resp.Error.Message, nil)
	}
	return translateAnthropicResponse(&resp), nil
}

func (p *AnthropicProvider) StreamChatCompletion(ctx context.Context, req ChatRequest) (<-chan ChatChunk, error) {
	wire := p.buildRequest(req, true)

	// Open the stream synchronously so connection/5xx failures surface as the
	// returned error (and the Gateway can fail over) before any chunk is sent.
	//nolint:bodyclose // ownership transfers to consumeStream, which closes resp.Body (goroutine-stream false positive)
	resp, err := openStream(ctx, p.client, "POST", p.baseURL+"/v1/messages", p.headers(), wire)
	if err != nil {
		return nil, orjerrors.Internal("anthropic: stream request failed", err)
	}

	ch := make(chan ChatChunk)
	go func() {
		defer close(ch)
		if err := p.consumeStream(ctx, resp, ch); err != nil {
			// TAD §2.7's ChatChunk carries no error field: a mid-stream failure
			// surfaces as a short stream that closes without a finish chunk.
			slog.Warn("anthropic.stream_interrupted", "err", err)
		}
	}()
	return ch, nil
}

// consumeStream drains resp, forwarding SSE deltas to ch. It owns resp.Body
// and closes it on every exit path (TAD §2.7 streaming contract).
func (p *AnthropicProvider) consumeStream(ctx context.Context, resp *http.Response, ch chan<- ChatChunk) error {
	defer func() { _ = resp.Body.Close() }()

	var currentToolID, currentToolName string
	return streamEvents(ctx, resp, func(data []byte) error {
		var ev anthropicStreamEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			return orjerrors.Internal("anthropic: malformed stream event", err)
		}
		switch ev.Type {
		case "content_block_start":
			if ev.ContentBlock != nil && ev.ContentBlock.Type == "tool_use" {
				currentToolID = ev.ContentBlock.ID
				currentToolName = ev.ContentBlock.Name
			}
		case "content_block_delta":
			if ev.Delta == nil {
				return nil
			}
			switch ev.Delta.Type {
			case "text_delta":
				select {
				case ch <- ChatChunk{Content: ev.Delta.Text}:
				case <-ctx.Done():
					return ctx.Err()
				}
			case "input_json_delta":
				select {
				case ch <- ChatChunk{ToolCalls: []ToolCall{{
					ID:        currentToolID,
					Name:      currentToolName,
					Arguments: ev.Delta.PartialJSON,
				}}}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		case "content_block_stop":
			currentToolID, currentToolName = "", ""
		case "message_delta":
			if ev.Delta != nil && ev.Delta.StopReason != "" {
				select {
				case ch <- ChatChunk{FinishReason: mapStopReason(ev.Delta.StopReason)}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
		return nil
	})
}

type anthropicDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
}

type anthropicStreamEvent struct {
	Type         string                 `json:"type"`
	ContentBlock *anthropicContentBlock `json:"content_block,omitempty"`
	Delta        *anthropicDelta        `json:"delta,omitempty"`
}

func (p *AnthropicProvider) headers() map[string]string {
	return map[string]string{
		"x-api-key":         p.apiKey,
		"anthropic-version": anthropicAPIVersion,
	}
}
