package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	// Register the modernc SQLite driver.
	_ "modernc.org/sqlite"

	"github.com/oklog/ulid/v2"
	"github.com/orjanda-framework/orjanda/dal"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/schema"
)

// DB wraps *sql.DB and implements dal.Database for SQLite.
type DB struct {
	db           *sql.DB
	dialect      *Dialect
	tableNames   map[string]string
	compiledDocs map[string]*schema.CompiledDoc
}

// Open opens a SQLite database at the given DSN (file path or ":memory:").
func Open(dsn string) (*DB, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, orjerrors.Internal("failed to open sqlite database", err)
	}
	// Enforce WAL mode for concurrent read/write safety.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, orjerrors.Internal("failed to enable WAL mode", err)
	}
	// Enable foreign-key enforcement.
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, orjerrors.Internal("failed to enable foreign keys", err)
	}
	return &DB{
		db:           db,
		dialect:      New(),
		tableNames:   make(map[string]string),
		compiledDocs: make(map[string]*schema.CompiledDoc),
	}, nil
}

// RegisterDoc registers a docType→tableName mapping.
func (d *DB) RegisterDoc(docType, tableName string) {
	d.tableNames[docType] = tableName
}

// RegisterDocs registers mappings from a slice of CompiledDocs.
func (d *DB) RegisterDocs(docs []*schema.CompiledDoc) {
	for _, doc := range docs {
		d.tableNames[doc.Name] = doc.TableName
		d.compiledDocs[doc.Name] = doc
	}
}

// Dialect returns the underlying dal.Dialect implementation.
func (d *DB) Dialect() dal.Dialect { return d.dialect }

// Close closes the underlying connection pool.
func (d *DB) Close() error { return d.db.Close() }

// Query executes a Select and returns rows as maps.
func (d *DB) Query(ctx context.Context, q dal.Select) ([]map[string]any, error) {
	if doc, ok := d.compiledDocs[q.DocType]; ok {
		q = resolveSelect(doc, q)
	} else if q.TableName == "" {
		tn, ok := d.tableNames[q.DocType]
		if !ok {
			return nil, orjerrors.NotFound(fmt.Sprintf("no table mapping for docType %q", q.DocType))
		}
		q.TableName = tn
	}
	sqlStr, args := d.dialect.SelectSQL(q)
	for i, a := range args {
		args[i] = formatValue(a)
	}
	rows, err := d.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, orjerrors.Internal("query failed", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

// Insert writes a new record and returns the generated or provided ULID.
func (d *DB) Insert(ctx context.Context, docType string, data map[string]any) (string, error) {
	tn, ok := d.tableNames[docType]
	if !ok {
		return "", orjerrors.NotFound(fmt.Sprintf("no table mapping for docType %q", docType))
	}
	id, ok := data["id"].(string)
	if !ok || id == "" {
		id = ulid.Make().String()
	}

	dataCopy := make(map[string]any, len(data)+1)
	for k, v := range data {
		dataCopy[k] = v
	}
	dataCopy["id"] = id

	converted := make(map[string]any, len(dataCopy))
	for k, v := range dataCopy {
		converted[k] = formatValue(v)
	}

	sqlStr, args := d.dialect.InsertSQL(tn, converted)
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
	converted := make(map[string]any, len(data))
	for k, v := range data {
		converted[k] = formatValue(v)
	}
	sqlStr, args := d.dialect.UpdateSQL(tn, id, converted)
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
	t := &txDB{
		tx:           tx,
		dialect:      d.dialect,
		tableNames:   d.tableNames,
		compiledDocs: d.compiledDocs,
	}
	if err := fn(t); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return orjerrors.Internal("failed to commit transaction", err)
	}
	return nil
}

// ----------------------------------------------------------------------------
// txDB — the Tx wrapper
// ----------------------------------------------------------------------------

type txDB struct {
	tx           *sql.Tx
	dialect      *Dialect
	tableNames   map[string]string
	compiledDocs map[string]*schema.CompiledDoc
}

func (t *txDB) Dialect() dal.Dialect { return t.dialect }

func (t *txDB) Close() error { return nil }

func (t *txDB) Query(ctx context.Context, q dal.Select) ([]map[string]any, error) {
	if doc, ok := t.compiledDocs[q.DocType]; ok {
		q = resolveSelect(doc, q)
	} else if q.TableName == "" {
		tn, ok := t.tableNames[q.DocType]
		if !ok {
			return nil, orjerrors.NotFound(fmt.Sprintf("no table mapping for docType %q", q.DocType))
		}
		q.TableName = tn
	}
	sqlStr, args := t.dialect.SelectSQL(q)
	for i, a := range args {
		args[i] = formatValue(a)
	}
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
	id, ok := data["id"].(string)
	if !ok || id == "" {
		id = ulid.Make().String()
	}

	dataCopy := make(map[string]any, len(data)+1)
	for k, v := range data {
		dataCopy[k] = v
	}
	dataCopy["id"] = id

	converted := make(map[string]any, len(dataCopy))
	for k, v := range dataCopy {
		converted[k] = formatValue(v)
	}
	sqlStr, args := t.dialect.InsertSQL(tn, converted)
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
	converted := make(map[string]any, len(data))
	for k, v := range data {
		converted[k] = formatValue(v)
	}
	sqlStr, args := t.dialect.UpdateSQL(tn, id, converted)
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
	return fn(t)
}

func (t *txDB) Commit() error {
	return orjerrors.Internal("Commit() called on inner txDB — use the outer Transaction fn return", nil)
}
func (t *txDB) Rollback() error {
	return t.tx.Rollback()
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func resolveSelect(doc *schema.CompiledDoc, q dal.Select) dal.Select {
	res := q
	if res.TableName == "" {
		res.TableName = doc.TableName
	}
	if len(q.Fields) > 0 {
		resFields := make([]string, 0, len(q.Fields))
		for _, f := range q.Fields {
			resFields = append(resFields, resolveField(doc, f))
		}
		res.Fields = resFields
	}
	if len(q.Filters) > 0 {
		resFilters := make(map[string]any, len(q.Filters))
		for k, v := range q.Filters {
			resFilters[resolveField(doc, k)] = v
		}
		res.Filters = resFilters
	}
	if q.OrderBy != "" {
		parts := strings.Split(q.OrderBy, " ")
		colName := parts[0]
		dir := ""
		if len(parts) > 1 {
			dir = " " + parts[1]
		}
		res.OrderBy = resolveField(doc, colName) + dir
	}
	return res
}

func resolveField(doc *schema.CompiledDoc, name string) string {
	for _, f := range doc.Fields {
		if f.Name == name || f.DBColumn == name {
			return f.DBColumn
		}
	}
	return name
}

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

// CreateTables creates all tables for the given compiled docs (idempotent).
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

// ExistingTables returns the set of table names currently in the database.
func (d *DB) ExistingTables() (map[string]bool, error) {
	rows, err := d.db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'goose_%'")
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
	rows, err := d.db.Query(fmt.Sprintf("PRAGMA table_info(%q)", tableName))
	if err != nil {
		return nil, orjerrors.Internal("PRAGMA table_info failed", err)
	}
	defer rows.Close()
	cols := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, orjerrors.Internal("scan column info", err)
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

// Underlying returns the raw *sql.DB.
func (d *DB) Underlying() *sql.DB { return d.db }

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
