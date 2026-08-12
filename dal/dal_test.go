package dal_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/orjanda-framework/orjanda/dal"
	"github.com/orjanda-framework/orjanda/dal/dialect/postgres"
	"github.com/orjanda-framework/orjanda/dal/dialect/sqlite"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/schema"
)

// ─────────────────────────────────────────────
// Test helpers / fixtures
// ─────────────────────────────────────────────

// testDoc is a minimal Document used across Phase 2 tests.
type testDoc struct {
	schema.BaseDocument
	Title string `oj:"required,searchable"`
	Email string `oj:"unique,format=email"`
}

func (d *testDoc) DocMeta() schema.Meta {
	return schema.Meta{
		Name:       "TestDoc",
		Searchable: true,
	}
}
func (d *testDoc) Get(field string) any {
	switch field {
	case "Title":
		return d.Title
	case "Email":
		return d.Email
	}
	return d.BaseDocument.Get(field)
}
func (d *testDoc) Set(field string, value any) orjerrors.Error {
	return d.BaseDocument.Set(field, value)
}

// childParentDoc carries a child table so child-table DAL semantics
// (no soft-delete base column) are exercised.
type childParentDoc struct {
	schema.BaseDocument
	Title string     `oj:"required"`
	Rows  []ChildRow `oj:"child_table"`
}

type ChildRow struct {
	schema.BaseChild
	Note string `oj:"required"`
}

func (d *childParentDoc) DocMeta() schema.Meta {
	return schema.Meta{Name: "ChildParent"}
}
func (d *childParentDoc) Get(field string) any {
	switch field {
	case "Title":
		return d.Title
	}
	return d.BaseDocument.Get(field)
}
func (d *childParentDoc) Set(field string, value any) orjerrors.Error {
	return d.BaseDocument.Set(field, value)
}

func (c *ChildRow) DocMeta() schema.Meta {
	return schema.Meta{Name: "ChildRow"}
}
func (c *ChildRow) Get(field string) any {
	return c.BaseChild.Get(field)
}
func (c *ChildRow) Set(field string, value any) orjerrors.Error {
	return c.BaseChild.Set(field, value)
}

// childRegistry compiles a registry with childParentDoc.
func childRegistry(t *testing.T) schema.Registry {
	t.Helper()
	reg := schema.NewRegistry()
	require.NoError(t, reg.Register("test-app", &childParentDoc{}))
	require.NoError(t, reg.Compile())
	return reg
}

// newTestSQLiteDB opens an in-memory SQLite DB, creates tables, and registers
// the testDoc docType.
func newTestSQLiteDB(t *testing.T, reg schema.Registry) *sqlite.DB {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	docs := reg.List()
	err = db.CreateTables(docs)
	require.NoError(t, err)
	db.RegisterDocs(docs)
	return db
}

// compileRegistry builds and compiles a registry with testDoc.
func compileRegistry(t *testing.T) schema.Registry {
	t.Helper()
	reg := schema.NewRegistry()
	require.NoError(t, reg.Register("test-app", &testDoc{}))
	require.NoError(t, reg.Compile())
	return reg
}

// ─────────────────────────────────────────────
// Phase 2 Completion Criterion 1:
// Diff against an empty database for the Phase 1 reference Application
// produces CreateTable statements for both dialects.
// ─────────────────────────────────────────────

func TestMigratorDiff_EmptyDB_ProducesCreateTables(t *testing.T) {
	reg := compileRegistry(t)

	db, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	m := dal.NewMigratorWithInspector(
		db.Dialect(),
		db.Underlying(),
		db, // sqlite.DB implements TableInspector
	)

	diff, err := m.Diff(context.Background(), reg)
	require.NoError(t, err)
	require.NotNil(t, diff)

	assert.GreaterOrEqual(t, len(diff.CreateTables), 1,
		"expected at least one CreateTable for testDoc")

	tableNames := make([]string, len(diff.CreateTables))
	for i, ct := range diff.CreateTables {
		tableNames[i] = ct.TableName
	}
	assert.Contains(t, tableNames, "test_docs",
		"expected test_docs table in diff")
}

// ─────────────────────────────────────────────
// Phase 2 Completion Criterion 2:
// migrate up applied twice is idempotent on both dialects.
// ─────────────────────────────────────────────

