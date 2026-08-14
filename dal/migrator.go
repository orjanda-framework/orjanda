package dal

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	atlas "ariga.io/atlas/sql/schema"
	"github.com/pressly/goose/v3"

	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/schema"
)

// migrator implements dal.Migrator backed by Atlas schema representation
// and Goose execution engine. See TAD §14 and PRD §13.4.
type migrator struct {
	dialect Dialect
	dbFn    func() *sql.DB
}

// NewMigrator creates a Migrator for the given Dialect and *sql.DB.
// Introspects live schema directly from the database connection.
func NewMigrator(d Dialect, db *sql.DB) Migrator {
	return &migrator{dialect: d, dbFn: func() *sql.DB { return db }}
}

// TableInspector allows passing a custom schema inspector.
type TableInspector interface {
	ExistingTables() (map[string]bool, error)
	// ExistingColumns returns the live column names for a table mapped to the
	// type each column reports (dialect-reported, unnormalized).
	ExistingColumns(tableName string) (map[string]string, error)
}

type migratorWithInspector struct {
	migrator
	inspector TableInspector
}

// NewMigratorWithInspector creates a Migrator with a custom TableInspector.
func NewMigratorWithInspector(d Dialect, db *sql.DB, inspector TableInspector) Migrator {
	return &migratorWithInspector{
		migrator:  migrator{dialect: d, dbFn: func() *sql.DB { return db }},
		inspector: inspector,
	}
}

// Diff compares the compiled Registry against the live database using Atlas schema models.
// Introspects tables, including child tables. See TAD §14.1 step 1.
func (m *migrator) Diff(ctx context.Context, reg schema.Registry) (*schema.SchemaDiff, error) {
	db := m.dbFn()
	var inspector TableInspector
	if db != nil {
		inspector = &dbInspector{db: db, dialect: m.dialect}
	} else if i, ok := interface{}(m).(interface{ getInspector() TableInspector }); ok {
		inspector = i.getInspector()
	} else {
		return nil, orjerrors.Internal("no database connection or inspector available for Diff", nil)
	}

	return m.diffWithInspector(ctx, reg, inspector)
}

func (m *migratorWithInspector) getInspector() TableInspector {
	return m.inspector
}

func (m *migratorWithInspector) Diff(ctx context.Context, reg schema.Registry) (*schema.SchemaDiff, error) {
	return m.diffWithInspector(ctx, reg, m.inspector)
}

