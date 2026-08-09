package document

import (
	"context"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/orjanda-framework/orjanda/dal"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/schema"
)

// DocStatus values per PRD §10.2.
const (
	DocStatusDraft     = 0
	DocStatusSubmitted = 1
	DocStatusCancelled = 2
)

// Engine is the Document Engine that drives Create/Read/Update/Delete/List
// through the DAL with schema validation. Phase 3 delivers the bare CRUD layer;
// Phase 4 adds hooks, permissions, workflow, and audit. See TAD §3.2.
type Engine struct {
	db  dal.Database
	reg schema.Registry
}

// New constructs a Document Engine backed by the given Database and Registry.
// The Registry must already be compiled (Registry.Compile() must have been called).
func New(db dal.Database, reg schema.Registry) *Engine {
	return &Engine{db: db, reg: reg}
}

// ----------------------------------------------------------------------------
// Create
// ----------------------------------------------------------------------------

// Create validates data against the CompiledDoc for docType and inserts a new
// record inside a transaction. Returns the new record's ID.
//
// Auto fields applied by Create:
//   - ID: generated ULID if not supplied.
//   - CreatedAt / UpdatedAt: set to now.
//   - Deleted: false.
//   - DocStatus: DocStatusDraft (0) for Submittable documents if not supplied.
//
// See TAD §3.2 steps 5–7 (validation + DAL insert, no hooks/perm in Phase 3).
func (e *Engine) Create(ctx context.Context, docType string, data map[string]any) (string, error) {
	compiled, err := e.reg.Get(docType)
	if err != nil {
		return "", err
	}

	// Validate all fields.
	if err := e.validateFields(ctx, compiled, data, false); err != nil {
		return "", err
	}

	// Normalize Go field names → DB column names.
	norm := normalizeToColumns(compiled, data)

	// Check uniqueness before inserting.
	if err := e.checkUnique(ctx, compiled, "", norm); err != nil {
		return "", err
	}

	var id string

	txErr := e.db.Transaction(ctx, func(tx dal.Tx) error {
		row := buildInsertRow(compiled, norm)

		var insertErr error
		id, insertErr = tx.Insert(ctx, docType, row)
		return insertErr
	})
	if txErr != nil {
		return "", txErr
	}
	return id, nil
}

// ----------------------------------------------------------------------------
// Read
// ----------------------------------------------------------------------------

// Read fetches a single record by its ID. Returns errors.CodeNotFound if the
// record does not exist or has been soft-deleted (unless the caller explicitly
// includes deleted records via the DAL layer, which this method does not expose).
//
// See TAD §3.2 (read path, Phase 3 scope).
func (e *Engine) Read(ctx context.Context, docType, id string) (map[string]any, error) {
	compiled, err := e.reg.Get(docType)
	if err != nil {
		return nil, err
	}

	rows, err := e.db.Query(ctx, dal.Select{
		DocType:   docType,
		TableName: compiled.TableName,
		Filters:   map[string]any{"id": id},
	})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, orjerrors.NotFound(fmt.Sprintf("record %q not found in %q", id, docType))
	}
	return rows[0], nil
}

// ----------------------------------------------------------------------------
// Update
// ----------------------------------------------------------------------------

// Update validates data and applies partial updates to the record identified by
// id. Only fields present in data are updated (patch semantics). UpdatedAt is
// always refreshed.
//
// See TAD §3.2 (update path, Phase 3 scope).
func (e *Engine) Update(ctx context.Context, docType, id string, data map[string]any) error {
	compiled, err := e.reg.Get(docType)
	if err != nil {
		return err
	}

	// Verify record exists.
	if _, err := e.Read(ctx, docType, id); err != nil {
		return err
	}

	// Validate only the supplied fields (update is patch — only present fields
	// must satisfy validation rules). required is checked only for non-nil values
	// to allow partial updates.
	if err := e.validateFieldsForUpdate(ctx, compiled, data); err != nil {
		return err
	}

	// Normalize Go field names → DB column names.
	norm := normalizeToColumns(compiled, data)

	// Check uniqueness for updated fields.
	if err := e.checkUnique(ctx, compiled, id, norm); err != nil {
		return err
	}

	row := buildUpdateRow(norm)

	return e.db.Transaction(ctx, func(tx dal.Tx) error {
		return tx.Update(ctx, docType, id, row)
	})
}

// ----------------------------------------------------------------------------
// Delete (soft-delete)
// ----------------------------------------------------------------------------

