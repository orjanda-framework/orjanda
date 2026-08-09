package postgres

import (
	"context"
	"database/sql"
	"fmt"

	// Register the pgx stdlib driver.
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/oklog/ulid/v2"
	"github.com/orjanda-framework/orjanda/dal"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/schema"
)

// DB wraps *sql.DB and implements dal.Database for PostgreSQL.
type DB struct {
	db         *sql.DB
	dialect    *Dialect
	tableNames map[string]string
}

// Open opens a PostgreSQL database at the given DSN (postgres:// URL).
func Open(dsn string) (*DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, orjerrors.Internal("failed to open postgres database", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, orjerrors.Internal("failed to ping postgres database", err)
	}
	return &DB{db: db, dialect: New(), tableNames: make(map[string]string)}, nil
}

// RegisterDoc registers a docType→tableName mapping.
func (d *DB) RegisterDoc(docType, tableName string) {
	d.tableNames[docType] = tableName
}

// RegisterDocs registers mappings from a slice of CompiledDocs.
func (d *DB) RegisterDocs(docs []*schema.CompiledDoc) {
	for _, doc := range docs {
		d.tableNames[doc.Name] = doc.TableName
	}
}

// Dialect returns the underlying dal.Dialect.
func (d *DB) Dialect() dal.Dialect { return d.dialect }

// Close releases the connection pool.
func (d *DB) Close() error { return d.db.Close() }

// Query executes a Select and returns rows as maps.
func (d *DB) Query(ctx context.Context, q dal.Select) ([]map[string]any, error) {
	if q.TableName == "" {
		tn, ok := d.tableNames[q.DocType]
		if !ok {
			return nil, orjerrors.NotFound(fmt.Sprintf("no table mapping for docType %q", q.DocType))
		}
		q.TableName = tn
	}
	sqlStr, args := d.dialect.SelectSQL(q)
	rows, err := d.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, orjerrors.Internal("query failed", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

// Insert writes a new record and returns the generated ULID.
func (d *DB) Insert(ctx context.Context, docType string, data map[string]any) (string, error) {
	tn, ok := d.tableNames[docType]
	if !ok {
		return "", orjerrors.NotFound(fmt.Sprintf("no table mapping for docType %q", docType))
	}
	id := ulid.Make().String()
	data["id"] = id
	sqlStr, args := d.dialect.InsertSQL(tn, data)
	if _, err := d.db.ExecContext(ctx, sqlStr, args...); err != nil {
		return "", orjerrors.Internal("insert failed", err)
	}
	return id, nil
}

// Update mutates an existing record.
func (d *DB) Update(ctx context.Context, docType string, id string, data map[string]any) error {
	tn, ok := d.tableNames[docType]
	if !ok {
		return orjerrors.NotFound(fmt.Sprintf("no table mapping for docType %q", docType))
	}
	sqlStr, args := d.dialect.UpdateSQL(tn, id, data)
	res, err := d.db.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		return orjerrors.Internal("update failed", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return orjerrors.NotFound(fmt.Sprintf("record %q not found in %q", id, docType))
	}
	return nil
}

// Delete hard-deletes a record.
func (d *DB) Delete(ctx context.Context, docType string, id string) error {
	tn, ok := d.tableNames[docType]
	if !ok {
		return orjerrors.NotFound(fmt.Sprintf("no table mapping for docType %q", docType))
	}
	sqlStr, args := d.dialect.DeleteSQL(tn, id)
	res, err := d.db.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		return orjerrors.Internal("delete failed", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return orjerrors.NotFound(fmt.Sprintf("record %q not found in %q", id, docType))
	}
	return nil
}

// Transaction runs fn inside a database transaction.
func (d *DB) Transaction(ctx context.Context, fn func(dal.Tx) error) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return orjerrors.Internal("failed to begin transaction", err)
	}
	t := &txDB{tx: tx, dialect: d.dialect, tableNames: d.tableNames}
	if err := fn(t); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return orjerrors.Internal("failed to commit transaction", err)
	}
	return nil
}

