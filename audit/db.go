package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/orjanda-framework/orjanda/dal"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
)

// DBLog is the DB-backed audit.Log. It satisfies TAD §13.1 by writing through
// the caller's dal.Tx (WriteTx), so a failed audit write rolls back the data
// write; reads and the standalone Write path use the underlying connection.
//
// The backing table is TableName, registered with the DAL under the pseudo
// DocType (see DocType) at site compile time so tx.Insert resolves it. It is
// not a Registry Document: audit entries are never exposed as operable
// documents (TAD §13).
type DBLog struct {
	db      *sql.DB
	dialect dal.Dialect
}

// NewDBLog creates a DB-backed audit log on the given connection using the
// active dialect. The table must exist before writes; call EnsureSchema (or
// let Site.InitAuditLog do it) first.
func NewDBLog(db *sql.DB, dialect dal.Dialect) *DBLog {
	return &DBLog{db: db, dialect: dialect}
}

// EnsureSchema creates the audit table if it does not exist (idempotent).
func (l *DBLog) EnsureSchema(ctx context.Context) error {
	if _, err := l.db.ExecContext(ctx, l.createTableSQL()); err != nil {
		return orjerrors.Internal("audit: create table failed", err)
	}
	return nil
}

// Write records e as a standalone (autocommit) write. Engines use WriteTx
// inside their transaction; this path exists for direct Log callers that do
// not have a transaction (TAD §13.1 only applies to Engine writes).
func (l *DBLog) Write(ctx context.Context, e Entry) error {
	sqlStr, args := l.insertSQL(e)
	if _, err := l.db.ExecContext(ctx, sqlStr, args...); err != nil {
		return orjerrors.Internal("audit: write failed", err)
	}
	return nil
}

// WriteTx records e inside tx, sharing the transaction with the data write.
// A failure here returns an error to the caller's transaction callback, which
// rolls back the whole operation (TAD §13.1).
func (l *DBLog) WriteTx(ctx context.Context, tx dal.Tx, e Entry) error {
	if _, err := tx.Insert(ctx, DocType, entryRow(e, l.dialect.Name())); err != nil {
		return err
	}
	return nil
}

