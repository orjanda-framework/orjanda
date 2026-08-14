package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	// Register the modernc SQLite driver.
	_ "modernc.org/sqlite"

	"github.com/oklog/ulid/v2"
	"github.com/orjanda-framework/orjanda/dal"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/schema"
)

// DB wraps *sql.DB and implements dal.Database for SQLite.
type DB struct {
	db            *sql.DB
	dialect       *Dialect
	tableNames    map[string]string
	compiledDocs  map[string]*schema.CompiledDoc
	childDocTypes map[string]bool
}

// Open opens a SQLite database at the given DSN (file path or ":memory:").
//
// Pragmas are configured per-connection via modernc shorthand DSN keys, not
// ExecContext: an ExecContext PRAGMA only touches the single pooled connection
// that executes it, leaving every later connection (and therefore concurrent
// writers) unconfigured.
//   - _journal_mode=WAL: concurrent read/write safety.
//   - _foreign_keys=1: enable foreign-key enforcement.
//   - _busy_timeout=5000: wait a bounded time for a held write lock instead of
//     failing immediately with SQLITE_BUSY.
//   - _txlock=immediate: every transaction is BEGIN IMMEDIATE, taking the write
//     lock at transaction start. A deferred transaction that reads first and
//     writes later holds a stale WAL snapshot and fails the upgrade with
//     SQLITE_BUSY_SNAPSHOT, which the busy handler does not retry — the exact
//     failure mode of two serve instances racing the first-run bootstrap
//     (REVIEW-2026-08-12 finding 12).
func Open(dsn string) (*DB, error) {
	const pragmas = "_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=1&_txlock=immediate"
	if strings.Contains(dsn, "?") {
		dsn += "&" + pragmas
	} else {
		dsn += "?" + pragmas
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, orjerrors.Internal("failed to open sqlite database", err)
	}
	return &DB{
		db:            db,
		dialect:       New(),
		tableNames:    make(map[string]string),
		compiledDocs:  make(map[string]*schema.CompiledDoc),
		childDocTypes: make(map[string]bool),
	}, nil
}

// RegisterDoc registers a docType→tableName mapping.
func (d *DB) RegisterDoc(docType, tableName string) {
	d.tableNames[docType] = tableName
}

// RegisterDocs registers mappings from a slice of CompiledDocs, including the
// parent→child table mappings so child-table writes (e.g. core.UserRole) resolve.
func (d *DB) RegisterDocs(docs []*schema.CompiledDoc) {
	for _, doc := range docs {
		d.tableNames[doc.Name] = doc.TableName
		d.compiledDocs[doc.Name] = doc
		for _, child := range doc.ChildTables {
			// Child DocType is singular snake (TAD §2.1); TableName is the
			// pluralized snake_case name computed at compile time (TAD §1.4).
			d.tableNames[child.TypeName] = child.TableName
			// Child tables carry no soft-delete base column; their queries must
			// not emit a "deleted" predicate (TAD §2.3 child-table semantics).
			d.childDocTypes[child.TypeName] = true
		}
	}
}

// Dialect returns the underlying dal.Dialect implementation.
func (d *DB) Dialect() dal.Dialect { return d.dialect }

// Close closes the underlying connection pool.
func (d *DB) Close() error { return d.db.Close() }

