// Package dal implements the Data Access Layer: Database/Tx/Dialect interfaces,
// query builder, transaction management, and Migrator (TAD §2.3, §14, PRD §13).
package dal

import (
	"context"

	"github.com/orjanda-framework/orjanda/schema"
)

// Select describes a read query against a single DocType. Used by Database.Query
// and rendered to SQL by the active Dialect. See TAD §2.3.
type Select struct {
	// DocType is the canonical Document name (e.g. "Employee").
	DocType string
	// TableName is the SQL table to query (resolved from CompiledDoc.TableName).
	TableName string
	// Fields lists the column names to return; nil/empty returns all.
	Fields []string
	// Filters is a map of column_name → value for simple equality predicates.
	// Advanced filters (range, IN, etc.) are post-MVP per TAD §2.3.
	Filters map[string]any
	// IDs restricts the query to an explicit id set rendered as "id IN (...)";
	// the DAL populates it when translating the "q" full-text filter (PRD §688).
	IDs []string
	// OrderBy is the column name for ORDER BY (empty = no ordering).
	OrderBy string
	// Limit is the maximum rows to return (0 = no limit).
	Limit int
	// Offset is the number of rows to skip (for pagination).
	Offset int
	// IncludeDeleted, when true, includes soft-deleted rows (Deleted=true).
	// By default the DAL always appends "deleted = false" for Document tables.
	IncludeDeleted bool
}

// Database is the top-level data access interface that the Document Engine and
// other subsystems call. It abstracts driver and dialect differences and
// standardises query generation. See TAD §2.3.
type Database interface {
	// Query executes a Select and returns rows as maps of column→value.
	Query(ctx context.Context, q Select) ([]map[string]any, error)
	// Insert writes a new record and returns its generated ID.
	Insert(ctx context.Context, docType string, data map[string]any) (string, error)
	// Update mutates an existing record identified by id.
	Update(ctx context.Context, docType string, id string, data map[string]any) error
	// Delete removes the record identified by id (hard delete at DAL level;
	// soft-delete is the Document Engine's responsibility via Update).
	Delete(ctx context.Context, docType string, id string) error
	// Transaction runs fn inside a database transaction. The Tx passed to fn
	// implements Database so all the same operations are available inside it.
	// fn returning a non-nil error causes automatic rollback.
	Transaction(ctx context.Context, fn func(Tx) error) error
	// Close releases the underlying connection pool.
	Close() error
}

// Tx is a transactional Database handle. It embeds Database so the same
// CRUD operations are available inside a transaction. See TAD §2.3.
type Tx interface {
	Database
	Commit() error
	Rollback() error
}

// Dialect generates dialect-specific SQL for schema management and DML
// operations. Implementations live under dal/dialect/{postgres,sqlite}.
// See TAD §2.3.
type Dialect interface {
	// CreateTable returns the full CREATE TABLE DDL for a compiled Document.
	CreateTable(doc schema.CompiledDoc) string
	// AlterTable returns ALTER TABLE statements for a set of column changes.
	AlterTable(diff schema.TableAlteration) []string
	// SelectSQL renders a Select query to (sql, args).
	SelectSQL(q Select) (string, []any)
	// InsertSQL renders an INSERT statement to (sql, args) — excludes the ID
	// field; the caller supplies it as part of the data map.
	InsertSQL(tableName string, fields map[string]any) (string, []any)
	// UpdateSQL renders an UPDATE statement to (sql, args).
	UpdateSQL(tableName string, id string, fields map[string]any) (string, []any)
	// DeleteSQL renders a DELETE statement to (sql, args).
	DeleteSQL(tableName string, id string) (string, []any)
	// FullTextSearch renders a full-text search query to (sql, args).
	// Returns matching row IDs. See TAD §9.1 (search.Backend default).
	FullTextSearch(tableName string, query string, fields []string) (string, []any)
	// Placeholder returns the positional parameter placeholder for argument n
	// (1-indexed). PostgreSQL: "$1"; SQLite: "?".
	Placeholder(n int) string
	// DriverName returns the driver identifier used with database/sql.
	DriverName() string
	// Name returns a human-readable dialect name ("postgres" or "sqlite").
	Name() string
}

// MigrationStatus represents the current state of a single migration file.
type MigrationStatus struct {
	// Version is the Goose version timestamp prefix.
	Version int64
	// Name is the filename stem.
	Name string
	// Applied is true if the migration has been applied to the database.
	Applied bool
}

// Migrator compares the Registry against the live database schema and generates
// Goose migration files. See TAD §14 and PRD §13.4.
type Migrator interface {
	// Diff introspects the live database and computes the delta against the
	// compiled Registry. Returns a SchemaDiff describing what would change.
	Diff(ctx context.Context, reg schema.Registry) (*schema.SchemaDiff, error)
	// Write persists diff as a versioned Goose SQL file under dir.
	// Returns the filename written. If diff contains destructive changes and
	// allowDestructive is false, Write returns an error with a list of the
	// skipped statements. See TAD §14.1 step 2.
	Write(diff *schema.SchemaDiff, dir string, allowDestructive bool) (filename string, err error)
	// Up applies all pending Goose migration files in dir.
	Up(ctx context.Context, dir string) error
	// Status reports the applied/pending state of each migration file in dir.
	Status(ctx context.Context, dir string) ([]MigrationStatus, error)
}
