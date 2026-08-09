package document

import (
	"context"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/orjanda-framework/orjanda/audit"
	"github.com/orjanda-framework/orjanda/dal"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/event"
	"github.com/orjanda-framework/orjanda/perm"
	"github.com/orjanda-framework/orjanda/schema"
)

// DocStatus values per PRD §10.2.
const (
	DocStatusDraft     = 0
	DocStatusSubmitted = 1
	DocStatusCancelled = 2
)

// Engine is the Document Engine that drives Create/Read/Update/Delete/List
// through the DAL with schema validation, permission checks, lifecycle hooks,
// and audit logging. See TAD §3.2.
type Engine struct {
	db       dal.Database
	reg      schema.Registry
	perm     perm.Engine
	bus      event.Bus
	auditLog audit.Log
}

// New constructs a Document Engine backed by the given Database and Registry.
// The Registry must already be compiled (Registry.Compile() must have been called).
func New(db dal.Database, reg schema.Registry) *Engine {
	return &Engine{db: db, reg: reg}
}

// NewWithServices constructs a Document Engine with permission engine, event bus, and audit log attached.
func NewWithServices(db dal.Database, reg schema.Registry, permEngine perm.Engine, bus event.Bus, auditLog audit.Log) *Engine {
	return &Engine{
		db:       db,
		reg:      reg,
		perm:     permEngine,
		bus:      bus,
		auditLog: auditLog,
	}
}

// SetPermEngine sets the permission engine.
func (e *Engine) SetPermEngine(p perm.Engine) { e.perm = p }

// SetEventBus sets the event bus.
func (e *Engine) SetEventBus(b event.Bus) { e.bus = b }

// SetAuditLog sets the audit log.
func (e *Engine) SetAuditLog(a audit.Log) { e.auditLog = a }

// ----------------------------------------------------------------------------
// Create
// ----------------------------------------------------------------------------

// Create validates data against the CompiledDoc for docType, enforces permissions,
// triggers lifecycle hooks, inserts a new record, and writes an audit entry inside
// a transaction. Returns the new record's ID.
//
// See TAD §3.2 (full request lifecycle).
func (e *Engine) Create(ctx context.Context, docType string, data map[string]any) (string, error) {
	compiled, err := e.reg.Get(docType)
	if err != nil {
		return "", err
	}

	// 1. Permission check (RBAC + ABAC rule check) BEFORE any DAL call is made.
	if e.perm != nil {
		if err := e.perm.CheckAction(ctx, docType, "create"); err != nil {
			return "", err
		}

		// Field-level write permission filter.
		filtered, err := e.perm.FilterWrite(ctx, docType, data)
		if err != nil {
			return "", err
		}
		data = filtered
	}

	// 2. Lifecycle event: before_validate
	if e.bus != nil {
		if err := e.bus.Emit(ctx, docType, event.EventBeforeValidate, data); err != nil {
			return "", err
		}
	}

	// 3. Field validation & uniqueness check.
	if err := e.validateFields(ctx, compiled, data, false); err != nil {
		return "", err
	}

	norm := normalizeToColumns(compiled, data)

	if err := e.checkUnique(ctx, compiled, "", norm); err != nil {
		return "", err
	}

	// 4. Lifecycle event: after_validate
	if e.bus != nil {
		if err := e.bus.Emit(ctx, docType, event.EventAfterValidate, norm); err != nil {
			return "", err
		}
	}

	// 5. Lifecycle events: before_insert & before_save
	if e.bus != nil {
		if err := e.bus.Emit(ctx, docType, event.EventBeforeInsert, norm); err != nil {
			return "", err
		}
		if err := e.bus.Emit(ctx, docType, event.EventBeforeSave, norm); err != nil {
			return "", err
		}
	}

	var id string

	// 6. DB write, after hooks, and audit log write inside a single dal.Tx.
	txErr := e.db.Transaction(ctx, func(tx dal.Tx) error {
		row := buildInsertRow(compiled, norm)

		var insertErr error
		id, insertErr = tx.Insert(ctx, docType, row)
		if insertErr != nil {
			return insertErr
		}

		if e.bus != nil {
			if err := e.bus.Emit(ctx, docType, event.EventAfterInsert, row); err != nil {
				return err
			}
			if err := e.bus.Emit(ctx, docType, event.EventAfterSave, row); err != nil {
				return err
			}
		}

		if e.auditLog != nil {
			diff := audit.DiffMaps(nil, row)
			entry := audit.BuildEntry(ctx, "create", docType, id, diff)
			if err := e.auditLog.Write(ctx, entry); err != nil {
				return err
			}
		}

		return nil
	})

	if txErr != nil {
		return "", txErr
	}
	return id, nil
}

