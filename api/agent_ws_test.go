package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// agentWSSetup builds the sqlite-backed engine stack used by every agent WS
// test, mirroring the site wiring (TAD §11.1/§12.1): Registry → tables →
// perm.Engine → Document Engine → ToolRegistry.
func agentWSSetup(t *testing.T) (schema.Registry, perm.Engine, *document.Engine) {
	t.Helper()
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
	return reg, permEngine, docEngine
}

// startAgentWS mounts the agent WS route (the same RouterOptions wiring as the
// site, api/router.go) and returns the ws:// URL and the JWT provider.
func startAgentWS(t *testing.T, base runtime.Options, corsOrigins []string) (string, *auth.JWTProvider) {
	t.Helper()
	jwtProvider := auth.NewJWTProvider([]byte("test-agent-ws-secret-123456"), 15*time.Minute, 7*24*time.Hour)
	router := api.NewRouter(api.RouterOptions{
		CORSOrigins:  corsOrigins,
		AuthProvider: jwtProvider,
		RateLimit:    1000,
		RateWindow:   time.Minute,
		Cache:        cache.NewLRUStore(100),
		PermEngine:   base.PermEngine,
		Registry:     base.Registry,
		DocEngine:    base.DocEngine,
		AgentRuntime: &base,
	})
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/agent/stream", jwtProvider
}

// wsScriptedProvider extends scriptedProvider with a one-shot gate: the first
// ChatCompletion blocks until gate is closed and signals reached first, making
// the turn queue's saturation deterministic in the overflow test.
type wsScriptedProvider struct {
	scriptedProvider
	gate    chan struct{}
	reached chan struct{}
	gated   bool
}

func (p *wsScriptedProvider) ChatCompletion(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	if p.gated {
		p.gated = false
		close(p.reached)
		select {
		case <-p.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return p.scriptedProvider.ChatCompletion(ctx, req)
}

// agentWSRead reads one server event, failing the test on error.
func agentWSRead(t *testing.T, ctx context.Context, conn *websocket.Conn) map[string]any {
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

// agentWSSend writes a client message (TAD §6.2).
func agentWSSend(t *testing.T, ctx context.Context, conn *websocket.Conn, v any) {
	t.Helper()
	raw, _ := json.Marshal(v)
	if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
		t.Fatalf("write: %v", err)
	}
}

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

// agentWSOptions builds runtime.Options with a compiled ToolRegistry.
func agentWSOptions(t *testing.T, reg schema.Registry, permEngine perm.Engine, docEngine *document.Engine, provider llm.Provider, policy safety.SafetyPolicy) runtime.Options {
	t.Helper()
	tr := tools.NewToolRegistry(permEngine, nil)
	if err := tr.Compile(reg); err != nil {
		t.Fatalf("tool compile: %v", err)
	}
	return runtime.Options{
		Provider:   provider,
		PermEngine: permEngine,
		Registry:   reg,
		DocEngine:  docEngine,
		Safety:     safety.NewLayer(policy, cache.NewLRUStore(100)),
		Tools:      tr,
	}
}

// TestAgentStreamWebSocket_OriginPolicy verifies the upgrade origin check
// mirrors the CORS allowlist (REVIEW-2026-08-12 finding 13): non-browser
// clients (no Origin) and same-origin upgrades always pass, an allowlisted
// origin passes, and a cross-origin page gets 403 before the handshake.
func TestAgentStreamWebSocket_OriginPolicy(t *testing.T) {
	reg, permEngine, docEngine := agentWSSetup(t)
	base := agentWSOptions(t, reg, permEngine, docEngine, &scriptedProvider{},
		safety.SafetyPolicy{AutoApprove: []string{"read", "search", "list"}})
	wsURL, jwtProvider := startAgentWS(t, base, []string{"http://allowed.example"})
	token := generateToken(t, jwtProvider, "usr_admin", "admin@localhost", []string{"System Administrator"})

	dial := func(t *testing.T, origin string) (*websocket.Conn, *http.Response, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		h := http.Header{"Authorization": {"Bearer " + token}}
		if origin != "" {
			h.Set("Origin", origin)
		}
		return websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: h})
	}
	expectUpgrade := func(t *testing.T, origin string) {
		t.Helper()
		conn, resp, err := dial(t, origin)
		if err != nil {
			t.Fatalf("origin %q rejected: %v", origin, err)
		}
		if resp.StatusCode != http.StatusSwitchingProtocols {
			t.Fatalf("origin %q: status = %d, want 101", origin, resp.StatusCode)
		}
		conn.Close(websocket.StatusNormalClosure, "")
	}

	t.Run("no origin passes", func(t *testing.T) { expectUpgrade(t, "") })

	u, err := url.Parse(wsURL)
	if err != nil {
		t.Fatalf("parse ws url: %v", err)
	}
	t.Run("same-origin passes", func(t *testing.T) { expectUpgrade(t, "http://"+u.Host) })
	t.Run("allowlisted origin passes", func(t *testing.T) { expectUpgrade(t, "http://allowed.example") })

	t.Run("cross-origin rejected", func(t *testing.T) {
		conn, resp, err := dial(t, "http://evil.example")
		if err == nil {
			conn.Close(websocket.StatusNormalClosure, "")
			t.Fatal("cross-origin dial succeeded, want 403")
		}
		if resp == nil || resp.StatusCode != http.StatusForbidden {
			t.Fatalf("cross-origin status = %v, want 403", resp)
		}
	})
}

