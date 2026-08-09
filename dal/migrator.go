package dal

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pressly/goose/v3"

	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/schema"
)

// migrator implements dal.Migrator. See TAD §14 and PRD §13.4.
type migrator struct {
	// dialect is the active Dialect used to generate DDL.
	dialect Dialect
	// dbFn provides a *sql.DB for Goose operations. Called lazily.
	dbFn func() *sql.DB
}

// NewMigrator creates a Migrator for the given Dialect and underlying *sql.DB.
// The db parameter is used only by Up/Status (Goose); Diff/Write do not
// require a live connection.
func NewMigrator(d Dialect, db *sql.DB) Migrator {
	return &migrator{dialect: d, dbFn: func() *sql.DB { return db }}
}

// -- TableInspector is a narrow interface the Migrator uses to inspect the
// live DB schema. Implemented by both sqlite.DB and postgres.DB.
type TableInspector interface {
	ExistingTables() (map[string]bool, error)
	ExistingColumns(tableName string) (map[string]bool, error)
}

// migrator2 extends migrator to carry an inspector.
type migrator2 struct {
	migrator
	inspector TableInspector
}

// NewMigratorWithInspector creates a Migrator that can Diff against a live DB.
func NewMigratorWithInspector(d Dialect, db *sql.DB, inspector TableInspector) Migrator {
	return &migrator2{
		migrator:  migrator{dialect: d, dbFn: func() *sql.DB { return db }},
		inspector: inspector,
	}
}

// Diff computes the delta between the compiled Registry and the live database.
// See TAD §14.1 step 1.
func (m *migrator2) Diff(ctx context.Context, reg schema.Registry) (*schema.SchemaDiff, error) {
	docs := reg.List()
	existingTables, err := m.inspector.ExistingTables()
	if err != nil {
		return nil, orjerrors.Internal("failed to inspect existing tables", err)
	}

	diff := &schema.SchemaDiff{}

	for _, doc := range docs {
		if !existingTables[doc.TableName] {
			// Whole table is new.
			diff.CreateTables = append(diff.CreateTables, *doc)
			continue
		}
		// Table exists — check for column additions/drops.
		existingCols, err := m.inspector.ExistingColumns(doc.TableName)
		if err != nil {
			return nil, orjerrors.Internal(fmt.Sprintf("failed to inspect columns for %q", doc.TableName), err)
		}

		alteration := schema.TableAlteration{TableName: doc.TableName}
		compiledColSet := make(map[string]bool)
		for _, f := range doc.Fields {
			if f.Type == schema.FieldTypeChildTable {
				continue
			}
			compiledColSet[f.DBColumn] = true
			if !existingCols[f.DBColumn] {
				alteration.AddColumns = append(alteration.AddColumns, f)
			}
		}
		// Detect dropped columns.
		for existingCol := range existingCols {
			if !compiledColSet[existingCol] {
				alteration.DropColumns = append(alteration.DropColumns, existingCol)
			}
		}

		if len(alteration.AddColumns) > 0 || len(alteration.DropColumns) > 0 {
			diff.AlterTables = append(diff.AlterTables, alteration)
		}
	}

	return diff, nil
}

// Write renders the SchemaDiff as a Goose-formatted SQL migration file.
// See TAD §14.1 step 2–3.
func (m *migrator) Write(diff *schema.SchemaDiff, dir string, allowDestructive bool) (string, error) {
	if diff == nil {
		return "", orjerrors.Validation("diff must not be nil", nil)
	}

	// Check for destructive changes.
	var destructiveStatements []string
	for _, alter := range diff.AlterTables {
		if len(alter.DropColumns) > 0 {
			for _, col := range alter.DropColumns {
				destructiveStatements = append(destructiveStatements,
					fmt.Sprintf("-- DROP COLUMN %q from table %q", col, alter.TableName))
			}
		}
	}
	if len(destructiveStatements) > 0 && !allowDestructive {
		return "", orjerrors.Validation(
			"migration contains destructive changes (dropped columns). Re-run with --allow-destructive to include them. Skipped statements:\n"+
				strings.Join(destructiveStatements, "\n"),
			nil,
		)
	}

	// Generate the SQL content.
	var upStmts []string

	for _, doc := range diff.CreateTables {
		upStmts = append(upStmts, m.dialect.CreateTable(doc)+";")
		// Child tables
		for _, child := range doc.ChildTables {
			childDoc := schema.CompiledDoc{
				TableName: child.DocType + "s",
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

	if len(upStmts) == 0 {
		return "", nil // Nothing to write
	}

	// Compose the Goose file content (TAD §14.1 step 3).
	var sb strings.Builder
	sb.WriteString("-- +goose Up\n")
	sb.WriteString(strings.Join(upStmts, "\n"))
	sb.WriteString("\n\n-- +goose Down\n")
	sb.WriteString("-- down migrations are not generated; author manually if needed\n")

	// File naming: {timestamp}_{dialect}.sql
	ts := time.Now().UTC().Format("20060102150405")
	filename := fmt.Sprintf("%s_auto_%s.sql", ts, m.dialect.Name())
	fullPath := filepath.Join(dir, filename)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", orjerrors.Internal("failed to create migrations directory", err)
	}
	if err := os.WriteFile(fullPath, []byte(sb.String()), 0644); err != nil {
		return "", orjerrors.Internal("failed to write migration file", err)
	}

	return filename, nil
}

// Up applies all pending Goose migration files. See TAD §14.1 step 4.
func (m *migrator) Up(ctx context.Context, dir string) error {
	db := m.dbFn()
	if db == nil {
		return orjerrors.Internal("no database connection available for migration", nil)
	}
	goose.SetBaseFS(nil)
	goose.SetLogger(goose.NopLogger())

	dialectName := m.dialect.Name()
	if dialectName == "sqlite" {
		goose.SetDialect("sqlite3") //nolint:errcheck
	} else {
		goose.SetDialect(dialectName) //nolint:errcheck
	}

	if err := goose.Up(db, dir); err != nil {
		return orjerrors.Internal("goose up failed", err)
	}
	return nil
}

// Status reports the applied/pending state of migration files. See TAD §14.
func (m *migrator) Status(ctx context.Context, dir string) ([]MigrationStatus, error) {
	db := m.dbFn()
	if db == nil {
		return nil, orjerrors.Internal("no database connection available for migration status", nil)
	}

	dialectName := m.dialect.Name()
	if dialectName == "sqlite" {
		goose.SetDialect("sqlite3") //nolint:errcheck
	} else {
		goose.SetDialect(dialectName) //nolint:errcheck
	}

	migrations, err := goose.CollectMigrations(dir, 0, goose.MaxVersion)
	if err != nil {
		return nil, orjerrors.Internal("failed to collect migrations", err)
	}

	var statuses []MigrationStatus
	for _, mig := range migrations {
		current, err := goose.GetDBVersion(db)
		if err != nil {
			return nil, orjerrors.Internal("failed to get db version", err)
		}
		applied := mig.Version <= current
		statuses = append(statuses, MigrationStatus{
			Version: mig.Version,
			Name:    mig.Source,
			Applied: applied,
		})
	}
	return statuses, nil
}

// Diff on the non-inspector migrator returns an error (no live DB access).
func (m *migrator) Diff(ctx context.Context, reg schema.Registry) (*schema.SchemaDiff, error) {
	return nil, orjerrors.Internal("Diff requires a TableInspector; use NewMigratorWithInspector", nil)
}
