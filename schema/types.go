package schema

import (
	"context"
	"time"

	"github.com/orjanda-framework/orjanda/errors"
)

// ----------------------------------------------------------------------------
// Field type aliases — see PRD §10.3
// ----------------------------------------------------------------------------

// Link is the Go type for a foreign-key reference to another Document.
type Link string

// Date is a time.Time constrained to calendar-date precision.
type Date time.Time

// DateTime is a time.Time stored at full timestamp precision.
type DateTime time.Time

// Currency is a decimal value for monetary amounts.
type Currency float64

// Text is a long-text string stored as TEXT.
type Text string

// RichText is a formatted-content string stored as TEXT.
type RichText string

// DynamicLink is a polymorphic reference; value is the target document's ID.
type DynamicLink string

// Attachment is a file-reference string (path or URL).
type Attachment string

// JSON is a raw JSON payload stored as JSONB (PostgreSQL) or TEXT (SQLite).
type JSON []byte

// ----------------------------------------------------------------------------
// Sorting helpers
// ----------------------------------------------------------------------------

// SortOrder indicates ascending or descending sort. Used in Meta.SortOrder.
type SortOrder int

const (
	Ascending  SortOrder = 0
	Descending SortOrder = 1
)

// ----------------------------------------------------------------------------
// MatchType — record-level permission matching
// ----------------------------------------------------------------------------

// MatchType qualifies a DocPermission to restrict it to a subset of records.
// See PRD §16.2.
type MatchType int

const (
	// MatchAll (zero value) allows access to every record of the DocType.
	MatchAll MatchType = 0
	// OwnerMatch restricts access to records whose Owner field equals the
	// calling user's ID. Evaluated by perm.Engine at runtime (Phase 4).
	OwnerMatch MatchType = 1
)

// ----------------------------------------------------------------------------
// DocPermission — per-role permission entry on a Document
// ----------------------------------------------------------------------------

// DocPermission declares the CRUD capabilities granted to Role on this DocType.
// A list of DocPermission values is returned from DocMeta() and compiled into
// CompiledDoc.Permissions. See PRD §16.3 and TAD §2.1.
type DocPermission struct {
	// Role is the role name this permission entry applies to (e.g. "HR Manager").
	Role string
	// CRUD flags.
	Read   bool
	Write  bool
	Create bool
	Delete bool
	// Submit controls whether the role may submit a Submittable Document.
	Submit bool
	// Match optionally restricts this permission to a subset of records.
	// Defaults to MatchAll (zero value).
	Match MatchType
}

// ----------------------------------------------------------------------------
// Meta — developer-supplied Document metadata
// ----------------------------------------------------------------------------

// Meta is the structured metadata a Document developer returns from DocMeta().
// Fields here override or supplement what the oj struct-tag parser can derive
// from the struct alone. See PRD §10.1 and TAD §2.1.
type Meta struct {
	// Name is the canonical DocType name (e.g. "Employee"). Must be unique
	// within the Registry. Required.
	Name string
	// Module is the logical grouping within the Application (e.g. "HR").
	// Used by the Admin UI sidebar. Optional.
	Module string
	// Searchable marks the DocType as full-text searchable.
	// Generates a search_* agent tool (TAD §10.1 step 1).
	Searchable bool
	// Submittable marks the DocType as having a submission lifecycle.
	// Adds DocStatus field handling (PRD §10.2).
	Submittable bool
	// Icon is a UI hint for the Admin UI sidebar icon.
	Icon string
	// Description is a human-readable summary surfaced in the UI and Metadata API.
	Description string
	// AgentHidden excludes the entire DocType from agent tool generation
	// (TAD §10.1 — the schema-level agent_hidden flag, distinct from the
	// per-field hidden/agent_hidden tags of PRD §10.4 / TAD §12.2).
	AgentHidden bool
	// TitleField is the expression (field name or "First + Last" style) that
	// produces a human-readable record title in list views. See PRD §10.1.
	TitleField string
	// SortField is the default sort field for list queries.
	SortField string
	// SortOrder is Ascending (default) or Descending.
	SortOrder SortOrder
	// Permissions declares per-role CRUD grants for this DocType.
	Permissions []DocPermission
}