// Query executes a Select and returns rows as maps.
func (d *DB) Query(ctx context.Context, q dal.Select) ([]map[string]any, error) {
	var doc *schema.CompiledDoc
	if cd, ok := d.compiledDocs[q.DocType]; ok {
		doc = cd
		q = resolveSelect(cd, q)
	} else if q.TableName == "" {
		tn, ok := d.tableNames[q.DocType]
		if !ok {
			return nil, orjerrors.NotFound(fmt.Sprintf("no table mapping for docType %q", q.DocType))
		}
		q.TableName = tn
		if d.childDocTypes[q.DocType] {
			q.IncludeDeleted = true
		}
	}
	searchApplied := false
	if doc != nil {
		var err error
		q, searchApplied, err = resolveSearchFilter(ctx, d.db, d.dialect, doc, q)
		if err != nil {
			return nil, err
		}
	}
	if searchApplied && len(q.IDs) == 0 {
		return []map[string]any{}, nil
	}
	sqlStr, args := d.dialect.SelectSQL(q)
	for i, a := range args {
		args[i] = formatValue(a)
	}
	rows, err := d.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, orjerrors.Internal("query failed", err)
	}
	defer func() { _ = rows.Close() }()
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
func (d *DB) Update(ctx context.Context, docType, id string, data map[string]any) error {
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
func (d *DB) Delete(ctx context.Context, docType, id string) error {
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
		tx:            tx,
		dialect:       d.dialect,
		tableNames:    d.tableNames,
		compiledDocs:  d.compiledDocs,
		childDocTypes: d.childDocTypes,
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
	tx            *sql.Tx
	dialect       *Dialect
	tableNames    map[string]string
	compiledDocs  map[string]*schema.CompiledDoc
	childDocTypes map[string]bool
}

func (t *txDB) Dialect() dal.Dialect { return t.dialect }

func (t *txDB) Close() error { return nil }

func (t *txDB) Query(ctx context.Context, q dal.Select) ([]map[string]any, error) {
	var doc *schema.CompiledDoc
	if cd, ok := t.compiledDocs[q.DocType]; ok {
		doc = cd
		q = resolveSelect(cd, q)
	} else if q.TableName == "" {
		tn, ok := t.tableNames[q.DocType]
		if !ok {
			return nil, orjerrors.NotFound(fmt.Sprintf("no table mapping for docType %q", q.DocType))
		}
		q.TableName = tn
		if t.childDocTypes[q.DocType] {
			q.IncludeDeleted = true
		}
	}
	searchApplied := false
	if doc != nil {
		var err error
		q, searchApplied, err = resolveSearchFilter(ctx, t.tx, t.dialect, doc, q)
		if err != nil {
			return nil, err
		}
	}
	if searchApplied && len(q.IDs) == 0 {
		return []map[string]any{}, nil
	}
	sqlStr, args := t.dialect.SelectSQL(q)
	for i, a := range args {
		args[i] = formatValue(a)
	}
	rows, err := t.tx.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, orjerrors.Internal("query failed", err)
	}
	defer func() { _ = rows.Close() }()
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

func (t *txDB) Update(ctx context.Context, docType, id string, data map[string]any) error {
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

func (t *txDB) Delete(ctx context.Context, docType, id string) error {
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

func (t *txDB) Transaction(_ context.Context, fn func(dal.Tx) error) error {
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

// queryRower abstracts *sql.DB and *sql.Tx for full-text search lookups.
type queryRower interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// resolveSearchFilter translates the "q" full-text filter (PRD §688 List
// parameter) into an id-set restriction using the active Dialect's
// FullTextSearch. It returns the possibly-modified Select and whether a "q"
// filter was consumed; a consumed filter with zero matching IDs means the
// caller must return no rows.
func resolveSearchFilter(ctx context.Context, db queryRower, dialect *Dialect, doc *schema.CompiledDoc, q dal.Select) (dal.Select, bool, error) {
	qVal, ok := q.Filters["q"]
	if !ok {
		return q, false, nil
	}
	delete(q.Filters, "q")

	query := fmt.Sprint(qVal)
	fields := make([]string, 0, len(doc.Fields))
	for _, f := range doc.Fields {
		if f.Searchable && f.Type != schema.FieldTypeChildTable {
			fields = append(fields, f.DBColumn)
		}
	}
	if strings.TrimSpace(query) == "" || len(fields) == 0 {
		return q, true, nil
	}

	sqlStr, args := dialect.FullTextSearch(doc.TableName, query, fields)
	for i, a := range args {
		args[i] = formatValue(a)
	}
	rows, err := db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return q, true, orjerrors.Internal("full-text search failed", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return q, true, orjerrors.Internal("scan search result", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return q, true, orjerrors.Internal("search result iteration failed", err)
	}
	q.IDs = ids
	return q, true, nil
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
	ctx := context.Background()
	for _, doc := range docs {
		ddl := d.dialect.CreateTable(*doc)
		if _, err := d.db.ExecContext(ctx, ddl); err != nil {
			return orjerrors.Internal(fmt.Sprintf("create table %q failed", doc.TableName), err)
		}
		for _, child := range doc.ChildTables {
			childDoc := schema.CompiledDoc{
				TableName: child.TableName,
				Fields:    child.Fields,
			}
			childDDL := d.dialect.CreateTable(childDoc)
			if _, err := d.db.ExecContext(ctx, childDDL); err != nil {
				return orjerrors.Internal(fmt.Sprintf("create child table %q failed", childDoc.TableName), err)
			}
		}
	}
	return nil
}

// ExistingTables returns the set of table names currently in the database.
func (d *DB) ExistingTables() (map[string]bool, error) {
	rows, err := d.db.QueryContext(context.Background(), "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'goose_%'")
	if err != nil {
		return nil, orjerrors.Internal("failed to list tables", err)
	}
	defer func() { _ = rows.Close() }()
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

// ExistingColumns returns the live column names of a table mapped to the type
// each column reports (PRAGMA type strings, as written at CREATE time).
func (d *DB) ExistingColumns(tableName string) (map[string]string, error) {
	rows, err := d.db.QueryContext(context.Background(), fmt.Sprintf("PRAGMA table_info(%q)", tableName))
	if err != nil {
		return nil, orjerrors.Internal("PRAGMA table_info failed", err)
	}
	defer func() { _ = rows.Close() }()
	cols := make(map[string]string)
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, orjerrors.Internal("scan column info", err)
		}
		cols[name] = typ
	}
	return cols, rows.Err()
}

// Underlying returns the raw *sql.DB.
func (d *DB) Underlying() *sql.DB { return d.db }

func formatValue(v any) any {
	switch val := v.(type) {
	case time.Time:
		return val.Format(time.RFC3339)
	default:
		return v
	}
}