func (m *migrator) diffWithInspector(ctx context.Context, reg schema.Registry, inspector TableInspector) (*schema.SchemaDiff, error) {
	docs := reg.List()
	existingTables, err := inspector.ExistingTables()
	if err != nil {
		return nil, orjerrors.Internal("failed to inspect existing tables", err)
	}

	// 1. Convert Registry into Atlas Target Schema representation
	targetAtlasSchema := &atlas.Schema{Name: "public"}
	docTableMap := make(map[string]*schema.CompiledDoc)

	for _, doc := range docs {
		t := docToAtlasTable(doc)
		targetAtlasSchema.Tables = append(targetAtlasSchema.Tables, t)
		docTableMap[doc.TableName] = doc

		// Include child tables in target schema
		for _, child := range doc.ChildTables {
			childTableName := child.TableName
			if _, exists := docTableMap[childTableName]; !exists {
				childDoc := &schema.CompiledDoc{
					TableName: childTableName,
					Fields:    child.Fields,
				}
				targetAtlasSchema.Tables = append(targetAtlasSchema.Tables, docToAtlasTable(childDoc))
				docTableMap[childTableName] = childDoc
			}
		}
	}

	diff := &schema.SchemaDiff{}
	targetTableSet := make(map[string]bool, len(targetAtlasSchema.Tables))

	// 2. Compare Atlas target schema tables against introspected live database tables
	for _, targetTable := range targetAtlasSchema.Tables {
		tableName := targetTable.Name
		targetTableSet[tableName] = true
		if !existingTables[tableName] {
			// New table — add to CreateTables
			if doc, ok := docTableMap[tableName]; ok {
				diff.CreateTables = append(diff.CreateTables, *doc)
			}
			continue
		}

		// Existing table — inspect columns using Atlas column models
		existingCols, err := inspector.ExistingColumns(tableName)
		if err != nil {
			return nil, orjerrors.Internal(fmt.Sprintf("failed to inspect columns for table %q", tableName), err)
		}

		alteration := schema.TableAlteration{TableName: tableName}
		targetColMap := make(map[string]*atlas.Column)

		for _, col := range targetTable.Columns {
			targetColMap[col.Name] = col
			existingType, exists := existingCols[col.Name]
			if !exists {
				// Find corresponding schema.Field from CompiledDoc
				if doc, ok := docTableMap[tableName]; ok {
					for _, f := range doc.Fields {
						if f.DBColumn == col.Name {
							alteration.AddColumns = append(alteration.AddColumns, f)
							break
						}
					}
				}
				continue
			}

			// Column exists — detect type changes (REVIEW-2026-08-12 finding 9:
			// AlterColumns was never populated, so type changes were silently
			// ignored). Comparison is on canonicalized types so dialect-reported
			// spelling/case differences don't false-positive.
			if doc, ok := docTableMap[tableName]; ok {
				for _, f := range doc.Fields {
					if f.DBColumn == col.Name && f.Type != schema.FieldTypeChildTable {
						if m.dialect.NormalizeColumnType(m.dialect.ColumnType(f)) != m.dialect.NormalizeColumnType(existingType) {
							alteration.AlterColumns = append(alteration.AlterColumns, schema.ColumnAlteration{
								FieldName:  f.Name,
								ColumnName: f.DBColumn,
								OldColumn:  existingType,
								NewColumn:  m.dialect.ColumnType(f),
							})
						}
						break
					}
				}
			}
		}

		// Detect dropped columns
		for existingCol := range existingCols {
			if _, inTarget := targetColMap[existingCol]; !inTarget {
				alteration.DropColumns = append(alteration.DropColumns, existingCol)
			}
		}

		if len(alteration.AddColumns) > 0 || len(alteration.DropColumns) > 0 || len(alteration.AlterColumns) > 0 {
			diff.AlterTables = append(diff.AlterTables, alteration)
		}
	}

	// 3. Detect orphaned tables: tables present in the live database that the
	// Registry no longer produces. Only tables that look Orjanda-owned (both
	// the "id" and "deleted" BaseDocument columns) are flagged — system tables
	// (audit_entries) and unrelated user tables are left alone. The check is
	// deliberately conservative: child tables (no "deleted" column) are not
	// flagged, and any false positive still requires --allow-destructive plus
	// human review before goose applies it (TAD §14.1 step 2).
	for existingTable := range existingTables {
		if targetTableSet[existingTable] {
			continue
		}
		cols, err := inspector.ExistingColumns(existingTable)
		if err != nil {
			return nil, orjerrors.Internal(fmt.Sprintf("failed to inspect orphaned table %q", existingTable), err)
		}
		if _, hasID := cols["id"]; !hasID {
			continue
		}
		if _, hasDeleted := cols["deleted"]; !hasDeleted {
			continue
		}
		diff.DropTables = append(diff.DropTables, existingTable)
	}
	sort.Strings(diff.DropTables)

	return diff, nil
}

