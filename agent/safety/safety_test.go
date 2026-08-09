package safety

import (
	"context"
	"testing"

	"github.com/orjanda-framework/orjanda/agent/llm"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/cache"
)

// fakeUsage is a scripted UsageSource.
type fakeUsage struct {
	usage map[string]llm.TokenUsage
}

func (f *fakeUsage) UsageFor(key string) llm.TokenUsage {
	return f.usage[key]
}

func testIdentity(roles ...string) auth.Identity {
	return auth.Identity{UserID: "u-1", Roles: roles}
}

func testUser(userID string, roles ...string) auth.Identity {
	return auth.Identity{UserID: userID, Roles: roles}
}

func defaultPolicy() SafetyPolicy {
	return SafetyPolicy{
		AutoApprove:       []string{"read", "search", "list"},
		RequireApproval:   []string{"create", "update", "submit"},
		MaxBulkOperations: 5,
	}
}

func TestRequiresApproval_EvaluationOrder(t *testing.T) {
	l := NewLayer(defaultPolicy(), cache.NewLRUStore(100))
	ctx := context.Background()
	id := testIdentity("employee")

	cases := []struct {
		name   string
		tool   string
		args   map[string]any
		info   ToolCallInfo
		want   bool
		reason ApprovalReason
	}{
		{
			name:   "autoapprove read",
			tool:   "get_employee",
			args:   map[string]any{"id": "x"},
			want:   false,
			reason: "",
		},
		{
			name:   "autoapprove list",
			tool:   "list_employee",
			args:   map[string]any{},
			want:   false,
			reason: "",
		},
		{
			name:   "require approval create",
			tool:   "create_employee",
			args:   map[string]any{"first_name": "A"},
			want:   true,
			reason: ReasonRequireApproval,
		},
		{
			name:   "require approval update",
			tool:   "update_employee",
			args:   map[string]any{"id": "x"},
			want:   true,
			reason: ReasonRequireApproval,
		},
		{
			name:   "workflow submit requires approval",
			tool:   "execute_action_leave_request",
			args:   map[string]any{"id": "x", "action": "Submit"},
			want:   true,
			reason: ReasonRequireApproval,
		},
		{
			name:   "always approve delete regardless of config",
			tool:   "delete_employee",
			args:   map[string]any{"id": "x"},
			want:   true,
			reason: ReasonAlwaysRequireApproval,
		},
		{
			name:   "unrecognized verb fails closed",
			tool:   "hr_leave_get_balance",
			args:   map[string]any{},
			want:   true,
			reason: ReasonRequireApproval,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := tc.info
			if info.ToolName == "" {
				info = ToolCallInfo{ToolName: tc.tool, Args: tc.args}
			}
			got := l.RequiresApprovalWithReason(ctx, id, info)
			if got.Required != tc.want || got.Reason != tc.reason {
				t.Errorf("Required = %v (want %v), Reason = %q (want %q)",
					got.Required, tc.want, got.Reason, tc.reason)
			}
		})
	}
}

// Phase 8 completion criterion: a delete operation is always gated by
// approval regardless of SafetyPolicy configuration (TAD §12.1 step 1).
func TestDeleteAlwaysRequiresApproval_EvenWhenPolicySaysAutoApprove(t *testing.T) {
	policy := defaultPolicy()
	policy.AutoApprove = append(policy.AutoApprove, "delete")
	policy.AlwaysRequireApproval = nil
	policy.RequireApprovalForRoles = map[string][]string{"employee": {}}
	// Even an empty delete tool with no role override must require approval.
	l := NewLayer(policy, cache.NewLRUStore(100))

	if !l.RequiresApproval(context.Background(), testIdentity("employee"), "delete_employee", map[string]any{"id": "x"}) {
		t.Fatal("delete must always require approval even when configured as auto-approve")
	}
}

// Phase 8 completion criterion: a bulk operation exceeding MaxBulkOperations
// requires approval even when its verb is in AutoApprove (TAD §12.1 step 2).
func TestBulkLimitRequiresApproval_EvenWhenVerbAutoApproved(t *testing.T) {
	l := NewLayer(defaultPolicy(), cache.NewLRUStore(100))
	ctx := context.Background()
	id := testIdentity("employee")

	// "list" is auto-approved, but a prior search/list result in the session
	// transcript put 50 target records in scope.
	info := ToolCallInfo{
		Verb:        "list",
		ToolName:    "list_employee",
		Args:        map[string]any{},
		TargetCount: 50,
	}
	got := l.RequiresApprovalWithReason(ctx, id, info)
	if !got.Required || got.Reason != ReasonBulkLimit || got.TargetCount != 50 {
		t.Fatalf("bulk over limit must require approval, got %+v", got)
	}

	// Explicit array argument over the limit also triggers BulkLimit.
	args := map[string]any{"records": make([]any, 6)}
	got = l.RequiresApprovalWithReason(ctx, id, ToolCallInfo{Verb: "create", ToolName: "create_employee", Args: args})
	if !got.Required || got.Reason != ReasonBulkLimit || got.TargetCount != 6 {
		t.Fatalf("array arg over limit must require approval, got %+v", got)
	}

	// Under the limit: auto-approve path.
	info = ToolCallInfo{Verb: "list", ToolName: "list_employee", Args: map[string]any{}, TargetCount: 3}
	if got := l.RequiresApprovalWithReason(ctx, id, info); got.Required {
		t.Fatalf("bulk under limit must not require approval, got %+v", got)
	}
}

