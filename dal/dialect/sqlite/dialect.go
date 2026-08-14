package sqlite

import (
	"fmt"
	"sort"
	"strings"

	"github.com/orjanda-framework/orjanda/dal"
	"github.com/orjanda-framework/orjanda/schema"
)

// Dialect implements dal.Dialect for SQLite.
type Dialect struct{}

// New creates a new SQLite Dialect.
func New() *Dialect { return &Dialect{} }

// Name returns "sqlite".
func (d *Dialect) Name() string { return "sqlite" }

// DriverName returns "sqlite".
func (d *Dialect) DriverName() string { return "sqlite" }

// Quote returns the double-quoted string identifier.
func (d *Dialect) Quote(s string) string { return fmt.Sprintf("%q", s) }

// Placeholder returns "?" for all positions (SQLite positional style).
func (d *Dialect) Placeholder(_ int) string { return "?" }

// isSortDirection reports whether tok is an ORDER BY direction token. Only
// ASC/DESC (case-insensitive) is ever emitted; anything else is dropped so an
// unvalidated direction can never reach the rendered SQL (REVIEW-2026-08-12
// finding 10 — the engine allowlists, this is defense in depth).
func isSortDirection(tok string) bool {
	t := strings.ToUpper(strings.TrimSpace(tok))
	return t == "ASC" || t == "DESC"
}

// CreateTable generates CREATE TABLE SQL for the given CompiledDoc.
func (d *Dialect) CreateTable(doc schema.CompiledDoc) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "CREATE TABLE IF NOT EXISTS %q (\n", doc.TableName)

	colDefs := make([]string, 0, len(doc.Fields))
	for _, f := range doc.Fields {
		if f.Type == schema.FieldTypeChildTable {
			continue // child tables get their own CREATE TABLE
		}
		col := fmt.Sprintf("  %q %s", f.DBColumn, sqliteType(f))
		if f.Name == "ID" {
			col += " PRIMARY KEY"
		}
		if f.Required && f.Name != "ID" {
			col += " NOT NULL"
		}
		if f.Unique {
			// Match the PostgreSQL dialect (dal/dialect/postgres/dialect.go):
			// unique constraints are emitted as inline column constraints so a
			// concurrent duplicate insert fails at the database rather than
			// silently succeeding (REVIEW-2026-08-12 finding 12).
			col += " UNIQUE"
		}
		colDefs = append(colDefs, col)
	}

	sb.WriteString(strings.Join(colDefs, ",\n"))
	sb.WriteString("\n)")
	return sb.String()
}

// ColumnType returns the DDL type SQLite uses for the given Field.
func (d *Dialect) ColumnType(f schema.Field) string { return sqliteType(f) }

// NormalizeColumnType canonicalizes a PRAGMA-reported column type (which for
// SQLite is the literal type string written at CREATE time) for comparison
// with ColumnType's output.
func (d *Dialect) NormalizeColumnType(raw string) string {
	return strings.ToUpper(strings.TrimSpace(raw))
}

// AlterTable generates ALTER TABLE SQL for column additions and drops.
// Note: SQLite does not support DROP COLUMN in older versions natively,
// but modern sqlite (3.35.0+) supports ALTER TABLE DROP COLUMN.
// SQLite has no ALTER COLUMN TYPE; type changes are surfaced as a review
// comment so the author can write the required table rebuild manually
// (forward-only, TAD §14.1 step 2).
func (d *Dialect) AlterTable(diff schema.TableAlteration) []string {
	stmts := make([]string, 0)
	for _, f := range diff.AddColumns {
		if f.Type == schema.FieldTypeChildTable {
			continue
		}
		col := fmt.Sprintf("ALTER TABLE %q ADD COLUMN %q %s", diff.TableName, f.DBColumn, sqliteType(f))
		if f.Required {
			col += " NOT NULL DEFAULT ''"
		}
		stmts = append(stmts, col)
	}
	for _, col := range diff.DropColumns {
		stmts = append(stmts, fmt.Sprintf("ALTER TABLE %q DROP COLUMN %q", diff.TableName, col))
	}
	for _, a := range diff.AlterColumns {
		stmts = append(stmts, fmt.Sprintf(
			"-- NOTE (sqlite): ALTER COLUMN TYPE is unsupported; rebuild %q manually to change %q from %s to %s (see TAD §14.1 step 2)",
			diff.TableName, a.ColumnName, a.OldColumn, a.NewColumn))
	}
	return stmts
}

