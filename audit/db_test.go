package audit_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/orjanda-framework/orjanda/audit"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/dal"
	"github.com/orjanda-framework/orjanda/dal/dialect/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newSQLiteDB opens a fresh in-memory SQLite database with the audit table
// created and its docType mapping registered, returning the DB, the DBLog, and
// the underlying connection pinned to one connection (required for :memory:).
func newSQLiteDB(t *testing.T) (*sqlite.DB, *audit.DBLog) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.Underlying().SetMaxOpenConns(1)

	log := audit.NewDBLog(db.Underlying(), db.Dialect())
	require.NoError(t, log.EnsureSchema(context.Background()))
	db.RegisterDoc(audit.DocType, audit.TableName)
	return db, log
}

func TestDBLog_WriteTxAndQuery(t *testing.T) {
	db, log := newSQLiteDB(t)
	ctx := auth.NewContext(context.Background(), auth.Identity{UserID: "usr_100"})
	ctx = audit.WithAgent(ctx, "session_1", "Approve leave request")
	ctx = audit.WithRequestID(ctx, "req_abc")

	entry := audit.BuildEntry(ctx, "update", "LeaveRequest", "lr_001", []audit.FieldChange{
		{Field: "status", OldValue: "Draft", NewValue: "Submitted"},
	})

	err := db.Transaction(ctx, func(tx dal.Tx) error {
		return log.WriteTx(ctx, tx, entry)
	})
	require.NoError(t, err)

	entries, err := log.Query(ctx, audit.QueryFilter{DocType: "LeaveRequest", DocID: "lr_001"})
	require.NoError(t, err)
	require.Len(t, entries, 1)

	e := entries[0]
	assert.Equal(t, entry.ID, e.ID)
	assert.Equal(t, "usr_100", e.UserID)
	assert.Equal(t, "update", e.Action)
	assert.True(t, e.ViaAgent)
	assert.Equal(t, "session_1", e.AgentSession)
	assert.Equal(t, "Approve leave request", e.AgentPrompt)
	assert.Equal(t, "req_abc", e.RequestID)
	assert.Len(t, e.Changes, 1)
	assert.Equal(t, "status", e.Changes[0].Field)
	assert.Equal(t, "Draft", e.Changes[0].OldValue)
	assert.Equal(t, "Submitted", e.Changes[0].NewValue)
	assert.False(t, e.Timestamp.IsZero())
	assert.WithinDuration(t, entry.Timestamp, e.Timestamp, time.Second)
}

func TestDBLog_QueryFilters(t *testing.T) {
	db, log := newSQLiteDB(t)
	ctx := auth.NewContext(context.Background(), auth.Identity{UserID: "usr_1"})

	mk := func(action, docType, docID string) {
		t.Helper()
		e := audit.BuildEntry(ctx, action, docType, docID, nil)
		require.NoError(t, db.Transaction(ctx, func(tx dal.Tx) error {
			return log.WriteTx(ctx, tx, e)
		}))
	}
	mk("create", "Employee", "emp_1")
	mk("update", "Employee", "emp_1")
	mk("create", "LeaveRequest", "lr_1")

	t.Run("by doc type", func(t *testing.T) {
		entries, err := log.Query(ctx, audit.QueryFilter{DocType: "Employee"})
		require.NoError(t, err)
		assert.Len(t, entries, 2)
	})
	t.Run("by doc id", func(t *testing.T) {
		entries, err := log.Query(ctx, audit.QueryFilter{DocID: "emp_1"})
		require.NoError(t, err)
		assert.Len(t, entries, 2)
	})
	t.Run("by action doc id", func(t *testing.T) {
		entries, err := log.Query(ctx, audit.QueryFilter{DocType: "Employee", DocID: "emp_1", UserID: "usr_1"})
		require.NoError(t, err)
		assert.Len(t, entries, 2)
	})
	t.Run("limit returns newest first", func(t *testing.T) {
		entries, err := log.Query(ctx, audit.QueryFilter{DocType: "Employee", Limit: 1})
		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.Equal(t, "update", entries[0].Action)
	})
	t.Run("since filters", func(t *testing.T) {
		entries, err := log.Query(ctx, audit.QueryFilter{DocType: "Employee", Since: time.Now().Add(time.Hour)})
		require.NoError(t, err)
		assert.Len(t, entries, 0)
	})
	t.Run("via_agent filter", func(t *testing.T) {
		viaAgent := true
		entries, err := log.Query(ctx, audit.QueryFilter{DocType: "Employee", ViaAgent: &viaAgent})
		require.NoError(t, err)
		assert.Len(t, entries, 0)
	})
}

func TestDBLog_StandaloneWrite(t *testing.T) {
	_, log := newSQLiteDB(t)
	ctx := auth.NewContext(context.Background(), auth.Identity{UserID: "usr_2"})

	entry := audit.BuildEntry(ctx, "create", "Employee", "emp_9", nil)
	require.NoError(t, log.Write(ctx, entry))

	entries, err := log.Query(ctx, audit.QueryFilter{DocType: "Employee"})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "emp_9", entries[0].DocID)
}

func TestDBLog_PersistsAcrossRestart(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "audit.db")

	// First "process": write an entry, then close the database.
	func() {
		db, err := sqlite.Open(dsn)
		require.NoError(t, err)
		defer func() { require.NoError(t, db.Close()) }()

		log := audit.NewDBLog(db.Underlying(), db.Dialect())
		require.NoError(t, log.EnsureSchema(context.Background()))
		db.RegisterDoc(audit.DocType, audit.TableName)

		ctx := auth.NewContext(context.Background(), auth.Identity{UserID: "usr_3"})
		entry := audit.BuildEntry(ctx, "create", "Employee", "emp_7", nil)
		require.NoError(t, db.Transaction(ctx, func(tx dal.Tx) error {
			return log.WriteTx(ctx, tx, entry)
		}))
	}()

	// Second "process": reopen the same file and read the entry back.
	db, err := sqlite.Open(dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	log := audit.NewDBLog(db.Underlying(), db.Dialect())
	require.NoError(t, log.EnsureSchema(context.Background()))
	db.RegisterDoc(audit.DocType, audit.TableName)

	entries, err := log.Query(context.Background(), audit.QueryFilter{DocType: "Employee"})
	require.NoError(t, err)
	require.Len(t, entries, 1, "audit records must survive a restart")
	assert.Equal(t, "emp_7", entries[0].DocID)
	assert.Equal(t, "usr_3", entries[0].UserID)
	assert.Equal(t, "create", entries[0].Action)
}

func TestDBLog_TransactionRollbackDiscardsEntry(t *testing.T) {
	db, log := newSQLiteDB(t)
	ctx := auth.NewContext(context.Background(), auth.Identity{UserID: "usr_4"})

	entry := audit.BuildEntry(ctx, "create", "Employee", "emp_4", nil)

	err := db.Transaction(ctx, func(tx dal.Tx) error {
		if werr := log.WriteTx(ctx, tx, entry); werr != nil {
			return werr
		}
		return assert.AnError
	})
	require.Error(t, err)

	entries, err := log.Query(ctx, audit.QueryFilter{DocType: "Employee"})
	require.NoError(t, err)
	assert.Len(t, entries, 0, "audit write must roll back with the transaction")
}