// ----------------------------------------------------------------------------
// FieldType — enumeration of recognised field types
// ----------------------------------------------------------------------------

// FieldType is a stable string identifier for a field's storage and
// interpretation category. Used in CompiledDoc.Fields. See PRD §10.3.
type FieldType string

const (
	FieldTypeString      FieldType = "string"
	FieldTypeInt         FieldType = "int"
	FieldTypeInt64       FieldType = "int64"
	FieldTypeFloat64     FieldType = "float64"
	FieldTypeBool        FieldType = "bool"
	FieldTypeDate        FieldType = "date"
	FieldTypeDateTime    FieldType = "datetime"
	FieldTypeCurrency    FieldType = "currency"
	FieldTypeText        FieldType = "text"
	FieldTypeRichText    FieldType = "richtext"
	FieldTypeLink        FieldType = "link"
	FieldTypeDynamicLink FieldType = "dynamiclink"
	FieldTypeAttachment  FieldType = "attachment"
	FieldTypeJSON        FieldType = "json"
	FieldTypeChildTable  FieldType = "child_table"
)

// ----------------------------------------------------------------------------
// Field — compiled representation of a single struct field
// ----------------------------------------------------------------------------

// Field is the compiled metadata for one field of a Document.
// Built by the oj tag parser and stored in CompiledDoc.Fields.
// See TAD §2.1.
type Field struct {
	// Name is the Go struct field name (e.g. "FirstName").
	Name string
	// DBColumn is the snake_case column name (e.g. "first_name").
	DBColumn string
	// Type identifies the storage and agent interpretation category.
	Type FieldType
	// Required — field must have a non-zero value on create/update.
	Required bool
	// Unique — uniqueness constraint enforced by the DB and Document Engine.
	Unique bool
	// Searchable — included in full-text search index.
	Searchable bool
	// Label is the human-readable field label for UI and agent descriptions.
	Label string
	// Options holds the enumerated allowed values from oj:"options=A|B|C".
	Options []string
	// Default holds the string-encoded default value from oj:"default=X".
	Default string
	// Format is an optional validation format string: "email", "url", "phone".
	Format string
	// LinkTarget is the DocType name that a Link field references.
	LinkTarget string
	// ChildTypeName is the Go type name of child structs for FieldTypeChildTable.
	ChildTypeName string
	// Precision is the decimal precision for Currency fields.
	Precision int
	// PermissionRole gates access to this field to identities holding the
	// given role (oj:"permission=role"). Empty string means no gate.
	PermissionRole string
	// Hidden excludes the field from the default UI and agent BaseSchema.
	Hidden bool
	// System marks a framework-managed field (id, owner, created_at, ...).
	// Hidden system fields stay sortable; hidden user-data fields do not
	// (REVIEW-2026-08-12 finding 10: order_by must not reveal hidden data).
	System bool
	// ReadOnly prevents modification after initial creation.
	ReadOnly bool
	// Computed marks a derived value that is not stored.
	Computed bool
	// AgentHint is additional plain-text context appended to the field's
	// JSON Schema description in agent tool definitions. See PRD §24.4.
	AgentHint string
	// ValidatorName holds the oj:"validator=Name" registered validator name.
	ValidatorName string
	// AgentHidden excludes the field from the agent BaseSchema entirely,
	// stronger than Hidden (which still appears in the DB/API). See TAD §12.2.
	AgentHidden bool
}

// ----------------------------------------------------------------------------
// CompiledChild — a compiled child table reference
// ----------------------------------------------------------------------------

