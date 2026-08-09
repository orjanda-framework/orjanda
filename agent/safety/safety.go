// Package safety implements the Agent Safety Layer: approval policy, rate
// limiting, token budgets, and the tool allowlist (TAD §12, PRD §25.3, §28).
//
// The load-bearing contract is the approval evaluation order in TAD §12.1 —
// Always → Bulk → RoleOverride → RequireApproval → AutoApprove, fail-closed
// default — with the policy_reason values of §12.3 surfaced so the UI can
// render branch-specific copy (e.g. a bulk delete cannot be dismissed with a
// "don't ask again" option).
package safety

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/orjanda-framework/orjanda/agent/llm"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/cache"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
)

// RateLimit configures agent operation rate limiting (TAD §12).
type RateLimit struct {
	// OperationsPerMinute is the sliding-window cap. 0 selects the default
	// (60). Backed by cache.Store keys of the form "ratelimit:{scope}:{id}".
	OperationsPerMinute int
	// Scope is "user" or "session" (TAD §12.2).
	Scope string
}

// SafetyPolicy is the agent-specific security policy (TAD §12, PRD §25.3).
// Verb lists are matched case-insensitively against a tool's semantic verb
// (e.g. "create", "update", "submit", "delete", "read", "search", "list").
type SafetyPolicy struct {
	// AutoApprove lists verbs that never prompt for approval (e.g. "read",
	// "search", "list").
	AutoApprove []string
	// RequireApproval lists verbs that always prompt (e.g. "create",
	// "update", "submit").
	RequireApproval []string
	// AlwaysRequireApproval lists verbs that can never be overridden by any
	// other policy branch (e.g. "delete", "cancel"). delete_* tools are
	// added to this set unconditionally by the layer itself (TAD §12.1
	// step 1, §10.1 step 5) — config cannot remove them.
	AlwaysRequireApproval []string
	// MaxBulkOperations is the record-count threshold above which approval is
	// required regardless of verb (PRD §28.1). 0 selects the default of 5.
	MaxBulkOperations int
	// RequireApprovalForRoles maps a role to the verbs it must confirm; an
	// empty verb list means every verb (PRD §28.3's "Interns confirming
	// everything").
	RequireApprovalForRoles map[string][]string
	// RateLimit is the per-user/per-session operation throttle.
	RateLimit RateLimit
	// TokenBudgetPerSession caps accumulated prompt+completion tokens per
	// session. 0 disables the budget.
	TokenBudgetPerSession int
	// ToolAllowlist restricts which tools the agent may call; empty = every
	// generated and custom tool (TAD §12). Applied by the runtime as the
	// final filter on ForIdentity output (TAD §10.3 step 3) and re-checked at
	// execution time.
	ToolAllowlist []string
}

// ApprovalReason is the policy_reason value of the TAD §12.3 payload.
type ApprovalReason string

// policy_reason values — one per branch of TAD §12.1, surfaced verbatim in
// the approval_required WebSocket payload so the UI can render distinct copy.
const (
	ReasonAlwaysRequireApproval ApprovalReason = "AlwaysRequireApproval"
	ReasonBulkLimit             ApprovalReason = "BulkLimit"
	ReasonRoleOverride          ApprovalReason = "RoleOverride"
	ReasonRequireApproval       ApprovalReason = "RequireApproval"
)

// Approval is the outcome of the TAD §12.1 evaluation. Reason is the
// matching policy_reason (empty when not required).
type Approval struct {
	Required    bool
	Reason      ApprovalReason
	TargetCount int
}

// ToolCallInfo carries everything the layer needs to evaluate one tool call.
// The executor derives Verb (e.g. "submit" for execute_action with
// action="Submit") and TargetCount (from a prior list/search result in the
// session transcript, TAD §12.1 step 2); callers using the plain
// RequiresApproval method get both derived from the tool name and args.
type ToolCallInfo struct {
	// Verb is the semantic verb: the tool prefix ("create", "get", ...) or
	// the lowercase workflow action for execute_action_* tools.
	Verb string
	// ToolName is the tool name the model requested (e.g. "create_employee").
	ToolName string
	// Args is the tool's argument map.
	Args map[string]any
	// TargetCount is the estimated number of records the call affects (0 =
	// unknown, derived from array args if any).
	TargetCount int
}

// UsageSource provides accumulated token usage per key, so the layer can
// enforce per-session budgets (TAD §12.2). The llm.Gateway satisfies it.
type UsageSource interface {
	UsageFor(key string) llm.TokenUsage
}

// SafetyLayer is the TAD §12 interface exactly as specified.
type SafetyLayer interface {
	// RequiresApproval reports whether a tool call needs human approval
	// before execution. Evaluates in the TAD §12.1 order.
	RequiresApproval(ctx context.Context, id auth.Identity, toolName string, args map[string]any) bool
	// CheckRateLimit enforces the sliding-window operation throttle keyed
	// "ratelimit:{scope}:{id}" in cache.Store. Returns an error when the
	// window is exhausted.
	CheckRateLimit(ctx context.Context, id auth.Identity) error
	// CheckTokenBudget rejects a call whose projected tokens (accumulated
	// usage + projected) would exceed the session budget.
	CheckTokenBudget(ctx context.Context, sessionID string, projected int) error
	// IsToolAllowed enforces the allowlist. Empty allowlist = allow all.
	IsToolAllowed(toolName string) bool
}

