//go:build integration

package testing

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/moby/moby/api/types/network"
	"github.com/orjanda-framework/orjanda/dal"
	"github.com/orjanda-framework/orjanda/dal/dialect/postgres"
	"github.com/orjanda-framework/orjanda/schema"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// openPostgresTestDB provisions a throwaway PostgreSQL instance via
// testcontainers-go, creates every table for docs, and registers cleanup with
// the test. This file only compiles under -tags integration; the harness
// fails fast otherwise (PRD §32.1 Integration test row, TAD §17.1 guarantee 1).
func openPostgresTestDB(t *testing.T, docs []*schema.CompiledDoc) dal.Database {
	t.Helper()
	ctx := context.Background()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_USER":     "orjanda",
				"POSTGRES_PASSWORD": "orjanda",
				"POSTGRES_DB":       "orjanda_test",
			},
			WaitingFor: wait.ForSQL("5432/tcp", "pgx",
				func(host string, port network.Port) string {
					return fmt.Sprintf("postgres://orjanda:orjanda@%s:%s/orjanda_test?sslmode=disable", host, port.Port())
				}).WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	require.NoError(t, err, "start postgres testcontainer")
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)

	dsn := fmt.Sprintf("postgres://orjanda:orjanda@%s:%s/orjanda_test?sslmode=disable", host, port.Port())
	db, err := postgres.Open(dsn)
	require.NoError(t, err, "connect to postgres testcontainer")
	t.Cleanup(func() { _ = db.Close() })

	db.RegisterDocs(docs)
	require.NoError(t, db.CreateTables(docs))
	return db
}
