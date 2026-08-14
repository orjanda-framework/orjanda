package runtime

import (
	"context"
	"errors"
	"testing"

	toolreg "github.com/orjanda-framework/orjanda/agent/tools"
	"github.com/orjanda-framework/orjanda/api/rpc"
	"github.com/orjanda-framework/orjanda/auth"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/perm"
	"github.com/orjanda-framework/orjanda/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDispatch_MethodToolEnforcesRoles asserts the Agent Executor re-checks a
// method tool's AllowedRoles at execution time (TAD §9.2 / PRD §25.1), so a
// tool name invoked directly by a non-role-holder is denied even though the
// ToolRegistry would not have advertised it to that identity.
func TestDispatch_MethodToolEnforcesRoles(t *testing.T) {
	rpc.ResetRegistry()
	t.Cleanup(rpc.ResetRegistry)

	rpc.RegisterMethod("task.assign", func(ctx context.Context, args map[string]any) (any, error) {
		return map[string]any{"assigned": true}, nil
	}, rpc.MethodOpts{AllowedRoles: []string{"Task Manager"}})

	rt := &Runtime{
		permEngine: perm.NewEngine(schema.NewRegistry()),
		methodTool: map[string]string{"task_assign": "task.assign"},
	}

	empCtx := auth.NewContext(context.Background(), auth.Identity{UserID: "u-emp", Roles: []string{"Viewer"}})
	_, err := rt.dispatch(empCtx, "task_assign", map[string]any{"task_id": "t1"})
	var ojErr orjerrors.Error
	require.True(t, errors.As(err, &ojErr), "expected CodePermission, got %v", err)
	assert.Equal(t, orjerrors.CodePermission, ojErr.Code())

	mgrCtx := auth.NewContext(context.Background(), auth.Identity{UserID: "u-mgr", Roles: []string{"Task Manager"}})
	res, err := rt.dispatch(mgrCtx, "task_assign", map[string]any{"task_id": "t1"})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"assigned": true}, res)
}

// TestDispatch_CustomToolEnforcesRoles covers the same gate for custom agent
// tools (TAD §10.4): AllowedRoles is enforced at execution, not just at
// ForIdentity projection.
func TestDispatch_CustomToolEnforcesRoles(t *testing.T) {
	toolreg.ResetCustomTools()
	t.Cleanup(toolreg.ResetCustomTools)

	toolreg.RegisterCustomTool(toolreg.Tool{
		Name:         "secret_helper",
		Description:  "role-gated helper",
		Parameters:   map[string]any{"type": "object"},
		AllowedRoles: []string{"Task Manager"},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			return "done", nil
		},
	})

	rt := &Runtime{
		permEngine: perm.NewEngine(schema.NewRegistry()),
		customTool: make(map[string]toolreg.Tool),
	}
	for _, c := range toolreg.CustomTools() {
		rt.customTool[c.Name] = c
	}

	empCtx := auth.NewContext(context.Background(), auth.Identity{UserID: "u-emp", Roles: []string{"Viewer"}})
	_, err := rt.dispatch(empCtx, "secret_helper", nil)
	var ojErr orjerrors.Error
	require.True(t, errors.As(err, &ojErr), "expected CodePermission, got %v", err)
	assert.Equal(t, orjerrors.CodePermission, ojErr.Code())

	mgrCtx := auth.NewContext(context.Background(), auth.Identity{UserID: "u-mgr", Roles: []string{"Task Manager"}})
	res, err := rt.dispatch(mgrCtx, "secret_helper", nil)
	require.NoError(t, err)
	assert.Equal(t, "done", res)
}