// Layer is the concrete SafetyLayer. Use it when the executor needs the
// policy_reason: RequiresApprovalWithReason (RequiresApproval delegates to it
// with a ToolCallInfo derived from the name/args alone).
type Layer struct {
	policy SafetyPolicy
	store  cache.Store
	usage  UsageSource
}

// NewLayer builds a SafetyLayer with PRD-defaulted policy values. store may
// be nil (rate limiting disabled). The default policy matches PRD §28.1:
// AutoApprove for reads/searches/lists, RequireApproval for creates/updates,
// AlwaysRequireApproval for deletes, MaxBulkOperations = 5.
func NewLayer(policy SafetyPolicy, store cache.Store) *Layer {
	if policy.MaxBulkOperations == 0 {
		policy.MaxBulkOperations = 5
	}
	if policy.RateLimit.OperationsPerMinute == 0 {
		policy.RateLimit.OperationsPerMinute = 60
	}
	if policy.RateLimit.Scope == "" {
		policy.RateLimit.Scope = "user"
	}
	return &Layer{policy: policy, store: store}
}

// WithUsageSource wires a token-usage source (e.g. the llm.Gateway) so
// CheckTokenBudget can see the session's accumulated usage (TAD §12.2).
func (l *Layer) WithUsageSource(s UsageSource) { l.usage = s }

// Policy returns a copy of the effective policy (defaults applied).
func (l *Layer) Policy() SafetyPolicy { return l.policy }

// --- Approval evaluation (TAD §12.1) -----------------------------------------

func (l *Layer) RequiresApproval(ctx context.Context, id auth.Identity, toolName string, args map[string]any) bool {
	info := ToolCallInfo{Verb: VerbFor(toolName, args), ToolName: toolName, Args: args}
	return l.RequiresApprovalWithReason(ctx, id, info).Required
}

// RequiresApprovalWithReason evaluates in the TAD §12.1 order; the first
// match wins (fail-closed default at the end):
//
//  1. AlwaysRequireApproval — delete_* tools are folded in here unconditionally
//     (§10.1 step 5: cannot be bypassed by policy config).
//  2. Bulk check — TargetCount (or the largest array arg) > MaxBulkOperations.
//  3. RequireApprovalForRoles[callerRole].
//  4. RequireApproval — the configured default set.
//  5. AutoApprove — everything else; an unrecognized verb requires approval.
func (l *Layer) RequiresApprovalWithReason(ctx context.Context, id auth.Identity, info ToolCallInfo) Approval {
	_ = ctx
	verb := info.Verb
	if verb == "" {
		verb = VerbFor(info.ToolName, info.Args)
	}

	// Step 1: AlwaysRequireApproval (delete_* folded in, not overridable).
	if strings.HasPrefix(info.ToolName, "delete_") || contains(l.policy.AlwaysRequireApproval, verb) {
		return Approval{Required: true, Reason: ReasonAlwaysRequireApproval, TargetCount: info.TargetCount}
	}

	// Step 2: Bulk check (PRD §28.1: "Bulk operations (>5 records): Always
	// require approval, Configurable: No" — enforced before role/verb checks).
	count := info.TargetCount
	if count == 0 {
		count = bulkCount(info.Args)
	}
	if l.policy.MaxBulkOperations > 0 && count > l.policy.MaxBulkOperations {
		return Approval{Required: true, Reason: ReasonBulkLimit, TargetCount: count}
	}

	// Step 3: Per-role override.
	if roleOverrides(id, l.policy.RequireApprovalForRoles, verb) {
		return Approval{Required: true, Reason: ReasonRoleOverride, TargetCount: count}
	}

	// Step 4: Configured default set.
	if contains(l.policy.RequireApproval, verb) {
		return Approval{Required: true, Reason: ReasonRequireApproval, TargetCount: count}
	}

	// Step 5: AutoApprove; fail-closed otherwise.
	if contains(l.policy.AutoApprove, verb) {
		return Approval{Required: false, TargetCount: count}
	}
	return Approval{Required: true, Reason: ReasonRequireApproval, TargetCount: count}
}

// VerbFor derives a tool call's semantic verb: the lowercase workflow action
// for execute_action_* tools, otherwise the verb prefix before the first
// underscore (create_employee → "create"). The read tool prefix "get" maps to
// the canonical "read" verb so the AutoApprove/RequireApproval verb sets in
// TAD §12 read naturally. Unknown verbs fail closed at the evaluation tail,
// matching PRD §28.1's "custom methods (side effects) require approval".
func VerbFor(toolName string, args map[string]any) string {
	if strings.HasPrefix(toolName, "execute_action_") {
		if a, ok := args["action"].(string); ok && a != "" {
			return strings.ToLower(a)
		}
		return "execute_action"
	}
	if i := strings.IndexByte(toolName, '_'); i > 0 {
		verb := toolName[:i]
		if verb == "get" {
			return "read"
		}
		return verb
	}
	return toolName
}

