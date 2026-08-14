// Package runtime implements the Agent Runtime: the Execute loop
// (TAD §3.3 / PRD §27.2), Session Manager, Context Manager (TAD §11.1
// discovery/operation split, §11.2 planning-mode classification), and the
// Agent Executor that routes every tool call through the same Document and
// Workflow Engine entry points the API layer uses — never a separate agent
// execution path (PRD §23.1) — with audit.WithAgent set unconditionally so
// agent-initiated writes are always flagged via_agent=true (TAD §12.2, §13.3).
package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/orjanda-framework/orjanda/agent/llm"
	"github.com/orjanda-framework/orjanda/agent/planner"
	"github.com/orjanda-framework/orjanda/agent/safety"
	toolreg "github.com/orjanda-framework/orjanda/agent/tools"
	"github.com/orjanda-framework/orjanda/api/rpc"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/document"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/perm"
	"github.com/orjanda-framework/orjanda/schema"
	"github.com/orjanda-framework/orjanda/workflow"
)

// Response is the result of one Runtime.Execute turn (TAD §2.6).
type Response struct {
	// Content is the assistant's final answer text.
	Content string
	// SessionID identifies the session the turn ran in.
	SessionID string
	// ToolCalls is the number of tool invocations executed during the turn.
	ToolCalls int
}

// DefaultSessionTTL is the inactivity timeout applied when Options.SessionTTL
// is zero: an agent session untouched for this long is evicted, bounding the
// SessionManager's memory to live conversations (REVIEW-2026-08-12 finding 3).
const DefaultSessionTTL = 30 * time.Minute

// Event types emitted to a Sink. Their JSON shapes on the WebSocket wire are
// fixed by TAD §6.2 (token/tool_start/tool_end/approval_required).
const (
	EventToken            = "token"
	EventToolStart        = "tool_start"
	EventToolEnd          = "tool_end"
	EventApprovalRequired = "approval_required"
	EventPlan             = "plan"
)

// Event is a streaming observation from the runtime, forwarded verbatim by
// the WebSocket endpoint (TAD §6.2) and the CLI chat stub.
type Event struct {
	Type     string           `json:"type"`
	Content  string           `json:"content,omitempty"`
	Tool     string           `json:"tool,omitempty"`
	Success  bool             `json:"success,omitempty"`
	Approval *ApprovalPayload `json:"approval,omitempty"`
}

// MarshalJSON flattens an approval_required event to the TAD §6.2 wire shape
// (action_id and details at top level) instead of the internal nested
// approval object:
//
//	{"type":"approval_required","action_id":"req-123","details":{...}}
func (e Event) MarshalJSON() ([]byte, error) {
	if e.Type == EventApprovalRequired && e.Approval != nil {
		return json.Marshal(struct {
			Type     string          `json:"type"`
			ActionID string          `json:"action_id"`
			Details  ApprovalDetails `json:"details"`
		}{Type: e.Type, ActionID: e.Approval.ActionID, Details: e.Approval.Details})
	}
	type alias Event
	return json.Marshal(alias(e))
}

// ApprovalPayload is the extended approval_required payload of TAD §12.3:
// the §6.2 shape plus policy_reason so the UI can render branch-specific copy.
type ApprovalPayload struct {
	ActionID string          `json:"action_id"`
	Details  ApprovalDetails `json:"details"`
}

// ApprovalDetails is the per-approval detail block (TAD §12.3).
type ApprovalDetails struct {
	DocType      string         `json:"doctype"`
	Action       string         `json:"action"`
	Payload      map[string]any `json:"payload"`
	PolicyReason string         `json:"policy_reason"`
}

// ApprovalResponse is the human's answer to an approval_required round trip.
// Payload is non-nil when the human chose Modify (PRD §38.2) with corrected
// arguments; the executor substitutes them before executing.
type ApprovalResponse struct {
	ActionID string
	Approved bool
	Payload  map[string]any
}

// Sink receives streaming events. The WebSocket handler and CLI stub both
// implement it; a nil sink on the Runtime is a no-op.
type Sink interface {
	Send(Event)
}

