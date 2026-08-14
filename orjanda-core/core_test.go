package core_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"

	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/dal"
	"github.com/orjanda-framework/orjanda/dal/dialect/sqlite"
	core "github.com/orjanda-framework/orjanda/orjanda-core"
	"github.com/orjanda-framework/orjanda/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	modsqlite "modernc.org/sqlite"
)

// buildRegistry compiles a schema.Registry containing the four core documents.
func buildRegistry(t *testing.T) schema.Registry {
	t.Helper()
	reg := schema.NewRegistry()
	require.NoError(t, reg.Register("core", &core.User{}))
	require.NoError(t, reg.Register("core", &core.Role{}))
	require.NoError(t, reg.Register("core", &core.RolePermission{}))
	require.NoError(t, reg.Compile())
	return reg
}

// buildDB creates an in-memory SQLite database and creates all tables for reg.
func buildDB(t *testing.T, reg schema.Registry) *sqlite.DB {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	docs := reg.List()
	db.RegisterDocs(docs)

	// Also register child-table name mappings so Insert("UserRole",...) resolves.
	for _, doc := range docs {
		for _, child := range doc.ChildTables {
			db.RegisterDoc(child.TypeName, child.TableName)
		}
	}

	require.NoError(t, db.CreateTables(docs))
	return db
}

// sel is a convenience wrapper for dal.Select with optional filters.
func sel(docType string, filters map[string]any) dal.Select {
	return dal.Select{DocType: docType, Filters: filters}
}

// ---------------------------------------------------------------------------
// Core Document schema registration
// ---------------------------------------------------------------------------

func TestCoreDocuments_RegisterInRegistry(t *testing.T) {
	reg := buildRegistry(t)

	user, err := reg.Get("User")
	require.NoError(t, err)
	assert.Equal(t, "User", user.Name)

	role, err := reg.Get("Role")
	require.NoError(t, err)
	assert.Equal(t, "Role", role.Name)

	rp, err := reg.Get("RolePermission")
	require.NoError(t, err)
	assert.Equal(t, "RolePermission", rp.Name)
}

// ---------------------------------------------------------------------------
// User Get/Set
// ---------------------------------------------------------------------------

func TestUser_GetSet(t *testing.T) {
	u := &core.User{}
	require.NoError(t, u.Set("Email", "alice@example.com"))
	require.NoError(t, u.Set("FullName", "Alice"))
	require.NoError(t, u.Set("Active", true))
	require.NoError(t, u.Set("Password", "hashed"))

	assert.Equal(t, "alice@example.com", u.Get("Email"))
	assert.Equal(t, "Alice", u.Get("FullName"))
	assert.Equal(t, true, u.Get("Active"))
	assert.Equal(t, "hashed", u.Get("Password"))
}

// ---------------------------------------------------------------------------
// Role Get/Set
// ---------------------------------------------------------------------------

func TestRole_GetSet(t *testing.T) {
	r := &core.Role{}
	require.NoError(t, r.Set("RoleName", "HR Manager"))
	assert.Equal(t, "HR Manager", r.Get("RoleName"))
}

// ---------------------------------------------------------------------------
// RolePermission Get/Set
// ---------------------------------------------------------------------------

func TestRolePermission_GetSet(t *testing.T) {
	rp := &core.RolePermission{}
	require.NoError(t, rp.Set("Role", "System Administrator"))
	require.NoError(t, rp.Set("DocType", "Employee"))
	require.NoError(t, rp.Set("Read", true))
	require.NoError(t, rp.Set("Write", false))
	require.NoError(t, rp.Set("Create", true))
	require.NoError(t, rp.Set("Delete", false))
	require.NoError(t, rp.Set("Submit", true))

	assert.Equal(t, "System Administrator", rp.Get("Role"))
	assert.Equal(t, "Employee", rp.Get("DocType"))
	assert.Equal(t, true, rp.Get("Read"))
	assert.Equal(t, false, rp.Get("Write"))
	assert.Equal(t, true, rp.Get("Create"))
	assert.Equal(t, false, rp.Get("Delete"))
	assert.Equal(t, true, rp.Get("Submit"))
}

