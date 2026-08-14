package document_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/orjanda-framework/orjanda/document"
	"github.com/orjanda-framework/orjanda/schema"
)

// orderDoc exercises List order_by validation: a default SortField on a
// hidden system column (created_at), plus a hidden user-data field (secret)
// that must never be sortable (REVIEW-2026-08-12 finding 10).
type orderDoc struct {
	schema.BaseDocument
	Title  string `oj:"required"`
	Secret string `oj:"hidden"`
}

func (d *orderDoc) DocMeta() schema.Meta {
	return schema.Meta{
		Name:       "OrderDoc",
		SortField:  "created_at",
		SortOrder:  schema.Descending,
		Searchable: true,
	}
}

func TestEngine_List_OrderByAllowlist(t *testing.T) {
	eng, _ := newTestEngine(t, &orderDoc{})
	ctx := context.Background()

	for _, title := range []string{"alpha", "beta", "gamma"} {
		_, err := eng.Create(ctx, "OrderDoc", map[string]any{"Title": title})
		require.NoError(t, err)
	}

	tests := []struct {
		name      string
		orderBy   string
		wantErr   bool
		wantFirst string // first returned title when sorted by Title
	}{
		{name: "field name", orderBy: "Title", wantErr: false, wantFirst: "alpha"},
		{name: "column name", orderBy: "title", wantErr: false, wantFirst: "alpha"},
		{name: "field with ASC", orderBy: "Title ASC", wantErr: false, wantFirst: "alpha"},
		{name: "field with DESC", orderBy: "Title DESC", wantErr: false, wantFirst: "gamma"},
		{name: "system hidden column", orderBy: "created_at DESC", wantErr: false},
		{name: "unknown field", orderBy: "nosuchfield", wantErr: true},
		{name: "unknown direction", orderBy: "Title SENSITIVE", wantErr: true},
		{name: "extra tokens", orderBy: "Title ASC junk", wantErr: true},
		{name: "bare direction", orderBy: "DESC", wantErr: true},
		{name: "hidden user field", orderBy: "Secret", wantErr: true},
		{name: "hidden user column", orderBy: "secret DESC", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := eng.List(ctx, "OrderDoc", document.ListOpts{OrderBy: tc.orderBy})
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.wantFirst != "" {
				require.Len(t, rows, 3)
				assert.Equal(t, tc.wantFirst, rows[0]["title"])
			}
		})
	}
}

func TestEngine_List_DefaultSortField(t *testing.T) {
	eng, _ := newTestEngine(t, &orderDoc{})
	ctx := context.Background()

	for _, title := range []string{"alpha", "beta", "gamma"} {
		_, err := eng.Create(ctx, "OrderDoc", map[string]any{"Title": title})
		require.NoError(t, err)
	}

	// No explicit OrderBy: falls back to the trusted DocMeta default
	// (SortField: created_at, Descending), which is a hidden system field and
	// therefore still allowlisted.
	rows, err := eng.List(ctx, "OrderDoc", document.ListOpts{})
	require.NoError(t, err)
	require.Len(t, rows, 3)
}
