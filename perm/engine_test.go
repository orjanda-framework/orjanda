package perm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/orjanda-framework/orjanda/auth"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/perm"
	"github.com/orjanda-framework/orjanda/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type DummyDoc struct {
	schema.BaseDocument
	Salary float64 `oj:"permission=HR Manager"`
}

func (d DummyDoc) DocMeta() schema.Meta {
	return schema.Meta{
		Name: "DummyDoc",
		Permissions: []schema.DocPermission{
			{Role: "Employee", Read: true},
			{Role: "HR Manager", Read: true, Write: true, Create: true, Delete: true},
		},
	}
}

type denyAllRule struct{}

func (r denyAllRule) Evaluate(ctx context.Context, check perm.Check) error {
	return orjerrors.Permission("denied by custom rule")
}

func setupRegistry(t *testing.T) schema.Registry {
	reg := schema.NewRegistry()
	require.NoError(t, reg.Register("test-app", &DummyDoc{}))
	require.NoError(t, reg.Compile())
	return reg
}

func TestPermEngine_RBACCheckAction(t *testing.T) {
	reg := setupRegistry(t)
	pEngine := perm.NewEngine(reg)

	empCtx := auth.NewContext(context.Background(), auth.Identity{
		UserID: "usr_1",
		Roles:  []string{"Employee"},
	})
	hrCtx := auth.NewContext(context.Background(), auth.Identity{
		UserID: "usr_2",
		Roles:  []string{"HR Manager"},
	})

	// Employee can Read, but not Create/Write/Delete
	assert.NoError(t, pEngine.CheckAction(empCtx, "DummyDoc", "read"))
	err := pEngine.CheckAction(empCtx, "DummyDoc", "create")
	require.Error(t, err)
	var ojErr orjerrors.Error
	require.True(t, errors.As(err, &ojErr))
	assert.Equal(t, orjerrors.CodePermission, ojErr.Code())

	// HR Manager can do everything
	assert.NoError(t, pEngine.CheckAction(hrCtx, "DummyDoc", "read"))
	assert.NoError(t, pEngine.CheckAction(hrCtx, "DummyDoc", "create"))
	assert.NoError(t, pEngine.CheckAction(hrCtx, "DummyDoc", "write"))
	assert.NoError(t, pEngine.CheckAction(hrCtx, "DummyDoc", "delete"))
}

func TestPermEngine_ABACRuleAndComposition(t *testing.T) {
	reg := setupRegistry(t)
	pEngine := perm.NewEngine(reg)

	hrCtx := auth.NewContext(context.Background(), auth.Identity{
		UserID: "usr_2",
		Roles:  []string{"HR Manager"},
	})

	// RBAC passes for HR Manager
	assert.NoError(t, pEngine.CheckAction(hrCtx, "DummyDoc", "read"))

	// Register custom rule that denies
	pEngine.RegisterRule(denyAllRule{})

	// RBAC passes, but custom Rule denies -> AND composition
	err := pEngine.CheckAction(hrCtx, "DummyDoc", "read")
	require.Error(t, err)
	var ojErr orjerrors.Error
	require.True(t, errors.As(err, &ojErr))
	assert.Equal(t, orjerrors.CodePermission, ojErr.Code())
}

func TestPermEngine_FieldLevelFilterReadAndWrite(t *testing.T) {
	reg := setupRegistry(t)
	pEngine := perm.NewEngine(reg)

	empCtx := auth.NewContext(context.Background(), auth.Identity{
		UserID: "usr_1",
		Roles:  []string{"Employee"},
	})
	hrCtx := auth.NewContext(context.Background(), auth.Identity{
		UserID: "usr_2",
		Roles:  []string{"HR Manager"},
	})

	data := map[string]any{
		"id":     "doc_1",
		"Salary": 100000.0,
	}

	// FilterRead: Employee does NOT see Salary
	empRead, err := pEngine.FilterRead(empCtx, "DummyDoc", data)
	require.NoError(t, err)
	_, hasSalary := empRead["Salary"]
	assert.False(t, hasSalary, "Salary field must be filtered out for Employee")

	// FilterRead: HR Manager sees Salary
	hrRead, err := pEngine.FilterRead(hrCtx, "DummyDoc", data)
	require.NoError(t, err)
	assert.Equal(t, 100000.0, hrRead["Salary"])

	// FilterWrite: Employee supplying Salary gets REJECTED (CodePermission)
	_, err = pEngine.FilterWrite(empCtx, "DummyDoc", data)
	require.Error(t, err)
	var ojErr orjerrors.Error
	require.True(t, errors.As(err, &ojErr))
	assert.Equal(t, orjerrors.CodePermission, ojErr.Code(), "Gated field must be actively rejected on write")

	// FilterWrite: HR Manager supplying Salary passes
	hrWrite, err := pEngine.FilterWrite(hrCtx, "DummyDoc", data)
	require.NoError(t, err)
	assert.Equal(t, 100000.0, hrWrite["Salary"])
}

// TestPermEngine_CheckRoles covers the synthetic-docType role gate of
// TAD §9.2 ("method:<name>") and TAD §8.1 step 3 (workflow transitions):
// union-of-roles, System Administrator override, "*" wildcard, empty list
// grants (public), case-insensitivity, and CodePermission denial.
func TestPermEngine_CheckRoles(t *testing.T) {
	pEngine := perm.NewEngine(schema.NewRegistry())

	cases := []struct {
		name  string
		roles []string
		user  []string
		want  bool
	}{
		{"empty list grants (public)", nil, []string{"Viewer"}, true},
		{"matching role grants", []string{"Task Manager"}, []string{"Task Manager"}, true},
		{"case-insensitive match grants", []string{"task manager"}, []string{"Task Manager"}, true},
		{"wildcard grants any role", []string{"*"}, []string{"Viewer"}, true},
		{"System Administrator always grants", []string{"Task Manager"}, []string{"System Administrator"}, true},
		{"no matching role denies", []string{"Task Manager"}, []string{"Viewer"}, false},
		{"identity with no roles denies gated method", []string{"Task Manager"}, nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := auth.NewContext(context.Background(), auth.Identity{
				UserID: "usr_1",
				Roles:  tc.user,
			})
			err := pEngine.CheckRoles(ctx, "method:task.assign", "call", tc.roles)
			if tc.want {
				assert.NoError(t, err)
				return
			}
			var ojErr orjerrors.Error
			require.True(t, errors.As(err, &ojErr), "expected CodePermission, got %v", err)
			assert.Equal(t, orjerrors.CodePermission, ojErr.Code())
		})
	}
}
