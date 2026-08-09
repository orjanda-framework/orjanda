// Package llm defines the llm.Provider abstraction and implements OpenAI and
// Anthropic adapters with streaming, tool-calling, structured-output support,
// failover, circuit-breaking, and token tracking (TAD §2.6–§2.7, PRD §26).
package llm

import "context"

// Provider is the interface every LLM backend must implement. It is the
// framework's single abstraction over chat-completion providers; the Agent
// Runtime (Phase 8) and the Gateway (failover/circuit-breaker, this package)
// consume only this interface. See TAD §2.7 and PRD §26.1.
type Provider interface {
	// ChatCompletion performs a complete, non-streaming chat round trip.
	ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error)

	// StreamChatCompletion performs a streaming chat round trip. The initial
	// request is made synchronously so connection-level failures are returned
	// as the error; once established, partial results arrive on the channel,
	// which the provider closes when the stream completes. A stream that fails
	// mid-flight is closed early (the TAD §2.7 contract carries no per-chunk
	// error field, so callers must treat a short stream as an error).
	StreamChatCompletion(ctx context.Context, req ChatRequest) (<-chan ChatChunk, error)

	// SupportsToolCalling reports whether the provider can emit and consume
	// function-call tool definitions.
	SupportsToolCalling() bool

	// SupportsStructuredOutput reports whether the provider can constrain
	// output to a JSON Schema via ChatRequest.ResponseFormat.
	SupportsStructuredOutput() bool

	// ModelInfo returns static capabilities for the provider's configured model.
	ModelInfo() ModelInfo
}

// ChatRequest is the provider-agnostic chat completion request.
// See TAD §2.7 and PRD §26.1.
type ChatRequest struct {
	// Model overrides the provider's configured default model. Empty = default.
	Model string
	// Messages is the full conversation history in order.
	Messages []Message
	// Tools is the set of tool definitions to expose to the model.
	Tools []ToolDefinition
	// Temperature controls sampling. 0 = deterministic (default).
	Temperature float64
	// MaxTokens caps the completion length. 0 = provider default.
	MaxTokens int
	// ResponseFormat constrains output to a JSON Schema. Non-nil only in
	// Plan-and-Execute mode (TAD §11.3). Providers that do not support it
	// (SupportsStructuredOutput() == false) ignore it.
	ResponseFormat *JSONSchemaFormat
}

// Message is a single conversation turn in the provider-agnostic format.
// Roles are "system", "user", "assistant", and "tool".
type Message struct {
	// Role identifies the speaker: "system" | "user" | "assistant" | "tool".
	Role string
	// Content is the plain-text body of the message.
	Content string
	// Name is the tool name for "tool" role messages.
	Name string
	// ToolCallID ties a "tool" role message to the assistant's tool call.
	ToolCallID string
	// ToolCalls appears on "assistant" messages that invoke tools.
	ToolCalls []ToolCall
}

// ToolDefinition is the provider-agnostic JSON Schema description of a tool.
// It is the exact shape ToolRegistry.ForIdentity returns (TAD §10).
type ToolDefinition struct {
	Name        string
	Description string
	// Parameters is a JSON Schema object ("type": "object", "properties",
	// "required", ...). See TAD §10.2 for the field-mapping rules.
	Parameters map[string]any
}

// ToolCall is a function invocation requested by the model.
type ToolCall struct {
	// ID is the provider-assigned invocation id, echoed back on the tool result.
	ID string
	// Name is the tool name the model chose to invoke.
	Name string
	// Arguments is the JSON-encoded argument map the model produced.
	Arguments string
}

// JSONSchemaFormat constrains model output to a named JSON Schema (TAD §11.3).
type JSONSchemaFormat struct {
	Name   string
	Schema map[string]any
}

// ChatResponse is the provider-agnostic completion result.
// See PRD §26.1.
type ChatResponse struct {
	// Content is the model's text output (empty when ToolCalls is non-empty).
	Content string
	// ToolCalls is the set of function invocations the model requested.
	ToolCalls []ToolCall
	// Usage reports token consumption for this completion.
	Usage TokenUsage
	// FinishReason is a provider-agnostic stop cause: "stop", "length",
	// "tool_calls", "content_filter", ...
	FinishReason string
}

// ChatChunk is a partial streaming result.
type ChatChunk struct {
	// Content is a text delta.
	Content string
	// ToolCalls carries incremental tool-call fragments (argument text arrives
	// across multiple chunks and must be accumulated by the caller).
	ToolCalls []ToolCall
	// FinishReason is set on the final chunk when the model signals a stop.
	FinishReason string
}

// TokenUsage tracks token consumption for a single completion.
type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// ModelInfo describes the static capabilities of a configured model.
type ModelInfo struct {
	// Name is the provider model identifier.
	Name string
	// SupportsTools mirrors Provider.SupportsToolCalling.
	SupportsTools bool
	// SupportsStructuredOutput mirrors Provider.SupportsStructuredOutput.
	SupportsStructuredOutput bool
}
