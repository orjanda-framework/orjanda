//go:build integration

package testing

import (
	"testing"

	"github.com/orjanda-framework/orjanda/perm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPostgresLeaveRequestCreation runs the PRD §32.2 acceptance flow against
// a real PostgreSQL instance instead of in-memory SQLite, proving the harness
// honors WithDialect across backends (TAD §17.1 guarantee 1, PRD §32.1
// Integration tests row). It is gated behind the "integration" build tag so
// the default unit-test lane never depends on Docker.
func TestPostgresLeaveRequestCreation(t *testing.T) {
	site := NewTestSite(t, WithDialect("postgres"), WithDocuments("hr-test", &testLeaveRequest{}))

	// Create test user with specific roles
	user := site.CreateUser(t, "jane@test.com", "HR Manager")
	ctx := site.WithUser(user)

	// Test document creation
	id, err := site.Document.Create(ctx, "LeaveRequest", map[string]any{
		"Employee":      "EMP-001",
		"LeaveType":     "Annual",
		"FromDate":      "2026-08-15",
		"ToDate":        "2026-08-16",
		"WorkflowState": "Draft",
	})
	require.NoError(t, err)

	doc, err := site.Document.Read(ctx, "LeaveRequest", id)
	require.NoError(t, err)
	assert.Equal(t, "Draft", doc["workflow_state"])
	assert.Equal(t, id, doc["id"])

	// Test permission enforcement
	intern := site.CreateUser(t, "bob@test.com", "Intern")
	err = site.Document.Delete(site.WithUser(intern), "LeaveRequest", id)
	assert.ErrorIs(t, err, perm.ErrPermissionDenied)
}