// ApprovalGateway performs the human-in-the-loop round trip. The WebSocket
// endpoint implements it by sending approval_required and blocking on the
// matching approval_response; the CLI stub prompts the terminal. Tests use a
// scripted implementation.
type ApprovalGateway interface {
	RequestApproval(ctx context.Context, req ApprovalPayload) (ApprovalResponse, error)
}

// Options wires a Runtime together. Only Provider, Registry, DocEngine, and
// Safety are required; the rest fall back to safe defaults where possible.
type Options struct {
	// Provider is the LLM backend driving the loop (TAD §2.7).
	Provider llm.Provider
	// Tools is a compiled ToolRegistry (Compile must already have run).
	// If nil, a new registry is built from permEngine/workflow and compiled
	// against Registry.
	Tools toolreg.ToolRegistry
	// PermEngine is required when Tools is nil (to build the registry). When
	// set, the Executor also re-checks method/custom tool AllowedRoles at
	// execution time through it (TAD §9.2 / §10.4, PRD §25.1).
	PermEngine perm.Engine
	// Registry is the compiled schema registry.
	Registry schema.Registry
	// DocEngine is the same Document Engine the REST API layer uses.
	DocEngine *document.Engine
	// Workflow is the same workflow Engine the API layer uses; nil when no
	// workflowed DocTypes exist.
	Workflow workflow.Engine
	// Safety is the Safety Layer (rate limit, approvals, allowlist, budget).
	Safety *safety.Layer
	// Sink receives streaming events (may be nil).
	Sink Sink
	// Approvals resolves human-in-the-loop approvals (may be nil, in which
	// case an approval-requiring call is rejected with an observation).
	Approvals ApprovalGateway
	// Model overrides the provider's default model (empty = provider default).
	Model string
	// MaxSteps caps tool-call iterations per turn. 0 = default (10).
	MaxSteps int
	// SystemPrompt overrides the default system message.
	SystemPrompt string
	// SessionTTL is the inactivity timeout after which a session is evicted
	// from the SessionManager (0 = DefaultSessionTTL; see TAD §11.1/§12.1
	// continuity across turns and REVIEW-2026-08-12 finding 3).
	SessionTTL time.Duration
}

// ExecuteOption configures a single Runtime.Execute turn (TAD §3.3). It is
// how a caller supplies a per-turn LLM provider (e.g. a test MockLLM) or
// approval gateway without constructing a fresh Runtime.
type ExecuteOption func(*executeConfig)

type executeConfig struct {
	provider  llm.Provider
	approvals ApprovalGateway
}

// WithProvider overrides the Runtime's configured LLM provider for this turn
// only. When the override also implements ApprovalGateway and no explicit
// WithApprovals is given, it serves as the turn's approval gateway as well —
// which is what lets orjanda/testing.MockLLM script approval round trips from
// the same step queue as the tool/text responses (TAD §17, §12.3).
func WithProvider(p llm.Provider) ExecuteOption {
	return func(c *executeConfig) { c.provider = p }
}

// WithApprovals overrides the Runtime's human-in-the-loop gateway for this
// turn only (TAD §12.3). It takes precedence over the WithProvider auto-wire.
func WithApprovals(a ApprovalGateway) ExecuteOption {
	return func(c *executeConfig) { c.approvals = a }
}

// Runtime is the concrete agent Runtime (TAD §2.6).
type Runtime struct {
	provider   llm.Provider
	regTools   toolreg.ToolRegistry
	schemaReg  schema.Registry
	docEngine  *document.Engine
	wfEngine   workflow.Engine
	permEngine perm.Engine
	safety     *safety.Layer
	sessions   *SessionManager

	sink      Sink
	approvals ApprovalGateway
	model     string
	maxSteps  int
	system    string

	once          sync.Once
	docTypeByTool map[string]string // snake_case(DocType) → DocType
	methodTool    map[string]string // tool name (dots→underscores) → RPC method name
	customTool    map[string]toolreg.Tool
}

