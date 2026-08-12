package llm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/orjanda-framework/orjanda/agent/llm"
)

// The openai_compatible adapter shares the OpenAI chat-completions wire format,
// so it must pass the identical contract suite (Plan Phase 7 completion
// criterion, extended to compatible endpoints).
func TestOpenAICompatibleProviderContract(t *testing.T) {
	runContractSuite(t, providerContract{
		name: "openai_compatible",
		newProvider: func(baseURL string) llm.Provider {
			p, err := llm.NewOpenAICompatibleProvider(llm.ProviderOptions{
				APIKey:  "test-key",
				Model:   "test-model",
				BaseURL: baseURL,
			})
			if err != nil {
				t.Fatalf("NewOpenAICompatibleProvider: %v", err)
			}
			return p
		},
		chatBody: func(content, finish string) string {
			return fmt.Sprintf(`{
				"id": "chatcmpl_1",
				"object": "chat.completion",
				"model": "test-model",
				"choices": [{
					"index": 0,
					"message": {"role": "assistant", "content": %q},
					"finish_reason": %q
				}],
				"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
			}`, content, finish)
		},
		toolBody: func(id, name, args string) string {
			return fmt.Sprintf(`{
				"id": "chatcmpl_2",
				"object": "chat.completion",
				"model": "test-model",
				"choices": [{
					"index": 0,
					"message": {
						"role": "assistant",
						"content": "",
						"tool_calls": [{
							"id": %q,
							"type": "function",
							"function": {"name": %q, "arguments": %q}
						}]
					},
					"finish_reason": "tool_calls"
				}],
				"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
			}`, id, name, args)
		},
		streamBody: func(chunks []string) string {
			var sb []byte
			for _, c := range chunks {
				ev, _ := json.Marshal(map[string]any{
					"choices": []map[string]any{{
						"index": 0,
						"delta": map[string]any{"content": c},
					}},
				})
				sb = append(sb, "data: "...)
				sb = append(sb, ev...)
				sb = append(sb, '\n', '\n')
			}
			final, _ := json.Marshal(map[string]any{
				"choices": []map[string]any{{
					"index":         0,
					"delta":         map[string]any{},
					"finish_reason": "stop",
				}},
			})
			sb = append(sb, "data: "...)
			sb = append(sb, final...)
			sb = append(sb, '\n', '\n')
			sb = append(sb, "data: [DONE]\n\n"...)
			return string(sb)
		},
		wireCheck: func(t *testing.T, body map[string]any) {
			t.Helper()
			msgs, _ := body["messages"].([]any)
			if len(msgs) != 2 {
				t.Errorf("openai_compatible messages = %d entries, want 2 (system kept in messages)", len(msgs))
			}
			tools, _ := body["tools"].([]any)
			for _, raw := range tools {
				tool, _ := raw.(map[string]any)
				if tool["type"] != "function" {
					t.Errorf("tool type = %v, want function", tool["type"])
				}
			}
		},
		toolWireCheck: func(t *testing.T, body map[string]any) {
			t.Helper()
			tools, _ := body["tools"].([]any)
			for _, raw := range tools {
				tool, _ := raw.(map[string]any)
				if tool["type"] != "function" {
					t.Errorf("tool type = %v, want function", tool["type"])
				}
				fn, _ := tool["function"].(map[string]any)
				if fn["name"] != "create_employee" {
					t.Errorf("tool function name = %v, want create_employee", fn["name"])
				}
			}
		},
	})
}

// TestOpenAICompatibleProviderRequiresBaseURL verifies the one hard
// configuration requirement of the compatible adapter: an http(s) endpoint.
func TestOpenAICompatibleProviderRequiresBaseURL(t *testing.T) {
	if _, err := llm.NewOpenAICompatibleProvider(llm.ProviderOptions{Model: "test-model"}); err == nil {
		t.Error("expected error when base_url is empty")
	}
	if _, err := llm.NewOpenAICompatibleProvider(llm.ProviderOptions{Model: "test-model", BaseURL: "not-a-url"}); err == nil {
		t.Error("expected error when base_url is not an http(s) endpoint")
	}
}

// TestOpenAICompatibleProviderAuth verifies the keyless-by-default behavior:
// requests authenticate only when api_key is set, unless auth explicitly
// overrides the mode.
func TestOpenAICompatibleProviderAuth(t *testing.T) {
	cases := []struct {
		name       string
		apiKey     string
		auth       llm.AuthMode
		wantHeader bool
	}{
		{"no key, default", "", "", false},
		{"no key, bearer_if_key", "", llm.AuthBearerIfKey, false},
		{"key, default", "sk-test", "", true},
		{"key, bearer_if_key", "sk-test", llm.AuthBearerIfKey, true},
		{"key, none", "sk-test", llm.AuthNone, false},
		{"key, bearer", "sk-test", llm.AuthBearer, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotAuth string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("Authorization")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"x","choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"total_tokens":1}}`))
			}))
			defer srv.Close()

			p, err := llm.NewOpenAICompatibleProvider(llm.ProviderOptions{
				APIKey:  tc.apiKey,
				Model:   "test-model",
				BaseURL: srv.URL,
				Auth:    tc.auth,
			})
			if err != nil {
				t.Fatalf("NewOpenAICompatibleProvider: %v", err)
			}
			if _, err := p.ChatCompletion(context.Background(), llm.ChatRequest{
				Messages: []llm.Message{{Role: "user", Content: "hi"}},
			}); err != nil {
				t.Fatalf("ChatCompletion: %v", err)
			}
			if got := (gotAuth != ""); got != tc.wantHeader {
				t.Errorf("Authorization header present = %v (value %q), want %v", got, gotAuth, tc.wantHeader)
			}
		})
	}
}

// TestOpenAICompatibleProviderCapabilities verifies per-instance capability
// signaling: compatible servers vary in tool and structured-output support.
func TestOpenAICompatibleProviderCapabilities(t *testing.T) {
	disableTools, disableStructured := false, false

	p, err := llm.NewOpenAICompatibleProvider(llm.ProviderOptions{
		Model:            "test-model",
		BaseURL:          "http://localhost:11434/v1",
		ToolCalling:      &disableTools,
		StructuredOutput: &disableStructured,
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider: %v", err)
	}
	if p.SupportsToolCalling() {
		t.Error("SupportsToolCalling() = true, want false after override")
	}
	if p.SupportsStructuredOutput() {
		t.Error("SupportsStructuredOutput() = true, want false after override")
	}
	mi := p.ModelInfo()
	if mi.SupportsTools || mi.SupportsStructuredOutput {
		t.Errorf("ModelInfo() = %+v, want both capability flags false", mi)
	}
	if mi.Name != "test-model" {
		t.Errorf("ModelInfo().Name = %q, want test-model", mi.Name)
	}

	def, err := llm.NewOpenAICompatibleProvider(llm.ProviderOptions{
		Model:   "test-model",
		BaseURL: "http://localhost:11434/v1",
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider: %v", err)
	}
	if !def.SupportsToolCalling() || !def.SupportsStructuredOutput() {
		t.Error("default capabilities must be tool calling + structured output")
	}
}
