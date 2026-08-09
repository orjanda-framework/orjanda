package llm_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/orjanda-framework/orjanda/agent/llm"
)

func TestOpenAIProviderContract(t *testing.T) {
	runContractSuite(t, providerContract{
		name: "openai",
		newProvider: func(baseURL string) llm.Provider {
			return llm.NewOpenAIProvider(llm.ProviderOptions{
				APIKey:  "test-key",
				Model:   "test-model",
				BaseURL: baseURL,
			})
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
				t.Errorf("OpenAI messages = %d entries, want 2 (system kept in messages)", len(msgs))
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

func TestOpenAIProviderStructuredOutput(t *testing.T) {
	p := llm.NewOpenAIProvider(llm.ProviderOptions{Model: "test-model"})
	if !p.SupportsStructuredOutput() {
		t.Error("SupportsStructuredOutput() = false, want true")
	}
	if got := p.ModelInfo(); !got.SupportsStructuredOutput || !got.SupportsTools {
		t.Errorf("ModelInfo() = %+v, want both capability flags true", got)
	}
}
