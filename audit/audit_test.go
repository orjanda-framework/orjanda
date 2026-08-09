package audit_test

import (
	"context"
	"testing"

	"github.com/orjanda-framework/orjanda/audit"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAudit_DiffMaps(t *testing.T) {
	oldRow := map[string]any{
		"id":     "123",
		"status": "Draft",
		"title":  "Old Title",
	}

	newRow := map[string]any{
		"id":     "123",
		"status": "Submitted",
		"title":  "Old Title",
	}

	changes := audit.DiffMaps(oldRow, newRow)
	require.Len(t, changes, 1, "Only modified fields should be included")
	assert.Equal(t, "status", changes[0].Field)
	assert.Equal(t, "Draft", changes[0].OldValue)
	assert.Equal(t, "Submitted", changes[0].NewValue)
}

func TestAudit_WriteAndQuery(t *testing.T) {
	log := audit.NewInMemoryLog()
	ctx := auth.NewContext(context.Background(), auth.Identity{UserID: "usr_100"})
	ctx = audit.WithAgent(ctx, "session_1", "Approve leave request")

	entry := audit.BuildEntry(ctx, "update", "LeaveRequest", "lr_001", []audit.FieldChange{
		{Field: "status", OldValue: "Draft", NewValue: "Submitted"},
	})

	err := log.Write(ctx, entry)
	require.NoError(t, err)

	// Query by DocType and DocID
	entries, err := log.Query(ctx, audit.QueryFilter{
		DocType: "LeaveRequest",
		DocID:   "lr_001",
	})
	require.NoError(t, err)
	require.Len(t, entries, 1)

	e := entries[0]
	assert.Equal(t, "usr_100", e.UserID)
	assert.Equal(t, "update", e.Action)
	assert.True(t, e.ViaAgent)
	assert.Equal(t, "session_1", e.AgentSession)
	assert.Equal(t, "Approve leave request", e.AgentPrompt)
	assert.Len(t, e.Changes, 1)
	assert.False(t, e.Timestamp.IsZero())
}
