package search

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/orjanda-framework/orjanda/dal"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
)

// Backend is the search interface. The MVP default delegates to the active
// Dialect's FullTextSearch (no external search process required).
// See TAD §9.1 and PRD §13.3.
type Backend interface {
	// Index records the given field values for a document in the search index.
	// The default Dialect-based backend is a no-op here (FTS is query-time).
	Index(ctx context.Context, docType, id string, fields map[string]any) error
	// Remove removes a document from the search index.
	// No-op for the Dialect-based backend.
	Remove(ctx context.Context, docType, id string) error
	// Search executes a full-text search and returns matching document IDs.
	Search(ctx context.Context, docType, query string, limit int) ([]string, error)
}

// ----------------------------------------------------------------------------
// Default Dialect-backed Backend
// ----------------------------------------------------------------------------

// dialectBackend satisfies Backend using the active Dialect's FullTextSearch.
type dialectBackend struct {
	// dialect generates the FTS SQL.
	dialect dal.Dialect
	// db executes the query.
	db *sql.DB
	// tableNames maps docType → tableName.
	tableNames map[string]string
	// searchFields maps docType → list of searchable column names.
	searchFields map[string][]string
}

// NewDialectBackend creates a Backend that delegates FTS to the active Dialect.
// tableNames and searchFields are populated from CompiledDocs at startup.
func NewDialectBackend(d dal.Dialect, db *sql.DB, tableNames, searchFields map[string][]string) Backend {
	tn := make(map[string]string, len(tableNames))
	for k, v := range tableNames {
		if len(v) > 0 {
			tn[k] = v[0] // tableNames values are slices; take first element
		}
	}
	return &dialectBackend{
		dialect:      d,
		db:           db,
		tableNames:   tn,
		searchFields: searchFields,
	}
}

// NewDialectBackendSimple creates a Backend from simple docType→tableName and
// docType→searchFields maps.
func NewDialectBackendSimple(d dal.Dialect, db *sql.DB, tableNames map[string]string, searchFields map[string][]string) Backend {
	return &dialectBackend{
		dialect:      d,
		db:           db,
		tableNames:   tableNames,
		searchFields: searchFields,
	}
}

// Index is a no-op for the Dialect backend (FTS is computed at query time).
func (b *dialectBackend) Index(_ context.Context, _, _ string, _ map[string]any) error {
	return nil
}

// Remove is a no-op for the Dialect backend.
func (b *dialectBackend) Remove(_ context.Context, _, _ string) error {
	return nil
}

// Search executes a FTS query via the Dialect and returns matching IDs.
func (b *dialectBackend) Search(ctx context.Context, docType, query string, limit int) ([]string, error) {
	tn, ok := b.tableNames[docType]
	if !ok {
		return nil, orjerrors.NotFound(fmt.Sprintf("no table mapping for docType %q", docType))
	}
	fields := b.searchFields[docType]
	if len(fields) == 0 {
		return nil, nil
	}

	sqlStr, args := b.dialect.FullTextSearch(tn, query, fields)
	if limit > 0 {
		sqlStr += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := b.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, orjerrors.Internal("full-text search failed", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, orjerrors.Internal("scan search result", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
