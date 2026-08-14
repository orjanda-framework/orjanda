package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// wireMessage is the minimal OpenAI-compatible message shape the fake chat
// completions server decodes from each runtime request.
type wireMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type wireRequest struct {
	Messages []wireMessage `json:"messages"`
}

// TestRunAgentChatCarriesSessionAcrossTurns proves the CLI agent chat path
// (REVIEW-2026-08-12 finding 3): turns driven through runAgentChat share one
// session, so the second LLM request carries the first turn's transcript.
// Before the fix each turn created a fresh session and the second request held
// only [system, user "again"].
func TestRunAgentChatCarriesSessionAcrossTurns(t *testing.T) {
	var mu sync.Mutex
	var reqs []wireRequest

	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read llm request: %v", err)
		}
		var wreq wireRequest
		if err := json.Unmarshal(body, &wreq); err != nil {
			t.Errorf("unmarshal llm request: %v", err)
		}
		mu.Lock()
		reqs = append(reqs, wreq)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(
			`{"id":"fake","choices":[{"message":{"role":"assistant","content":"reply"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
		))
	}))
	defer llmSrv.Close()

	cfgFile := filepath.Join(t.TempDir(), "orjanda.yaml")
	dsn := filepath.Join(t.TempDir(), "chat.db")
	cfg := fmt.Sprintf(`
auth:
  jwt_secret: cli-agent-session-test-secret-0123456789-0123456789
database:
  driver: sqlite
  dsn: %s
llm:
  default_provider: openai_compatible
  providers:
    openai_compatible:
      base_url: %s
      model: fake
`, dsn, llmSrv.URL)
	if err := os.WriteFile(cfgFile, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = r.Close()
		_ = w.Close()
	})
	if _, err := fmt.Fprintf(w, "hello\nagain\n"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	if err := runAgentChat(context.Background(), siteBuilder{}, cfgFile, "admin", ""); err != nil {
		t.Fatalf("runAgentChat: %v", err)
	}

	mu.Lock()
	requests := reqs
	mu.Unlock()

	if n := len(requests); n != 2 {
		t.Fatalf("recorded %d LLM requests, want 2 (one per turn)", n)
	}
	second := requests[1]
	wantMsgLen := 4 // system + user "hello" + assistant "reply" + user "again"
	if got := len(second.Messages); got != wantMsgLen {
		t.Fatalf("turn-2 request has %d messages, want %d: the session was not carried across turns", got, wantMsgLen)
	}
	if got := second.Messages[1].Content; got != "hello" {
		t.Errorf("turn-2 request missing turn-1 user message, got %q", got)
	}
	if got := second.Messages[2].Role; got != "assistant" {
		t.Errorf("turn-2 request missing turn-1 assistant reply (role %q)", got)
	}
	if got := second.Messages[3].Content; got != "again" {
		t.Errorf("turn-2 request missing its own user message, got %q", got)
	}
}