// NewRuntime builds a Runtime from opts. The ToolRegistry (either provided or
// freshly compiled) must expose the identity-projected tool list; the Safety
// Layer is wired to the provider's token usage when the provider tracks it.
func NewRuntime(opts Options) (*Runtime, error) {
	if opts.Provider == nil {
		return nil, errors.New("runtime: a provider is required")
	}
	if opts.Registry == nil {
		return nil, errors.New("runtime: a schema registry is required")
	}
	if opts.DocEngine == nil {
		return nil, errors.New("runtime: a document engine is required")
	}
	if opts.Safety == nil {
		return nil, errors.New("runtime: a safety layer is required")
	}

	regTools := opts.Tools
	if regTools == nil {
		if opts.PermEngine == nil {
			return nil, errors.New("runtime: a perm engine is required to build the tool registry")
		}
		regTools = toolreg.NewToolRegistry(opts.PermEngine, opts.Workflow)
		if err := regTools.Compile(opts.Registry); err != nil {
			return nil, err
		}
	}

	maxSteps := opts.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 10
	}

	system := opts.SystemPrompt
	if system == "" {
		system = defaultSystemPrompt
	}

	sessionTTL := opts.SessionTTL
	if sessionTTL <= 0 {
		sessionTTL = DefaultSessionTTL
	}

	r := &Runtime{
		provider:   opts.Provider,
		regTools:   regTools,
		schemaReg:  opts.Registry,
		docEngine:  opts.DocEngine,
		wfEngine:   opts.Workflow,
		permEngine: opts.PermEngine,
		safety:     opts.Safety,
		sessions:   NewSessionManagerWithTTL(sessionTTL),
		sink:       opts.Sink,
		approvals:  opts.Approvals,
		model:      opts.Model,
		maxSteps:   maxSteps,
		system:     system,
	}

	// Auto-wire token usage: when the provider tracks usage (the llm.Gateway
	// does), the Safety Layer can enforce per-session budgets (TAD §12.2).
	if us, ok := opts.Provider.(safety.UsageSource); ok {
		r.safety.WithUsageSource(us)
	}

	return r, nil
}

// RegisterTool registers a custom agent tool (TAD §2.6, §10.4). It is
// re-exported to the runtime so callers can wire tools at runtime
// construction rather than only via package-level registration.
func (r *Runtime) RegisterTool(t toolreg.Tool) {
	toolreg.RegisterCustomTool(t)
}

// NewSession creates a fresh, registered session for an identity.
func (r *Runtime) NewSession(id auth.Identity) *Session {
	return r.sessions.New(id)
}

// Session returns a registered session by id (nil when unknown).
func (r *Runtime) Session(id string) *Session {
	return r.sessions.Get(id)
}

// --- Execute (TAD §3.3 / PRD §27.2) ------------------------------------------

// Execute runs one agent turn for the identity on ctx. The session id is read
// from the context (safety.WithSession); when absent a new session is created
// and returned in the Response. Per-turn overrides (ExecuteOption) apply to
// this call only and leave the Runtime untouched.
func (r *Runtime) Execute(ctx context.Context, userMessage string, opts ...ExecuteOption) (*Response, error) {
	r.buildMaps()

	cfg := executeConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	provider := r.provider
	if cfg.provider != nil {
		provider = cfg.provider
	}
	approvals := r.approvals
	if cfg.approvals != nil {
		approvals = cfg.approvals
	} else if cfg.provider != nil {
		if ag, ok := cfg.provider.(ApprovalGateway); ok {
			approvals = ag
		}
	}

	id := auth.FromContext(ctx)
	sess := r.sessionFor(ctx, id)

	ctx = auth.NewContext(ctx, id)
	ctx = safety.WithSession(ctx, sess.ID)

	// Safety gates before any LLM call (TAD §12.2 rate limit + token budget).
	if err := r.safety.CheckRateLimit(ctx, id); err != nil {
		return nil, err
	}
	if err := r.safety.CheckTokenBudget(ctx, sess.ID, r.projectedFor(userMessage, sess)); err != nil {
		return nil, err
	}

	sess.addMessage(llm.Message{Role: "user", Content: userMessage})

	tools := r.toolsForTurn(ctx, id, sess)

	// First LLM call of the turn (ReAct default, TAD §11.2 step 1).
	resp, err := r.chat(ctx, provider, sess, tools, nil)
	if err != nil {
		return nil, err
	}
	r.emitTokens(resp.Content)

	toolSteps := 0
	var final string

	for {
		if len(resp.ToolCalls) == 0 {
			final = resp.Content
			break
		}

		toolSteps++
		if toolSteps > r.maxSteps {
			return nil, orjerrors.Validation("agent exceeded the maximum number of tool-call steps", map[string]any{
				"max_steps": r.maxSteps,
			})
		}

		// Mode classification on the current response (TAD §11.2 steps 2–3):
		// a data dependency between calls escalates this turn to
		// Plan-and-Execute. The check runs on every iteration, so a ReAct
		// session that only later introduces a dependency chain still
		// escalates (step 3). The Context Manager runs again first, so a
		// describe_document on a prior iteration has attached that DocType's
		// operation tools for the plan request (TAD §11.1).
		if hasDataDependency(resp.ToolCalls) {
			planTools := r.toolsForTurn(ctx, id, sess)
			return r.executePlanMode(ctx, sess, userMessage, planTools, r.schemasFor(planTools), provider, approvals)
		}

		// ReAct: append the assistant tool-call message, execute each call,
		// feed results back, and loop.
		sess.addMessage(llm.Message{Role: "assistant", ToolCalls: resp.ToolCalls})
		for _, call := range resp.ToolCalls {
			obs := r.executeToolCall(ctx, sess, call, userMessage, approvals)
			sess.addMessage(llm.Message{
				Role:       "tool",
				Name:       call.Name,
				ToolCallID: call.ID,
				Content:    obs,
			})
		}

		// Context Manager re-run (TAD §11.1): tool results mark DocTypes seen,
		// attaching their operation tools to the next LLM request.
		tools = r.toolsForTurn(ctx, id, sess)

		resp, err = r.chat(ctx, provider, sess, tools, nil)
		if err != nil {
			return nil, err
		}
		r.emitTokens(resp.Content)
	}

	sess.addMessage(llm.Message{Role: "assistant", Content: final})
	return &Response{Content: final, SessionID: sess.ID, ToolCalls: toolSteps}, nil
}