// ----------------------------------------------------------------------------
// Read
// ----------------------------------------------------------------------------

// Read fetches a single record by its ID and filters fields based on caller role.
func (e *Engine) Read(ctx context.Context, docType, id string) (map[string]any, error) {
	compiled, err := e.reg.Get(docType)
	if err != nil {
		return nil, err
	}

	if e.perm != nil {
		if err := e.perm.CheckAction(ctx, docType, "read"); err != nil {
			return nil, err
		}
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

	row := rows[0]
	if e.perm != nil {
		return e.perm.FilterRead(ctx, docType, row)
	}
	return row, nil
}

// ----------------------------------------------------------------------------
// Update
// ----------------------------------------------------------------------------

// Update validates data and applies partial updates with permission enforcement,
// hooks, and audit logging inside a transaction.
func (e *Engine) Update(ctx context.Context, docType, id string, data map[string]any) error {
	compiled, err := e.reg.Get(docType)
	if err != nil {
		return err
	}

	if e.perm != nil {
		if err := e.perm.CheckAction(ctx, docType, "write"); err != nil {
			return err
		}

		filtered, err := e.perm.FilterWrite(ctx, docType, data)
		if err != nil {
			return err
		}
		data = filtered
	}

	// Fetch existing record (unfiltered for audit diffing).
	oldRows, err := e.db.Query(ctx, dal.Select{
		DocType:   docType,
		TableName: compiled.TableName,
		Filters:   map[string]any{"id": id},
	})
	if err != nil {
		return err
	}
	if len(oldRows) == 0 {
		return orjerrors.NotFound(fmt.Sprintf("record %q not found in %q", id, docType))
	}
	oldRow := oldRows[0]

	if e.bus != nil {
		if err := e.bus.Emit(ctx, docType, event.EventBeforeValidate, data); err != nil {
			return err
		}
	}

	if err := e.validateFieldsForUpdate(ctx, compiled, data); err != nil {
		return err
	}

	norm := normalizeToColumns(compiled, data)

	if err := e.checkUnique(ctx, compiled, id, norm); err != nil {
		return err
	}

	if e.bus != nil {
		if err := e.bus.Emit(ctx, docType, event.EventAfterValidate, norm); err != nil {
			return err
		}
		if err := e.bus.Emit(ctx, docType, event.EventBeforeUpdate, norm); err != nil {
			return err
		}
		if err := e.bus.Emit(ctx, docType, event.EventBeforeSave, norm); err != nil {
			return err
		}
	}

	return e.db.Transaction(ctx, func(tx dal.Tx) error {
		row := buildUpdateRow(norm)
		if err := tx.Update(ctx, docType, id, row); err != nil {
			return err
		}

		// Build merged row representation for after hooks and audit log
		mergedRow := make(map[string]any, len(oldRow)+len(row))
		for k, v := range oldRow {
			mergedRow[k] = v
		}
		for k, v := range row {
			mergedRow[k] = v
		}

		if e.bus != nil {
			if err := e.bus.Emit(ctx, docType, event.EventAfterUpdate, mergedRow); err != nil {
				return err
			}
			if err := e.bus.Emit(ctx, docType, event.EventAfterSave, mergedRow); err != nil {
				return err
			}
		}

		if e.auditLog != nil {
			diff := audit.DiffMaps(oldRow, mergedRow)
			entry := audit.BuildEntry(ctx, "update", docType, id, diff)
			if err := e.auditLog.Write(ctx, entry); err != nil {
				return err
			}
		}

		return nil
	})
}

// ----------------------------------------------------------------------------
// Delete (soft-delete)
// ----------------------------------------------------------------------------

// Delete soft-deletes the record by setting Deleted=true inside a transaction.
func (e *Engine) Delete(ctx context.Context, docType, id string) error {
	compiled, err := e.reg.Get(docType)
	if err != nil {
		return err
	}

	if e.perm != nil {
		if err := e.perm.CheckAction(ctx, docType, "delete"); err != nil {
			return err
		}
	}

	oldRows, err := e.db.Query(ctx, dal.Select{
		DocType:   docType,
		TableName: compiled.TableName,
		Filters:   map[string]any{"id": id},
	})
	if err != nil {
		return err
	}
	if len(oldRows) == 0 {
		return orjerrors.NotFound(fmt.Sprintf("record %q not found in %q", id, docType))
	}
	oldRow := oldRows[0]

	if e.bus != nil {
		if err := e.bus.Emit(ctx, docType, event.EventBeforeDelete, oldRow); err != nil {
			return err
		}
	}

	now := time.Now()
	updateData := map[string]any{
		"deleted":    true,
		"updated_at": now,
	}

	return e.db.Transaction(ctx, func(tx dal.Tx) error {
		if err := tx.Update(ctx, docType, id, updateData); err != nil {
			return err
		}

		if e.bus != nil {
			if err := e.bus.Emit(ctx, docType, event.EventAfterDelete, oldRow); err != nil {
				return err
			}
		}

		if e.auditLog != nil {
			diff := audit.DiffMaps(oldRow, map[string]any{"deleted": true})
			entry := audit.BuildEntry(ctx, "delete", docType, id, diff)
			if err := e.auditLog.Write(ctx, entry); err != nil {
				return err
			}
		}

		return nil
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

// List returns records for docType, filtered by caller permissions.
func (e *Engine) List(ctx context.Context, docType string, opts ListOpts) ([]map[string]any, error) {
	compiled, err := e.reg.Get(docType)
	if err != nil {
		return nil, err
	}

	if e.perm != nil {
		if err := e.perm.CheckAction(ctx, docType, "read"); err != nil {
			return nil, err
		}
	}

	orderBy := opts.OrderBy
	if orderBy == "" && compiled.SortField != "" {
		orderBy = compiled.SortField
		if compiled.SortOrder == schema.Descending {
			orderBy += " DESC"
		}
	}

	rows, err := e.db.Query(ctx, dal.Select{
		DocType:        docType,
		TableName:      compiled.TableName,
		Filters:        opts.Filters,
		OrderBy:        orderBy,
		Limit:          opts.Limit,
		Offset:         opts.Offset,
		IncludeDeleted: opts.IncludeDeleted,
	})
	if err != nil {
		return nil, err
	}

	if e.perm != nil {
		filteredRows := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			fRow, err := e.perm.FilterRead(ctx, docType, row)
			if err != nil {
				return nil, err
			}
			filteredRows = append(filteredRows, fRow)
		}
		return filteredRows, nil
	}

	return rows, nil
}

// ----------------------------------------------------------------------------
// Validation helpers
// ----------------------------------------------------------------------------

func (e *Engine) validateFields(ctx context.Context, compiled *schema.CompiledDoc, data map[string]any, isUpdate bool) error {
	details := map[string]any{}

	for i := range compiled.Fields {
		f := &compiled.Fields[i]

		if isSystemField(f.DBColumn) {
			continue
		}
		if f.Type == schema.FieldTypeChildTable {
			continue
		}

		val, present := fieldValue(data, f)

		if !isUpdate && f.Required {
			if !present || isZero(val) {
				details[f.Name] = "field is required"
			}
		}

		if !present || isZero(val) {
			continue
		}

		if f.Format != "" {
			if err := validateFormat(f.Format, val); err != nil {
				details[f.Name] = err.Error()
			}
		}

		if len(f.Options) > 0 {
			if err := validateOptions(f.Options, val); err != nil {
				details[f.Name] = err.Error()
			}
		}

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

func (e *Engine) validateFieldsForUpdate(ctx context.Context, compiled *schema.CompiledDoc, data map[string]any) error {
	return e.validateFields(ctx, compiled, data, true)
}

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

func buildInsertRow(compiled *schema.CompiledDoc, data map[string]any) map[string]any {
	now := time.Now()

	row := make(map[string]any, len(data)+8)
	for k, v := range data {
		row[k] = v
	}

	if id, ok := row["id"].(string); !ok || id == "" {
		row["id"] = ulid.Make().String()
	}

	if _, ok := row["created_at"]; !ok {
		row["created_at"] = now
	}
	row["updated_at"] = now

	if _, ok := row["deleted"]; !ok {
		row["deleted"] = false
	}

	if compiled.Submittable {
		if _, ok := row["doc_status"]; !ok {
			row["doc_status"] = DocStatusDraft
		}
	}

	return row
}

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

func fieldValue(data map[string]any, f *schema.Field) (any, bool) {
	if v, ok := data[f.Name]; ok {
		return v, true
	}
	if v, ok := data[f.DBColumn]; ok {
		return v, true
	}
	return nil, false
}

func normalizeToColumns(compiled *schema.CompiledDoc, data map[string]any) map[string]any {
	out := make(map[string]any, len(data))
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
		return nil
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
