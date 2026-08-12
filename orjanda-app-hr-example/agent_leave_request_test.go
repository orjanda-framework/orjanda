package main

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orjanda-framework/orjanda/agent"
	"github.com/orjanda-framework/orjanda/agent/llm"
	"github.com/orjanda-framework/orjanda/agent/runtime"
	"github.com/orjanda-framework/orjanda/audit"
	"github.com/orjanda-framework/orjanda/document"
	orjtesting "github.com/orjanda-framework/orjanda/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedLeaveRequestData seeds the Department/Employee/LeaveType rows the
// leave-request tests operate on, returning the created employee and leave
// type ids. Runs through the real Document Engine under an HR Manager
// identity (an employee cannot create Departments), exactly as the REST API
// and agent tool would.
func seedLeaveRequestData(t *testing.T, site *orjtesting.TestSite) (empID, ltID string) {
	t.Helper()
	hr := site.CreateUser(t, "hr@test.com", "hr_manager")
	ctx := site.WithUser(hr)
	deptID, err := site.Document.Create(ctx, "Department", map[string]any{"Name": "Engineering"})
	require.NoError(t, err)
	empID, err = site.Document.Create(ctx, "Employee", map[string]any{
		"FirstName": "Ada", "LastName": "Lovelace",
		"Email": "ada@test.com", "Department": deptID, "JoinDate": "2021-01-01",
	})
	require.NoError(t, err)
	ltID, err = site.Document.Create(ctx, "LeaveType", map[string]any{
		"Name": "Annual", "MaxDaysPerYear": 20, "IsPaid": true,
	})
	require.NoError(t, err)
	return empID, ltID
}

// TestAgentLeaveRequestApprovalRoundTrip is the deterministic regression test
// for the Phase 12 criterion "the agent creates a leave request with approval
// confirmation" (PRD §44.4 item 4b, TAD §12.3): a scripted LLM drives live
// tool calls through the real Agent Runtime and Document Engine against the
// seeded database. The create_leave_request verb ("create") is not
// auto-approved by the Safety Layer, so the runtime must run an
// approval_required round trip before the record is written — the persisted
// row and its audit entry prove the round trip happened and only then
// executed. Runs in the default test lane (no network, no API key).
func TestAgentLeaveRequestApprovalRoundTrip(t *testing.T) {
	site := newHRSite(t)
	emp := site.CreateUser(t, "emp@test.com", "employee")
	ctx := site.WithUser(emp)

	empID, ltID := seedLeaveRequestData(t, site)

	prompt := "Create a leave request for Ada Lovelace: Annual leave, 2026-08-20 to 2026-08-21, reason family event. Do not submit it."
	steps := []orjtesting.MockStep{
		orjtesting.ToolCall("describe_document", map[string]any{"doc_type": "Employee"}),
		orjtesting.ToolCall("describe_document", map[string]any{"doc_type": "LeaveType"}),
		orjtesting.ToolCall("describe_document", map[string]any{"doc_type": "LeaveRequest"}),
		orjtesting.ToolCall("list_employee", map[string]any{"limit": 100}),
		orjtesting.ToolCall("list_leave_type", map[string]any{"limit": 100}),
		orjtesting.ToolCall("create_leave_request", map[string]any{
			"employee": empID, "leave_type": ltID,
			"from_date": "2026-08-20", "to_date": "2026-08-21", "reason": "Family event",
		}),
		orjtesting.ApprovalPrompt(),
		orjtesting.TextResponse("I created the leave request for Ada."),
	}

	resp, err := site.Agent.Execute(ctx, prompt, agent.WithProvider(orjtesting.MockLLM(t, steps...)))
	require.NoError(t, err)
	assert.Equal(t, 6, resp.ToolCalls, "exactly the six live tool calls of the discovery->create flow")
	assert.Contains(t, resp.Content, "created")

	// The LeaveRequest is persisted via the single Document Engine path, with
	// the seeded link ids, the reason, and the workflow state seeded by the
	// app's before_save hook (TAD §8.1 step 1).
	rows, err := site.Document.List(ctx, "LeaveRequest", document.ListOpts{Limit: 100})
	require.NoError(t, err)
	require.Len(t, rows, 1, "exactly one LeaveRequest must be created")
	lr := rows[0]
	assert.Equal(t, empID, lr["employee"])
	assert.Equal(t, ltID, lr["leave_type"])
	assert.Equal(t, "Family event", lr["reason"])
	assert.Equal(t, "Draft", lr["workflow_state"])

	// The create was agent-initiated: the audit entry is attributed to the
	// agent session and prompt (TAD §13.3 via_agent), not a human user.
	entries, err := site.AuditLog.Query(ctx, audit.QueryFilter{DocType: "LeaveRequest"})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "create", entries[0].Action)
	assert.True(t, entries[0].ViaAgent, "agent write must be marked via_agent")
	assert.Equal(t, resp.SessionID, entries[0].AgentSession)
	assert.Equal(t, prompt, entries[0].AgentPrompt)
}