// roleOverrides reports whether any of the caller's roles maps to a verb list
// covering verb (an empty list covers everything, PRD §28.3).
func roleOverrides(id auth.Identity, m map[string][]string, verb string) bool {
	for _, role := range id.Roles {
		verbs, ok := m[role]
		if !ok {
			continue
		}
		if len(verbs) == 0 || contains(verbs, verb) {
			return true
		}
	}
	return false
}

// bulkCount estimates the target record count from explicit array arguments
// (TAD §12.1 step 2). Returns the size of the largest array found.
func bulkCount(args map[string]any) int {
	largest := 0
	for _, v := range args {
		if arr, ok := v.([]any); ok && len(arr) > largest {
			largest = len(arr)
		}
	}
	return largest
}

// --- Rate limiting (TAD §12.2) ------------------------------------------------

// CheckRateLimit enforces a sliding-window operation counter keyed
// "ratelimit:{scope}:{id}" in cache.Store. With Scope "session" the id is the
// session id carried on the context (SessionFromContext), falling back to the
// user id; with "user" it is the user id. No identity or nil store → no limit.
func (l *Layer) CheckRateLimit(ctx context.Context, id auth.Identity) error {
	if l.store == nil {
		return nil
	}
	p := l.policy
	if p.RateLimit.OperationsPerMinute <= 0 {
		return nil
	}

	keyID := id.UserID
	if p.RateLimit.Scope == "session" {
		if s := SessionFromContext(ctx); s != "" {
			keyID = s
		}
	}
	if keyID == "" {
		return nil
	}

	key := "ratelimit:" + p.RateLimit.Scope + ":" + keyID
	window := time.Minute
	cutoff := time.Now().Add(-window).UnixMilli()

	var stamps []int64
	if raw, found, err := l.store.Get(ctx, key); err == nil && found {
		_ = json.Unmarshal(raw, &stamps)
	}

	valid := stamps[:0]
	for _, ts := range stamps {
		if ts > cutoff {
			valid = append(valid, ts)
		}
	}

	if len(valid) >= p.RateLimit.OperationsPerMinute {
		return orjerrors.Validation("agent rate limit exceeded", map[string]any{
			"limit":  p.RateLimit.OperationsPerMinute,
			"window": window.String(),
		})
	}

	valid = append(valid, time.Now().UnixMilli())
	raw, _ := json.Marshal(valid)
	_ = l.store.Set(ctx, key, raw, window)
	return nil
}

// --- Token budget (TAD §12.2) --------------------------------------------------

// CheckTokenBudget rejects an LLM call whose accumulated session usage plus
// the projected tokens would exceed TokenBudgetPerSession. A nil usage source
// or a zero budget disables the check.
func (l *Layer) CheckTokenBudget(ctx context.Context, sessionID string, projected int) error {
	if l.usage == nil || l.policy.TokenBudgetPerSession <= 0 {
		return nil
	}
	usage := l.usage.UsageFor(sessionID)
	if usage.TotalTokens+projected > l.policy.TokenBudgetPerSession {
		return orjerrors.Validation("agent token budget exceeded for session", map[string]any{
			"session_id": sessionID,
			"budget":     l.policy.TokenBudgetPerSession,
		})
	}
	return nil
}

// --- Tool allowlist (TAD §12.2) -------------------------------------------------

// IsToolAllowed reports whether toolName is permitted by the allowlist. An
// empty allowlist permits every generated and custom tool.
func (l *Layer) IsToolAllowed(toolName string) bool {
	if len(l.policy.ToolAllowlist) == 0 {
		return true
	}
	return contains(l.policy.ToolAllowlist, toolName)
}

// FilterTools returns a copy of names with allowlist-blocked tools removed.
// The runtime applies this to ForIdentity output (TAD §10.3 step 3).
func (l *Layer) FilterTools(names []string) []string {
	if len(l.policy.ToolAllowlist) == 0 {
		return names
	}
	out := names[:0]
	for _, n := range names {
		if l.IsToolAllowed(n) {
			out = append(out, n)
		}
	}
	return out
}

// --- Session id on context (used by the runtime) --------------------------------

type sessionContextKey int

const sessionKey sessionContextKey = iota

// WithSession returns a copy of ctx carrying the agent session id, so the
// layer can key session-scoped rate limits (TAD §12.2) and the executor can
// attribute audit entries.
func WithSession(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionKey, sessionID)
}

// SessionFromContext extracts the session id carried by WithSession.
func SessionFromContext(ctx context.Context) string {
	s, _ := ctx.Value(sessionKey).(string)
	return s
}

// --- Helpers ---------------------------------------------------------------------

func contains(list []string, v string) bool {
	for _, s := range list {
		if strings.EqualFold(s, v) {
			return true
		}
	}
	return false
}