// CompiledChild describes a child table embedded in a parent Document.
// See PRD §10.1 (EmployeeSkill example) and TAD §2.1.
type CompiledChild struct {
	// FieldName is the parent struct field name (e.g. "Skills").
	FieldName string
	// TypeName is the Go type name of the child struct (e.g. "EmployeeSkill").
	TypeName string
	// DocType is the canonical DocType name for the child table
	// (snake_case of TypeName by default, or set explicitly via DocMeta).
	DocType string
	// TableName is the pluralized snake_case table name, derived from TypeName
	// by the exact same rule as main tables (camelToSnake + "s", TAD §1.4).
	// Computed once at compile time so no consumer can derive it differently.
	TableName string
	// Fields is the compiled field set of the child struct.
	Fields []Field
}

// ----------------------------------------------------------------------------
// CompiledDoc — the Registry's compiled representation of a Document
// ----------------------------------------------------------------------------

// CompiledDoc is the immutable, compiler-output record for one Document type.
// Produced by Registry.Compile() from a Document's struct reflection +
// DocMeta() return. All downstream subsystems (DAL, perm, agent) read from
// CompiledDoc rather than reflecting on the original struct. See TAD §2.1.
type CompiledDoc struct {
	// Name is the canonical DocType name (e.g. "Employee").
	Name string
	// App is the app.Definition.Name that registered this Document.
	App string
	// Module is the logical grouping within the Application.
	Module string
	// TableName is the SQL table name (plural snake_case of Name).
	TableName string
	// Searchable — see Meta.Searchable.
	Searchable bool
	// Submittable — see Meta.Submittable.
	Submittable bool
	// Icon — see Meta.Icon.
	Icon string
	// Description — see Meta.Description.
	Description string
	// TitleField — see Meta.TitleField.
	TitleField string
	// SortField — see Meta.SortField.
	SortField string
	// SortOrder — see Meta.SortOrder.
	SortOrder SortOrder
	// Fields is the ordered list of compiled fields (base fields excluded —
	// they live in BaseDocument and are prepended by the Registry compiler).
	Fields []Field
	// Permissions is the per-role permission list from DocMeta().
	Permissions []DocPermission
	// ChildTables is the list of embedded child table types.
	ChildTables []CompiledChild
	// AgentHidden excludes the entire DocType from agent tool generation.
	AgentHidden bool
}

// ----------------------------------------------------------------------------
// Relationship — describes a link between two DocTypes
// ----------------------------------------------------------------------------

// Relationship describes a directional link from one DocType to another.
// Returned by Registry.Relationships() for the Agent Runtime and Metadata API.
// See PRD §10.5 step 3.
type Relationship struct {
	// FromDoc is the DocType that declares the link.
	FromDoc string
	// FromField is the field name on FromDoc that holds the reference.
	FromField string
	// ToDoc is the target DocType.
	ToDoc string
	// IsChildTable is true when the relationship is a child table embed,
	// false when it is a Link (foreign key).
	IsChildTable bool
}

// SchemaDiff represents the delta between the compiled Registry and the live
// database schema. Used by dal.Migrator (Phase 2). Defined here so the schema
// package owns the type and dal.Dialect can import it without a cycle.
// See TAD §2.3 and §14.
type SchemaDiff struct {
	// CreateTables is the list of new DocTypes that need a CREATE TABLE.
	CreateTables []CompiledDoc
	// AlterTables is the list of existing tables with column changes.
	AlterTables []TableAlteration
	// DropTables lists orphaned Orjanda-owned tables that exist in the live
	// database but are no longer produced by the Registry (requires
	// --allow-destructive, see TAD §14.1 step 2).
	DropTables []string
}

// ChangeCount returns the total number of pending schema changes across all
// diff categories. Used by the bench fail-fast gate and `migrate diff`'s
// "no schema changes" check (REVIEW-2026-08-12 finding 9: dropped tables must
// count).
func (d *SchemaDiff) ChangeCount() int {
	if d == nil {
		return 0
	}
	return len(d.CreateTables) + len(d.AlterTables) + len(d.DropTables)
}

