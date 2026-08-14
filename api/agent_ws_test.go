package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/orjanda-framework/orjanda/agent/llm"
	"github.com/orjanda-framework/orjanda/agent/runtime"
	"github.com/orjanda-framework/orjanda/agent/safety"
	"github.com/orjanda-framework/orjanda/agent/tools"
	"github.com/orjanda-framework/orjanda/api"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/cache"
	"github.com/orjanda-framework/orjanda/dal/dialect/sqlite"
	"github.com/orjanda-framework/orjanda/document"
	"github.com/orjanda-framework/orjanda/event"
	"github.com/orjanda-framework/orjanda/perm"
	"github.com/orjanda-framework/orjanda/schema"
	"sync"
)

// scriptedProvider returns a fixed queue of responses and records requests.
type scriptedProvider struct {
	mu        sync.Mutex
	responses []*llm.ChatResponse
	requests  []*llm.ChatRequest
}

func (p *scriptedProvider) ChatCompletion(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, &req)
	if len(p.responses) == 0 {
		return &llm.ChatResponse{Content: "ok"}, nil
	}
	head := p.responses[0]
	p.responses = p.responses[1:]
	return head, nil
}

func (p *scriptedProvider) allRequests() []*llm.ChatRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requests
}

func (p *scriptedProvider) StreamChatCompletion(_ context.Context, req llm.ChatRequest) (<-chan llm.ChatChunk, error) {
	resp, err := p.ChatCompletion(context.Background(), req)
	if err != nil {
		return nil, err
	}
	ch := make(chan llm.ChatChunk, 1)
	ch <- llm.ChatChunk{Content: resp.Content, FinishReason: resp.FinishReason}
	close(ch)
	return ch, nil
}

func (p *scriptedProvider) SupportsToolCalling() bool      { return true }
func (p *scriptedProvider) SupportsStructuredOutput() bool { return true }
func (p *scriptedProvider) ModelInfo() llm.ModelInfo {
	return llm.ModelInfo{Name: "fake", SupportsTools: true, SupportsStructuredOutput: true}
}

// TestAgentStreamWebSocket verifies the TAD §6.2 chat WebSocket end-to-end:
// upgrade with an authenticated identity, message in, token/tool events out,
// with the runtime wired exactly as the site wires it.
func TestAgentStreamWebSocket(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	reg := schema.NewRegistry()
	if err := reg.Register("test_app", &Task{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := reg.Compile(); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if err := db.CreateTables(reg.List()); err != nil {
		t.Fatalf("create tables: %v", err)
	}
	db.RegisterDocs(reg.List())

	permEngine := perm.NewEngine(reg)
	permEngine.SetDatabase(db)
	docEngine := document.NewWithServices(db, reg, permEngine, event.NewBus(), nil)

	tr := tools.NewToolRegistry(permEngine, nil)
	if err := tr.Compile(reg); err != nil {
		t.Fatalf("tool compile: %v", err)
	}

	provider := &scriptedProvider{
		responses: []*llm.ChatResponse{
			{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "describe_document", Arguments: `{"doc_type":"Task"}`}}},
			{Content: "Task describes the unit of work."},
		},
	}

	base := runtime.Options{
		Provider:  provider,
		Registry:  reg,
		DocEngine: docEngine,
		Safety:    safety.NewLayer(safety.SafetyPolicy{AutoApprove: []string{"read", "search", "list"}}, cache.NewLRUStore(100)),
		Tools:     tr,
	}

	jwtProvider := auth.NewJWTProvider([]byte("test-agent-ws-secret-123456"), 15*time.Minute, 7*24*time.Hour)
	router := api.NewRouter(api.RouterOptions{
		CORSOrigins:  []string{"*"},
		AuthProvider: jwtProvider,
		RateLimit:    1000,
		RateWindow:   time.Minute,
		Cache:        cache.NewLRUStore(100),
		PermEngine:   permEngine,
		Registry:     reg,
		DocEngine:    docEngine,
		AgentRuntime: &base,
	})

	srv := httptest.NewServer(router)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/agent/stream"
	token := generateToken(t, jwtProvider, "usr_admin", "admin@localhost", []string{"System Administrator"})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	headers := http.Header{"Authorization": {"Bearer " + token}}
	conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	send := func(v any) {
		t.Helper()
		raw, _ := json.Marshal(v)
		if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	read := func() map[string]any {
		t.Helper()
		_, raw, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var evt map[string]any
		if err := json.Unmarshal(raw, &evt); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return evt
	}

	// Query that triggers a tool round trip: discovery tool executes, then the
	// model answers. The stream must carry tool_start/tool_end/token events.
	send(map[string]any{"type": "message", "text": "describe the Task document"})

	gotToolStart, gotToolEnd, gotToken := false, false, false
	for !gotToken {
		evt := read()
		switch evt["type"] {
		case "tool_start":
			if evt["tool"] != "describe_document" {
				t.Fatalf("tool_start tool = %v", evt["tool"])
			}
			gotToolStart = true
		case "tool_end":
			if evt["tool"] != "describe_document" {
				t.Fatalf("tool_end tool = %v", evt["tool"])
			}
			gotToolEnd = true
		case "token":
			if !strings.Contains(evt["content"].(string), "Task") {
				t.Fatalf("token content = %v", evt["content"])
			}
			gotToken = true
		}
	}
	if !gotToolStart || !gotToolEnd {
		t.Errorf("tool events missing: start=%v end=%v", gotToolStart, gotToolEnd)
	}

	// A second turn on the same connection must reuse the session (same
	// identity): the turn-2 LLM request carries turn-1's full transcript.
	// Before the finding-3 fix each message created a fresh session, so the
	// second request held only [system, user "again"].
	send(map[string]any{"type": "message", "text": "again"})
	for {
		evt := read()
		if evt["type"] == "token" {
			break
		}
	}

	reqs := provider.allRequests()
	if n := len(reqs); n != 3 {
		t.Fatalf("recorded %d LLM requests, want 3 (2 for turn 1, 1 for turn 2)", n)
	}
	last := reqs[2]
	wantMsgLen := 6 // system + turn-1 [user, assistant(toolcalls), tool, assistant] + user "again"
	if got := len(last.Messages); got != wantMsgLen {
		t.Fatalf("turn-2 request has %d messages, want %d: the session was not carried across turns", got, wantMsgLen)
	}
	if got := last.Messages[1].Content; got != "describe the Task document" {
		t.Errorf("turn-2 request missing turn-1 user message, got %q", got)
	}
	if got := last.Messages[5].Content; got != "again" {
		t.Errorf("turn-2 request missing its own user message, got %q", got)
	}
}