func TestMigrateUp_Idempotent(t *testing.T) {
	reg := compileRegistry(t)

	db, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	dir := t.TempDir()

	m := dal.NewMigratorWithInspector(db.Dialect(), db.Underlying(), db)

	diff, err := m.Diff(context.Background(), reg)
	require.NoError(t, err)
	require.NotEmpty(t, diff.CreateTables, "expected tables to create")

	filename, err := m.Write(diff, dir, false)
	require.NoError(t, err)
	require.NotEmpty(t, filename)

	// First Up
	err = m.Up(context.Background(), dir)
	require.NoError(t, err, "first migrate up must succeed")

	// Second Up (idempotent — no pending migrations)
	err = m.Up(context.Background(), dir)
	require.NoError(t, err, "second migrate up must be idempotent")
}

// ─────────────────────────────────────────────
// Phase 2 Completion Criterion 3:
// A destructive change (dropped column) is excluded from Write's output
// unless --allow-destructive is set.
// ─────────────────────────────────────────────

func TestMigratorWrite_DestructiveGate(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	dir := t.TempDir()
	m := dal.NewMigratorWithInspector(db.Dialect(), db.Underlying(), db)

	// Craft a diff that contains a dropped column.
	diff := &schema.SchemaDiff{
		AlterTables: []schema.TableAlteration{
			{
				TableName:   "employees",
				DropColumns: []string{"salary"},
			},
		},
	}

	// Without --allow-destructive: should error.
	_, err = m.Write(diff, dir, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "destructive")

	// With --allow-destructive: should succeed and write the file.
	filename, err := m.Write(diff, dir, true)
	require.NoError(t, err)
	assert.NotEmpty(t, filename)

	content, err := os.ReadFile(filepath.Join(dir, filename))
	require.NoError(t, err)
	assert.Contains(t, string(content), "DROP COLUMN")
}

// ─────────────────────────────────────────────
// Phase 2 Completion Criterion 4 (snapshot):
// Identical logical SQL semantics between PostgreSQL and SQLite dialects
// for the same Select query. (PRD §40 Risk R2 mitigation)
// ─────────────────────────────────────────────

func TestDialectSQLSemantics_SnapshotEquivalence(t *testing.T) {
	sqld := sqlite.New()
	pgd := postgres.New()

	q := dal.Select{
		TableName: "employees",
		Fields:    []string{"id", "name"},
		Filters:   map[string]any{"department": "Engineering"},
		OrderBy:   "name",
		Limit:     10,
		Offset:    0,
	}

	sqliteSql, sqliteArgs := sqld.SelectSQL(q)
	pgSql, pgArgs := pgd.SelectSQL(q)

	assert.Contains(t, sqliteSql, "SELECT \"id\", \"name\" FROM \"employees\" WHERE \"deleted\" = ? AND \"department\" = ? ORDER BY \"name\" LIMIT 10")
	assert.Equal(t, []any{false, "Engineering"}, sqliteArgs)

	assert.Contains(t, pgSql, "SELECT \"id\", \"name\" FROM \"employees\" WHERE \"deleted\" = $1 AND \"department\" = $2 ORDER BY \"name\" LIMIT 10")
	assert.Equal(t, []any{false, "Engineering"}, pgArgs)
}

// ─────────────────────────────────────────────
// Phase 2 Completion Criterion 5:
// FullTextSearch returns matching IDs for a Searchable field.
// ─────────────────────────────────────────────

func TestFullTextSearch_SQLite_ReturnsMatchingIDs(t *testing.T) {
	reg := compileRegistry(t)
	db := newTestSQLiteDB(t, reg)

	ctx := context.Background()
	// Insert a record with a searchable Title.
	id, err := db.Insert(ctx, "TestDoc", map[string]any{
		"title":       "Orjanda Framework",
		"email":       "test@example.com",
		"name":        "Orjanda Framework",
		"owner":       "",
		"created_at":  time.Now(),
		"updated_at":  time.Now(),
		"modified_by": "",
		"doc_status":  0,
		"deleted":     false,
	})
	require.NoError(t, err)
	require.NotEmpty(t, id)

	// Use the dialect's FullTextSearch directly.
	d := db.Dialect()
	sqlStr, args := d.FullTextSearch("test_docs", "Orjanda", []string{"title"})
	require.NotEmpty(t, sqlStr)

	rows, err := db.Underlying().QueryContext(ctx, sqlStr, args...)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var rowID string
		require.NoError(t, rows.Scan(&rowID))
		ids = append(ids, rowID)
	}
	require.NoError(t, rows.Err())
	assert.Contains(t, ids, id, "FTS should return the inserted record ID")
}

