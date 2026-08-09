package sqlite

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/orjanda-framework/orjanda/dal"
	"github.com/orjanda-framework/orjanda/schema"
)

// Dialect implements dal.Dialect for SQLite (modernc.org/sqlite, pure-Go).
// See TAD §2.3 and PRD §13.3.
type Dialect struct{}

// New returns a new SQLite Dialect.
func New() *Dialect { return &Dialect{} }

// Name returns "sqlite".
func (d *Dialect) Name() string { return "sqlite" }

// DriverName returns the driver string registered by modernc.org/sqlite.
func (d *Dialect) DriverName() string { return "sqlite" }

// Placeholder returns "?" for all positions (SQLite positional style).
func (d *Dialect) Placeholder(_ int) string { return "?" }

// CreateTable generates CREATE TABLE SQL for the given CompiledDoc.
func (d *Dialect) CreateTable(doc schema.CompiledDoc) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("CREATE TABLE IF NOT EXISTS %q (\n", doc.TableName))

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
		col := fmt.Sprintf("ALTER TABLE %q ADD COLUMN %q %s", diff.TableName, f.DBColumn, sqliteType(f))
		if f.Required {
			col += " NOT NULL DEFAULT ''"
		}
		stmts = append(stmts, col)
	}
	for _, col := range diff.DropColumns {
		stmts = append(stmts, fmt.Sprintf("ALTER TABLE %q DROP COLUMN %q", diff.TableName, col))
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
	sb.WriteString(fmt.Sprintf("SELECT %s FROM %q", cols, q.TableName))

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
		sb.WriteString(fmt.Sprintf(" ORDER BY %q%s", col, dir))
	}
	if q.Limit > 0 {
		sb.WriteString(fmt.Sprintf(" LIMIT %d", q.Limit))
	}
	if q.Offset > 0 {
		sb.WriteString(fmt.Sprintf(" OFFSET %d", q.Offset))
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
	for i, col := range cols {
		placeholders[i] = "?"
		args[i] = fields[col]
	}

	quotedCols := make([]string, len(cols))
	for i, c := range cols {
		quotedCols[i] = fmt.Sprintf("%q", c)
	}

	sql := fmt.Sprintf("INSERT INTO %q (%s) VALUES (%s)",
		tableName,
		strings.Join(quotedCols, ", "),
		strings.Join(placeholders, ", "),
	)
	return sql, args
}

// UpdateSQL renders an UPDATE statement for SQLite (? placeholders).
func (d *Dialect) UpdateSQL(tableName string, id string, fields map[string]any) (string, []any) {
	cols := make([]string, 0, len(fields))
	for k := range fields {
		cols = append(cols, k)
	}
	sort.Strings(cols)

	setClauses := make([]string, len(cols))
	args := make([]any, len(cols))
	for i, col := range cols {
		setClauses[i] = fmt.Sprintf("%q = ?", col)
		args[i] = fields[col]
	}
	args = append(args, id)

	sql := fmt.Sprintf("UPDATE %q SET %s WHERE %q = ?",
		tableName,
		strings.Join(setClauses, ", "),
		"id",
	)
	return sql, args
}

// DeleteSQL renders a hard DELETE statement for SQLite.
func (d *Dialect) DeleteSQL(tableName string, id string) (string, []any) {
	return fmt.Sprintf("DELETE FROM %q WHERE %q = ?", tableName, "id"), []any{id}
}

// FullTextSearch renders a basic LIKE-based full-text search for SQLite.
// Soft-deleted records are automatically filtered out. See TAD §9.1.
func (d *Dialect) FullTextSearch(tableName string, query string, fields []string) (string, []any) {
	if len(fields) == 0 {
		return fmt.Sprintf("SELECT %q FROM %q WHERE 1=0", "id", tableName), nil
	}

	conditions := make([]string, len(fields))
	args := []any{false}
	likeVal := "%" + query + "%"
	for i, f := range fields {
		conditions[i] = fmt.Sprintf("%q LIKE ?", f)
		args = append(args, likeVal)
	}

	sql := fmt.Sprintf(`SELECT "id" FROM %q WHERE "deleted" = ? AND (%s)`,
		tableName,
		strings.Join(conditions, " OR "),
	)
	return sql, args
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func sqliteType(f schema.Field) string {
	switch f.Type {
	case schema.FieldTypeInt, schema.FieldTypeInt64:
		return "INTEGER"
	case schema.FieldTypeBool:
		return "INTEGER" // 0/1
	case schema.FieldTypeFloat64, schema.FieldTypeCurrency:
		return "REAL"
	case schema.FieldTypeDate, schema.FieldTypeDateTime:
		return "TEXT"
	case schema.FieldTypeJSON:
		return "TEXT"
	case schema.FieldTypeText, schema.FieldTypeRichText:
		return "TEXT"
	default:
		return "TEXT"
	}
}

func formatValue(v any) any {
	switch val := v.(type) {
	case time.Time:
		return val.UTC().Format(time.RFC3339Nano)
	case bool:
		if val {
			return 1
		}
		return 0
	}
	return v
}
