package main

import (
	"context"
	"testing"

	"github.com/orjanda-framework/orjanda/agent/llm"
	"github.com/orjanda-framework/orjanda/auth"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	orjtesting "github.com/orjanda-framework/orjanda/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	leavedocs "github.com/orjanda-framework/orjanda-app-hr-example/modules/leave/documents"
	orgdocs "github.com/orjanda-framework/orjanda-app-hr-example/modules/org/documents"
)

// newHRSite provisions the reference Application on the orjanda/testing harness
// (TAD §17): all four Documents registered under app "hr", the LeaveApproval
// workflow registered, and the custom tool's site handle attached. Temporary
// validation test — not part of the committed Application.
func newHRSite(t *testing.T) *orjtesting.TestSite {
	t.Helper()
	site := orjtesting.NewTestSite(t,
		orjtesting.WithDocuments("hr",
			&orgdocs.Department{}, &orgdocs.Employee{},
			&leavedocs.LeaveType{}, &leavedocs.LeaveRequest{}),
	)
	require.NoError(t, site.Workflows.Register(LeaveApproval))
	registerHooks(site.Site)
	appSite = site.Site
	return site
}

// TestHRDocumentsCRUD covers the PRD §36.1 requirement that all four Documents
// are operable through the Document Engine under an HR Manager.
func TestHRDocumentsCRUD(t *testing.T) {
	site := newHRSite(t)
	hr := site.CreateUser(t, "hr@test.com", "hr_manager")
	ctx := site.WithUser(hr)

	deptID, err := site.Document.Create(ctx, "Department", map[string]any{
		"Name": "Engineering", "Description": "Builds the product",
	})
	require.NoError(t, err)

	empID, err := site.Document.Create(ctx, "Employee", map[string]any{
		"FirstName": "Jane", "LastName": "Doe",
		"Email":      "jane@test.com",
		"Department": deptID,
		"JoinDate":   "2020-01-15",
		"Salary":     75000.0,
	})
	require.NoError(t, err)

	ltID, err := site.Document.Create(ctx, "LeaveType", map[string]any{
		"Name": "Annual", "MaxDaysPerYear": 20, "IsPaid": true,
	})
	require.NoError(t, err)

	lrID, err := site.Document.Create(ctx, "LeaveRequest", map[string]any{
		"Employee": empID, "LeaveType": ltID,
		"FromDate": "2026-08-15", "ToDate": "2026-08-16", "Reason": "Vacation",
	})
	require.NoError(t, err)

	dept, err := site.Document.Read(ctx, "Department", deptID)
	require.NoError(t, err)
	assert.Equal(t, "Engineering", dept["name"])
	assert.Equal(t, "Builds the product", dept["description"])

	emp, err := site.Document.Read(ctx, "Employee", empID)
	require.NoError(t, err)
	assert.Equal(t, "Jane", emp["first_name"])
	assert.Equal(t, 75000.0, emp["salary"])

	lt, err := site.Document.Read(ctx, "LeaveType", ltID)
	require.NoError(t, err)
	assert.Equal(t, 20, asInt(lt["max_days_per_year"]))
	assert.Equal(t, int64(1), lt["is_paid"], "sqlite returns booleans as int64")

	lr, err := site.Document.Read(ctx, "LeaveRequest", lrID)
	require.NoError(t, err)
	assert.Equal(t, "Draft", lr["workflow_state"], "workflow seeds Draft on insert")
	assert.Equal(t, "Vacation", lr["reason"])
}