// Write renders SchemaDiff into a versioned Goose SQL migration file.
// Enforces --allow-destructive gate per TAD §14.1 step 2.
func (m *migrator) Write(diff *schema.SchemaDiff, dir string, allowDestructive bool) (string, error) {
	if diff == nil {
		return "", orjerrors.Validation("diff must not be nil", nil)
	}

	// Gate destructive changes (TAD §14.1 step 2): dropped columns and dropped
	// tables are computed but excluded from the written file unless
	// --allow-destructive is set.
	var destructiveStatements []string
	for _, alter := range diff.AlterTables {
		for _, col := range alter.DropColumns {
			destructiveStatements = append(destructiveStatements,
				fmt.Sprintf("ALTER TABLE %q DROP COLUMN %q;", alter.TableName, col))
		}
	}
	for _, t := range diff.DropTables {
		destructiveStatements = append(destructiveStatements, fmt.Sprintf("DROP TABLE %q;", t))
	}

	if len(destructiveStatements) > 0 && !allowDestructive {
		return "", orjerrors.Validation(
			"migration contains destructive changes (dropped columns/tables). Re-run with --allow-destructive to include them. Skipped statements:\n"+
				strings.Join(destructiveStatements, "\n"),
			nil,
		)
	}

	var upStmts []string

	for _, doc := range diff.CreateTables {
		upStmts = append(upStmts, m.dialect.CreateTable(doc)+";")
		for _, child := range doc.ChildTables {
			childDoc := schema.CompiledDoc{
				TableName: child.TableName,
				Fields:    child.Fields,
			}
			upStmts = append(upStmts, m.dialect.CreateTable(childDoc)+";")
		}
	}

	for _, alter := range diff.AlterTables {
		for _, stmt := range m.dialect.AlterTable(alter) {
			if !allowDestructive && strings.Contains(strings.ToUpper(stmt), "DROP COLUMN") {
				continue
			}
			upStmts = append(upStmts, stmt+";")
		}
	}

	// Drops run last, after creates/alters, and only with --allow-destructive.
	if allowDestructive {
		for _, t := range diff.DropTables {
			upStmts = append(upStmts, fmt.Sprintf("DROP TABLE %q;", t))
		}
	}

	if len(upStmts) == 0 {
		return "", nil // Nothing to write
	}

	var sb strings.Builder
	sb.WriteString("-- +goose Up\n")
	sb.WriteString(strings.Join(upStmts, "\n"))
	sb.WriteString("\n\n-- +goose Down\n")
	sb.WriteString("-- down migrations are not generated; author manually if needed\n")

	ts := time.Now().UTC().Format("20060102150405")
	filename := fmt.Sprintf("%s_auto_%s.sql", ts, m.dialect.Name())
	fullPath := filepath.Join(dir, filename)
	// Two diffs within the same second would otherwise overwrite each other
	// (REVIEW-2026-08-12 finding 9). Disambiguate by appending a counter; the
	// goose version prefix (leading digits) is unchanged.
	for n := 2; fileExists(fullPath); n++ {
		filename = fmt.Sprintf("%s_auto_%d_%s.sql", ts, n, m.dialect.Name())
		fullPath = filepath.Join(dir, filename)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", orjerrors.Internal("failed to create migrations directory", err)
	}
	if err := os.WriteFile(fullPath, []byte(sb.String()), 0o644); err != nil {
		return "", orjerrors.Internal("failed to write migration file", err)
	}

	return filename, nil
}

// migrationMu serializes goose's package-global dialect/FS configuration and
// the Up/Status runs that depend on it. goose.SetDialect/SetBaseFS are process
// globals, so concurrent migrators for different dialects would corrupt each
// other's state (REVIEW-2026-08-12 finding 9).
var migrationMu sync.Mutex

// Up applies pending migrations matching the active dialect using Goose.
// Isolates dialect-specific migration files when multiple dialect files exist.
// See TAD §14.1 step 4–5.
func (m *migrator) Up(ctx context.Context, dir string) error {
	migrationMu.Lock()
	defer migrationMu.Unlock()

	db := m.dbFn()
	if db == nil {
		return orjerrors.Internal("no database connection available for migration", nil)
	}

	dialectName := m.dialect.Name()
	gooseDialect := dialectName
	if dialectName == "sqlite" {
		gooseDialect = "sqlite3"
	}

	if err := goose.SetDialect(gooseDialect); err != nil {
		return orjerrors.Internal("failed to set goose dialect", err)
	}

	// Filter migration files to exclude files targeting other dialects
	filteredDirFS := &filteredDialectFS{
		base:          os.DirFS(dir),
		activeDialect: dialectName,
	}

	goose.SetBaseFS(filteredDirFS)
	goose.SetLogger(goose.NopLogger())

	if err := goose.Up(db, "."); err != nil {
		return orjerrors.Internal("goose up failed", err)
	}
	return nil
}

// Status reports migration state using Goose. See TAD §14.
func (m *migrator) Status(ctx context.Context, dir string) ([]MigrationStatus, error) {
	migrationMu.Lock()
	defer migrationMu.Unlock()

	db := m.dbFn()
	if db == nil {
		return nil, orjerrors.Internal("no database connection available for migration status", nil)
	}

	dialectName := m.dialect.Name()
	gooseDialect := dialectName
	if dialectName == "sqlite" {
		gooseDialect = "sqlite3"
	}
	_ = goose.SetDialect(gooseDialect)

	filteredDirFS := &filteredDialectFS{
		base:          os.DirFS(dir),
		activeDialect: dialectName,
	}
	goose.SetBaseFS(filteredDirFS)
	goose.SetLogger(goose.NopLogger())

	migrations, err := goose.CollectMigrations(".", 0, goose.MaxVersion)
	if err != nil {
		return nil, orjerrors.Internal("failed to collect migrations", err)
	}

	current, err := goose.GetDBVersion(db)
	if err != nil {
		return nil, orjerrors.Internal("failed to get db version", err)
	}

	var statuses []MigrationStatus
	for _, mig := range migrations {
		statuses = append(statuses, MigrationStatus{
			Version: mig.Version,
			Name:    mig.Source,
			Applied: mig.Version <= current,
		})
	}
	return statuses, nil
}