// Delete soft-deletes the record by setting Deleted=true and UpdatedAt=now.
// The record is excluded from List and Read by default after deletion.
//
// See PRD §10.2 (Deleted flag) and TAD §3.2 (Phase 3 scope).
func (e *Engine) Delete(ctx context.Context, docType, id string) error {
	_, err := e.reg.Get(docType)
	if err != nil {
		return err
	}

	// Verify record exists (and is not already deleted).
	if _, err := e.Read(ctx, docType, id); err != nil {
		return err
	}

	now := time.Now()
	return e.db.Transaction(ctx, func(tx dal.Tx) error {
		return tx.Update(ctx, docType, id, map[string]any{
			"deleted":    true,
			"updated_at": now,
		})
	})
}

// ----------------------------------------------------------------------------
// List
// ----------------------------------------------------------------------------

// ListOpts controls the List query.
type ListOpts struct {
	// Filters are equality predicates applied to the query (column or field name).
	Filters map[string]any
	// OrderBy is the column/field to sort by (e.g. "created_at DESC").
	OrderBy string
	// Limit is the max number of rows to return (0 = no limit).
	Limit int
	// Offset is the number of rows to skip.
	Offset int
	// IncludeDeleted, when true, includes soft-deleted records.
	IncludeDeleted bool
}

// List returns records for docType. Soft-deleted records are excluded by
// default; set ListOpts.IncludeDeleted to true to include them.
//
// See TAD §3.2 (list path, Phase 3 scope).
func (e *Engine) List(ctx context.Context, docType string, opts ListOpts) ([]map[string]any, error) {
	compiled, err := e.reg.Get(docType)
	if err != nil {
		return nil, err
	}

	orderBy := opts.OrderBy
	if orderBy == "" && compiled.SortField != "" {
		orderBy = compiled.SortField
		if compiled.SortOrder == schema.Descending {
			orderBy += " DESC"
		}
	}

	return e.db.Query(ctx, dal.Select{
		DocType:        docType,
		TableName:      compiled.TableName,
		Filters:        opts.Filters,
		OrderBy:        orderBy,
		Limit:          opts.Limit,
		Offset:         opts.Offset,
		IncludeDeleted: opts.IncludeDeleted,
	})
}

// ----------------------------------------------------------------------------
// Validation helpers
// ----------------------------------------------------------------------------

// validateFields validates all fields in data for a Create operation.
// required fields are always checked; format, options, and custom validators
// are applied to any non-nil value.
func (e *Engine) validateFields(ctx context.Context, compiled *schema.CompiledDoc, data map[string]any, isUpdate bool) error {
	details := map[string]any{}

	for i := range compiled.Fields {
		f := &compiled.Fields[i]

		// Skip auto-managed system fields.
		if isSystemField(f.DBColumn) {
			continue
		}
		// Skip child-table fields — they are managed separately.
		if f.Type == schema.FieldTypeChildTable {
			continue
		}

		// Resolve value (accept both Go field name and DB column name).
		val, present := fieldValue(data, f)

		// required check (only on Create).
		if !isUpdate && f.Required {
			if !present || isZero(val) {
				details[f.Name] = "field is required"
			}
		}

		if !present || isZero(val) {
			continue
		}

		// format check.
		if f.Format != "" {
			if err := validateFormat(f.Format, val); err != nil {
				details[f.Name] = err.Error()
			}
		}

		// options check.
		if len(f.Options) > 0 {
			if err := validateOptions(f.Options, val); err != nil {
				details[f.Name] = err.Error()
			}
		}

		// Custom validator.
		if f.ValidatorName != "" {
			v := schema.LookupValidator(f.ValidatorName)
			if v != nil {
				if err := v.Validate(ctx, *f, val); err != nil {
					details[f.Name] = err.Error()
				}
			}
		}
	}

	if len(details) > 0 {
		return orjerrors.Validation("validation failed", details)
	}
	return nil
}

// validateFieldsForUpdate validates only the fields present in data, skipping
// the required check (patch semantics — only supplied values are validated).
func (e *Engine) validateFieldsForUpdate(ctx context.Context, compiled *schema.CompiledDoc, data map[string]any) error {
	return e.validateFields(ctx, compiled, data, true)
}

// checkUnique queries the database to enforce uniqueness for fields marked
// oj:"unique". currentID is empty on Create (any existing row is a conflict);
// on Update it is the record being modified (so matching its own row is OK).
func (e *Engine) checkUnique(ctx context.Context, compiled *schema.CompiledDoc, currentID string, data map[string]any) error {
	details := map[string]any{}

	for i := range compiled.Fields {
		f := &compiled.Fields[i]
		if !f.Unique {
			continue
		}

		val, present := fieldValue(data, f)
		if !present || isZero(val) {
			continue
		}

		// Query for any row with this value.
		rows, err := e.db.Query(ctx, dal.Select{
			DocType:   compiled.Name,
			TableName: compiled.TableName,
			Filters:   map[string]any{f.DBColumn: val},
		})
		if err != nil {
			return orjerrors.Internal("uniqueness check failed", err)
		}

		for _, row := range rows {
			existingID, _ := row["id"].(string)
			if existingID != currentID {
				details[f.Name] = "value must be unique"
				break
			}
		}
	}

	if len(details) > 0 {
		return orjerrors.Validation("uniqueness violation", details)
	}
	return nil
}