// TestLeaveApprovalWorkflow verifies PRD §19.3: Draft → Pending Approval →
// Approved with role-gated transitions — an Employee may Submit but only a
// Department Head / HR Manager may Approve.
func TestLeaveApprovalWorkflow(t *testing.T) {
	site := newHRSite(t)
	hr := site.CreateUser(t, "hr@test.com", "hr_manager")
	head := site.CreateUser(t, "head@test.com", "department_head")
	emp := site.CreateUser(t, "emp@test.com", "employee")
	hrCtx := site.WithUser(hr)
	empCtx := site.WithUser(emp)

	deptID, err := site.Document.Create(hrCtx, "Department", map[string]any{"Name": "Engineering"})
	require.NoError(t, err)
	empID, err := site.Document.Create(hrCtx, "Employee", map[string]any{
		"FirstName": "Jane", "LastName": "Doe",
		"Email": "jane@test.com", "Department": deptID, "JoinDate": "2020-01-15",
	})
	require.NoError(t, err)
	ltID, err := site.Document.Create(hrCtx, "LeaveType", map[string]any{
		"Name": "Annual", "MaxDaysPerYear": 20, "IsPaid": true,
	})
	require.NoError(t, err)

	lrID, err := site.Document.Create(empCtx, "LeaveRequest", map[string]any{
		"Employee": empID, "LeaveType": ltID,
		"FromDate": "2026-08-15", "ToDate": "2026-08-16", "Reason": "Vacation",
	})
	require.NoError(t, err)

	// Employee submits their own request.
	require.NoError(t, site.Workflows.Execute(empCtx, "LeaveRequest", lrID, "Submit"))
	lr, err := site.Document.Read(empCtx, "LeaveRequest", lrID)
	require.NoError(t, err)
	assert.Equal(t, "Pending Approval", lr["workflow_state"])

	// An Employee cannot Approve.
	err = site.Workflows.Execute(empCtx, "LeaveRequest", lrID, "Approve")
	require.Error(t, err)
	assert.True(t, orjerrors.Is(err, orjerrors.ErrPermission),
		"Approve must be denied to an Employee, got: %v", err)

	// A Department Head can Approve.
	headCtx := site.WithUser(head)
	require.NoError(t, site.Workflows.Execute(headCtx, "LeaveRequest", lrID, "Approve"))
	lr, err = site.Document.Read(empCtx, "LeaveRequest", lrID)
	require.NoError(t, err)
	assert.Equal(t, "Approved", lr["workflow_state"])
}

// TestEmployeeSalaryFieldGating verifies the Phase 12 criterion that Salary is
// invisible to roles without hr_manager — in Read results, in write attempts,
// and in the per-identity agent tool schema.
func TestEmployeeSalaryFieldGating(t *testing.T) {
	site := newHRSite(t)
	hr := site.CreateUser(t, "hr@test.com", "hr_manager")
	emp := site.CreateUser(t, "emp@test.com", "employee")
	hrCtx := site.WithUser(hr)
	empCtx := site.WithUser(emp)

	deptID, err := site.Document.Create(hrCtx, "Department", map[string]any{"Name": "Engineering"})
	require.NoError(t, err)
	empID, err := site.Document.Create(hrCtx, "Employee", map[string]any{
		"FirstName": "Jane", "LastName": "Doe",
		"Email": "jane@test.com", "Department": deptID, "JoinDate": "2020-01-15",
		"Salary": 75000.0,
	})
	require.NoError(t, err)

	asEmp, err := site.Document.Read(empCtx, "Employee", empID)
	require.NoError(t, err)
	_, ok := asEmp["salary"]
	assert.False(t, ok, "employee read must not expose salary")

	asHR, err := site.Document.Read(hrCtx, "Employee", empID)
	require.NoError(t, err)
	assert.Equal(t, 75000.0, asHR["salary"])

	_, err = site.Document.Create(empCtx, "Employee", map[string]any{
		"FirstName": "Bob", "LastName": "Nope",
		"Email": "bob@test.com", "Department": deptID, "JoinDate": "2020-01-15",
		"Salary": 99999.0,
	})
	require.Error(t, err)
	assert.True(t, orjerrors.Is(err, orjerrors.ErrPermission),
		"writing salary without hr_manager must be rejected, got: %v", err)

	// Agent tool schema: employees cannot create Employee records at all (no
	// create_employee tool), while hr_manager's create_employee exposes salary.
	// The "field absent in the tool schema" guarantee therefore cannot be
	// observed via create for employees — the read-strip and write-rejection
	// assertions above cover the negative path (PRD §25.1, TAD §10.3).
	assert.Nil(t, toolFor(site, emp, "create_employee"), "employee must not see create_employee")
	hrSchema := toolFor(site, hr, "create_employee")
	require.NotNil(t, hrSchema, "hr_manager must see create_employee")
	assert.Contains(t, props(hrSchema), "salary")
}