// Query returns entries matching f, ordered newest first. Since and Limit are
// applied server-side; the remaining filters are simple equality predicates.
func (l *DBLog) Query(ctx context.Context, f QueryFilter) ([]Entry, error) {
	var (
		where  []string
		args   []any
		ph     = l.dialect.Placeholder
	)
	if f.DocType != "" {
		args = append(args, f.DocType)
		where = append(where, "doc_type = "+ph(len(args)))
	}
	if f.DocID != "" {
		args = append(args, f.DocID)
		where = append(where, "doc_id = "+ph(len(args)))
	}
	if f.UserID != "" {
		args = append(args, f.UserID)
		where = append(where, "user_id = "+ph(len(args)))
	}
	if f.ViaAgent != nil {
		args = append(args, *f.ViaAgent)
		where = append(where, "via_agent = "+ph(len(args)))
	}
	if !f.Since.IsZero() {
		args = append(args, l.timestampArg(f.Since))
		where = append(where, "timestamp >= "+ph(len(args)))
	}

	sqlStr := "SELECT id, timestamp, user_id, doc_type, doc_id, action, changes, " +
		"via_agent, agent_session, agent_prompt, ip_address, user_agent, request_id " +
		"FROM " + TableName
	if len(where) > 0 {
		sqlStr += " WHERE " + strings.Join(where, " AND ")
	}
	sqlStr += " ORDER BY timestamp DESC"
	if f.Limit > 0 {
		args = append(args, f.Limit)
		sqlStr += " LIMIT " + ph(len(args))
	}

	rows, err := l.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, orjerrors.Internal("audit: query failed", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Entry
	for rows.Next() {
		var (
			e            Entry
			changesJSON  string
			rawTimestamp any
			rawViaAgent  any
		)
		if err := rows.Scan(
			&e.ID, &rawTimestamp, &e.UserID, &e.DocType, &e.DocID, &e.Action,
			&changesJSON, &rawViaAgent, &e.AgentSession, &e.AgentPrompt,
			&e.IPAddress, &e.UserAgent, &e.RequestID,
		); err != nil {
			return nil, orjerrors.Internal("audit: scan failed", err)
		}
		e.Timestamp = l.timestampFromRaw(rawTimestamp)
		e.ViaAgent = l.boolFromRaw(rawViaAgent)
		if changesJSON != "" {
			if err := json.Unmarshal([]byte(changesJSON), &e.Changes); err != nil {
				return nil, orjerrors.Internal("audit: decode changes failed", err)
			}
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, orjerrors.Internal("audit: query iteration failed", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// SQL helpers
// ---------------------------------------------------------------------------

// createTableSQL returns dialect-specific DDL for the audit table.
func (l *DBLog) createTableSQL() string {
	switch l.dialect.Name() {
	case "postgres":
		return `CREATE TABLE IF NOT EXISTS audit_entries (
	id TEXT PRIMARY KEY,
	timestamp TIMESTAMPTZ NOT NULL,
	user_id TEXT NOT NULL DEFAULT '',
	doc_type TEXT NOT NULL,
	doc_id TEXT NOT NULL,
	action TEXT NOT NULL,
	changes TEXT NOT NULL DEFAULT '',
	via_agent BOOLEAN NOT NULL DEFAULT FALSE,
	agent_session TEXT NOT NULL DEFAULT '',
	agent_prompt TEXT NOT NULL DEFAULT '',
	ip_address TEXT NOT NULL DEFAULT '',
	user_agent TEXT NOT NULL DEFAULT '',
	request_id TEXT NOT NULL DEFAULT ''
)`
	default: // sqlite
		return `CREATE TABLE IF NOT EXISTS audit_entries (
	id TEXT PRIMARY KEY,
	timestamp INTEGER NOT NULL,
	user_id TEXT NOT NULL DEFAULT '',
	doc_type TEXT NOT NULL,
	doc_id TEXT NOT NULL,
	action TEXT NOT NULL,
	changes TEXT NOT NULL DEFAULT '',
	via_agent INTEGER NOT NULL DEFAULT 0,
	agent_session TEXT NOT NULL DEFAULT '',
	agent_prompt TEXT NOT NULL DEFAULT '',
	ip_address TEXT NOT NULL DEFAULT '',
	user_agent TEXT NOT NULL DEFAULT '',
	request_id TEXT NOT NULL DEFAULT ''
)`
	}
}

// entryRow builds the column map used by the transactional write (tx.Insert
// applies per-dialect value formatting). SQLite stores timestamps as Unix
// nanoseconds so ORDER BY is exact; PostgreSQL uses TIMESTAMPTZ.
func entryRow(e Entry, dialectName string) map[string]any {
	var ts any
	if dialectName == "sqlite" {
		ts = entryTimestamp(e).UnixNano()
	} else {
		ts = entryTimestamp(e)
	}
	return map[string]any{
		"id":            entryID(e),
		"timestamp":     ts,
		"user_id":       e.UserID,
		"doc_type":      e.DocType,
		"doc_id":        e.DocID,
		"action":        e.Action,
		"changes":       changesJSON(e.Changes),
		"via_agent":     e.ViaAgent,
		"agent_session": e.AgentSession,
		"agent_prompt":  e.AgentPrompt,
		"ip_address":    e.IPAddress,
		"user_agent":    e.UserAgent,
		"request_id":    e.RequestID,
	}
}

// insertSQL renders the standalone-write INSERT (Write) with the same column
// set and per-dialect value formatting.
func (l *DBLog) insertSQL(e Entry) (string, []any) {
	row := entryRow(e, l.dialect.Name())
	cols := []string{
		"id", "timestamp", "user_id", "doc_type", "doc_id", "action", "changes",
		"via_agent", "agent_session", "agent_prompt", "ip_address", "user_agent",
		"request_id",
	}
	sqlStr := "INSERT INTO " + TableName + " ("
	for i, c := range cols {
		if i > 0 {
			sqlStr += ", "
		}
		sqlStr += c
	}
	sqlStr += ") VALUES ("
	args := make([]any, 0, len(cols))
	for i, c := range cols {
		if i > 0 {
			sqlStr += ", "
		}
		sqlStr += l.dialect.Placeholder(i + 1)
		args = append(args, row[c])
	}
	sqlStr += ")"
	return sqlStr, args
}

func entryID(e Entry) string {
	if e.ID != "" {
		return e.ID
	}
	return ulid.Make().String()
}

func entryTimestamp(e Entry) time.Time {
	if e.Timestamp.IsZero() {
		return time.Now()
	}
	return e.Timestamp
}

func changesJSON(changes []FieldChange) string {
	if len(changes) == 0 {
		return ""
	}
	b, err := json.Marshal(changes)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// timestampArg formats a Since bound for the active dialect.
func (l *DBLog) timestampArg(t time.Time) any {
	if l.dialect.Name() == "sqlite" {
		return t.UnixNano()
	}
	return t
}

func (l *DBLog) timestampFromRaw(v any) time.Time {
	switch val := v.(type) {
	case time.Time:
		return val
	case int64:
		return time.Unix(0, val)
	case int:
		return time.Unix(0, int64(val))
	case string:
		if t, err := time.Parse(time.RFC3339, val); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (l *DBLog) boolFromRaw(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case int64:
		return val != 0
	case int:
		return val != 0
	}
	return false
}