// TestAgentStreamWebSocket_WildcardOrigin verifies "*" in the allowlist admits
// any browser origin (the CORS middleware's wildcard semantics, PRD §12.2).
func TestAgentStreamWebSocket_WildcardOrigin(t *testing.T) {
	reg, permEngine, docEngine := agentWSSetup(t)
	base := agentWSOptions(t, reg, permEngine, docEngine, &scriptedProvider{},
		safety.SafetyPolicy{AutoApprove: []string{"read", "search", "list"}})
	wsURL, jwtProvider := startAgentWS(t, base, []string{"*"})
	token := generateToken(t, jwtProvider, "usr_admin", "admin@localhost", []string{"System Administrator"})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": {"Bearer " + token},
		"Origin":        {"http://evil.example"},
	}})
	if err != nil {
		t.Fatalf("wildcard origin rejected: %v", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want 101", resp.StatusCode)
	}
	conn.Close(websocket.StatusNormalClosure, "")
}

// TestAgentStreamWebSocket_ApprovalRoundTrip verifies the TAD §12.3 round trip
// over the wire: a create behind RequireApproval emits approval_required with
// action_id + policy_reason, no tool executes before the human answers, and
// the approved write lands in the Document Engine afterwards.
func TestAgentStreamWebSocket_ApprovalRoundTrip(t *testing.T) {
	reg, permEngine, docEngine := agentWSSetup(t)
	provider := &scriptedProvider{
		responses: []*llm.ChatResponse{
			{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "create_task", Arguments: `{"title":"Approved Task"}`}}},
			{Content: "Task created."},
		},
	}
	base := agentWSOptions(t, reg, permEngine, docEngine, provider,
		safety.SafetyPolicy{
			AutoApprove:     []string{"read", "search", "list"},
			RequireApproval: []string{"create"},
		})
	wsURL, jwtProvider := startAgentWS(t, base, []string{"*"})
	token := generateToken(t, jwtProvider, "usr_admin", "admin@localhost", []string{"System Administrator"})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": {"Bearer " + token},
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	agentWSSend(t, ctx, conn, map[string]any{"type": "message", "text": "create a task"})

	// The approval gate must fire before any tool execution or token.
	var actionID string
	for {
		evt := agentWSRead(t, ctx, conn)
		switch evt["type"] {
		case "approval_required":
			actionID, _ = evt["action_id"].(string)
			if actionID == "" {
				t.Fatal("approval_required without action_id")
			}
			details, _ := evt["details"].(map[string]any)
			if got := details["policy_reason"]; got != "RequireApproval" {
				t.Fatalf("policy_reason = %v, want RequireApproval", got)
			}
		case "tool_end":
			t.Fatalf("tool_end %v arrived before approval", evt["tool"])
		case "token":
			t.Fatal("token arrived before approval")
		}
		if actionID != "" {
			break
		}
	}

	agentWSSend(t, ctx, conn, map[string]any{"type": "approval_response", "action_id": actionID, "approved": true})

	// The write executes only after approval, then the model answers.
	gotToolEnd := false
	for {
		evt := agentWSRead(t, ctx, conn)
		switch evt["type"] {
		case "tool_start":
			if evt["tool"] != "create_task" {
				t.Fatalf("tool_start tool = %v", evt["tool"])
			}
		case "tool_end":
			if evt["tool"] != "create_task" {
				t.Fatalf("tool_end tool = %v", evt["tool"])
			}
			if success, _ := evt["success"].(bool); !success {
				t.Fatal("tool_end success = false")
			}
			gotToolEnd = true
		case "token":
			if !gotToolEnd {
				t.Fatal("token arrived before tool_end")
			}
			if !strings.Contains(evt["content"].(string), "Task") {
				t.Fatalf("token content = %v", evt["content"])
			}
			rows, err := docEngine.List(auth.NewContext(ctx, auth.Identity{
				UserID: "usr_admin",
				Email:  "admin@localhost",
				Roles:  []string{"System Administrator"},
			}), "Task", document.ListOpts{})
			if err != nil {
				t.Fatalf("list tasks: %v", err)
			}
			if len(rows) != 1 {
				t.Fatalf("task count = %d, want 1 (approved write must land)", len(rows))
			}
			if got := rows[0]["title"]; got != "Approved Task" {
				t.Fatalf("task title = %v", got)
			}
			return
		}
	}
}