// TestHRHooks verifies the two named business-logic hooks: active employees
// must have a department (PRD §19.2) and leave requests must not be
// backdated (PRD §37 validate hook).
func TestHRHooks(t *testing.T) {
	site := newHRSite(t)
	hr := site.CreateUser(t, "hr@test.com", "hr_manager")
	ctx := site.WithUser(hr)

	_, err := site.Document.Create(ctx, "Employee", map[string]any{
		"FirstName": "Ada", "LastName": "Lovelace",
		"Email": "ada@test.com", "JoinDate": "2021-01-01",
	})
	require.Error(t, err)
	assert.True(t, orjerrors.Is(err, orjerrors.ErrValidation),
		"active employee without department must be rejected, got: %v", err)

	deptID, err := site.Document.Create(ctx, "Department", map[string]any{"Name": "Engineering"})
	require.NoError(t, err)
	empID, err := site.Document.Create(ctx, "Employee", map[string]any{
		"FirstName": "Ada", "LastName": "Lovelace",
		"Email": "ada@test.com", "Department": deptID, "JoinDate": "2021-01-01",
	})
	require.NoError(t, err)
	ltID, err := site.Document.Create(ctx, "LeaveType", map[string]any{
		"Name": "Annual", "MaxDaysPerYear": 20, "IsPaid": true,
	})
	require.NoError(t, err)

	_, err = site.Document.Create(ctx, "LeaveRequest", map[string]any{
		"Employee": empID, "LeaveType": ltID,
		"FromDate": "2026-08-16", "ToDate": "2026-08-15", "Reason": "oops",
	})
	require.Error(t, err)
	assert.True(t, orjerrors.Is(err, orjerrors.ErrValidation),
		"backdated leave request must be rejected, got: %v", err)
}

// TestCalculateLeaveBalanceTool verifies the one hand-written agent tool
// (PRD §24.3): it is role-gated to employees and HR Managers, absent for other
// roles, and computes remaining days from approved requests.
func TestCalculateLeaveBalanceTool(t *testing.T) {
	site := newHRSite(t)
	hr := site.CreateUser(t, "hr@test.com", "hr_manager")
	head := site.CreateUser(t, "head@test.com", "department_head")
	emp := site.CreateUser(t, "emp@test.com", "employee")
	intern := site.CreateUser(t, "intern@test.com", "Intern")
	hrCtx := site.WithUser(hr)
	empCtx := site.WithUser(emp)

	deptID, err := site.Document.Create(hrCtx, "Department", map[string]any{"Name": "Engineering"})
	require.NoError(t, err)
	empID, err := site.Document.Create(hrCtx, "Employee", map[string]any{
		"FirstName": "Jane", "LastName": "Doe",
		"Email": "jane@test.com", "Department": deptID, "JoinDate": "2020-01-15",
	})
	require.NoError(t, err)
	ltID, err := site.Document.Create(hrCtx, "LeaveType", map[string]any{
		"Name": "Annual", "MaxDaysPerYear": 20, "IsPaid": true,
	})
	require.NoError(t, err)

	// One 2-day approved request: 20 - 2 = 18 remaining.
	lrID, err := site.Document.Create(empCtx, "LeaveRequest", map[string]any{
		"Employee": empID, "LeaveType": ltID,
		"FromDate": "2026-08-15", "ToDate": "2026-08-16", "Reason": "Vacation",
	})
	require.NoError(t, err)
	require.NoError(t, site.Workflows.Execute(empCtx, "LeaveRequest", lrID, "Submit"))
	require.NoError(t, site.Workflows.Execute(site.WithUser(head), "LeaveRequest", lrID, "Approve"))

	assert.NotNil(t, toolFor(site, emp, "calculate_leave_balance"))
	assert.NotNil(t, toolFor(site, hr, "calculate_leave_balance"))
	assert.Nil(t, toolFor(site, intern, "calculate_leave_balance"), "Intern must not see the tool")

	res, err := calculateLeaveBalance(empCtx, map[string]any{
		"employee_id": empID,
		"leave_type":  "Annual",
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"employee_id": empID,
		"leave_type":  "Annual",
		"max_days":    20,
		"used_days":   2,
		"remaining":   18,
	}, res)
}

// toolFor returns the agent tool definition named name for identity id, or nil.
func toolFor(site *orjtesting.TestSite, id auth.Identity, name string) *llm.ToolDefinition {
	for _, tool := range site.Tools.ForIdentity(context.Background(), id) {
		if tool.Name == name {
			t := tool
			return &t
		}
	}
	return nil
}

// props returns the JSON Schema properties map of a tool's parameters.
func props(t *llm.ToolDefinition) map[string]any {
	if t == nil {
		return nil
	}
	p, _ := t.Parameters["properties"].(map[string]any)
	return p
}