// SelectSQL renders a Select into SQLite SQL and a positional args slice.
func (d *Dialect) SelectSQL(q dal.Select) (string, []any) {
	var sb strings.Builder
	var args []any

	cols := "*"
	if len(q.Fields) > 0 {
		quoted := make([]string, len(q.Fields))
		for i, f := range q.Fields {
			quoted[i] = fmt.Sprintf("%q", f)
		}
		cols = strings.Join(quoted, ", ")
	}
	fmt.Fprintf(&sb, "SELECT %s FROM %q", cols, q.TableName)

	conditions := make([]string, 0)
	if !q.IncludeDeleted {
		conditions = append(conditions, `"deleted" = ?`)
		args = append(args, false)
	}

	keys := make([]string, 0, len(q.Filters))
	for k := range q.Filters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		conditions = append(conditions, fmt.Sprintf("%q = ?", k))
		args = append(args, q.Filters[k])
	}

	if len(q.IDs) > 0 {
		placeholders := make([]string, len(q.IDs))
		for i := range q.IDs {
			placeholders[i] = "?"
			args = append(args, q.IDs[i])
		}
		conditions = append(conditions, fmt.Sprintf("%q IN (%s)", "id", strings.Join(placeholders, ", ")))
	}

	if len(conditions) > 0 {
		sb.WriteString(" WHERE " + strings.Join(conditions, " AND "))
	}

	if q.OrderBy != "" {
		parts := strings.Split(q.OrderBy, " ")
		col := parts[0]
		dir := ""
		if len(parts) > 1 && isSortDirection(parts[1]) {
			dir = " " + strings.ToUpper(parts[1])
		}
		fmt.Fprintf(&sb, " ORDER BY %q%s", col, dir)
	}
	if q.Limit > 0 {
		fmt.Fprintf(&sb, " LIMIT %d", q.Limit)
	}
	if q.Offset > 0 {
		fmt.Fprintf(&sb, " OFFSET %d", q.Offset)
	}

	return sb.String(), args
}

// InsertSQL renders an INSERT statement for SQLite (? placeholders).
func (d *Dialect) InsertSQL(tableName string, fields map[string]any) (string, []any) {
	cols := make([]string, 0, len(fields))
	for k := range fields {
		cols = append(cols, k)
	}
	sort.Strings(cols)

	placeholders := make([]string, len(cols))
	args := make([]any, len(cols))
	quotedCols := make([]string, len(cols))

	for i, col := range cols {
		quotedCols[i] = fmt.Sprintf("%q", col)
		placeholders[i] = "?"
		args[i] = fields[col]
	}

	sql := fmt.Sprintf(
		"INSERT INTO %q (%s) VALUES (%s)",
		tableName,
		strings.Join(quotedCols, ", "),
		strings.Join(placeholders, ", "),
	)
	return sql, args
}

// UpdateSQL renders an UPDATE statement for SQLite.
func (d *Dialect) UpdateSQL(tableName, id string, fields map[string]any) (string, []any) {
	cols := make([]string, 0, len(fields))
	for k := range fields {
		if k == "id" {
			continue // ID is set in WHERE
		}
		cols = append(cols, k)
	}
	sort.Strings(cols)

	setClauses := make([]string, len(cols))
	args := make([]any, 0, len(cols)+1)

	for i, col := range cols {
		setClauses[i] = fmt.Sprintf("%q = ?", col)
		args = append(args, fields[col])
	}
	args = append(args, id)

	sql := fmt.Sprintf(
		"UPDATE %q SET %s WHERE \"id\" = ?",
		tableName,
		strings.Join(setClauses, ", "),
	)
	return sql, args
}

// DeleteSQL renders a hard DELETE statement.
func (d *Dialect) DeleteSQL(tableName, id string) (string, []any) {
	sql := fmt.Sprintf("DELETE FROM %q WHERE \"id\" = ?", tableName)
	return sql, []any{id}
}

// FullTextSearch renders a full-text search query.
// For SQLite, uses LIKE %query% across all searchable text columns as a lightweight MVP implementation.
func (d *Dialect) FullTextSearch(tableName, query string, searchableFields []string) (string, []any) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "SELECT \"id\" FROM %q WHERE (\"deleted\" = ? OR \"deleted\" IS NULL)", tableName)

	args := []any{false}
	pattern := "%" + query + "%"

	if len(searchableFields) > 0 {
		conditions := make([]string, len(searchableFields))
		for i, field := range searchableFields {
			conditions[i] = fmt.Sprintf("%q LIKE ?", field)
			args = append(args, pattern)
		}
		sb.WriteString(" AND (" + strings.Join(conditions, " OR ") + ")")
	}

	return sb.String(), args
}

func sqliteType(f schema.Field) string {
	switch f.Type {
	case schema.FieldTypeInt, schema.FieldTypeInt64:
		return "INTEGER"
	case schema.FieldTypeFloat64, schema.FieldTypeCurrency:
		return "REAL"
	case schema.FieldTypeBool:
		return "INTEGER" // SQLite stores booleans as 0 or 1
	case schema.FieldTypeDate, schema.FieldTypeDateTime:
		return "TEXT" // ISO-8601 strings
	case schema.FieldTypeText, schema.FieldTypeRichText, schema.FieldTypeJSON:
		return "TEXT"
	default:
		return "TEXT"
	}
}