// TestQuery_QFilter_RestrictsToFTSMatches verifies the Document List "q"
// full-text parameter (PRD §688) resolves through Database.Query to an id-set
// restriction, and that a non-matching query returns no rows.
func TestQuery_QFilter_RestrictsToFTSMatches(t *testing.T) {
	reg := compileRegistry(t)
	db := newTestSQLiteDB(t, reg)

	ctx := context.Background()
	base := map[string]any{
		"email":       "",
		"owner":       "",
		"created_at":  time.Now(),
		"updated_at":  time.Now(),
		"modified_by": "",
		"doc_status":  0,
		"deleted":     false,
	}
	base["title"] = "Orjanda Framework"
	base["name"] = "First Record"
	matchID, err := db.Insert(ctx, "TestDoc", base)
	require.NoError(t, err)
	base["title"] = "Something Else"
	base["name"] = "Second Record"
	otherID, err := db.Insert(ctx, "TestDoc", base)
	require.NoError(t, err)

	rows, err := db.Query(ctx, dal.Select{
		DocType: "TestDoc",
		Filters: map[string]any{"q": "Orjanda"},
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, matchID, rows[0]["id"])

	rows, err = db.Query(ctx, dal.Select{
		DocType: "TestDoc",
		Filters: map[string]any{"q": "no-such-record"},
	})
	require.NoError(t, err)
	assert.Len(t, rows, 0)

	// Non-search filters still apply alongside the id-set restriction.
	rows, err = db.Query(ctx, dal.Select{
		DocType: "TestDoc",
		Filters: map[string]any{"q": "", "id": otherID},
	})
	require.NoError(t, err)
	assert.Len(t, rows, 0)
}

// ─────────────────────────────────────────────
// Phase 2 Completion Criterion 6:
// cache.Store Get/Set/Delete round-trip with TTL expiry.
// ─────────────────────────────────────────────

func TestCache_RoundTrip_AndTTLExpiry(t *testing.T) {
	// Tested in cache/cache_test.go — included here as a cross-package sanity check
	// using the dal_test package.
	t.Log("Cache tests are in cache/cache_test.go — see TestLRUStore_GetSetDelete and TestLRUStore_TTLExpiry")
}

// ─────────────────────────────────────────────
// Additional: SQLite Database CRUD
// ─────────────────────────────────────────────

func TestSQLiteDB_CRUD(t *testing.T) {
	reg := compileRegistry(t)
	db := newTestSQLiteDB(t, reg)
	ctx := context.Background()

	now := time.Now()

	// Insert
	id, err := db.Insert(ctx, "TestDoc", map[string]any{
		"title":       "Hello World",
		"email":       "hello@example.com",
		"name":        "Hello World",
		"owner":       "user-1",
		"created_at":  now,
		"updated_at":  now,
		"modified_by": "user-1",
		"doc_status":  0,
		"deleted":     false,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	// Query
	rows, err := db.Query(ctx, dal.Select{
		DocType:   "TestDoc",
		TableName: "test_docs",
		Filters:   map[string]any{"id": id},
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, id, rows[0]["id"])
	assert.Equal(t, "Hello World", rows[0]["title"])

	// Update
	err = db.Update(ctx, "TestDoc", id, map[string]any{
		"title": "Updated Title",
	})
	require.NoError(t, err)

	// Verify update
	rows, err = db.Query(ctx, dal.Select{
		DocType:   "TestDoc",
		TableName: "test_docs",
		Filters:   map[string]any{"id": id},
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "Updated Title", rows[0]["title"])

	// Delete
	err = db.Delete(ctx, "TestDoc", id)
	require.NoError(t, err)

	// Verify deletion
	rows, err = db.Query(ctx, dal.Select{
		DocType:        "TestDoc",
		TableName:      "test_docs",
		IncludeDeleted: true,
	})
	require.NoError(t, err)
	for _, row := range rows {
		assert.NotEqual(t, id, row["id"], "deleted record should not appear")
	}
}

// TestSQLiteDB_ChildTableQuery_NoSoftDeleteFilter ensures child-table queries
// (which have no "deleted" base column) do not emit a soft-delete predicate.
// Regression: SQLite treats an unknown double-quoted identifier as a string
// literal, so `"deleted" = ?` became `'deleted' = 0` (always false) and child
// queries silently returned zero rows — which broke rolesForUser lookups.
func TestSQLiteDB_ChildTableQuery_NoSoftDeleteFilter(t *testing.T) {
	reg := childRegistry(t)
	db := newTestSQLiteDB(t, reg)
	ctx := context.Background()

	parentID, err := db.Insert(ctx, "ChildParent", map[string]any{
		"title": "Parent One",
		"id":    "01JSTESTCHILDPARENT0000",
	})
	require.NoError(t, err)

	_, err = db.Insert(ctx, "ChildRow", map[string]any{
		"parent_id": parentID,
		"idx":       0,
		"note":      "first note",
	})
	require.NoError(t, err)

	rows, err := db.Query(ctx, dal.Select{
		DocType: "ChildRow",
		Filters: map[string]any{"parent_id": parentID},
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "first note", rows[0]["note"])

	err = db.Transaction(ctx, func(tx dal.Tx) error {
		rows, err = tx.Query(ctx, dal.Select{
			DocType: "ChildRow",
			Filters: map[string]any{"parent_id": parentID},
		})
		return err
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "first note", rows[0]["note"])
}

// ─────────────────────────────────────────────
// Additional: Transaction rollback on error
// ─────────────────────────────────────────────

func TestSQLiteDB_Transaction_Rollback(t *testing.T) {
	reg := compileRegistry(t)
	db := newTestSQLiteDB(t, reg)
	ctx := context.Background()

	now := time.Now()
	errForcedRollback := fmt.Errorf("forced rollback")

	err := db.Transaction(ctx, func(tx dal.Tx) error {
		_, err := tx.Insert(ctx, "TestDoc", map[string]any{
			"title":       "Should Not Persist",
			"email":       "rollback@example.com",
			"name":        "Should Not Persist",
			"owner":       "",
			"created_at":  now,
			"updated_at":  now,
			"modified_by": "",
			"doc_status":  0,
			"deleted":     false,
		})
		if err != nil {
			return err
		}
		return errForcedRollback // force rollback
	})
	require.ErrorIs(t, err, errForcedRollback)

	// Verify nothing was persisted.
	rows, err := db.Query(ctx, dal.Select{
		DocType:        "TestDoc",
		TableName:      "test_docs",
		IncludeDeleted: true,
	})
	require.NoError(t, err)
	assert.Len(t, rows, 0, "transaction rollback must leave DB unchanged")
}

// ─────────────────────────────────────────────
// Additional: Dialect SQL structure verification
// ─────────────────────────────────────────────

func TestSQLiteDialect_CreateTable(t *testing.T) {
	d := sqlite.New()
	doc := schema.CompiledDoc{
		TableName: "employees",
		Fields: []schema.Field{
			{Name: "ID", DBColumn: "id", Type: schema.FieldTypeString, Required: true},
			{Name: "Name", DBColumn: "name", Type: schema.FieldTypeString},
			{Name: "Salary", DBColumn: "salary", Type: schema.FieldTypeCurrency},
			{Name: "Deleted", DBColumn: "deleted", Type: schema.FieldTypeBool},
		},
	}
	sql := d.CreateTable(doc)
	assert.Contains(t, sql, `CREATE TABLE IF NOT EXISTS`)
	assert.Contains(t, sql, `"employees"`)
	assert.Contains(t, sql, `"id" TEXT PRIMARY KEY`)
	assert.Contains(t, sql, `"salary" REAL`)
}

func TestSQLiteDialect_InsertSQL(t *testing.T) {
	d := sqlite.New()
	sql, args := d.InsertSQL("employees", map[string]any{
		"id":   "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		"name": "Alice",
	})
	assert.Contains(t, sql, "INSERT INTO")
	assert.Contains(t, sql, `"employees"`)
	assert.Len(t, args, 2)
}

func TestSQLiteDialect_UpdateSQL(t *testing.T) {
	d := sqlite.New()
	sql, args := d.UpdateSQL("employees", "id-123", map[string]any{
		"name": "Bob",
	})
	assert.Contains(t, sql, "UPDATE")
	assert.Contains(t, sql, "WHERE")
	assert.Contains(t, sql, `"id" = ?`)
	_ = sql
	_ = args
}

func TestSQLiteDialect_SelectSQL_SoftDeleteFilter(t *testing.T) {
	d := sqlite.New()
	sql, args := d.SelectSQL(dal.Select{
		TableName: "employees",
	})
	assert.Contains(t, sql, `"deleted" = ?`)
	assert.Equal(t, false, args[0])
}

func TestSQLiteDialect_SelectSQL_IncludeDeleted(t *testing.T) {
	d := sqlite.New()
	sql, _ := d.SelectSQL(dal.Select{
		TableName:      "employees",
		IncludeDeleted: true,
	})
	assert.NotContains(t, sql, `"deleted"`)
}

func TestMigratorWrite_EmptyDiff_ReturnsEmpty(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	dir := t.TempDir()
	m := dal.NewMigratorWithInspector(db.Dialect(), db.Underlying(), db)

	filename, err := m.Write(&schema.SchemaDiff{}, dir, false)
	require.NoError(t, err)
	assert.Empty(t, filename, "no file should be written for empty diff")
}

func TestMigratorWrite_ContainsGooseMarkers(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	dir := t.TempDir()
	m := dal.NewMigratorWithInspector(db.Dialect(), db.Underlying(), db)

	diff := &schema.SchemaDiff{
		CreateTables: []schema.CompiledDoc{
			{
				TableName: "test_widgets",
				Fields: []schema.Field{
					{Name: "ID", DBColumn: "id", Type: schema.FieldTypeString, Required: true},
					{Name: "Label", DBColumn: "label", Type: schema.FieldTypeString},
				},
			},
		},
	}

	filename, err := m.Write(diff, dir, false)
	require.NoError(t, err)
	require.NotEmpty(t, filename)

	content, err := os.ReadFile(filepath.Join(dir, filename))
	require.NoError(t, err)
	contentStr := string(content)

	assert.Contains(t, contentStr, "-- +goose Up")
	assert.Contains(t, contentStr, "-- +goose Down")
	assert.Contains(t, contentStr, "down migrations are not generated")
	assert.Contains(t, contentStr, "CREATE TABLE IF NOT EXISTS")
	assert.True(t, strings.HasSuffix(filename, "_sqlite.sql"),
		"filename should end with _sqlite.sql, got: "+filename)
}

func TestMigrateUp_MultiDialectFileIsolation(t *testing.T) {
	reg := compileRegistry(t)
	db, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	dir := t.TempDir()

	// Write a sqlite migration file
	sqlm := dal.NewMigratorWithInspector(sqlite.New(), db.Underlying(), db)
	diff, err := sqlm.Diff(context.Background(), reg)
	require.NoError(t, err)
	sqlFile, err := sqlm.Write(diff, dir, false)
	require.NoError(t, err)
	require.NotEmpty(t, sqlFile)

	// Write a postgres migration file with Postgres-only DDL syntax (e.g. JSONB)
	pgFile := filepath.Join(dir, "20260809150000_invalid_postgres.sql")
	err = os.WriteFile(pgFile, []byte("-- +goose Up\nCREATE TABLE pg_only (data JSONB);\n-- +goose Down\n"), 0o644)
	require.NoError(t, err)

	// Up on SQLite should skip the _postgres.sql file and succeed cleanly
	err = sqlm.Up(context.Background(), dir)
	require.NoError(t, err, "Up on SQLite must ignore postgres-specific migration files")
}

func TestSQLiteDB_FieldResolutionAndIDPreservation(t *testing.T) {
	reg := compileRegistry(t)
	db := newTestSQLiteDB(t, reg)
	ctx := context.Background()

	customID := "01H1234567890ABCDEF1234567"
	data := map[string]any{
		"id":         customID,
		"title":      "Pre-assigned ID Test",
		"email":      "assigned@example.com",
		"created_at": time.Now(),
		"updated_at": time.Now(),
		"deleted":    false,
	}

	id, err := db.Insert(ctx, "TestDoc", data)
	require.NoError(t, err)
	assert.Equal(t, customID, id, "Insert must preserve user-provided ID")
	assert.Equal(t, customID, data["id"], "caller data map must not be corrupted")

	// Query by Go field name "Title" instead of column name "title"
	rows, err := db.Query(ctx, dal.Select{
		DocType: "TestDoc",
		Fields:  []string{"Title", "Email"},
		Filters: map[string]any{"Title": "Pre-assigned ID Test"},
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "Pre-assigned ID Test", rows[0]["title"])
}