// TAD §12.1 step 3: a per-role override forces approval for a verb that would
// otherwise auto-approve.
func TestRoleOverrideForcesApproval(t *testing.T) {
	policy := defaultPolicy()
	policy.RequireApprovalForRoles = map[string][]string{
		"intern": {"search", "list", "read"},
	}
	l := NewLayer(policy, cache.NewLRUStore(100))

	intern := testIdentity("intern")
	if !l.RequiresApproval(context.Background(), intern, "search_employee", map[string]any{"query": "x"}) {
		t.Fatal("intern role override must force approval for search")
	}

	// Empty verb list covers every verb (PRD §28.3).
	policy = defaultPolicy()
	policy.RequireApprovalForRoles = map[string][]string{"intern": {}}
	l = NewLayer(policy, cache.NewLRUStore(100))
	if !l.RequiresApproval(context.Background(), intern, "get_employee", map[string]any{"id": "x"}) {
		t.Fatal("empty role verb list must force approval for every verb")
	}

	// Non-overridden role unaffected.
	if l.RequiresApproval(context.Background(), testIdentity("employee"), "get_employee", map[string]any{"id": "x"}) {
		t.Fatal("employee role must keep auto-approve for get")
	}
}

// TAD §12.1 step 5: a verb in AutoApprove is approved; an unrecognized verb
// fails closed.
func TestFailClosedDefault(t *testing.T) {
	l := NewLayer(SafetyPolicy{AutoApprove: []string{"read"}}, cache.NewLRUStore(100))
	if !l.RequiresApproval(context.Background(), testIdentity("employee"), "custom_verb_tool", map[string]any{}) {
		t.Fatal("unrecognized verb must fail closed (require approval)")
	}
	if l.RequiresApproval(context.Background(), testIdentity("employee"), "get_employee", map[string]any{"id": "x"}) {
		t.Fatal("get (read) must auto-approve")
	}
}

func TestCheckRateLimit_UserScope(t *testing.T) {
	policy := defaultPolicy()
	policy.RateLimit = RateLimit{OperationsPerMinute: 2, Scope: "user"}
	l := NewLayer(policy, cache.NewLRUStore(100))
	ctx := context.Background()
	id := testIdentity("employee")

	if err := l.CheckRateLimit(ctx, id); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := l.CheckRateLimit(ctx, id); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if err := l.CheckRateLimit(ctx, id); err == nil {
		t.Fatal("third call must be rate-limited")
	}

	// A different user is unaffected (per-user key).
	if err := l.CheckRateLimit(ctx, testUser("u-2")); err != nil {
		t.Fatalf("other user must not be limited: %v", err)
	}
}

func TestCheckRateLimit_SessionScope(t *testing.T) {
	policy := defaultPolicy()
	policy.RateLimit = RateLimit{OperationsPerMinute: 1, Scope: "session"}
	l := NewLayer(policy, cache.NewLRUStore(100))

	sessCtx := WithSession(context.Background(), "sess-1")
	id := testIdentity("employee")

	if err := l.CheckRateLimit(sessCtx, id); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := l.CheckRateLimit(sessCtx, id); err == nil {
		t.Fatal("second call in same session must be rate-limited")
	}

	// Different session id → different key.
	other := WithSession(context.Background(), "sess-2")
	if err := l.CheckRateLimit(other, id); err != nil {
		t.Fatalf("other session must not be limited: %v", err)
	}
}

func TestCheckTokenBudget(t *testing.T) {
	policy := defaultPolicy()
	policy.TokenBudgetPerSession = 1000
	l := NewLayer(policy, cache.NewLRUStore(100))

	// No usage source wired → check disabled.
	if err := l.CheckTokenBudget(context.Background(), "s1", 500); err != nil {
		t.Fatalf("no usage source must disable the check: %v", err)
	}

	usage := &fakeUsage{usage: map[string]llm.TokenUsage{}}
	usage.usage["s1"] = llm.TokenUsage{TotalTokens: 800}
	l.WithUsageSource(usage)

	if err := l.CheckTokenBudget(context.Background(), "s1", 300); err == nil {
		t.Fatal("budget exceeded must be rejected")
	}
	if err := l.CheckTokenBudget(context.Background(), "s1", 100); err != nil {
		t.Fatalf("within budget must pass: %v", err)
	}
	if err := l.CheckTokenBudget(context.Background(), "s2", 100); err != nil {
		t.Fatalf("unseen session must pass: %v", err)
	}
}

func TestIsToolAllowedAndFilter(t *testing.T) {
	policy := defaultPolicy()
	policy.ToolAllowlist = []string{"create_employee", "get_employee"}
	l := NewLayer(policy, cache.NewLRUStore(100))

	if !l.IsToolAllowed("create_employee") {
		t.Fatal("allowlisted tool must be allowed")
	}
	if l.IsToolAllowed("delete_employee") {
		t.Fatal("non-allowlisted tool must be blocked")
	}

	got := l.FilterTools([]string{"create_employee", "delete_employee", "get_employee"})
	want := []string{"create_employee", "get_employee"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("FilterTools = %v, want %v", got, want)
	}

	open := NewLayer(SafetyPolicy{}, cache.NewLRUStore(100))
	if !open.IsToolAllowed("anything") {
		t.Fatal("empty allowlist must allow everything")
	}
}

func TestVerbFor(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"create_employee", map[string]any{}, "create"},
		{"delete_employee", map[string]any{}, "delete"},
		{"execute_action_leave_request", map[string]any{"action": "Submit"}, "submit"},
		{"execute_action_leave_request", map[string]any{}, "execute_action"},
	}
	for _, tc := range cases {
		if got := VerbFor(tc.tool, tc.args); got != tc.want {
			t.Errorf("VerbFor(%q) = %q, want %q", tc.tool, got, tc.want)
		}
	}
}
