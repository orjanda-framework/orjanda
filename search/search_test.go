package search_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/orjanda-framework/orjanda/dal/dialect/sqlite"
	"github.com/orjanda-framework/orjanda/search"
)

// setupSearchDB opens an in-memory SQLite DB with an employees table and
// some seed data for FTS tests.
func setupSearchDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS "employees" (
			"id"       TEXT PRIMARY KEY,
			"name"     TEXT NOT NULL,
			"email"    TEXT,
			"deleted"  INTEGER DEFAULT 0
		)`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `INSERT INTO "employees" ("id", "name", "email", "deleted") VALUES
		('id-001', 'Alice Johnson', 'alice@example.com', 0),
		('id-002', 'Bob Smith',    'bob@example.com',   0),
		('id-003', 'Charlie Alice','charlie@example.com', 0)`)
	require.NoError(t, err)

	return db, func() { _ = db.Close() }
}

// Phase 2 Completion Criterion 5:
// FullTextSearch returns matching IDs for a Searchable field.

func TestDialectBackend_Search_ReturnsMatchingIDs(t *testing.T) {
	db, cleanup := setupSearchDB(t)
	defer cleanup()

	d := sqlite.New()
	backend := search.NewDialectBackendSimple(
		d,
		db,
		map[string]string{"Employee": "employees"},
		map[string][]string{"Employee": {"name", "email"}},
	)

	ctx := context.Background()

	// Search for "Alice" — should return id-001 and id-003.
	ids, err := backend.Search(ctx, "Employee", "Alice", 10)
	require.NoError(t, err)
	assert.Contains(t, ids, "id-001")
	assert.Contains(t, ids, "id-003")
	assert.NotContains(t, ids, "id-002")
}

func TestDialectBackend_Search_NoMatch(t *testing.T) {
	db, cleanup := setupSearchDB(t)
	defer cleanup()

	d := sqlite.New()
	backend := search.NewDialectBackendSimple(
		d, db,
		map[string]string{"Employee": "employees"},
		map[string][]string{"Employee": {"name"}},
	)

	ctx := context.Background()
	ids, err := backend.Search(ctx, "Employee", "Zzzzz_NoMatch", 10)
	require.NoError(t, err)
	assert.Empty(t, ids)
}

func TestDialectBackend_Index_Remove_NoOp(t *testing.T) {
	db, cleanup := setupSearchDB(t)
	defer cleanup()

	d := sqlite.New()
	backend := search.NewDialectBackendSimple(
		d, db,
		map[string]string{"Employee": "employees"},
		map[string][]string{"Employee": {"name"}},
	)

	ctx := context.Background()

	// Index and Remove are no-ops for the Dialect backend.
	err := backend.Index(ctx, "Employee", "id-001", map[string]any{"name": "Alice"})
	assert.NoError(t, err)

	err = backend.Remove(ctx, "Employee", "id-001")
	assert.NoError(t, err)
}

func TestDialectBackend_Search_UnknownDocType(t *testing.T) {
	db, cleanup := setupSearchDB(t)
	defer cleanup()

	d := sqlite.New()
	backend := search.NewDialectBackendSimple(
		d, db,
		map[string]string{},
		map[string][]string{},
	)

	ctx := context.Background()
	_, err := backend.Search(ctx, "UnknownType", "query", 10)
	require.Error(t, err)
}

func TestDialectBackend_Search_LimitApplied(t *testing.T) {
	db, cleanup := setupSearchDB(t)
	defer cleanup()

	d := sqlite.New()
	backend := search.NewDialectBackendSimple(
		d, db,
		map[string]string{"Employee": "employees"},
		map[string][]string{"Employee": {"name"}},
	)

	ctx := context.Background()
	// "Alice" matches id-001 and id-003 but limit=1 should return at most 1.
	ids, err := backend.Search(ctx, "Employee", "Alice", 1)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(ids), 1)
}

// Ensure the search package and sqlite package can be imported together
// without circular dependencies.
func TestImportSanity(_ *testing.T) {
	_ = time.Now() // use time to prevent import-only errors
}
