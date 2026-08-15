package dal_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/orjanda-framework/orjanda/dal"
	"github.com/orjanda-framework/orjanda/dal/dialect/postgres"
	"github.com/orjanda-framework/orjanda/dal/dialect/sqlite"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/schema"
)

// testDocInt mirrors testDoc but declares Title as an int, so a live type
// change (string -> int) can be exercised against the same table name.
type testDocInt struct {
	schema.BaseDocument
	Title int    `oj:"required"`
	Email string `oj:"unique"`
}

func (d *testDocInt) DocMeta() schema.Meta {
	return schema.Meta{Name: "TestDoc", Searchable: true}
}
func (d *testDocInt) Get(field string) any {
	switch field {
	case "Title":
		return d.Title
	case "Email":
		return d.Email
	}
	return d.BaseDocument.Get(field)
}
func (d *testDocInt) Set(field string, value any) orjerrors.Error {
	return d.BaseDocument.Set(field, value)
}

func compileIntRegistry(t *testing.T) schema.Registry {
	t.Helper()
	reg := schema.NewRegistry()
	require.NoError(t, reg.Register("test-app", &testDocInt{}))
	require.NoError(t, reg.Compile())
	return reg
}

func emptyRegistry(t *testing.T) schema.Registry {
	t.Helper()
	reg := schema.NewRegistry()
	require.NoError(t, reg.Compile())
	return reg
}

func assertTableGone(t *testing.T, db *sqlite.DB, table string) {
	t.Helper()
	rows, err := db.Underlying().Query("SELECT name FROM sqlite_master WHERE type='table' AND name = ?", table)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	require.False(t, rows.Next(), "table %q should have been dropped", table)
}

// TestSchemaDiff_ChangeCountIncludesDrops proves the production serve
// fail-fast gate and `migrate diff`'s no-change check count DropTables as a
// pending change (finding 9 — previously only CreateTables+AlterTables were
// counted, so an orphaned table never blocked startup).
func TestSchemaDiff_ChangeCountIncludesDrops(t *testing.T) {
	var diff *schema.SchemaDiff
	assert.Zero(t, diff.ChangeCount(), "nil diff has zero changes")
	assert.Zero(t, (&schema.SchemaDiff{}).ChangeCount())

	diff = &schema.SchemaDiff{
		CreateTables: []schema.CompiledDoc{{TableName: "a"}},
		AlterTables:  []schema.TableAlteration{{TableName: "b"}},
		DropTables:   []string{"c"},
	}
	assert.Equal(t, 3, diff.ChangeCount())
}

// TestMigratorDiff_DetectsOrphanedTable verifies orphaned Orjanda tables are
// reported as DropTables while non-Orjanda tables (audit_entries, an unrelated
// table without the id+deleted BaseDocument signature) are left alone.
// Regression for REVIEW-2026-08-12 finding 9 (no DropTables detection).
func TestMigratorDiff_DetectsOrphanedTable(t *testing.T) {
	reg := compileRegistry(t)
	db, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.CreateTables(reg.List()))

	// Non-Orjanda tables must not be flagged as drops.
	for _, ddl := range []string{
		`CREATE TABLE unrelated_stuff (id TEXT PRIMARY KEY, label TEXT)`,
		`CREATE TABLE audit_entries (id TEXT PRIMARY KEY, action TEXT NOT NULL)`,
	} {
		_, err := db.Underlying().Exec(ddl)
		require.NoError(t, err)
	}

	m := dal.NewMigratorWithInspector(db.Dialect(), db.Underlying(), db)

	// Same registry: no changes at all (guards against type-change false
	// positives from normalization).
	diff, err := m.Diff(context.Background(), reg)
	require.NoError(t, err)
	assert.Empty(t, diff.CreateTables, "no creates expected on a synced DB")
	assert.Empty(t, diff.AlterTables, "no alters expected on a synced DB")
	assert.Empty(t, diff.DropTables, "no drops expected on a synced DB")

	// Registry no longer produces test_docs: it becomes an orphaned Orjanda
	// table and is reported as a drop.
	diff, err = m.Diff(context.Background(), emptyRegistry(t))
	require.NoError(t, err)
	require.Contains(t, diff.DropTables, "test_docs")
	assert.NotContains(t, diff.DropTables, "unrelated_stuff")
	assert.NotContains(t, diff.DropTables, "audit_entries")
}