// ---------------------------------------------------------------------------
// Bootstrap — first-run
// ---------------------------------------------------------------------------

func TestBootstrap_CreatesAdminUserWithHashedPassword(t *testing.T) {
	reg := buildRegistry(t)
	db := buildDB(t, reg)
	ctx := context.Background()

	password, err := core.Bootstrap(ctx, db, reg)
	require.NoError(t, err)
	require.NotEmpty(t, password, "bootstrap must return the generated password on first run")

	users, err := db.Query(ctx, sel("User", map[string]any{"email": core.AdminEmail}))
	require.NoError(t, err)
	require.Len(t, users, 1)
	assert.Equal(t, core.AdminEmail, users[0]["email"])

	// Stored value is bcrypt hash, NOT the plain password
	hashed, _ := users[0]["password"].(string)
	require.NotEmpty(t, hashed)
	assert.True(t, auth.CheckPassword(hashed, password), "bcrypt hash must match the returned plaintext password")
	assert.NotEqual(t, password, hashed, "password must not be stored in plaintext")
}

func TestBootstrap_CreatesSystemAdministratorRole(t *testing.T) {
	reg := buildRegistry(t)
	db := buildDB(t, reg)
	ctx := context.Background()

	_, err := core.Bootstrap(ctx, db, reg)
	require.NoError(t, err)

	roles, err := db.Query(ctx, sel("Role", map[string]any{"role_name": core.AdminRole}))
	require.NoError(t, err)
	require.Len(t, roles, 1, "System Administrator role must be created")
	assert.Equal(t, core.AdminRole, roles[0]["role_name"])
}

