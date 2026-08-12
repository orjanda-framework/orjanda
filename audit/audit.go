package audit

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/dal"
)

// DocType is the pseudo DocType under which the audit table is registered with
// the DAL. It is not a Registry Document — audit entries are written only by
// the Document/Workflow Engines and are never exposed as operable documents
// (TAD §13).
const DocType = "audit"

// TableName is the SQL table backing the DB-backed audit log.
const TableName = "audit_entries"

// Entry is an immutable audit record written inside the same dal.Tx as the
// triggering Document Engine or Workflow Engine operation. See TAD §13.
type Entry struct {
	ID           string
	Timestamp    time.Time
	UserID       string
	DocType      string
	DocID        string
	Action       string // "create" | "update" | "delete" | "workflow_transition"
	Changes      []FieldChange
	ViaAgent     bool
	AgentSession string
	AgentPrompt  string
	IPAddress    string
	UserAgent    string
	RequestID    string
}

// FieldChange records the pre/post value of a single field. Unchanged fields
// are omitted entirely (TAD §13.2).
type FieldChange struct {
	Field    string
	OldValue any
	NewValue any
}

// QueryFilter selects entries from the audit log.
type QueryFilter struct {
	DocType  string
	DocID    string
	UserID   string
	ViaAgent *bool
	Since    time.Time
	Limit    int
}

// Log is the audit log interface. Implementations must write inside the
// caller's dal.Tx (TAD §13.1 write-path guarantee). The in-memory
// implementation provided here is sufficient for MVP unit tests; the DB-backed
// DBLog is wired at site compile time.
type Log interface {
	Write(ctx context.Context, e Entry) error
	Query(ctx context.Context, f QueryFilter) ([]Entry, error)
}

// TxWriter is implemented by Log implementations that write inside the
// caller's dal.Tx, giving TAD §13.1 its rollback guarantee: a failed audit
// write aborts the triggering operation. The in-memory log does not need a
// transaction handle; the DB-backed log does. Engines dispatch through
// WriteInTx, which prefers WriteTx when the configured log supports it.
type TxWriter interface {
	WriteTx(ctx context.Context, tx dal.Tx, e Entry) error
}

// WriteInTx writes e inside tx when log implements TxWriter (TAD §13.1), and
// falls back to the plain Write path for logs that don't participate in the
// caller's transaction (e.g. InMemoryLog). Engines must call this inside their
// dal.Tx callback so a failed audit write rolls back the data write.
func WriteInTx(ctx context.Context, tx dal.Tx, log Log, e Entry) error {
	if w, ok := log.(TxWriter); ok {
		return w.WriteTx(ctx, tx, e)
	}
	return log.Write(ctx, e)
}

// ---------------------------------------------------------------------------
// InMemoryLog — non-persistent implementation used for unit tests and as the
// session-only default when no database-backed log can be wired (nil DB, or
// an embedding without raw-connection access). DBLog is the persistent
// implementation used whenever a database is available.
// ---------------------------------------------------------------------------

type InMemoryLog struct {
	mu      sync.RWMutex
	entries []Entry
}

// NewInMemoryLog creates a new in-memory audit log.
func NewInMemoryLog() *InMemoryLog {
	return &InMemoryLog{}
}

func (l *InMemoryLog) Write(_ context.Context, e Entry) error {
	if e.ID == "" {
		e.ID = ulid.Make().String()
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, e)
	return nil
}

func (l *InMemoryLog) Query(_ context.Context, f QueryFilter) ([]Entry, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var out []Entry
	for _, e := range l.entries {
		if f.DocType != "" && e.DocType != f.DocType {
			continue
		}
		if f.DocID != "" && e.DocID != f.DocID {
			continue
		}
		if f.UserID != "" && e.UserID != f.UserID {
			continue
		}
		if f.ViaAgent != nil && e.ViaAgent != *f.ViaAgent {
			continue
		}
		if !f.Since.IsZero() && e.Timestamp.Before(f.Since) {
			continue
		}
		out = append(out, e)
		if f.Limit > 0 && len(out) >= f.Limit {
			break
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Diff helpers — TAD §13.2
// ---------------------------------------------------------------------------

// DiffMaps computes the per-field changes between oldRow and newRow,
// returning only fields whose value changed. Uses column names as field keys.
func DiffMaps(oldRow, newRow map[string]any) []FieldChange {
	seen := make(map[string]bool)
	var changes []FieldChange

	for k, newVal := range newRow {
		seen[k] = true
		oldVal := oldRow[k]
		if !valuesEqual(oldVal, newVal) {
			changes = append(changes, FieldChange{
				Field:    k,
				OldValue: oldVal,
				NewValue: newVal,
			})
		}
	}
	// Fields present in old but not in new (e.g. deleted).
	for k, oldVal := range oldRow {
		if !seen[k] {
			changes = append(changes, FieldChange{
				Field:    k,
				OldValue: oldVal,
				NewValue: nil,
			})
		}
	}
	return changes
}

func valuesEqual(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

// ---------------------------------------------------------------------------
// Context helpers for agent metadata
// ---------------------------------------------------------------------------

type agentContextKey int

const (
	viaAgentKey     agentContextKey = iota
	agentSessionKey agentContextKey = iota
	agentPromptKey  agentContextKey = iota
)

// WithAgent marks the context as an agent-initiated request.
func WithAgent(ctx context.Context, sessionID, prompt string) context.Context {
	ctx = context.WithValue(ctx, viaAgentKey, true)
	ctx = context.WithValue(ctx, agentSessionKey, sessionID)
	ctx = context.WithValue(ctx, agentPromptKey, prompt)
	return ctx
}

// BuildEntry constructs an audit Entry from the context and the given fields.
// Callers supply Action, DocType, DocID, and Changes; this function fills in
// identity, timestamp, and agent metadata.
func BuildEntry(ctx context.Context, action, docType, docID string, changes []FieldChange) Entry {
	id := auth.FromContext(ctx)

	viaAgent, _ := ctx.Value(viaAgentKey).(bool)
	agentSession, _ := ctx.Value(agentSessionKey).(string)
	agentPrompt, _ := ctx.Value(agentPromptKey).(string)

	requestID, _ := ctx.Value(requestIDKey).(string)

	return Entry{
		ID:           ulid.Make().String(),
		Timestamp:    time.Now(),
		UserID:       id.UserID,
		DocType:      docType,
		DocID:        docID,
		Action:       action,
		Changes:      changes,
		ViaAgent:     viaAgent,
		AgentSession: agentSession,
		AgentPrompt:  agentPrompt,
		RequestID:    requestID,
	}
}

// ---------------------------------------------------------------------------
// Request-ID context key (TAD §1.2)
// ---------------------------------------------------------------------------

type reqIDKey int

const requestIDKey reqIDKey = iota

// WithRequestID attaches a request_id to the context.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// RequestIDFromContext extracts the request_id from ctx, or empty string.
func RequestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey).(string)
	return v
}

// Ensure fmt is used.
var _ = fmt.Sprintf