// CreateTables executes CREATE TABLE IF NOT EXISTS for all docs (idempotent).
func (d *DB) CreateTables(docs []*schema.CompiledDoc) error {
	for _, doc := range docs {
		ddl := d.dialect.CreateTable(*doc)
		if _, err := d.db.Exec(ddl); err != nil {
			return orjerrors.Internal(fmt.Sprintf("create table %q failed", doc.TableName), err)
		}
		for _, child := range doc.ChildTables {
			childDoc := schema.CompiledDoc{
				TableName: child.DocType + "s",
				Fields:    child.Fields,
			}
			childDDL := d.dialect.CreateTable(childDoc)
			if _, err := d.db.Exec(childDDL); err != nil {
				return orjerrors.Internal(fmt.Sprintf("create child table %q failed", childDoc.TableName), err)
			}
		}
	}
	return nil
}

// ExistingTables returns the set of table names in the current schema.
func (d *DB) ExistingTables() (map[string]bool, error) {
	rows, err := d.db.Query(`
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE'`)
	if err != nil {
		return nil, orjerrors.Internal("failed to list tables", err)
	}
	defer rows.Close()
	tables := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, orjerrors.Internal("scan table name", err)
		}
		tables[name] = true
	}
	return tables, rows.Err()
}

// ExistingColumns returns the set of column names for a given table.
func (d *DB) ExistingColumns(tableName string) (map[string]bool, error) {
	rows, err := d.db.Query(`
		SELECT column_name FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1`, tableName)
	if err != nil {
		return nil, orjerrors.Internal("failed to list columns", err)
	}
	defer rows.Close()
	cols := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, orjerrors.Internal("scan column name", err)
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

// Underlying returns the raw *sql.DB (used by Goose).
func (d *DB) Underlying() *sql.DB { return d.db }

// ----------------------------------------------------------------------------
// txDB — Tx wrapper for PostgreSQL
// ----------------------------------------------------------------------------

type txDB struct {
	tx         *sql.Tx
	dialect    *Dialect
	tableNames map[string]string
}

func (t *txDB) Close() error { return nil }

func (t *txDB) Query(ctx context.Context, q dal.Select) ([]map[string]any, error) {
	if q.TableName == "" {
		tn, ok := t.tableNames[q.DocType]
		if !ok {
			return nil, orjerrors.NotFound(fmt.Sprintf("no table mapping for docType %q", q.DocType))
		}
		q.TableName = tn
	}
	sqlStr, args := t.dialect.SelectSQL(q)
	rows, err := t.tx.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, orjerrors.Internal("query failed", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

func (t *txDB) Insert(ctx context.Context, docType string, data map[string]any) (string, error) {
	tn, ok := t.tableNames[docType]
	if !ok {
		return "", orjerrors.NotFound(fmt.Sprintf("no table mapping for docType %q", docType))
	}
	id := ulid.Make().String()
	data["id"] = id
	sqlStr, args := t.dialect.InsertSQL(tn, data)
	if _, err := t.tx.ExecContext(ctx, sqlStr, args...); err != nil {
		return "", orjerrors.Internal("insert failed", err)
	}
	return id, nil
}

func (t *txDB) Update(ctx context.Context, docType string, id string, data map[string]any) error {
	tn, ok := t.tableNames[docType]
	if !ok {
		return orjerrors.NotFound(fmt.Sprintf("no table mapping for docType %q", docType))
	}
	sqlStr, args := t.dialect.UpdateSQL(tn, id, data)
	res, err := t.tx.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		return orjerrors.Internal("update failed", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return orjerrors.NotFound(fmt.Sprintf("record %q not found", id))
	}
	return nil
}

func (t *txDB) Delete(ctx context.Context, docType string, id string) error {
	tn, ok := t.tableNames[docType]
	if !ok {
		return orjerrors.NotFound(fmt.Sprintf("no table mapping for docType %q", docType))
	}
	sqlStr, args := t.dialect.DeleteSQL(tn, id)
	res, err := t.tx.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		return orjerrors.Internal("delete failed", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return orjerrors.NotFound(fmt.Sprintf("record %q not found", id))
	}
	return nil
}

func (t *txDB) Transaction(ctx context.Context, fn func(dal.Tx) error) error {
	// Nested: re-use same transaction (savepoints are post-MVP).
	return fn(t)
}

func (t *txDB) Commit() error {
	return orjerrors.Internal("Commit() called on inner txDB — use the outer Transaction fn return", nil)
}
func (t *txDB) Rollback() error { return t.tx.Rollback() }

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func scanRows(rows *sql.Rows) ([]map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, orjerrors.Internal("failed to get columns", err)
	}
	var result []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, orjerrors.Internal("scan failed", err)
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			v := vals[i]
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			row[col] = v
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, orjerrors.Internal("row iteration error", err)
	}
	return result, nil
}
