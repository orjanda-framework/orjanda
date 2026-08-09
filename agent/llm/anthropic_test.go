package llm_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/orjanda-framework/orjanda/agent/llm"
)

func TestAnthropicProviderContract(t *testing.T) {
	runContractSuite(t, providerContract{
		name: "anthropic",
		newProvider: func(baseURL string) llm.Provider {
			return llm.NewAnthropicProvider(llm.ProviderOptions{
				APIKey:  "test-key",
				Model:   "test-model",
				BaseURL: baseURL,
			})
		},
		chatBody: func(content, finish string) string {
			stop := "end_turn"
			if finish == "tool_calls" {
				stop = "tool_use"
			}
			return fmt.Sprintf(`{
				"id": "msg_1",
				"type": "message",
				"role": "assistant",
				"content": [{"type": "text", "text": %q}],
				"stop_reason": %q,
				"usage": {"input_tokens": 10, "output_tokens": 5}
			}`, content, stop)
		},
		toolBody: func(id, name, args string) string {
			var input map[string]any
			_ = json.Unmarshal([]byte(args), &input)
			return fmt.Sprintf(`{
				"id": "msg_2",
				"type": "message",
				"role": "assistant",
				"content": [{"type": "tool_use", "id": %q, "name": %q, "input": %s}],
				"stop_reason": "tool_use",
				"usage": {"input_tokens": 10, "output_tokens": 5}
			}`, id, name, mustJSON(t, input))
		},
		streamBody: func(chunks []string) string {
			var sb []byte
			writeEvent := func(payload map[string]any) {
				ev, _ := json.Marshal(payload)
				sb = append(sb, "data: "...)
				sb = append(sb, ev...)
				sb = append(sb, '\n', '\n')
			}
			writeEvent(map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_1"}})
			writeEvent(map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
			for _, c := range chunks {
				writeEvent(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": c}})
			}
			writeEvent(map[string]any{"type": "content_block_stop", "index": 0})
			writeEvent(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}})
			writeEvent(map[string]any{"type": "message_stop"})
			return string(sb)
		},
		wireCheck: func(t *testing.T, body map[string]any) {
			t.Helper()
			if body["system"] != "Be brief." {
				t.Errorf("Anthropic system = %v, want %q (hoisted out of messages)", body["system"], "Be brief.")
			}
			tools, _ := body["tools"].([]any)
			for _, raw := range tools {
				tool, _ := raw.(map[string]any)
				if tool["input_schema"] == nil {
					t.Errorf("tool %v missing input_schema", tool)
				}
			}
		},
		toolWireCheck: func(t *testing.T, body map[string]any) {
			t.Helper()
			tools, _ := body["tools"].([]any)
			for _, raw := range tools {
				tool, _ := raw.(map[string]any)
				if tool["name"] != "create_employee" {
					t.Errorf("tool name = %v, want create_employee", tool["name"])
				}
				if tool["input_schema"] == nil {
					t.Errorf("tool %v missing input_schema", tool)
				}
			}
		},
	})
}

func TestAnthropicProviderStructuredOutput(t *testing.T) {
	p := llm.NewAnthropicProvider(llm.ProviderOptions{Model: "test-model"})
	if p.SupportsStructuredOutput() {
		t.Error("SupportsStructuredOutput() = true, want false (Anthropic has no native structured output)")
	}
	if got := p.ModelInfo(); !got.SupportsTools || got.SupportsStructuredOutput {
		t.Errorf("ModelInfo() = %+v, want SupportsTools only", got)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}