// ----------------------------------------------------------------------------
// Helpers & Atlas conversions
// ----------------------------------------------------------------------------

// docToAtlasTable maps an Orjanda CompiledDoc to an Atlas Table schema node.
func docToAtlasTable(doc *schema.CompiledDoc) *atlas.Table {
	t := &atlas.Table{Name: doc.TableName}
	for _, f := range doc.Fields {
		if f.Type == schema.FieldTypeChildTable {
			continue
		}
		col := &atlas.Column{
			Name: f.DBColumn,
			Type: fieldToAtlasType(f),
		}
		col.SetNull(!f.Required || f.Name == "ID")
		t.Columns = append(t.Columns, col)
		if f.Name == "ID" {
			t.PrimaryKey = &atlas.Index{
				Name:   "pk_" + doc.TableName,
				Unique: true,
				Parts:  []*atlas.IndexPart{{C: col}},
			}
		}
	}
	return t
}

func fieldToAtlasType(f schema.Field) *atlas.ColumnType {
	switch f.Type {
	case schema.FieldTypeInt, schema.FieldTypeInt64:
		return &atlas.ColumnType{Type: &atlas.IntegerType{T: "integer"}}
	case schema.FieldTypeBool:
		return &atlas.ColumnType{Type: &atlas.BoolType{T: "boolean"}}
	case schema.FieldTypeFloat64:
		return &atlas.ColumnType{Type: &atlas.FloatType{T: "double"}}
	case schema.FieldTypeCurrency:
		return &atlas.ColumnType{Type: &atlas.DecimalType{T: "numeric"}}
	case schema.FieldTypeDate, schema.FieldTypeDateTime:
		return &atlas.ColumnType{Type: &atlas.TimeType{T: "timestamp"}}
	case schema.FieldTypeJSON:
		return &atlas.ColumnType{Type: &atlas.JSONType{T: "json"}}
	default:
		return &atlas.ColumnType{Type: &atlas.StringType{T: "text"}}
	}
}

// dbInspector inspects database metadata from *sql.DB directly.
type dbInspector struct {
	db      *sql.DB
	dialect Dialect
}

func (i *dbInspector) ExistingTables() (map[string]bool, error) {
	var query string
	if i.dialect.Name() == "sqlite" {
		query = "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'goose_%'"
	} else {
		query = "SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE' AND table_name NOT LIKE 'goose_%'"
	}

	rows, err := i.db.QueryContext(context.Background(), query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	tables := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables[name] = true
	}
	return tables, rows.Err()
}

func (i *dbInspector) ExistingColumns(tableName string) (map[string]string, error) {
	cols := make(map[string]string)
	if i.dialect.Name() == "sqlite" {
		rows, err := i.db.QueryContext(context.Background(), fmt.Sprintf("PRAGMA table_info(%q)", tableName))
		if err != nil {
			return nil, err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var cid int
			var name, typ string
			var notnull int
			var dflt any
			var pk int
			if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
				return nil, err
			}
			cols[name] = typ
		}
		return cols, rows.Err()
	}

	rows, err := i.db.QueryContext(context.Background(), `
		SELECT column_name, data_type FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1`, tableName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name, typ string
		if err := rows.Scan(&name, &typ); err != nil {
			return nil, err
		}
		cols[name] = typ
	}
	return cols, rows.Err()
}

// fileExists reports whether path exists in the filesystem.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// filteredDialectFS wraps an fs.FS to expose only migration files matching the active dialect.
type filteredDialectFS struct {
	base          fs.FS
	activeDialect string
}

func (f *filteredDialectFS) Open(name string) (fs.File, error) {
	return f.base.Open(name)
}

func (f *filteredDialectFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(f.base, name)
	if err != nil {
		return nil, err
	}

	var filtered []fs.DirEntry
	otherDialect := "postgres"
	if f.activeDialect == "postgres" {
		otherDialect = "sqlite"
	}

	for _, entry := range entries {
		if entry.IsDir() {
			filtered = append(filtered, entry)
			continue
		}
		fileName := entry.Name()
		// Exclude files explicitly targeting another dialect
		if strings.HasSuffix(fileName, "_"+otherDialect+".sql") {
			continue
		}
		filtered = append(filtered, entry)
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Name() < filtered[j].Name()
	})

	return filtered, nil
}
