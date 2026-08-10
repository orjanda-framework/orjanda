package testing

import (
	"testing"

	"github.com/orjanda-framework/orjanda/agent"
	"github.com/orjanda-framework/orjanda/document"
	"github.com/orjanda-framework/orjanda/perm"
	"github.com/orjanda-framework/orjanda/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testLeaveRequest is the PRD §32.2 application Document. It lives here rather
// than in a real Application because orjanda-app-hr-example (the reference
// Application that owns the production LeaveRequest) is Phase 12 scope.
type testLeaveRequest struct {
	schema.BaseDocument
	Employee  string `oj:"required"`
	LeaveType string `oj:"required"`
	FromDate  string `oj:"required"`
	ToDate    string `oj:"required"`
	// WorkflowState is supplied by the caller on create; the reference
	// Application's workflow seeds it to "Draft" (TAD §8.1 step 1).
	WorkflowState string
}

func (l *testLeaveRequest) DocMeta() schema.Meta {
	return schema.Meta{
		Name:        "LeaveRequest",
		Description: "Test LeaveRequest document (PRD §32.2)",
		Permissions: []schema.DocPermission{
			{Role: "HR Manager", Read: true, Create: true},
		},
	}
}

// testEmployee is the PRD §32.3 searchable Document.
type testEmployee struct {
	schema.BaseDocument
	FirstName  string `oj:"required,searchable"`
	LastName   string `oj:"required,searchable"`
	Email      string `oj:"required,format=email,unique,searchable"`
	Department string `oj:"searchable"`
}

func (e *testEmployee) DocMeta() schema.Meta {
	return schema.Meta{
		Name:        "Employee",
		Description: "Test employee document (PRD §32.3)",
		Permissions: []schema.DocPermission{
			{Role: "HR Manager", Read: true, Create: true},
		},
	}
}

// TestLeaveRequestCreation is the PRD §32.2 acceptance example, adapted only
// for the actual package paths: the Document Engine Create returns an id
// (which the read-back confirms), the workflow_state column is snake_case,
// and Delete returns just an error.
func TestLeaveRequestCreation(t *testing.T) {
	site := NewTestSite(t, WithDocuments("hr-test", &testLeaveRequest{}))

	// Create test user with specific roles
	user := site.CreateUser(t, "jane@test.com", "HR Manager")
	ctx := site.WithUser(user)

	// Test document creation
	id, err := site.Document.Create(ctx, "LeaveRequest", map[string]any{
		"Employee":      "EMP-001",
		"LeaveType":     "Annual",
		"FromDate":      "2026-08-15",
		"ToDate":        "2026-08-16",
		"WorkflowState": "Draft",
	})
	require.NoError(t, err)

	doc, err := site.Document.Read(ctx, "LeaveRequest", id)
	require.NoError(t, err)
	assert.Equal(t, "Draft", doc["workflow_state"])
	assert.Equal(t, id, doc["id"])

	// Test permission enforcement
	intern := site.CreateUser(t, "bob@test.com", "Intern")
	internCtx := site.WithUser(intern)
	err = site.Document.Delete(internCtx, "LeaveRequest", id)
	assert.ErrorIs(t, err, perm.ErrPermissionDenied)
}

// TestAgentCanSearchEmployees is the PRD §32.3 acceptance example, adapted to
// the real tool surface: the search tool is per-DocType search_employee
// (TAD §10.1 step 1) with a plain "query" argument, and the runtime Response
// carries the final answer in Content.
func TestAgentCanSearchEmployees(t *testing.T) {
	site := NewTestSite(t, WithDocuments("hr-test", &testEmployee{}))
	site.SeedFixtures(t, "testdata/employees.json")

	// Use a mock LLM that returns predetermined tool calls
	mock := MockLLM(t,
		ToolCall("search_employee", map[string]any{"query": "engineering"}),
		TextResponse("Found 5 employees in Engineering."),
	)

	user := site.CreateUser(t, "jane@test.com", "HR Manager")
	ctx := site.WithUser(user)

	resp, err := site.Agent.Execute(ctx, "How many employees are in Engineering?",
		agent.WithProvider(mock))
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "Engineering")
}

// TestNewTestSiteParallelIsolation proves guarantee 1 of TAD §17.1: every
// NewTestSite owns a fresh database, so sites built concurrently (as parallel
// tests do) cannot share records.
func TestNewTestSiteParallelIsolation(t *testing.T) {
	for _, who := range []string{"alice", "bob"} {
		who := who
		t.Run(who, func(t *testing.T) {
			t.Parallel()

			site := NewTestSite(t, WithDocuments("hr-test", &testEmployee{}))
			site.CreateUser(t, who+"@test.com", "HR Manager")

			// A brand-new site from the same binary must not contain this
			// user's record; cross-site leakage would be a harness bug.
			other := NewTestSite(t, WithDocuments("hr-test", &testEmployee{}))
			users, err := other.Document.List(sysadminCtx(), "User", document.ListOpts{})
			require.NoError(t, err)
			assert.Empty(t, users, "a fresh site must start with zero User records")
		})
	}
}

// TestMockLLMPlanAndExecuteWithApproval scripts a full Plan-and-Execute turn
// that escalates on a data dependency (TAD §11.2 step 2), is gated by a single
// combined plan approval round trip (TAD §11.2 step b / §12.3), then executes
// both steps and synthesizes the final summary — the phase-11 completion
// criterion that MockLLM script an ApprovalPrompt exchange. The create step
// requires approval under the harness default policy (only read/search/list
// are auto-approved), so the ApprovalPrompt step is what lets it run at all.
func TestMockLLMPlanAndExecuteWithApproval(t *testing.T) {
	site := NewTestSite(t, WithDocuments("hr-test", &testEmployee{}))

	mock := MockLLM(t,
		ToolCall("describe_document", map[string]any{"doc_type": "Employee"}),
		ToolCalls(
			ToolCall("create_employee", map[string]any{
				"first_name": "Grace",
				"last_name":  "Hopper",
				"email":      "grace@example.com",
			}),
			ToolCall("get_employee", map[string]any{"id": "ref:0.id"}),
		),
		TextResponse(`{"steps":[
			{"operation":"create_employee","args":{"first_name":"Grace","last_name":"Hopper","email":"grace@example.com"}},
			{"operation":"get_employee","args":{"id":"ref:0.id"}}
		]}`),
		ApprovalPrompt(),
		TextResponse("Created Grace Hopper after approval."),
	)

	user := site.CreateUser(t, "jane@test.com", "HR Manager")
	ctx := site.WithUser(user)

	resp, err := site.Agent.Execute(ctx, "create Grace Hopper then fetch her record",
		agent.WithProvider(mock))
	require.NoError(t, err)
	assert.Equal(t, 2, resp.ToolCalls, "both plan steps must have executed")
	assert.Contains(t, resp.Content, "Grace Hopper")

	// The plan was approved (the ApprovalPrompt step was consumed) and the
	// create really ran: the employee exists after the turn.
	rows, err := site.Document.List(ctx, "Employee", document.ListOpts{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "Grace", rows[0]["first_name"])
}