// sessionFor resolves the session for a turn: reuse the session id on ctx when
// it belongs to the caller, otherwise create a fresh one (identity isolation).
func (r *Runtime) sessionFor(ctx context.Context, id auth.Identity) *Session {
	if sessID := safety.SessionFromContext(ctx); sessID != "" {
		if s := r.sessions.Get(sessID); s != nil && (s.UserID == "" || s.UserID == id.UserID) {
			return s
		}
	}
	return r.sessions.New(id)
}

// chat performs one LLM call with the session transcript and tool set,
// enforcing the token budget and recording usage on the session.
func (r *Runtime) chat(ctx context.Context, provider llm.Provider, sess *Session, tools []llm.ToolDefinition, responseFormat *llm.JSONSchemaFormat) (*llm.ChatResponse, error) {
	msgs := r.buildMessages(sess)
	if err := r.safety.CheckTokenBudget(ctx, sess.ID, estimateTokens(msgs)); err != nil {
		return nil, err
	}

	resp, err := provider.ChatCompletion(ctx, llm.ChatRequest{
		Model:          r.model,
		Messages:       msgs,
		Tools:          tools,
		ResponseFormat: responseFormat,
	})
	if err != nil {
		return nil, err
	}
	r.trackUsage(provider, sess.ID, resp.Usage)
	return resp, nil
}

// trackUsage records token usage against the session when the provider tracks
// it (the llm.Gateway does), feeding the Safety Layer's token budget.
func (r *Runtime) trackUsage(provider llm.Provider, key string, usage llm.TokenUsage) {
	if g, ok := provider.(interface {
		TrackUsage(string, llm.TokenUsage)
	}); ok {
		g.TrackUsage(key, usage)
	}
}

// buildMessages returns system prompt + full transcript (TAD §11.1 context
// assembly; the system prompt is prepended fresh each turn).
func (r *Runtime) buildMessages(sess *Session) []llm.Message {
	msgs := make([]llm.Message, 0, len(sess.Transcript())+1)
	msgs = append(msgs, llm.Message{Role: "system", Content: r.system})
	return append(msgs, sess.Transcript()...)
}

func (r *Runtime) emit(evt Event) {
	if r.sink != nil {
		r.sink.Send(evt)
	}
}

func (r *Runtime) emitTokens(content string) {
	if content != "" {
		r.emit(Event{Type: EventToken, Content: content})
	}
}

