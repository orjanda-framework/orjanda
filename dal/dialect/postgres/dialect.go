package postgres

import (
	"fmt"
	"sort"
	"strings"

	"github.com/orjanda-framework/orjanda/dal"
	"github.com/orjanda-framework/orjanda/schema"
)

// Dialect implements dal.Dialect for PostgreSQL.
// Uses $N positional placeholders and PostgreSQL DDL conventions.
// See TAD §2.3 and PRD §13.3.
type Dialect struct{}

// New returns a new PostgreSQL Dialect.
func New() *Dialect { return &Dialect{} }

// Name returns "postgres".
func (d *Dialect) Name() string { return "postgres" }

// DriverName returns the driver string for database/sql registration via pgx.
func (d *Dialect) DriverName() string { return "pgx" }

// Placeholder returns $N for PostgreSQL positional parameters (1-indexed).
func (d *Dialect) Placeholder(n int) string { return fmt.Sprintf("$%d", n) }

// CreateTable generates CREATE TABLE SQL for the given CompiledDoc.
func (d *Dialect) CreateTable(doc schema.CompiledDoc) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "CREATE TABLE IF NOT EXISTS %q (\n", doc.TableName)

	colDefs := make([]string, 0, len(doc.Fields))
	for _, f := range doc.Fields {
		if f.Type == schema.FieldTypeChildTable {
			continue
		}
		col := fmt.Sprintf("  %q %s", f.DBColumn, pgType(f))
		if f.Name == "ID" {
			col += " PRIMARY KEY"
		}
		if f.Required && f.Name != "ID" {
			col += " NOT NULL"
		}
		if f.Unique {
			col += " UNIQUE"
		}
		colDefs = append(colDefs, col)
	}

	sb.WriteString(strings.Join(colDefs, ",\n"))
	sb.WriteString("\n)")
	return sb.String()
}

// AlterTable generates ALTER TABLE SQL for column additions and drops.
func (d *Dialect) AlterTable(diff schema.TableAlteration) []string {
	stmts := make([]string, 0)
	for _, f := range diff.AddColumns {
		if f.Type == schema.FieldTypeChildTable {
			continue
		}
		col := fmt.Sprintf("ALTER TABLE %q ADD COLUMN IF NOT EXISTS %q %s", diff.TableName, f.DBColumn, pgType(f))
		if f.Required {
			col += " NOT NULL DEFAULT ''"
		}
		stmts = append(stmts, col)
	}
	for _, col := range diff.DropColumns {
		stmts = append(stmts, fmt.Sprintf("ALTER TABLE %q DROP COLUMN IF EXISTS %q", diff.TableName, col))
	}
	return stmts
}

// SelectSQL renders a Select into PostgreSQL SQL and a positional args slice.
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

	n := 1
	conditions := make([]string, 0)
	if !q.IncludeDeleted {
		conditions = append(conditions, fmt.Sprintf(`"deleted" = %s`, d.Placeholder(n)))
		args = append(args, false)
		n++
	}

	keys := make([]string, 0, len(q.Filters))
	for k := range q.Filters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		conditions = append(conditions, fmt.Sprintf("%q = %s", k, d.Placeholder(n)))
		args = append(args, q.Filters[k])
		n++
	}

	if len(conditions) > 0 {
		sb.WriteString(" WHERE " + strings.Join(conditions, " AND "))
	}

	if q.OrderBy != "" {
		parts := strings.Split(q.OrderBy, " ")
		col := parts[0]
		dir := ""
		if len(parts) > 1 {
			dir = " " + parts[1]
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

// InsertSQL renders an INSERT statement with $N placeholders.
func (d *Dialect) InsertSQL(tableName string, fields map[string]any) (string, []any) {
	cols := make([]string, 0, len(fields))
	for k := range fields {
		cols = append(cols, k)
	}
	sort.Strings(cols)

	placeholders := make([]string, len(cols))
	args := make([]any, len(cols))
	for i, col := range cols {
		placeholders[i] = d.Placeholder(i + 1)
		args[i] = fields[col]
	}

	quotedCols := make([]string, len(cols))
	for i, c := range cols {
		quotedCols[i] = fmt.Sprintf("%q", c)
	}

	sqlStr := fmt.Sprintf("INSERT INTO %q (%s) VALUES (%s)",
		tableName,
		strings.Join(quotedCols, ", "),
		strings.Join(placeholders, ", "),
	)
	return sqlStr, args
}

// UpdateSQL renders an UPDATE statement with $N placeholders.
func (d *Dialect) UpdateSQL(tableName string, id string, fields map[string]any) (string, []any) {
	cols := make([]string, 0, len(fields))
	for k := range fields {
		cols = append(cols, k)
	}
	sort.Strings(cols)

	setClauses := make([]string, len(cols))
	args := make([]any, len(cols))
	for i, col := range cols {
		setClauses[i] = fmt.Sprintf("%q = %s", col, d.Placeholder(i+1))
		args[i] = fields[col]
	}
	args = append(args, id)

	sqlStr := fmt.Sprintf("UPDATE %q SET %s WHERE %q = %s",
		tableName,
		strings.Join(setClauses, ", "),
		"id",
		d.Placeholder(len(cols)+1),
	)
	return sqlStr, args
}

// DeleteSQL renders a hard DELETE statement with $N placeholder.
func (d *Dialect) DeleteSQL(tableName string, id string) (string, []any) {
	return fmt.Sprintf("DELETE FROM %q WHERE %q = $1", tableName, "id"), []any{id}
}

// FullTextSearch renders a PostgreSQL tsvector-based FTS query excluding soft-deleted records.
func (d *Dialect) FullTextSearch(tableName string, query string, fields []string) (string, []any) {
	if len(fields) == 0 {
		return fmt.Sprintf("SELECT %q FROM %q WHERE FALSE", "id", tableName), nil
	}

	tsVectors := make([]string, len(fields))
	for i, f := range fields {
		tsVectors[i] = fmt.Sprintf("to_tsvector('english', COALESCE(%q::text, ''))", f)
	}
	combined := strings.Join(tsVectors, " || ")

	sql := fmt.Sprintf(`SELECT "id" FROM %q WHERE "deleted" = FALSE AND ((%s) @@ plainto_tsquery('english', $1))`,
		tableName, combined)
	return sql, []any{query}
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func pgType(f schema.Field) string {
	switch f.Type {
	case schema.FieldTypeInt:
		return "INTEGER"
	case schema.FieldTypeInt64:
		return "BIGINT"
	case schema.FieldTypeBool:
		return "BOOLEAN"
	case schema.FieldTypeFloat64:
		return "DOUBLE PRECISION"
	case schema.FieldTypeCurrency:
		return "NUMERIC(20,6)"
	case schema.FieldTypeDate:
		return "DATE"
	case schema.FieldTypeDateTime:
		return "TIMESTAMPTZ"
	case schema.FieldTypeJSON:
		return "JSONB"
	case schema.FieldTypeText, schema.FieldTypeRichText:
		return "TEXT"
	default:
		return "TEXT"
	}
}