// TableAlteration describes column-level changes to an existing table.
type TableAlteration struct {
	// TableName is the SQL table name being altered.
	TableName string
	// AddColumns is the list of new fields to add as columns.
	AddColumns []Field
	// DropColumns lists column names to drop (requires --allow-destructive).
	DropColumns []string
	// AlterColumns lists columns whose type or constraints have changed.
	AlterColumns []ColumnAlteration
}

// ColumnAlteration describes a change to an existing column.
type ColumnAlteration struct {
	// FieldName is the Go struct field name.
	FieldName string
	// ColumnName is the SQL column name being altered. Added for type-change
	// rendering (TAD §14's struct is silent on it; without it ALTER COLUMN
	// cannot be generated).
	ColumnName string
	// OldColumn is the previous DB column definition (dialect-specific).
	OldColumn string
	// NewColumn is the desired DB column definition.
	NewColumn string
}

// ----------------------------------------------------------------------------
// Document interface
// ----------------------------------------------------------------------------

// Document is the interface every Orjanda business entity must satisfy.
// Embed schema.BaseDocument (or schema.BaseChild for child tables) to get the
// canonical auto fields and a zero-overhead implementation of GetID/SetID.
// Implement DocMeta() to supply module-level metadata.
// See TAD §2.1 and PRD §10.1.
type Document interface {
	// DocMeta returns the static metadata for this Document type.
	DocMeta() Meta

	// GetID returns the record's primary key (ULID string).
	GetID() string
	// SetID sets the primary key. Called by the Document Engine on creation.
	SetID(string)

	// Get returns a field value by Go struct field name.
	// Used by the Document Engine for map-based I/O without reflection.
	Get(field string) any
	// Set sets a field value by Go struct field name.
	// Returns errors.CodeValidation if the field name is unknown.
	Set(field string, value any) errors.Error
}

// ----------------------------------------------------------------------------
// BaseDocument — embed in every top-level Document
// ----------------------------------------------------------------------------

// BaseDocument provides the auto fields declared in PRD §10.2 and a partial
// implementation of the Document interface. Embed this struct as the first
// field of every top-level Document struct:
//
//	type Employee struct {
//	    schema.BaseDocument
//	    // ... your fields
//	}
//
// See PRD §10.2 and TAD §2.1.
type BaseDocument struct {
	ID         string    `oj:"-"` // ULID — set by Document Engine
	Name       string    `oj:"-"` // human-readable identifier
	Owner      string    `oj:"-"` // user ID who created the record
	CreatedAt  time.Time `oj:"-"`
	UpdatedAt  time.Time `oj:"-"`
	ModifiedBy string    `oj:"-"` // user ID who last modified
	DocStatus  int       `oj:"-"` // 0=Draft, 1=Submitted, 2=Cancelled
	Deleted    bool      `oj:"-"` // soft-delete flag
}

func (b *BaseDocument) GetID() string   { return b.ID }
func (b *BaseDocument) SetID(id string) { b.ID = id }

// Get and Set on BaseDocument handle the auto-fields only. Document structs
// that embed BaseDocument must override Get/Set to expose their own fields.
// The Registry compiler generates a reminder if Get/Set are not present.
func (b *BaseDocument) Get(field string) any {
	switch field {
	case "ID":
		return b.ID
	case "Name":
		return b.Name
	case "Owner":
		return b.Owner
	case "CreatedAt":
		return b.CreatedAt
	case "UpdatedAt":
		return b.UpdatedAt
	case "ModifiedBy":
		return b.ModifiedBy
	case "DocStatus":
		return b.DocStatus
	case "Deleted":
		return b.Deleted
	}
	return nil
}