// projectedFor is a coarse token estimate for the initial budget check.
func (r *Runtime) projectedFor(userMessage string, sess *Session) int {
	return estimateTokens(sess.Transcript()) + len(userMessage)/4 + 256
}

func estimateTokens(msgs []llm.Message) int {
	total := 0
	for _, m := range msgs {
		total += len(m.Content) / 4
		total += len(m.Name) / 4
		total += 4 // per-message overhead
		for _, tc := range m.ToolCalls {
			total += len(tc.Arguments) / 4
		}
	}
	return total
}

// hasDataDependency reports whether this response's tool calls form a data
// dependency chain: a later call's arguments reference an earlier call's
// result via the "ref:<i>" marker (TAD §11.2 step 2). This is the signal that
// escalates ReAct to Plan-and-Execute.
func hasDataDependency(calls []llm.ToolCall) bool {
	for i, call := range calls {
		for j := 0; j < i; j++ {
			if strings.Contains(call.Arguments, planner.RefPrefix+fmt.Sprintf("%d", j)) {
				return true
			}
		}
	}
	return false
}

// --- Tool dispatch helpers ----------------------------------------------------

func (r *Runtime) buildMaps() {
	r.once.Do(func() {
		r.docTypeByTool = make(map[string]string)
		for _, doc := range r.schemaReg.List() {
			r.docTypeByTool[snakeCase(doc.Name)] = doc.Name
		}

		r.methodTool = make(map[string]string)
		for _, m := range rpc.Methods() {
			r.methodTool[strings.ReplaceAll(m.Name, ".", "_")] = m.Name
		}

		r.customTool = make(map[string]toolreg.Tool)
		for _, c := range toolreg.CustomTools() {
			r.customTool[c.Name] = c
		}
	})
}

// docTypeFor resolves the canonical DocType for an operation tool name like
// "create_employee" → "Employee". Empty when the name is not a DocType tool.
func (r *Runtime) docTypeFor(toolName string) string {
	if op, rest := splitOperationTool(toolName); op != "" {
		if dt, ok := r.docTypeByTool[rest]; ok {
			return dt
		}
	}
	return ""
}

// snakeDocTypeFor resolves the snake_case DocType key of an operation tool
// name ("list_employee" → "employee"), matching the session's seen-doc and
// target-count keys. Discovery, method, and custom tools resolve to "".
func (r *Runtime) snakeDocTypeFor(toolName string) string {
	if dt := r.docTypeFor(toolName); dt != "" {
		return snakeCase(dt)
	}
	return ""
}

// splitOperationTool splits a Registry-derived operation tool into (verb,
// snake_case DocType). Returns ("", "") for discovery, method, or custom toolreg.
func splitOperationTool(name string) (verb, snake string) {
	for _, v := range []string{"create_", "update_", "delete_", "get_", "list_", "search_", "execute_action_"} {
		if strings.HasPrefix(name, v) {
			return strings.TrimSuffix(v, "_"), strings.TrimPrefix(name, v)
		}
	}
	return "", ""
}

// snakeCase converts a CamelCase DocType to snake_case, matching the naming
// rule of TAD §1.4 (LeaveRequest → leave_requests; here singular for tool
// names: create_leave_request).
func snakeCase(s string) string {
	var out strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			out.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			out.WriteRune(r + ('a' - 'A'))
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// defaultSystemPrompt is the baseline system message when Options.SystemPrompt
// is empty. It keeps the agent grounded in the single permission/approval path
// (PRD §23.4): the agent sees exactly the tools its identity is allowed to use,
// and destructive or bulk work is gated by the Safety Layer, not the prompt.
// The discovery step is called out explicitly because operation tools attach
// only after their target DocType has appeared in the session (TAD §11.1).
const defaultSystemPrompt = "You are the AI agent built into this business application. " +
	"Interact with business records through the provided tools only; never invent records, " +
	"fields, or permissions that are not exposed by the tools you are given. " +
	"To operate on a Document Type you must first discover it: call describe_document " +
	"(or list_document_types) for the Document Type you need, and only then do its operation " +
	"tools (list, get, search, create, update, delete, execute_action) become available. " +
	"Read operations are safe; create, update, delete, and workflow actions are gated by policy " +
	"and may require human approval before they execute. " +
	"Prefer a clear, concise final answer in the user's language."
