//go:build !integration

package testing

import (
	"testing"

	"github.com/orjanda-framework/orjanda/dal"
	"github.com/orjanda-framework/orjanda/schema"
)

// openPostgresTestDB is replaced by the build-tagged testcontainers-backed
// implementation in postgres_integration.go (built with -tags integration).
// Without that tag the harness fails fast rather than silently falling back
// to SQLite when WithDialect("postgres") was requested — a dialect-mismatched
// test must never pass against the wrong database (TAD §17.1 guarantee 1).
func openPostgresTestDB(t *testing.T, docs []*schema.CompiledDoc) dal.Database {
	t.Helper()
	t.Fatalf("testing: WithDialect(\"postgres\") requires the \"integration\" build tag " +
		"(go test -tags integration) — it runs a testcontainers-go PostgreSQL instance")
	return nil
}