func (b *BaseDocument) Set(field string, value any) errors.Error {
	switch field {
	case "ID":
		if v, ok := value.(string); ok {
			b.ID = v
			return nil
		}
	case "Name":
		if v, ok := value.(string); ok {
			b.Name = v
			return nil
		}
	case "Owner":
		if v, ok := value.(string); ok {
			b.Owner = v
			return nil
		}
	case "CreatedAt":
		if v, ok := value.(time.Time); ok {
			b.CreatedAt = v
			return nil
		}
	case "UpdatedAt":
		if v, ok := value.(time.Time); ok {
			b.UpdatedAt = v
			return nil
		}
	case "ModifiedBy":
		if v, ok := value.(string); ok {
			b.ModifiedBy = v
			return nil
		}
	case "DocStatus":
		if v, ok := value.(int); ok {
			b.DocStatus = v
			return nil
		}
	case "Deleted":
		if v, ok := value.(bool); ok {
			b.Deleted = v
			return nil
		}
	}
	return errors.Validation("unknown field: "+field, nil)
}

// ----------------------------------------------------------------------------
// BaseChild — embed in every child-table Document
// ----------------------------------------------------------------------------

// BaseChild provides the auto fields for child table records. Embed this as
// the first field of every child-table struct:
//
//	type EmployeeSkill struct {
//	    schema.BaseChild
//	    // ... your fields
//	}
//
// See PRD §10.1 (EmployeeSkill example).
type BaseChild struct {
	ID       string `oj:"-"` // ULID — set by Document Engine
	ParentID string `oj:"-"` // ID of the parent Document record
	Idx      int    `oj:"-"` // position within the parent's child list
}

func (c *BaseChild) GetID() string   { return c.ID }
func (c *BaseChild) SetID(id string) { c.ID = id }

func (c *BaseChild) Get(field string) any {
	switch field {
	case "ID":
		return c.ID
	case "ParentID":
		return c.ParentID
	case "Idx":
		return c.Idx
	}
	return nil
}

func (c *BaseChild) Set(field string, value any) errors.Error {
	switch field {
	case "ID":
		if v, ok := value.(string); ok {
			c.ID = v
			return nil
		}
	case "ParentID":
		if v, ok := value.(string); ok {
			c.ParentID = v
			return nil
		}
	case "Idx":
		if v, ok := value.(int); ok {
			c.Idx = v
			return nil
		}
	}
	return errors.Validation("unknown child field: "+field, nil)
}

// ----------------------------------------------------------------------------
// Validator extension point
// ----------------------------------------------------------------------------

// Validator is the extension point for custom field validation logic.
// Registered via oj:"validator=Name" + schema.RegisterValidator("Name", v).
// Called by the Document Engine during the validate phase (Phase 4).
// See TAD §9.1 and PRD §20.2.
type Validator interface {
	// Validate returns nil if the value passes, or an errors.Error with
	// Code() == errors.CodeValidation on failure.
	Validate(ctx context.Context, field Field, value any) error
}

var validatorRegistry = map[string]Validator{}

// RegisterValidator registers a named Validator for use via oj:"validator=Name".
// Panics if a validator with the same name is registered twice.
// Call from package init() in the same package that defines the validator.
// See TAD §9.1.
func RegisterValidator(name string, v Validator) {
	if _, dup := validatorRegistry[name]; dup {
		panic("schema: validator already registered: " + name)
	}
	validatorRegistry[name] = v
}

// LookupValidator returns the Validator registered under name, or nil if none.
// Used by the Document Engine during validation (Phase 4).
func LookupValidator(name string) Validator {
	return validatorRegistry[name]
}

// Registry is the core read-only metadata catalog for all Documents.
// Built during startup compilation (TAD §3.1).
type Registry interface {
	Get(docType string) (*CompiledDoc, error)
	List() []*CompiledDoc
	Relationships(docType string) []Relationship
	Register(app string, doc Document) error
	Compile() error
}
