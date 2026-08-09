package llm_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/orjanda-framework/orjanda/agent/llm"
)

// providerContract describes a provider adapter so the exact same assertion
// suite runs against both OpenAI and Anthropic (Plan Phase 7 completion
// criterion: "Both OpenAI and Anthropic pass identical contract test suite").
// Only the mock wire bodies and the wire-format request check are
// provider-specific; every assertion operates on the unified llm types.
type providerContract struct {
	name string
	// newProvider builds a provider against the given base URL.
	newProvider func(baseURL string) llm.Provider
	// chatBody returns a non-streaming response body that translates to the
	// given content and finish reason.
	chatBody func(content, finishReason string) string
	// toolBody returns a non-streaming response body producing one tool call.
	toolBody func(id, name, args string) string
	// streamBody returns the full SSE body for the given text chunks.
	streamBody func(chunks []string) string
	// wireCheck asserts provider-specific request-wire details for the chat
	// completion round trip (which includes a system message).
	wireCheck func(t *testing.T, body map[string]any)
	// toolWireCheck asserts provider-specific request-wire details for the
	// tool-calling round trip (no system message present).
	toolWireCheck func(t *testing.T, body map[string]any)
}

func runContractSuite(t *testing.T, pc providerContract) {
	t.Helper()

	t.Run("chat completion round trip", func(t *testing.T) {
		reqCh := make(chan map[string]any, 1)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode request: %v", err)
			}
			reqCh <- body
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(pc.chatBody("Hello, world!", "stop")))
		}))
		defer srv.Close()

		p := pc.newProvider(srv.URL)
		resp, err := p.ChatCompletion(context.Background(), llm.ChatRequest{
			Model: "test-model",
			Messages: []llm.Message{
				{Role: "system", Content: "Be brief."},
				{Role: "user", Content: "Say hello"},
			},
		})
		if err != nil {
			t.Fatalf("ChatCompletion: %v", err)
		}
		if resp.Content != "Hello, world!" {
			t.Errorf("Content = %q, want %q", resp.Content, "Hello, world!")
		}
		if resp.FinishReason != "stop" {
			t.Errorf("FinishReason = %q, want %q", resp.FinishReason, "stop")
		}
		if resp.Usage.TotalTokens <= 0 {
			t.Errorf("Usage.TotalTokens = %d, want > 0", resp.Usage.TotalTokens)
		}

		body := <-reqCh
		if body["model"] != "test-model" {
			t.Errorf("request model = %v, want %q", body["model"], "test-model")
		}
		msgs, ok := body["messages"].([]any)
		if !ok || len(msgs) == 0 {
			t.Fatalf("request messages = %#v, want at least 1 entry", body["messages"])
		}
		userSeen := false
		for _, raw := range msgs {
			m, _ := raw.(map[string]any)
			if m["role"] == "user" {
				userSeen = true
			}
		}
		if !userSeen {
			t.Errorf("request messages %#v lack a user turn", msgs)
		}
		if pc.wireCheck != nil {
			pc.wireCheck(t, body)
		}
	})

	t.Run("tool calling round trip", func(t *testing.T) {
		reqCh := make(chan map[string]any, 1)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode request: %v", err)
			}
			reqCh <- body
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(pc.toolBody("tool_1", "create_employee", `{"name":"Ada"}`)))
		}))
		defer srv.Close()

		p := pc.newProvider(srv.URL)
		resp, err := p.ChatCompletion(context.Background(), llm.ChatRequest{
			Messages: []llm.Message{{Role: "user", Content: "Create Ada"}},
			Tools: []llm.ToolDefinition{
				{
					Name:        "create_employee",
					Description: "Create an employee",
					Parameters: map[string]any{
						"type":       "object",
						"properties": map[string]any{"name": map[string]any{"type": "string"}},
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("ChatCompletion: %v", err)
		}
		if len(resp.ToolCalls) != 1 {
			t.Fatalf("ToolCalls = %#v, want exactly 1", resp.ToolCalls)
		}
		tc := resp.ToolCalls[0]
		if tc.Name != "create_employee" {
			t.Errorf("ToolCall.Name = %q, want %q", tc.Name, "create_employee")
		}
		if tc.ID != "tool_1" {
			t.Errorf("ToolCall.ID = %q, want %q", tc.ID, "tool_1")
		}
		if !strings.Contains(tc.Arguments, "Ada") {
			t.Errorf("ToolCall.Arguments = %q, want to contain Ada", tc.Arguments)
		}

		body := <-reqCh
		tools, ok := body["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("request tools = %#v, want 1 entry", body["tools"])
		}
		if pc.toolWireCheck != nil {
			pc.toolWireCheck(t, body)
		}
	})

	t.Run("streaming accumulates chunks", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(pc.streamBody([]string{"Hel", "lo, ", "world!"})))
		}))
		defer srv.Close()

		p := pc.newProvider(srv.URL)
		ch, err := p.StreamChatCompletion(context.Background(), llm.ChatRequest{
			Messages: []llm.Message{{Role: "user", Content: "Hi"}},
		})
		if err != nil {
			t.Fatalf("StreamChatCompletion: %v", err)
		}
		var content strings.Builder
		var finishSeen bool
		for c := range ch {
			content.WriteString(c.Content)
			if c.FinishReason != "" {
				finishSeen = true
			}
		}
		if got := content.String(); got != "Hello, world!" {
			t.Errorf("streamed content = %q, want %q", got, "Hello, world!")
		}
		if !finishSeen {
			t.Error("stream closed without a finish chunk")
		}
	})

	t.Run("error mapping", func(t *testing.T) {
		for _, status := range []int{400, 401, 500, 503} {
			t.Run(http.StatusText(status), func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.Error(w, "upstream boom", status)
				}))
				defer srv.Close()

				p := pc.newProvider(srv.URL)
				_, err := p.ChatCompletion(context.Background(), llm.ChatRequest{
					Messages: []llm.Message{{Role: "user", Content: "Hi"}},
				})
				if err == nil {
					t.Fatalf("expected error for status %d", status)
				}
				var httpErr *llm.HTTPStatusError
				if !errors.As(err, &httpErr) {
					t.Fatalf("err = %v, want *HTTPStatusError reachable via errors.As", err)
				}
				if httpErr.StatusCode != status {
					t.Errorf("StatusCode = %d, want %d", httpErr.StatusCode, status)
				}
			})
		}
	})

	t.Run("stream open failure surfaces synchronously", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "upstream boom", http.StatusServiceUnavailable)
		}))
		defer srv.Close()

		p := pc.newProvider(srv.URL)
		_, err := p.StreamChatCompletion(context.Background(), llm.ChatRequest{
			Messages: []llm.Message{{Role: "user", Content: "Hi"}},
		})
		if err == nil {
			t.Fatal("expected error from stream open")
		}
		var httpErr *llm.HTTPStatusError
		if !errors.As(err, &httpErr) {
			t.Fatalf("err = %v, want *HTTPStatusError reachable via errors.As", err)
		}
		if httpErr.StatusCode != 503 {
			t.Errorf("StatusCode = %d, want 503", httpErr.StatusCode)
		}
	})

	t.Run("tool calling capability and model info", func(t *testing.T) {
		p := pc.newProvider("http://unused.invalid")
		if !p.SupportsToolCalling() {
			t.Error("SupportsToolCalling() = false, want true")
		}
		if got := p.ModelInfo().Name; got == "" {
			t.Error("ModelInfo().Name is empty")
		}
	})
}