// maxQueuedTurnsTest mirrors the handler's per-connection queue cap
// (api.maxQueuedTurns). Kept in sync with api/agent.go.
const maxQueuedTurnsTest = 16

// TestAgentStreamWebSocket_TurnQueueOverflow verifies the per-connection turn
// queue is bounded (REVIEW-2026-08-12 finding 13): with the worker pinned on
// one blocked turn, exactly maxQueuedTurnsTest messages buffer and the excess
// is dropped with an overflow event instead of spawning unbounded goroutines.
func TestAgentStreamWebSocket_TurnQueueOverflow(t *testing.T) {
	reg, permEngine, docEngine := agentWSSetup(t)
	gate := make(chan struct{})
	provider := &wsScriptedProvider{gated: true, gate: gate, reached: make(chan struct{})}
	base := agentWSOptions(t, reg, permEngine, docEngine, provider,
		safety.SafetyPolicy{AutoApprove: []string{"read", "search", "list"}})
	wsURL, jwtProvider := startAgentWS(t, base, []string{"*"})
	token := generateToken(t, jwtProvider, "usr_admin", "admin@localhost", []string{"System Administrator"})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": {"Bearer " + token},
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// Pin the worker on one turn so queue saturation is deterministic.
	agentWSSend(t, ctx, conn, map[string]any{"type": "message", "text": "turn"})
	select {
	case <-provider.reached:
	case <-ctx.Done():
		t.Fatal("worker never reached the gated LLM call")
	}

	// One turn is executing and maxQueuedTurns are buffered; every message
	// beyond that must be dropped with an overflow event.
	const extra = maxQueuedTurnsTest + 1 // 16 buffer + 1 overflow
	for i := 0; i < extra; i++ {
		agentWSSend(t, ctx, conn, map[string]any{"type": "message", "text": "turn"})
	}

	drops := 0
	for drops < 1 {
		evt := agentWSRead(t, ctx, conn)
		if evt["type"] == "tool_end" && evt["content"] == "too many pending requests; try again" {
			drops++
		}
	}

	close(gate) // release the blocked turn; the buffered turns drain serially

	tokens := 0
	for tokens < maxQueuedTurnsTest+1 { // 1 gated turn + 16 buffered turns
		evt := agentWSRead(t, ctx, conn)
		if evt["type"] == "token" {
			tokens++
		}
	}

	reqs := provider.allRequests()
	if want := maxQueuedTurnsTest + 1; len(reqs) != want {
		t.Fatalf("LLM requests = %d, want %d", len(reqs), want)
	}
	if drops != 1 {
		t.Fatalf("overflow drops = %d, want 1", drops)
	}
}