// ----------------------------------------------------------------------------
// Row builders
// ----------------------------------------------------------------------------

// buildInsertRow constructs the complete data map for an INSERT, applying
// auto fields (ID, timestamps, Deleted, DocStatus).
func buildInsertRow(compiled *schema.CompiledDoc, data map[string]any) map[string]any {
	now := time.Now()

	row := make(map[string]any, len(data)+8)
	for k, v := range data {
		row[k] = v
	}

	// Assign a new ULID if not already present.
	if id, ok := row["id"].(string); !ok || id == "" {
		row["id"] = ulid.Make().String()
	}

	// Timestamps.
	if _, ok := row["created_at"]; !ok {
		row["created_at"] = now
	}
	row["updated_at"] = now

	// Soft-delete defaults to false.
	if _, ok := row["deleted"]; !ok {
		row["deleted"] = false
	}

	// DocStatus: draft for Submittable documents.
	if compiled.Submittable {
		if _, ok := row["doc_status"]; !ok {
			row["doc_status"] = DocStatusDraft
		}
	}

	return row
}

// buildUpdateRow constructs the partial data map for an UPDATE, always
// refreshing updated_at.
func buildUpdateRow(data map[string]any) map[string]any {
	row := make(map[string]any, len(data)+1)
	for k, v := range data {
		row[k] = v
	}
	row["updated_at"] = time.Now()
	return row
}

// ----------------------------------------------------------------------------
// Value helpers
// ----------------------------------------------------------------------------

// fieldValue looks up a field's value in data by its Go name or DB column name.
func fieldValue(data map[string]any, f *schema.Field) (any, bool) {
	if v, ok := data[f.Name]; ok {
		return v, true
	}
	if v, ok := data[f.DBColumn]; ok {
		return v, true
	}
	return nil, false
}

// normalizeToColumns converts a data map that may contain Go field names into
// one that uses only DB column names. Values keyed by column name are kept;
// values keyed by Go field name are re-keyed. Unknown keys are passed through
// unchanged so that callers supplying raw column names still work.
func normalizeToColumns(compiled *schema.CompiledDoc, data map[string]any) map[string]any {
	out := make(map[string]any, len(data))
	// Build a lookup: GoName → DBColumn.
	goToCol := make(map[string]string, len(compiled.Fields))
	for i := range compiled.Fields {
		f := &compiled.Fields[i]
		goToCol[f.Name] = f.DBColumn
	}
	for k, v := range data {
		if col, ok := goToCol[k]; ok {
			out[col] = v
		} else {
			out[k] = v
		}
	}
	return out
}

// isZero reports whether v is the zero value for its type (nil, "", 0, false, etc.).
func isZero(v any) bool {
	if v == nil {
		return true
	}
	switch val := v.(type) {
	case string:
		return val == ""
	case int:
		return val == 0
	case int64:
		return val == 0
	case float64:
		return val == 0
	case bool:
		return !val
	case time.Time:
		return val.IsZero()
	}
	return false
}

// isSystemField returns true for auto-managed columns that the Document Engine
// controls and that callers should not supply in validation paths.
func isSystemField(col string) bool {
	switch col {
	case "id", "created_at", "updated_at", "deleted", "doc_status",
		"owner", "name", "modified_by":
		return true
	}
	return false
}

// ----------------------------------------------------------------------------
// Format validators
// ----------------------------------------------------------------------------

var (
	emailRegex = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	urlRegex   = regexp.MustCompile(`^https?://`)
	phoneRegex = regexp.MustCompile(`^\+?[\d\s\-().]{7,}$`)
)

func validateFormat(format string, val any) error {
	s, ok := val.(string)
	if !ok {
		return nil // non-string values skip format validation
	}
	switch format {
	case "email":
		if _, err := mail.ParseAddress(s); err != nil {
			if !emailRegex.MatchString(s) {
				return fmt.Errorf("invalid email format")
			}
		}
	case "url":
		if !urlRegex.MatchString(s) {
			return fmt.Errorf("invalid URL format")
		}
	case "phone":
		if !phoneRegex.MatchString(s) {
			return fmt.Errorf("invalid phone format")
		}
	}
	return nil
}

func validateOptions(options []string, val any) error {
	s := fmt.Sprintf("%v", val)
	for _, opt := range options {
		if strings.EqualFold(opt, s) {
			return nil
		}
	}
	return fmt.Errorf("value %q is not one of the allowed options: %s", s, strings.Join(options, ", "))
}