func TestBootstrap_GrantsAllPermissionsOnAllDocTypes(t *testing.T) {
	reg := buildRegistry(t)
	db := buildDB(t, reg)
	ctx := context.Background()

	_, err := core.Bootstrap(ctx, db, reg)
	require.NoError(t, err)

	compiledDocs := reg.List()
	for _, doc := range compiledDocs {
		perms, qErr := db.Query(ctx, sel("RolePermission", map[string]any{
			"role":     core.AdminRole,
			"doc_type": doc.Name,
		}))
		require.NoError(t, qErr)
		assert.NotEmpty(t, perms, "expected RolePermission for DocType %q", doc.Name)

		// All CRUD+Submit flags must be granted
		if len(perms) > 0 {
			assert.Equal(t, int64(1), perms[0]["read"], "read must be granted for %s", doc.Name)
			assert.Equal(t, int64(1), perms[0]["write"], "write must be granted for %s", doc.Name)
			assert.Equal(t, int64(1), perms[0]["create"], "create must be granted for %s", doc.Name)
			assert.Equal(t, int64(1), perms[0]["delete"], "delete must be granted for %s", doc.Name)
			assert.Equal(t, int64(1), perms[0]["submit"], "submit must be granted for %s", doc.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// Bootstrap — idempotency
// ---------------------------------------------------------------------------

func TestBootstrap_IsIdempotent(t *testing.T) {
	reg := buildRegistry(t)
	db := buildDB(t, reg)
	ctx := context.Background()

	// First call bootstraps
	password, err := core.Bootstrap(ctx, db, reg)
	require.NoError(t, err)
	require.NotEmpty(t, password)

	// Second call is a no-op
	password2, err := core.Bootstrap(ctx, db, reg)
	require.NoError(t, err)
	assert.Empty(t, password2, "second bootstrap call must return empty password (already bootstrapped)")

	// Exactly one user must exist
	users, err := db.Query(ctx, sel("User", nil))
	require.NoError(t, err)
	assert.Len(t, users, 1, "only one user must exist after two bootstrap calls")
}

// ---------------------------------------------------------------------------
// Bootstrap — concurrency safety (REVIEW-2026-08-12 finding 12)
// ---------------------------------------------------------------------------

func TestBootstrap_ConcurrentCallsCreateSingleAdmin(t *testing.T) {
	reg := buildRegistry(t)
	// File-backed DB so concurrent connections share one database (a
	// ":memory:" DSN gives every pooled connection its own private database).
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "bootstrap.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	docs := reg.List()
	db.RegisterDocs(docs)
	for _, doc := range docs {
		for _, child := range doc.ChildTables {
			db.RegisterDoc(child.TypeName, child.TableName)
		}
	}
	require.NoError(t, db.CreateTables(docs))

	ctx := context.Background()
	const callers = 8

	var wg sync.WaitGroup
	errs := make([]error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, e := core.Bootstrap(ctx, db, reg)
			errs[idx] = e
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		assert.NoError(t, e, "concurrent bootstrap call %d must not error", i)
	}

	// Exactly one admin user, one role, one permission set per DocType.
	users, err := db.Query(ctx, sel("User", nil))
	require.NoError(t, err)
	assert.Len(t, users, 1, "exactly one admin user must exist after %d concurrent bootstraps", callers)

	roles, err := db.Query(ctx, sel("Role", map[string]any{"role_name": core.AdminRole}))
	require.NoError(t, err)
	assert.Len(t, roles, 1, "exactly one System Administrator role must exist")

	for _, doc := range reg.List() {
		perms, qErr := db.Query(ctx, sel("RolePermission", map[string]any{
			"role":     core.AdminRole,
			"doc_type": doc.Name,
		}))
		require.NoError(t, qErr)
		assert.Len(t, perms, 1, "exactly one permission row per DocType after concurrent bootstraps")
	}
}

// captureHandler records slog records as text for assertions.
type captureHandler struct {
	buf *bytes.Buffer
}

func (h *captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	fmt.Fprintln(h.buf, r.Message)
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(h.buf, "  %s=%v\n", a.Key, a.Value.Any())
		return true
	})
	return nil
}

func (h *captureHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(_ string) slog.Handler      { return h }

func TestBootstrap_DoesNotLogPassword(t *testing.T) {
	reg := buildRegistry(t)
	db := buildDB(t, reg)
	ctx := context.Background()

	old := slog.Default()
	defer slog.SetDefault(old)
	var buf bytes.Buffer
	slog.SetDefault(slog.New(&captureHandler{buf: &buf}))

	password, err := core.Bootstrap(ctx, db, reg)
	require.NoError(t, err)
	require.NotEmpty(t, password)

	assert.Contains(t, buf.String(), core.AdminEmail, "bootstrap still logs the admin email")
	assert.NotContains(t, buf.String(), password, "the generated password must never reach the log stream (REVIEW-2026-08-12 finding 12)")
}

// TestDB_UniqueConstraintPreventsDuplicateAdminEmail proves the SQLite dialect
// emits UNIQUE for oj:"unique" fields (matching PostgreSQL). Bootstrap bypasses
// the document engine, so the database constraint is the only guard against a
// concurrent duplicate admin; a duplicate insert here must fail at the DAL.
func TestDB_UniqueConstraintPreventsDuplicateAdminEmail(t *testing.T) {
	reg := buildRegistry(t)
	db := buildDB(t, reg)
	ctx := context.Background()

	_, err := core.Bootstrap(ctx, db, reg)
	require.NoError(t, err)

	_, err = db.Insert(ctx, "User", map[string]any{
		"email":     core.AdminEmail,
		"full_name": "Second Admin",
	})
	require.Error(t, err, "duplicate admin email must be rejected by the database")

	var sqliteErr *modsqlite.Error
	require.True(t, errors.As(err, &sqliteErr), "error must unwrap to the sqlite driver error, got %T", err)
	assert.Equal(t, 2067, sqliteErr.Code(), "expected SQLITE_CONSTRAINT_UNIQUE (2067)")
}

// ---------------------------------------------------------------------------
// App definition
// ---------------------------------------------------------------------------

func TestAppDefinition(t *testing.T) {
	assert.Equal(t, "core", core.App.Name)
	assert.Equal(t, "0.1.0", core.App.Version)
	require.Len(t, core.App.Modules, 1)
	assert.Equal(t, "core", core.App.Modules[0].Name)
}
