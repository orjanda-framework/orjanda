package workflow_test

import (
	"context"
	"errors"
	"testing"

	"github.com/orjanda-framework/orjanda/audit"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/dal"
	"github.com/orjanda-framework/orjanda/dal/dialect/sqlite"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/event"
	"github.com/orjanda-framework/orjanda/perm"
	"github.com/orjanda-framework/orjanda/schema"
	"github.com/orjanda-framework/orjanda/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type LeaveRequest struct {
	schema.BaseDocument
	Employee string `oj:"required"`
	Days     int    `oj:"required"`
}

func (l LeaveRequest) DocMeta() schema.Meta {
	return schema.Meta{
		Name: "LeaveRequest",
		Permissions: []schema.DocPermission{
			{Role: "Employee", Read: true, Write: true, Create: true},
			{Role: "HR Manager", Read: true, Write: true, Create: true, Submit: true},
		},
	}
}

func setupWorkflowEngine(t *testing.T) (workflow.Engine, dal.Database, schema.Registry, event.Bus, *audit.InMemoryLog) {
	db, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	reg := schema.NewRegistry()
	require.NoError(t, reg.Register("test-app", &LeaveRequest{}))
	require.NoError(t, reg.Compile())

	permEngine := perm.NewEngine(reg)
	eventBus := event.NewBus()
	auditLog := audit.NewInMemoryLog()

	wfEngine := workflow.NewEngine(db, reg, permEngine, eventBus, auditLog)

	def := workflow.Definition{
		DocType: "LeaveRequest",
		States: []workflow.State{
			{Name: "Draft"},
			{Name: "Submitted"},
			{Name: "Approved"},
		},
		Transitions: []workflow.Transition{
			{
				From:         "Draft",
				To:           "Submitted",
				Action:       "Submit",
				AllowedRoles: []string{"Employee", "HR Manager"},
			},
			{
				From:         "Submitted",
				To:           "Approved",
				Action:       "Approve",
				AllowedRoles: []string{"HR Manager"},
			},
		},
	}

	require.NoError(t, wfEngine.Register(def))

	compiled := reg.List()
	require.NoError(t, db.CreateTables(compiled))
	db.RegisterDocs(compiled)

	return wfEngine, db, reg, eventBus, auditLog
}

func TestWorkflow_RegistrationAndStateField(t *testing.T) {
	_, _, reg, _, _ := setupWorkflowEngine(t)
	compiled, err := reg.Get("LeaveRequest")
	require.NoError(t, err)

	hasWFField := false
	for _, f := range compiled.Fields {
		if f.DBColumn == "workflow_state" {
			hasWFField = true
			break
		}
	}
	assert.True(t, hasWFField, "WorkflowState field must be added to CompiledDoc")
}

func TestWorkflow_AvailableTransitions(t *testing.T) {
	wfEngine, _, _, _, _ := setupWorkflowEngine(t)

	empCtx := auth.NewContext(context.Background(), auth.Identity{Roles: []string{"Employee"}})
	hrCtx := auth.NewContext(context.Background(), auth.Identity{Roles: []string{"HR Manager"}})

	// Employee in Draft state sees Submit transition
	empTrans := wfEngine.AvailableTransitions(empCtx, "LeaveRequest", "Draft")
	require.Len(t, empTrans, 1)
	assert.Equal(t, "Submit", empTrans[0].Action)

	// Employee in Submitted state sees no transition (Approve requires HR Manager)
	empTransSubmitted := wfEngine.AvailableTransitions(empCtx, "LeaveRequest", "Submitted")
	assert.Empty(t, empTransSubmitted)

	// HR Manager in Submitted state sees Approve transition
	hrTrans := wfEngine.AvailableTransitions(hrCtx, "LeaveRequest", "Submitted")
	require.Len(t, hrTrans, 1)
	assert.Equal(t, "Approve", hrTrans[0].Action)
}

func TestWorkflow_ExecuteRolePermissionCheck(t *testing.T) {
	wfEngine, db, _, _, _ := setupWorkflowEngine(t)
	ctx := context.Background()

	// Insert initial document in Draft state
	txErr := db.Transaction(ctx, func(tx dal.Tx) error {
		_, err := tx.Insert(ctx, "LeaveRequest", map[string]any{
			"id":             "lr_1",
			"employee":       "EMP-001",
			"days":           3,
			"workflow_state": "Submitted",
			"deleted":        false,
		})
		return err
	})
	require.NoError(t, txErr)

	// Employee attempts to Approve -> should be rejected with CodePermission
	empCtx := auth.NewContext(ctx, auth.Identity{UserID: "emp_1", Roles: []string{"Employee"}})
	err := wfEngine.Execute(empCtx, "LeaveRequest", "lr_1", "Approve")
	require.Error(t, err)
	var ojErr orjerrors.Error
	require.True(t, errors.As(err, &ojErr))
	assert.Equal(t, orjerrors.CodePermission, ojErr.Code(), "Workflow transition from unauthorized role must return CodePermission")

	// HR Manager attempts to Approve -> succeeds
	hrCtx := auth.NewContext(ctx, auth.Identity{UserID: "hr_1", Roles: []string{"HR Manager"}})
	err = wfEngine.Execute(hrCtx, "LeaveRequest", "lr_1", "Approve")
	assert.NoError(t, err)
}

func TestWorkflow_ExecuteGuardAndAuditLog(t *testing.T) {
	wfEngine, db, _, eventBus, auditLog := setupWorkflowEngine(t)
	ctx := auth.NewContext(context.Background(), auth.Identity{UserID: "hr_1", Roles: []string{"HR Manager"}})

	txErr := db.Transaction(ctx, func(tx dal.Tx) error {
		_, err := tx.Insert(ctx, "LeaveRequest", map[string]any{
			"id":             "lr_2",
			"employee":       "EMP-002",
			"days":           5,
			"workflow_state": "Draft",
			"deleted":        false,
		})
		return err
	})
	require.NoError(t, txErr)

	// Track event emission
	var eventFired bool
	eventBus.On("LeaveRequest", "on_workflow_transition", func(ctx context.Context, doc map[string]any) error {
		eventFired = true
		assert.Equal(t, "Submitted", doc["workflow_state"])
		return nil
	})

	err := wfEngine.Execute(ctx, "LeaveRequest", "lr_2", "Submit")
	require.NoError(t, err)
	assert.True(t, eventFired, "on_workflow_transition event must be emitted")

	// Verify Audit Log entry
	entries, err := auditLog.Query(ctx, audit.QueryFilter{
		DocType: "LeaveRequest",
		DocID:   "lr_2",
	})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "workflow_transition", entries[0].Action)
	assert.Equal(t, "hr_1", entries[0].UserID)
}
