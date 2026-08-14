package runtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/orjanda-framework/orjanda/agent/llm"
	"github.com/orjanda-framework/orjanda/agent/runtime"
	"github.com/orjanda-framework/orjanda/agent/safety"
	"github.com/orjanda-framework/orjanda/agent/tools"
	"github.com/orjanda-framework/orjanda/audit"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/cache"
	"github.com/orjanda-framework/orjanda/dal/dialect/sqlite"
	"github.com/orjanda-framework/orjanda/document"
	"github.com/orjanda-framework/orjanda/perm"
	"github.com/orjanda-framework/orjanda/schema"
	"github.com/orjanda-framework/orjanda/workflow"
)

// --- Fixtures ----------------------------------------------------------------

type Employee struct {
	schema.BaseDocument
	FirstName string `oj:"required,label=First Name"`
	LastName  string `oj:"required,label=Last Name"`
	Email     string `oj:"required,unique"`
}

func (e *Employee) DocMeta() schema.Meta {
	return schema.Meta{
		Name: "Employee",
		Permissions: []schema.DocPermission{
			{Role: "hr_manager", Read: true, Write: true, Create: true, Delete: true},
			{Role: "employee", Read: true, Create: true},
		},
	}
}

type LeaveRequest struct {
	schema.BaseDocument
	Reason string `oj:"required"`
}

func (l *LeaveRequest) DocMeta() schema.Meta {
	return schema.Meta{
		Name: "LeaveRequest",
		Permissions: []schema.DocPermission{
			{Role: "hr_manager", Read: true, Write: true, Create: true, Delete: true},
			{Role: "employee", Read: true, Create: true},
		},
	}
}

// scriptedProvider is a Provider that returns a fixed queue of responses and
// records every ChatRequest for assertions. With repeat set, the head response
// is returned indefinitely (for loop-limit tests).
type scriptedProvider struct {
	mu        sync.Mutex
	responses []*llm.ChatResponse
	requests  []llm.ChatRequest
	failNext  error
	repeat    bool
}

func (p *scriptedProvider) ChatCompletion(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, req)
	if p.failNext != nil {
		err := p.failNext
		p.failNext = nil
		return nil, err
	}
	if len(p.responses) == 0 {
		return nil, errors.New("scripted provider: no response scripted")
	}
	if p.repeat {
		return p.responses[0], nil
	}
	r := p.responses[0]
	p.responses = p.responses[1:]
	return r, nil
}

func (p *scriptedProvider) StreamChatCompletion(context.Context, llm.ChatRequest) (<-chan llm.ChatChunk, error) {
	return nil, errors.New("not implemented in test")
}

func (p *scriptedProvider) SupportsToolCalling() bool      { return true }
func (p *scriptedProvider) SupportsStructuredOutput() bool { return true }
func (p *scriptedProvider) ModelInfo() llm.ModelInfo       { return llm.ModelInfo{Name: "fake"} }

func (p *scriptedProvider) lastRequest() llm.ChatRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.requests) == 0 {
		return llm.ChatRequest{}
	}
	return p.requests[len(p.requests)-1]
}

// recSink records emitted events.
type recSink struct {
	mu     sync.Mutex
	events []runtime.Event
}

func (s *recSink) Send(evt runtime.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, evt)
}

func (s *recSink) eventsOf(typ string) []runtime.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []runtime.Event
	for _, e := range s.events {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

// scriptedApprovals resolves every approval request from a queue.
type scriptedApprovals struct {
	mu        sync.Mutex
	responses []runtime.ApprovalResponse
	requests  []runtime.ApprovalPayload
}

func (a *scriptedApprovals) RequestApproval(_ context.Context, req runtime.ApprovalPayload) (runtime.ApprovalResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requests = append(a.requests, req)
	if len(a.responses) == 0 {
		return runtime.ApprovalResponse{ActionID: req.ActionID, Approved: false}, nil
	}
	r := a.responses[0]
	a.responses = a.responses[1:]
	r.ActionID = req.ActionID
	return r, nil
}

func toolCallResponse(name, args string) *llm.ChatResponse {
	return &llm.ChatResponse{ToolCalls: []llm.ToolCall{{ID: "call-1", Name: name, Arguments: args}}}
}

func toolCallResponseN(calls ...llm.ToolCall) *llm.ChatResponse {
	return &llm.ChatResponse{ToolCalls: calls}
}

func textResponse(text string) *llm.ChatResponse {
	return &llm.ChatResponse{Content: text, FinishReason: "stop"}
}

// --- OpenAI wire helpers (real-provider regression tests) --------------------

// wireMsg is the minimal OpenAI chat-completion message shape used to validate
// request bodies captured by the stub server.
type wireMsg struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id"`
	ToolCalls  []wireCall `json:"tool_calls"`
}