// TestMigratorDiff_DetectsColumnTypeChange verifies a live column type change
// now populates AlterColumns. Regression for finding 9 (AlterColumns was never
// populated, so type changes were silently ignored).
func TestMigratorDiff_DetectsColumnTypeChange(t *testing.T) {
	regString := compileRegistry(t)
	db, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.CreateTables(regString.List()))

	m := dal.NewMigratorWithInspector(db.Dialect(), db.Underlying(), db)

	diff, err := m.Diff(context.Background(), compileIntRegistry(t))
	require.NoError(t, err)
	require.Len(t, diff.AlterTables, 1, "expected one altered table")
	alter := diff.AlterTables[0]
	assert.Equal(t, "test_docs", alter.TableName)

	found := false
	for _, a := range alter.AlterColumns {
		if a.ColumnName == "title" {
			found = true
			assert.Equal(t, "TEXT", a.OldColumn)
			assert.Equal(t, "INTEGER", a.NewColumn)
		}
	}
	assert.True(t, found, "expected title column type change in AlterColumns")
}

// TestMigratorWrite_DropTable_DestructiveGate verifies dropped tables follow
// the same --allow-destructive gate as dropped columns (TAD §14.1 step 2).
func TestMigratorWrite_DropTable_DestructiveGate(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	dir := t.TempDir()
	m := dal.NewMigratorWithInspector(db.Dialect(), db.Underlying(), db)

	diff := &schema.SchemaDiff{DropTables: []string{"orphaned_emps"}}

	// Without --allow-destructive: errors and names the skipped DROP TABLE.
	_, err = m.Write(diff, dir, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "destructive")
	assert.Contains(t, err.Error(), `DROP TABLE "orphaned_emps"`)

	// With --allow-destructive: writes the file with the DROP TABLE.
	filename, err := m.Write(diff, dir, true)
	require.NoError(t, err)
	require.NotEmpty(t, filename)
	content, err := os.ReadFile(filepath.Join(dir, filename))
	require.NoError(t, err)
	assert.Contains(t, string(content), `DROP TABLE "orphaned_emps";`)
}

// TestMigrateUp_DropTable_LiveDB applies a drop-table migration against a real
// SQLite DB and asserts the table is actually gone — the live-DB destructive
// path the review asked to test.
func TestMigrateUp_DropTable_LiveDB(t *testing.T) {
	reg := compileRegistry(t)
	db, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.CreateTables(reg.List()))

	dir := t.TempDir()
	m := dal.NewMigratorWithInspector(db.Dialect(), db.Underlying(), db)

	diff, err := m.Diff(context.Background(), emptyRegistry(t))
	require.NoError(t, err)
	require.Contains(t, diff.DropTables, "test_docs")

	// Refuse without the flag.
	_, err = m.Write(diff, dir, false)
	require.Error(t, err)

	// Write + apply with the flag.
	filename, err := m.Write(diff, dir, true)
	require.NoError(t, err)
	require.NotEmpty(t, filename)
	require.NoError(t, m.Up(context.Background(), dir))

	assertTableGone(t, db, "test_docs")

	// Idempotent: a second Up applies nothing.
	require.NoError(t, m.Up(context.Background(), dir))
}