// TestAgentLeaveRequestLiveOpenRouter runs the Phase 12 leave-request
// criterion end to end against a real LLM: the openai_compatible provider
// talking to OpenRouter, the real Agent Runtime, live tool calls, the Safety
// Layer approval_required round trip, and the final LeaveRequest read back
// from the database with its audit attribution. Skipped unless
// ORJANDA_OPENROUTER_API_KEY is set, so the default CI lane stays
// deterministic.
func TestAgentLeaveRequestLiveOpenRouter(t *testing.T) {
	apiKey := os.Getenv("ORJANDA_OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("set ORJANDA_OPENROUTER_API_KEY to run the live OpenRouter leave-request flow")
	}
	baseURL := os.Getenv("ORJANDA_OPENROUTER_BASE_URL")
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}
	model := os.Getenv("ORJANDA_OPENROUTER_MODEL")
	if model == "" {
		model = "nvidia/nemotron-3-super-120b-a12b:free"
	}

	site := newHRSite(t)
	emp := site.CreateUser(t, "emp@test.com", "employee")
	ctx, cancel := context.WithTimeout(site.WithUser(emp), 5*time.Minute)
	defer cancel()

	empID, ltID := seedLeaveRequestData(t, site)

	provider, err := llm.NewOpenAICompatibleProvider(llm.ProviderOptions{
		APIKey:    apiKey,
		BaseURL:   baseURL,
		Model:     model,
		MaxTokens: 1024,
	})
	require.NoError(t, err)

	gate := &approvingGateway{}
	resp, err := site.Agent.Execute(ctx,
		"Create a leave request for Ada Lovelace (email ada@test.com) using the Annual leave type, from 2026-08-20 to 2026-08-21, with the reason \"family event\". Do not submit the request; only create it. Look up the employee and the Annual leave type in the system first, then create the leave request.",
		agent.WithProvider(provider),
		agent.WithApprovals(gate))
	require.NoError(t, err)
	t.Logf("agent final answer: %q (tool calls: %d)", resp.Content, resp.ToolCalls)

	// The approval confirmation round trip must have been answered for the
	// create verb with the TAD §12.3 details (policy_reason require_approval).
	require.Greater(t, len(gate.requests), 0, "the create must have triggered an approval round trip")
	createApproval := gate.approvalFor("create")
	require.NotNil(t, createApproval, "create_leave_request must be gated by an approval, got %+v", gate.requests)
	assert.Equal(t, "LeaveRequest", createApproval.Details.DocType)
	assert.Equal(t, "RequireApproval", createApproval.Details.PolicyReason)

	// The LeaveRequest is persisted in the real database with the seeded link
	// ids, the reason the agent was asked for, and the workflow state seeded by
	// the before_save hook — the approval gate was the only way this write
	// could have happened (the runtime rejects ungated writes).
	rows, err := site.Document.List(ctx, "LeaveRequest", document.ListOpts{Limit: 100})
	require.NoError(t, err)
	require.NotEmpty(t, rows, "the agent must have created a LeaveRequest")
	lr := rows[0]
	assert.Equal(t, empID, lr["employee"])
	assert.Equal(t, ltID, lr["leave_type"])
	reason, _ := lr["reason"].(string)
	assert.Contains(t, strings.ToLower(reason), "family", "the persisted reason should carry the requested reason")
	assert.Equal(t, "Draft", lr["workflow_state"])

	entries, err := site.AuditLog.Query(ctx, audit.QueryFilter{DocType: "LeaveRequest"})
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	assert.True(t, entries[0].ViaAgent, "agent create must be audited via_agent")
}

// approvingGateway is a test ApprovalGateway (TAD §12.3) that answers every
// round trip with Approved while recording the payloads, so the test can
// assert the round trip happened and inspect its details.
type approvingGateway struct {
	mu       sync.Mutex
	requests []runtime.ApprovalPayload
}

func (g *approvingGateway) RequestApproval(_ context.Context, req runtime.ApprovalPayload) (runtime.ApprovalResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.requests = append(g.requests, req)
	return runtime.ApprovalResponse{ActionID: req.ActionID, Approved: true}, nil
}

func (g *approvingGateway) approvalFor(action string) *runtime.ApprovalPayload {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i := range g.requests {
		if g.requests[i].Details.Action == action {
			return &g.requests[i]
		}
	}
	return nil
}