type wireCall struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// wireText renders an OpenAI chat-completion response whose assistant message
// carries only text content.
func wireText(content string) string {
	payload, _ := json.Marshal(map[string]any{
		"id": "chatcmpl_test", "object": "chat.completion", "model": "test-model",
		"choices": []map[string]any{{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": content},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
	})
	return string(payload)
}

// wireToolCalls renders an OpenAI chat-completion response whose assistant
// message declares the given tool calls.
func wireToolCalls(calls ...llm.ToolCall) string {
	tcs := make([]map[string]any, 0, len(calls))
	for _, c := range calls {
		tcs = append(tcs, map[string]any{
			"id": c.ID, "type": "function",
			"function": map[string]any{"name": c.Name, "arguments": c.Arguments},
		})
	}
	payload, _ := json.Marshal(map[string]any{
		"id": "chatcmpl_test", "object": "chat.completion", "model": "test-model",
		"choices": []map[string]any{{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": "", "tool_calls": tcs},
			"finish_reason": "tool_calls",
		}},
		"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
	})
	return string(payload)
}

// assertWellFormedTranscript verifies the wire contract real providers enforce
// (REVIEW-2026-08-12 finding 4): every tool message references a tool_call_id
// declared by a preceding assistant tool_calls message, and no assistant tool
// call has an empty id.
func assertWellFormedTranscript(t *testing.T, req int, msgs []wireMsg) {
	t.Helper()
	declared := map[string]bool{}
	for _, m := range msgs {
		switch m.Role {
		case "assistant":
			for _, tc := range m.ToolCalls {
				if tc.ID == "" {
					t.Errorf("request %d: assistant tool_call with empty id", req)
				}
				declared[tc.ID] = true
			}
		case "tool":
			if m.ToolCallID == "" {
				t.Errorf("request %d: tool message with empty tool_call_id", req)
			} else if !declared[m.ToolCallID] {
				t.Errorf("request %d: tool message references undeclared tool_call_id %q", req, m.ToolCallID)
			}
		}
	}
}

// --- Test harness ------------------------------------------------------------

type testSite struct {
	reg       schema.Registry
	db        *sqlite.DB
	perm      perm.Engine
	doc       *document.Engine
	wf        workflow.Engine
	tr        tools.ToolRegistry
	safety    *safety.Layer
	provider  *scriptedProvider
	sink      *recSink
	approvals *scriptedApprovals
}

func newTestSite(t *testing.T) *testSite {
	t.Helper()
	reg := schema.NewRegistry()
	for _, d := range []schema.Document{&Employee{}, &LeaveRequest{}} {
		if err := reg.Register("test", d); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	if err := reg.Compile(); err != nil {
		t.Fatalf("compile: %v", err)
	}

	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.CreateTables(reg.List()); err != nil {
		t.Fatalf("create tables: %v", err)
	}
	db.RegisterDocs(reg.List())

	permEngine := perm.NewEngine(reg)
	wfEngine := workflow.NewEngine(db, reg, permEngine, nil, nil)
	if err := wfEngine.Register(workflow.Definition{
		DocType: "LeaveRequest",
		States: []workflow.State{
			{Name: "Draft"}, {Name: "Submitted"}, {Name: "Approved"},
		},
		Transitions: []workflow.Transition{
			{From: "Draft", To: "Submitted", Action: "Submit", AllowedRoles: []string{"employee"}},
			{From: "Submitted", To: "Approved", Action: "Approve", AllowedRoles: []string{"hr_manager"}},
		},
	}); err != nil {
		t.Fatalf("workflow register: %v", err)
	}

	tr := tools.NewToolRegistry(permEngine, wfEngine)
	if err := tr.Compile(reg); err != nil {
		t.Fatalf("tool compile: %v", err)
	}

	return &testSite{
		reg:       reg,
		db:        db,
		perm:      permEngine,
		doc:       document.NewWithServices(db, reg, permEngine, nil, nil),
		wf:        wfEngine,
		tr:        tr,
		safety:    safety.NewLayer(safety.SafetyPolicy{AutoApprove: []string{"read", "list", "search", "create", "update"}}, cache.NewLRUStore(100)),
		provider:  &scriptedProvider{},
		sink:      &recSink{},
		approvals: &scriptedApprovals{},
	}
}

// newRuntime builds a Runtime from the site. opt mutates Options first.
func (s *testSite) newRuntime(t *testing.T, opt func(*runtime.Options)) *runtime.Runtime {
	t.Helper()
	opts := runtime.Options{
		Provider:   s.provider,
		Registry:   s.reg,
		DocEngine:  s.doc,
		Workflow:   s.wf,
		Safety:     s.safety,
		Tools:      s.tr,
		PermEngine: s.perm,
		Sink:       s.sink,
		Approvals:  s.approvals,
	}
	if opt != nil {
		opt(&opts)
	}
	rt, err := runtime.NewRuntime(opts)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	return rt
}

func hrCtx() context.Context {
	return auth.NewContext(context.Background(), auth.Identity{UserID: "u-hr", Roles: []string{"hr_manager"}})
}

// --- Tests -------------------------------------------------------------------

func TestExecuteNoToolCalls(t *testing.T) {
	s := newTestSite(t)
	rt := s.newRuntime(t, nil)
	s.provider.responses = []*llm.ChatResponse{textResponse("Hello!")}

	resp, err := rt.Execute(hrCtx(), "hi")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Content != "Hello!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello!")
	}
	if resp.SessionID == "" {
		t.Error("SessionID empty")
	}
	if resp.ToolCalls != 0 {
		t.Errorf("ToolCalls = %d, want 0", resp.ToolCalls)
	}

	// TAD §11.1: an idle session's first LLM call carries only the discovery
	// set (~3 tools), regardless of how many Documents the Registry holds.
	req := s.provider.lastRequest()
	if len(req.Tools) != 3 {
		t.Errorf("first-turn tools = %d, want 3 (discovery only)", len(req.Tools))
	}
}

func TestExecuteReActReadTool(t *testing.T) {
	s := newTestSite(t)
	rt := s.newRuntime(t, nil)

	id, err := s.doc.Create(hrCtx(), "Employee", map[string]any{
		"first_name": "Ada", "last_name": "Lovelace", "email": "ada@example.com",
	})
	if err != nil {
		t.Fatalf("seed create: %v", err)
	}

	args, _ := json.Marshal(map[string]any{"id": id})
	s.provider.responses = []*llm.ChatResponse{
		toolCallResponse("describe_document", `{"doc_type":"Employee"}`),
		toolCallResponse("get_employee", string(args)),
		textResponse("That employee is Ada Lovelace."),
	}

	resp, err := rt.Execute(hrCtx(), "who is "+id)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Content != "That employee is Ada Lovelace." {
		t.Errorf("Content = %q", resp.Content)
	}
	if resp.ToolCalls != 2 {
		t.Errorf("ToolCalls = %d, want 2 (describe + get)", resp.ToolCalls)
	}

	// The Context Manager must have attached operation tools after the
	// describe_document result (TAD §11.1 lazy attachment).
	req1 := s.provider.requests[1]
	if len(req1.Tools) <= 3 {
		t.Errorf("tools after describe = %d, want > 3 (operation tools attached)", len(req1.Tools))
	}

	// The read tool result must have been fed back as a tool message.
	req := s.provider.lastRequest()
	foundToolMsg := false
	for _, m := range req.Messages {
		if m.Role == "tool" && m.Name == "get_employee" && strings.Contains(m.Content, "ada@example.com") {
			foundToolMsg = true
		}
	}
	if !foundToolMsg {
		t.Error("get_employee tool result not present in follow-up request")
	}
}

func TestExecutePlanMode(t *testing.T) {
	s := newTestSite(t)
	rt := s.newRuntime(t, nil)

	// Discovery first (TAD §11.1), then a response with two tool calls that
	// form a data dependency (ref:0) — the ReAct→Plan-and-Execute escalation
	// signal (TAD §11.2 step 2).
	s.provider.responses = []*llm.ChatResponse{
		toolCallResponse("describe_document", `{"doc_type":"Employee"}`),
		toolCallResponseN(
			llm.ToolCall{ID: "c1", Name: "create_employee", Arguments: `{"first_name":"Grace","last_name":"Hopper","email":"grace@example.com"}`},
			llm.ToolCall{ID: "c2", Name: "get_employee", Arguments: `{"id":"ref:0.id"}`},
		),
		// Structured-output plan request (TAD §11.3).
		textResponse(`{"steps":[
			{"operation":"create_employee","args":{"first_name":"Grace","last_name":"Hopper","email":"grace@example.com"}},
			{"operation":"get_employee","args":{"id":"ref:0.id"}}
		]}`),
		// Final summary synthesis.
		textResponse("Created Grace Hopper and fetched her record."),
	}

	resp, err := rt.Execute(hrCtx(), "create Grace Hopper then fetch her record")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.ToolCalls != 2 {
		t.Errorf("ToolCalls = %d, want 2", resp.ToolCalls)
	}
	if !strings.Contains(resp.Content, "Grace Hopper") {
		t.Errorf("Content = %q, want summary mentioning Grace Hopper", resp.Content)
	}

	// The second step's ref must have resolved to the created record id.
	req := s.provider.lastRequest()
	var createdID string
	for _, m := range req.Messages {
		if m.Role == "tool" && m.Name == "create_employee" {
			var out map[string]any
			if json.Unmarshal([]byte(m.Content), &out) == nil {
				createdID, _ = out["id"].(string)
			}
		}
	}
	if createdID == "" {
		t.Fatal("create_employee result not found in transcript")
	}
	// Verify the get_employee observation references that same id.
	found := false
	for _, m := range req.Messages {
		if m.Role == "tool" && m.Name == "get_employee" && strings.Contains(m.Content, createdID) {
			found = true
		}
	}
	if !found {
		t.Errorf("get_employee result %q does not reference created id %q", req.Messages, createdID)
	}
}

// TestExecutePlanModeWireFormat is the regression test for
// REVIEW-2026-08-12 finding 4: plan-mode transcripts were malformed for real
// providers (plan dropped from the transcript, tool messages with empty
// ToolCallID), which OpenAI/Anthropic reject with HTTP 400. The plan turn is
// driven end-to-end through a real OpenAIProvider against a stub server, and
// every captured request body is verified well-formed: each tool message
// carries a tool_call_id declared by a preceding assistant tool_calls message,
// the plan is recorded as an assistant content message, and the summary call
// runs against that transcript.
func TestExecutePlanModeWireFormat(t *testing.T) {
	s := newTestSite(t)

	var (
		mu     sync.Mutex
		bodies [][]byte
		reqNum atomic.Int64
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch reqNum.Add(1) {
		case 1:
			_, _ = w.Write([]byte(wireToolCalls(llm.ToolCall{ID: "call-1", Name: "describe_document", Arguments: `{"doc_type":"Employee"}`})))
		case 2:
			_, _ = w.Write([]byte(wireToolCalls(
				llm.ToolCall{ID: "call-2", Name: "create_employee", Arguments: `{"first_name":"Grace","last_name":"Hopper","email":"grace@example.com"}`},
				llm.ToolCall{ID: "call-3", Name: "get_employee", Arguments: `{"id":"ref:0.id"}`},
			)))
		case 3:
			_, _ = w.Write([]byte(wireText(`{"steps":[
				{"operation":"create_employee","args":{"first_name":"Grace","last_name":"Hopper","email":"grace@example.com"}},
				{"operation":"get_employee","args":{"id":"ref:0.id"}}
			]}`)))
		case 4:
			_, _ = w.Write([]byte(wireText("Created Grace Hopper and fetched her record.")))
		default:
			t.Errorf("unexpected extra request to stub server")
			_, _ = w.Write([]byte(wireText("ok")))
		}
	}))
	defer srv.Close()

	provider := llm.NewOpenAIProvider(llm.ProviderOptions{Model: "test-model", BaseURL: srv.URL})
	rt := s.newRuntime(t, nil)

	resp, err := rt.Execute(hrCtx(), "create Grace Hopper then fetch her record", runtime.WithProvider(provider))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.ToolCalls != 2 {
		t.Errorf("ToolCalls = %d, want 2", resp.ToolCalls)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 4 {
		t.Fatalf("requests = %d, want 4 (describe, dependency, plan, summary)", len(bodies))
	}
	for i, body := range bodies {
		var req struct {
			Messages       []wireMsg        `json:"messages"`
			Tools          []map[string]any `json:"tools"`
			ResponseFormat map[string]any   `json:"response_format"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("request %d not valid JSON: %v\n%s", i, err, body)
		}
		assertWellFormedTranscript(t, i, req.Messages)
		if i == 2 && req.ResponseFormat == nil {
			t.Error("plan request missing response_format (TAD §11.3)")
		}
	}

	// The summary call must run against a well-formed transcript that records
	// the plan as an assistant content message.
	var last wireMsg
	for _, m := range mustDecode(t, bodies[3], struct {
		Messages []wireMsg `json:"messages"`
	}{}).Messages {
		if m.Role == "assistant" && strings.Contains(m.Content, `"operation":"create_employee"`) {
			last = m
		}
	}
	if last.Content == "" {
		t.Error("summary request does not carry the plan as an assistant content message")
	}
}

// mustDecode unmarshals a request body into the given struct value (used for
// one-off shape checks inside wire-format tests).
func mustDecode[T any](t *testing.T, body []byte, into T) T {
	t.Helper()
	if err := json.Unmarshal(body, &into); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, body)
	}
	return into
}

func TestExecutePlanRejectedBeforeExecution(t *testing.T) {
	s := newTestSite(t)
	rt := s.newRuntime(t, nil)

	// Model discovers, then claims a dependency, then produces an invalid plan
	// (unknown operation) twice. Both plan attempts are rejected wholesale, so
	// zero steps execute (TAD §11.3 guardrail).
	s.provider.responses = []*llm.ChatResponse{
		toolCallResponse("describe_document", `{"doc_type":"Employee"}`),
		toolCallResponseN(
			llm.ToolCall{ID: "c1", Name: "create_employee", Arguments: `{"first_name":"A","last_name":"B","email":"a@b.c"}`},
			llm.ToolCall{ID: "c2", Name: "get_employee", Arguments: `{"id":"ref:0.id"}`},
		),
		textResponse(`{"steps":[{"operation":"no_such_tool","args":{}}]}`),
		textResponse(`{"steps":[{"operation":"also_bad","args":{}}]}`),
	}

	resp, err := rt.Execute(hrCtx(), "do something")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(resp.Content, "No operations were performed") {
		t.Errorf("Content = %q, want rejection message", resp.Content)
	}
	// Zero side effects: no employee record exists.
	rows, err := s.doc.List(hrCtx(), "Employee", document.ListOpts{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("plan was rejected but %d employee rows exist; want 0", len(rows))
	}
}

func TestExecuteApprovalDenied(t *testing.T) {
	s := newTestSite(t)
	s.safety = safety.NewLayer(safety.SafetyPolicy{
		AutoApprove:     []string{"read", "search", "list"},
		RequireApproval: []string{"update", "create", "delete"},
	}, cache.NewLRUStore(100))
	rt := s.newRuntime(t, nil)

	// delete_* always requires approval (TAD §12.1 step 1); the gateway denies.
	s.approvals.responses = []runtime.ApprovalResponse{{Approved: false}}
	s.provider.responses = []*llm.ChatResponse{
		toolCallResponse("describe_document", `{"doc_type":"Employee"}`),
		toolCallResponse("delete_employee", `{"id":"does-not-exist"}`),
		textResponse("The deletion was denied by the user."),
	}

	resp, err := rt.Execute(hrCtx(), "delete that employee")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(resp.Content, "denied") {
		t.Errorf("Content = %q, want denial acknowledgement", resp.Content)
	}

	// approval_required event was emitted with the §12.3 payload.
	events := s.sink.eventsOf(runtime.EventApprovalRequired)
	if len(events) != 1 {
		t.Fatalf("approval_required events = %d, want 1", len(events))
	}
	if events[0].Approval == nil || events[0].Approval.Details.PolicyReason != string(safety.ReasonAlwaysRequireApproval) {
		t.Errorf("approval payload = %+v, want policy_reason AlwaysRequireApproval", events[0].Approval)
	}
	if events[0].Approval.Details.DocType != "Employee" {
		t.Errorf("DocType = %q, want Employee", events[0].Approval.Details.DocType)
	}

	// The tool must NOT have run (no tool_end success event for delete).
	for _, e := range s.sink.eventsOf(runtime.EventToolEnd) {
		if e.Tool == "delete_employee" && e.Success {
			t.Error("delete_employee reported success despite denial")
		}
	}
	if len(s.approvals.requests) != 1 {
		t.Errorf("approval requests = %d, want 1", len(s.approvals.requests))
	}
}

func TestExecuteApprovalModifySubstitutesArgs(t *testing.T) {
	s := newTestSite(t)
	s.safety = safety.NewLayer(safety.SafetyPolicy{
		AutoApprove:     []string{"read", "search", "list"},
		RequireApproval: []string{"update"},
	}, cache.NewLRUStore(100))
	rt := s.newRuntime(t, nil)

	id, err := s.doc.Create(hrCtx(), "Employee", map[string]any{
		"first_name": "Ada", "last_name": "Lovelace", "email": "ada@example.com",
	})
	if err != nil {
		t.Fatalf("seed create: %v", err)
	}

	// Human chooses Modify (PRD §38.2): the payload corrects first_name.
	s.approvals.responses = []runtime.ApprovalResponse{
		{Approved: true, Payload: map[string]any{"id": id, "first_name": "Ada King"}},
	}
	args, _ := json.Marshal(map[string]any{"id": id, "first_name": "Ada"})
	s.provider.responses = []*llm.ChatResponse{
		toolCallResponse("describe_document", `{"doc_type":"Employee"}`),
		toolCallResponse("update_employee", string(args)),
		textResponse("Updated."),
	}

	if _, err := rt.Execute(hrCtx(), "rename Ada"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	rec, err := s.doc.Read(hrCtx(), "Employee", id)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got := rec["first_name"]; got != "Ada King" {
		t.Errorf("first_name = %v, want modified payload value Ada King", got)
	}
}

func TestExecuteToolAllowlistBlocks(t *testing.T) {
	s := newTestSite(t)
	s.safety = safety.NewLayer(safety.SafetyPolicy{
		AutoApprove:   []string{"read", "list", "search"},
		ToolAllowlist: []string{"list_employee"},
	}, cache.NewLRUStore(100))
	rt := s.newRuntime(t, nil)

	s.provider.responses = []*llm.ChatResponse{
		toolCallResponse("describe_document", `{"doc_type":"Employee"}`),
		toolCallResponse("get_employee", `{"id":"x"}`),
		textResponse("OK"),
	}

	resp, err := rt.Execute(hrCtx(), "fetch x")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(resp.Content, "OK") {
		t.Errorf("Content = %q", resp.Content)
	}
	req := s.provider.lastRequest()
	for _, m := range req.Messages {
		if m.Role == "tool" && strings.Contains(m.Content, "blocked by the safety policy tool allowlist") {
			return
		}
	}
	t.Error("get_employee tool result does not report allowlist block")
}

func TestExecuteMaxStepsExceeded(t *testing.T) {
	s := newTestSite(t)
	s.provider.repeat = true
	s.provider.responses = []*llm.ChatResponse{
		toolCallResponse("list_document_types", `{}`),
	}
	rt := s.newRuntime(t, func(o *runtime.Options) { o.MaxSteps = 3 })

	_, err := rt.Execute(hrCtx(), "loop forever")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "maximum number of tool-call steps") {
		t.Errorf("error = %v, want max-steps message", err)
	}
}

func TestExecuteRateLimitExceeded(t *testing.T) {
	s := newTestSite(t)
	s.safety = safety.NewLayer(safety.SafetyPolicy{
		AutoApprove: []string{"read"},
		RateLimit:   safety.RateLimit{OperationsPerMinute: 1, Scope: "user"},
	}, cache.NewLRUStore(100))
	rt := s.newRuntime(t, nil)

	s.provider.responses = []*llm.ChatResponse{
		textResponse("one"),
		textResponse("two"),
	}

	if _, err := rt.Execute(hrCtx(), "first"); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if _, err := rt.Execute(hrCtx(), "second"); err == nil {
		t.Fatal("expected rate-limit error on second Execute, got nil")
	}
}

func TestSessionIsolationPerIdentity(t *testing.T) {
	s := newTestSite(t)
	rt := s.newRuntime(t, nil)

	s.provider.responses = []*llm.ChatResponse{
		textResponse("a"),
		textResponse("b"),
	}

	ctx1 := auth.NewContext(context.Background(), auth.Identity{UserID: "u-1"})
	ctx2 := auth.NewContext(context.Background(), auth.Identity{UserID: "u-2"})

	r1, err := rt.Execute(ctx1, "hi")
	if err != nil {
		t.Fatalf("Execute u-1: %v", err)
	}
	r2, err := rt.Execute(ctx2, "hi")
	if err != nil {
		t.Fatalf("Execute u-2: %v", err)
	}
	if r1.SessionID == r2.SessionID {
		t.Error("two identities share a session; expected isolation")
	}

	// Reusing the same identity and passing the session id reuses the session.
	ctx1b := safety.WithSession(ctx1, r1.SessionID)
	s.provider.responses = []*llm.ChatResponse{textResponse("c")}
	r3, err := rt.Execute(ctx1b, "again")
	if err != nil {
		t.Fatalf("Execute u-1 again: %v", err)
	}
	if r3.SessionID != r1.SessionID {
		t.Errorf("session id changed across calls for same identity: %q → %q", r1.SessionID, r3.SessionID)
	}
}

func TestNewRuntimeRequiresDeps(t *testing.T) {
	cases := []struct {
		name string
		opt  func(*runtime.Options)
	}{
		{"missing provider", func(o *runtime.Options) { o.Provider = nil }},
		{"missing registry", func(o *runtime.Options) { o.Registry = nil }},
		{"missing doc engine", func(o *runtime.Options) { o.DocEngine = nil }},
		{"missing safety", func(o *runtime.Options) { o.Safety = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := runtime.Options{
				Provider:  &scriptedProvider{},
				Registry:  newTestSite(t).reg,
				DocEngine: newTestSite(t).doc,
				Safety:    safety.NewLayer(safety.SafetyPolicy{}, cache.NewLRUStore(10)),
			}
			tc.opt(&opts)
			if _, err := runtime.NewRuntime(opts); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

// TestAgentWritesAreAuditedViaAgent verifies TAD §11.1's unconditional
// audit.WithAgent marking: document writes performed by the agent carry
// ViaAgent + the session id + the agent prompt, while human writes do not.
func TestAgentWritesAreAuditedViaAgent(t *testing.T) {
	s := newTestSite(t)
	alog := audit.NewInMemoryLog()
	s.doc.SetAuditLog(alog)
	rt := s.newRuntime(t, nil)

	prompt := "create Grace Hopper then fetch her record"
	s.provider.responses = []*llm.ChatResponse{
		toolCallResponse("describe_document", `{"doc_type":"Employee"}`),
		toolCallResponseN(
			llm.ToolCall{ID: "c1", Name: "create_employee", Arguments: `{"first_name":"Grace","last_name":"Hopper","email":"grace@example.com"}`},
			llm.ToolCall{ID: "c2", Name: "get_employee", Arguments: `{"id":"ref:0.id"}`},
		),
		textResponse(`{"steps":[
			{"operation":"create_employee","args":{"first_name":"Grace","last_name":"Hopper","email":"grace@example.com"}},
			{"operation":"get_employee","args":{"id":"ref:0.id"}}
		]}`),
		textResponse("done"),
	}
	resp, err := rt.Execute(hrCtx(), prompt)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	agentEntries, err := alog.Query(context.Background(), audit.QueryFilter{DocType: "Employee"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(agentEntries) == 0 {
		t.Fatal("no Employee audit entries written by agent")
	}
	for _, e := range agentEntries {
		if !e.ViaAgent {
			t.Errorf("agent write not marked via_agent: %+v", e)
		}
		if e.AgentSession != resp.SessionID {
			t.Errorf("AgentSession = %q, want %q", e.AgentSession, resp.SessionID)
		}
		if e.AgentPrompt != prompt {
			t.Errorf("AgentPrompt = %q, want %q", e.AgentPrompt, prompt)
		}
	}

	// A human write through the same engine must NOT be marked via_agent.
	if _, err := s.doc.Create(hrCtx(), "Employee", map[string]any{
		"first_name": "Ada", "last_name": "Lovelace", "email": "ada@example.com",
	}); err != nil {
		t.Fatalf("human create: %v", err)
	}
	all, _ := alog.Query(context.Background(), audit.QueryFilter{DocType: "Employee"})
	var humanEntry *audit.Entry
	for i := range all {
		if all[i].AgentSession == "" {
			humanEntry = &all[i]
		}
	}
	if humanEntry == nil {
		t.Fatal("human write not audited")
	}
	if humanEntry.ViaAgent {
		t.Error("human write marked via_agent=true; want false")
	}
}

// TestBulkCountScopedToDocType is a regression test for a bug surfaced by the
// Phase 12 Criterion 5 live leave-request flow: the session-wide target count
// from one Document type's list/search result bled into unrelated subsequent
// calls and tripped the bulk-limit approval (TAD §12.1 step 2) on a plain
// read/discovery call. The count must apply only to calls on the same DocType
// the count was recorded for.
func TestBulkCountScopedToDocType(t *testing.T) {
	s := newTestSite(t)
	hr := hrCtx()

	for i := 0; i < 6; i++ {
		_, err := s.doc.Create(hr, "Employee", map[string]any{
			"FirstName": "Emp", "LastName": fmt.Sprintf("Loy%d", i),
			"Email": fmt.Sprintf("emp%d@test.com", i),
		})
		if err != nil {
			t.Fatalf("seed employee: %v", err)
		}
	}

	rt := s.newRuntime(t, nil)
	s.provider.responses = []*llm.ChatResponse{
		toolCallResponse("list_document_types", `{}`),
		toolCallResponse("describe_document", `{"doc_type":"LeaveRequest"}`),
		textResponse("done"),
	}

	if _, err := rt.Execute(hr, "list document types then describe LeaveRequest"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// The discovery list returns every registered DocType (7 > the default
	// MaxBulkOperations of 5). That count must never seed the session's bulk
	// target: the follow-up describe is auto-approved, so no approval round
	// trip may have run.
	if got := len(s.approvals.requests); got != 0 {
		t.Errorf("approval requests = %d, want 0 (discovery list count must not trip bulk approval): %+v", got, s.approvals.requests)
	}
}

// TestBulkCountAppliesToSameDocType verifies the positive side of the scoped
// bulk count: a large list of one DocType still trips the bulk-limit approval
// for a write on that same DocType (TAD §12.1 step 2).
func TestBulkCountAppliesToSameDocType(t *testing.T) {
	s := newTestSite(t)
	hr := hrCtx()

	for i := 0; i < 6; i++ {
		_, err := s.doc.Create(hr, "Employee", map[string]any{
			"FirstName": "Emp", "LastName": fmt.Sprintf("Loy%d", i),
			"Email": fmt.Sprintf("emp%d@test.com", i),
		})
		if err != nil {
			t.Fatalf("seed employee: %v", err)
		}
	}

	rt := s.newRuntime(t, nil)
	s.provider.responses = []*llm.ChatResponse{
		toolCallResponse("list_employee", `{"limit":100}`),
		toolCallResponse("create_employee", `{"first_name":"Ada","last_name":"Lovelace","email":"ada@test.com"}`),
		textResponse("done"),
	}

	if _, err := rt.Execute(hr, "list employees then create one"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// list_employee returned 6 records of the same DocType the create targets,
	// so the bulk-limit branch must have required an approval round trip.
	if got := len(s.approvals.requests); got != 1 {
		t.Errorf("approval requests = %d, want 1 (same-DocType bulk count must trip approval): %+v", got, s.approvals.requests)
	}
	if len(s.approvals.requests) == 1 && s.approvals.requests[0].Details.PolicyReason != "BulkLimit" {
		t.Errorf("policy reason = %q, want %q", s.approvals.requests[0].Details.PolicyReason, "BulkLimit")
	}
}

// TestPermissionDeniedSurfacedToAgent verifies that an operation a role is not
// allowed to perform is rejected by the perm engine and the denial surfaces in
// the tool observation (PRD §25.2 / TAD §11.1), rather than being retried.
func TestPermissionDeniedSurfacedToAgent(t *testing.T) {
	s := newTestSite(t)
	// AutoApprove update so the only gate left is the perm engine.
	s.safety = safety.NewLayer(safety.SafetyPolicy{
		AutoApprove: []string{"read", "list", "search", "update"},
	}, cache.NewLRUStore(100))
	rt := s.newRuntime(t, nil)

	// employee role has Read+Create but no Write: update_employee is denied.
	ctx := auth.NewContext(context.Background(), auth.Identity{UserID: "u-emp", Roles: []string{"employee"}})
	s.provider.responses = []*llm.ChatResponse{
		toolCallResponse("update_employee", `{"id":"emp-1","first_name":"Hijacked"}`),
		textResponse("I will not do that."),
	}

	resp, err := rt.Execute(ctx, "set first name to Hijacked")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Content == "" {
		t.Error("expected a closing message from the agent")
	}
	// The denial is surfaced verbatim in the tool observation, not a retry.
	for _, m := range s.provider.lastRequest().Messages {
		if m.Role == "tool" && m.Name == "update_employee" {
			if !strings.Contains(m.Content, "permission denied") {
				t.Errorf("tool observation = %q, want permission denied", m.Content)
			}
			return
		}
	}
	t.Error("update_employee tool observation not present in transcript")
}

// TestExecuteDeleteGatedEvenWhenPolicyAutoApprovesDelete is the TAD §12.1
// step 1 completion criterion: a SafetyPolicy that *tries* to auto-approve
// delete must not succeed — delete_* stays approval-gated no matter what.
func TestExecuteDeleteGatedEvenWhenPolicyAutoApprovesDelete(t *testing.T) {
	s := newTestSite(t)
	s.safety = safety.NewLayer(safety.SafetyPolicy{
		AutoApprove: []string{"read", "search", "list", "delete"},
	}, cache.NewLRUStore(100))
	rt := s.newRuntime(t, nil)

	s.approvals.responses = []runtime.ApprovalResponse{{Approved: true}}
	s.provider.responses = []*llm.ChatResponse{
		toolCallResponse("describe_document", `{"doc_type":"Employee"}`),
		toolCallResponse("delete_employee", `{"id":"does-not-exist"}`),
		textResponse("ok"),
	}

	if _, err := rt.Execute(hrCtx(), "delete it"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	events := s.sink.eventsOf(runtime.EventApprovalRequired)
	if len(events) != 1 {
		t.Fatalf("approval_required events = %d, want 1 despite AutoApprove[delete]", len(events))
	}
	if events[0].Approval.Details.PolicyReason != string(safety.ReasonAlwaysRequireApproval) {
		t.Errorf("policy_reason = %q, want %q", events[0].Approval.Details.PolicyReason, safety.ReasonAlwaysRequireApproval)
	}
	if len(s.approvals.requests) != 1 {
		t.Errorf("approval requests = %d, want 1", len(s.approvals.requests))
	}
}

// TestExecuteBulkLimitRequiresApproval is the TAD §12.1 step 2 completion
// criterion: a write following a list/search result larger than
// MaxBulkOperations requires approval even when its verb is in AutoApprove.
func TestExecuteBulkLimitRequiresApproval(t *testing.T) {
	s := newTestSite(t)
	s.safety = safety.NewLayer(safety.SafetyPolicy{
		AutoApprove:       []string{"read", "search", "list", "create"},
		MaxBulkOperations: 2,
	}, cache.NewLRUStore(100))
	rt := s.newRuntime(t, nil)

	for i, name := range []string{"Ada", "Grace", "Edsger"} {
		if _, err := s.doc.Create(hrCtx(), "Employee", map[string]any{
			"first_name": name, "last_name": "Seed", "email": name + "@seed.dev",
		}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	s.approvals.responses = []runtime.ApprovalResponse{{Approved: true}}
	s.provider.responses = []*llm.ChatResponse{
		toolCallResponse("describe_document", `{"doc_type":"Employee"}`),
		toolCallResponse("list_employee", `{}`),
		toolCallResponse("create_employee", `{"first_name":"Dennis","last_name":"Ritchie","email":"dmr@bell.dev"}`),
		textResponse("done"),
	}

	if _, err := rt.Execute(hrCtx(), "list then add Dennis"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	events := s.sink.eventsOf(runtime.EventApprovalRequired)
	if len(events) != 1 {
		t.Fatalf("approval_required events = %d, want 1 (list of 3 > MaxBulkOperations 2)", len(events))
	}
	if events[0].Approval.Details.PolicyReason != string(safety.ReasonBulkLimit) {
		t.Errorf("policy_reason = %q, want %q", events[0].Approval.Details.PolicyReason, safety.ReasonBulkLimit)
	}
	if len(s.approvals.requests) != 1 {
		t.Errorf("approval requests = %d, want 1", len(s.approvals.requests))
	}
	rows, err := s.doc.List(hrCtx(), "Employee", document.ListOpts{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 4 {
		t.Errorf("employee count = %d, want 4 (bulk-approval create ran)", len(rows))
	}
}

// TestExecutePlanModeWithApproval is the PRD §38.2 completion criterion: a
// multi-step plan whose steps require approval is gated by a single combined
// approval round trip before any step executes, then runs after approval.
func TestExecutePlanModeWithApproval(t *testing.T) {
	s := newTestSite(t)
	s.safety = safety.NewLayer(safety.SafetyPolicy{
		AutoApprove:     []string{"read", "search", "list"},
		RequireApproval: []string{"create"},
	}, cache.NewLRUStore(100))
	rt := s.newRuntime(t, nil)

	s.approvals.responses = []runtime.ApprovalResponse{{Approved: true}}
	s.provider.responses = []*llm.ChatResponse{
		toolCallResponse("describe_document", `{"doc_type":"Employee"}`),
		toolCallResponseN(
			llm.ToolCall{ID: "c1", Name: "create_employee", Arguments: `{"first_name":"Grace","last_name":"Hopper","email":"grace@example.com"}`},
			llm.ToolCall{ID: "c2", Name: "get_employee", Arguments: `{"id":"ref:0.id"}`},
		),
		textResponse(`{"steps":[
			{"operation":"create_employee","args":{"first_name":"Grace","last_name":"Hopper","email":"grace@example.com"}},
			{"operation":"get_employee","args":{"id":"ref:0.id"}}
		]}`),
		textResponse("Created Grace Hopper after approval."),
	}

	resp, err := rt.Execute(hrCtx(), "create Grace Hopper then fetch her record")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.ToolCalls != 2 {
		t.Errorf("ToolCalls = %d, want 2", resp.ToolCalls)
	}

	// The plan was gated by a single combined approval prompt (TAD §11.2 c /
	// §12.3), with the strictest step's policy_reason.
	events := s.sink.eventsOf(runtime.EventApprovalRequired)
	if len(events) != 1 {
		t.Fatalf("approval_required events = %d, want 1 combined plan approval", len(events))
	}
	if events[0].Approval.Details.Action != "plan" {
		t.Errorf("approval action = %q, want plan", events[0].Approval.Details.Action)
	}
	if events[0].Approval.Details.PolicyReason != string(safety.ReasonRequireApproval) {
		t.Errorf("policy_reason = %q, want %q", events[0].Approval.Details.PolicyReason, safety.ReasonRequireApproval)
	}
	if len(s.approvals.requests) != 1 {
		t.Errorf("approval requests = %d, want 1", len(s.approvals.requests))
	}

	rows, err := s.doc.List(hrCtx(), "Employee", document.ListOpts{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 || rows[0]["first_name"] != "Grace" {
		t.Errorf("employee rows = %v, want Grace Hopper created after approval", rows)
	}
}