// TestMigratorWrite_SameSecondDiffs_DoNotOverwrite verifies two diffs written
// within the same second produce distinct migration files instead of the second
// silently overwriting the first (finding 9).
func TestMigratorWrite_SameSecondDiffs_DoNotOverwrite(t *testing.T) {
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

	f1, err := m.Write(diff, dir, false)
	require.NoError(t, err)
	require.NotEmpty(t, f1)
	f2, err := m.Write(diff, dir, false)
	require.NoError(t, err)
	require.NotEmpty(t, f2)

	assert.NotEqual(t, f1, f2, "two diffs in one second must not collide")
	for _, f := range []string{f1, f2} {
		content, err := os.ReadFile(filepath.Join(dir, f))
		require.NoError(t, err)
		assert.Contains(t, string(content), "-- +goose Up")
	}
}

// Dialect-level rendering: postgres emits ALTER COLUMN ... TYPE; sqlite emits
// an explicit manual-rebuild note (no silent ignore, no broken DDL).

func TestPostgresDialect_AlterTable_TypeChange(t *testing.T) {
	d := postgres.New()
	stmts := d.AlterTable(schema.TableAlteration{
		TableName: "employees",
		AlterColumns: []schema.ColumnAlteration{
			{FieldName: "Salary", ColumnName: "salary", OldColumn: "double precision", NewColumn: "NUMERIC(20,6)"},
		},
	})
	require.Len(t, stmts, 1)
	assert.Contains(t, stmts[0], `ALTER TABLE "employees" ALTER COLUMN "salary" TYPE NUMERIC(20,6)`)
}

func TestSQLiteDialect_AlterTable_TypeChangeIsSurfaced(t *testing.T) {
	d := sqlite.New()
	stmts := d.AlterTable(schema.TableAlteration{
		TableName: "test_docs",
		AlterColumns: []schema.ColumnAlteration{
			{FieldName: "Title", ColumnName: "title", OldColumn: "TEXT", NewColumn: "INTEGER"},
		},
	})
	require.Len(t, stmts, 1)
	assert.True(t, strings.HasPrefix(stmts[0], "-- NOTE (sqlite)"), "type change must be surfaced, got: %s", stmts[0])
	assert.Contains(t, stmts[0], "test_docs")
	assert.Contains(t, stmts[0], "TEXT")
	assert.Contains(t, stmts[0], "INTEGER")
}

func TestDialect_NormalizeColumnType(t *testing.T) {
	cases := []struct {
		dialect func() dal.Dialect
		raw     string
		want    string
	}{
		{func() dal.Dialect { return sqlite.New() }, "TEXT", "TEXT"},
		{func() dal.Dialect { return sqlite.New() }, " text ", "TEXT"},
		{func() dal.Dialect { return sqlite.New() }, "INTEGER", "INTEGER"},
		{func() dal.Dialect { return postgres.New() }, "integer", "INTEGER"},
		{func() dal.Dialect { return postgres.New() }, "bigint", "BIGINT"},
		{func() dal.Dialect { return postgres.New() }, "boolean", "BOOLEAN"},
		{func() dal.Dialect { return postgres.New() }, "double precision", "DOUBLE PRECISION"},
		{func() dal.Dialect { return postgres.New() }, "numeric", "NUMERIC"},
		{func() dal.Dialect { return postgres.New() }, "NUMERIC(20,6)", "NUMERIC"},
		{func() dal.Dialect { return postgres.New() }, "timestamp with time zone", "TIMESTAMPTZ"},
		{func() dal.Dialect { return postgres.New() }, "TIMESTAMPTZ", "TIMESTAMPTZ"},
		{func() dal.Dialect { return postgres.New() }, "character varying", "TEXT"},
		{func() dal.Dialect { return postgres.New() }, "jsonb", "JSONB"},
		{func() dal.Dialect { return postgres.New() }, "date", "DATE"},
		{func() dal.Dialect { return postgres.New() }, "text", "TEXT"},
	}
	for _, tc := range cases {
		if got := tc.dialect().NormalizeColumnType(tc.raw); got != tc.want {
			t.Errorf("NormalizeColumnType(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}
