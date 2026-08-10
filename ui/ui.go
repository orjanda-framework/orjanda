// Package ui provides the admin UI page registry (TAD §9.1) and the codegen
// input contract consumed by the @orjanda/codegen pass (TAD §6.3).
//
// Page registrations surface custom frontend routes in the Admin UI sidebar
// (PRD §18.3); the codegen types mirror the GET /api/v1/meta shape so a single
// field-type-to-external-representation table feeds both the agent tool JSON
// Schemas (TAD §10.2) and the generated TypeScript client.
package ui

import (
	"github.com/orjanda-framework/orjanda/schema"
)

// Page describes a custom Admin UI page registered by an Application.
// See TAD §9.1 and PRD §18.3. The JSON tags fix the GET /api/v1/meta/pages
// wire shape consumed by the Admin UI sidebar.
type Page struct {
	// Path is the client-side route (e.g. "/app/hr/org-chart").
	Path string `json:"path"`
	// Title is the page title shown in the sidebar and document head.
	Title string `json:"title"`
	// Component is the JS module path resolved by the frontend bundle loader
	// (e.g. "hr/OrgChart").
	Component string `json:"component"`
	// Icon is an optional icon identifier for the sidebar entry.
	Icon string `json:"icon,omitempty"`
	// Menu groups the page under a sidebar heading.
	Menu string `json:"menu,omitempty"`
}

// Registry collects ui.Page registrations before routes mount.
// See TAD §9.1.
type Registry interface {
	RegisterPage(p Page)
	Pages() []Page
}

// NewRegistry builds an empty page Registry. Custom pages are registered via
// RegisterPage; the default per-Document list/form pages are derived from the
// Registry metadata at render time (PRD §17.3) and are not stored here.
func NewRegistry() Registry {
	return &registry{pages: make([]Page, 0)}
}

type registry struct {
	pages []Page
}

// RegisterPage appends a custom page. Later registrations win for equal Paths.
func (r *registry) RegisterPage(p Page) {
	for i := range r.pages {
		if r.pages[i].Path == p.Path {
			r.pages[i] = p
			return
		}
	}
	r.pages = append(r.pages, p)
}

// Pages returns the registered pages in registration order.
func (r *registry) Pages() []Page {
	out := make([]Page, len(r.pages))
	copy(out, r.pages)
	return out
}

// ---------------------------------------------------------------------------
// Codegen input contract (TAD §6.3): the full CompiledDoc[] payload identical
// in shape to GET /api/v1/meta/{doctype}, extended with the db_column mapping
// the REST layer returns records under and the child-table type names the TS
// generator needs.
// ---------------------------------------------------------------------------

// DocMetaJSON is the per-Document codegen input record.
type DocMetaJSON struct {
	Name        string           `json:"name"`
	App         string           `json:"app,omitempty"`
	Module      string           `json:"module,omitempty"`
	TitleField  string           `json:"title_field"`
	Searchable  bool             `json:"searchable"`
	Submittable bool             `json:"submittable"`
	Icon        string           `json:"icon,omitempty"`
	Description string           `json:"description,omitempty"`
	Fields      []FieldJSON      `json:"fields"`
	ChildTables []ChildTableJSON `json:"child_tables,omitempty"`
	Permissions PermissionsJSON  `json:"permissions"`
}

// FieldJSON is the compiled metadata for one field. Column mirrors the
// REST record key; Name mirrors the REST write key.
type FieldJSON struct {
	Name       string   `json:"name"`
	Column     string   `json:"db_column"`
	Type       string   `json:"type"`
	Label      string   `json:"label"`
	Required   bool     `json:"required"`
	Options    []string `json:"options,omitempty"`
	Link       string   `json:"link,omitempty"`
	Hidden     bool     `json:"hidden"`
	Permission string   `json:"permission,omitempty"`
	ReadOnly   bool     `json:"read_only,omitempty"`
}

// ChildTableJSON describes a compiled child-table embed for the codegen pass.
// DocType is the canonical child DocType the generated interface references.
type ChildTableJSON struct {
	FieldName string      `json:"field_name"`
	DocType   string      `json:"doc_type"`
	TypeName  string      `json:"type_name"`
	Fields    []FieldJSON `json:"fields"`
}

// PermissionsJSON is the identity-independent capability summary. It answers
// "does ANY role grant this verb?" so the generated client only exposes
// methods that a non-empty role set could invoke; the server still enforces
// per-request checks (PRD §25.1).
type PermissionsJSON struct {
	CanRead   bool `json:"can_read"`
	CanWrite  bool `json:"can_write"`
	CanCreate bool `json:"can_create"`
	CanDelete bool `json:"can_delete"`
	CanSubmit bool `json:"can_submit"`
}

// FieldTypeName is the stable external type identifier emitted on the wire.
// It is the string form of schema.FieldType (PRD §10.3's Field Types table).
type FieldTypeName = string

// BaseColumns are the auto-field columns present on every row (TAD §1.4).
var BaseColumns = []string{"id", "name", "owner", "created_at", "updated_at", "modified_by", "doc_status", "deleted"}

// CodegenInput builds the TAD §6.3 step-1 payload for a compiled Registry.
// Documents are ordered by Name for stable hashing and deterministic output.
func CodegenInput(reg schema.Registry) ([]DocMetaJSON, error) {
	docs := reg.List()
	out := make([]DocMetaJSON, 0, len(docs))
	for _, d := range docs {
		fields := make([]FieldJSON, 0, len(d.Fields))
		for _, f := range d.Fields {
			fields = append(fields, fieldToJSON(f))
		}
		children := make([]ChildTableJSON, 0, len(d.ChildTables))
		for _, c := range d.ChildTables {
			cf := make([]FieldJSON, 0, len(c.Fields))
			for _, f := range c.Fields {
				cf = append(cf, fieldToJSON(f))
			}
			children = append(children, ChildTableJSON{
				FieldName: c.FieldName,
				DocType:   c.DocType,
				TypeName:  c.TypeName,
				Fields:    cf,
			})
		}
		out = append(out, DocMetaJSON{
			Name:        d.Name,
			App:         d.App,
			Module:      d.Module,
			TitleField:  d.TitleField,
			Searchable:  d.Searchable,
			Submittable: d.Submittable,
			Icon:        d.Icon,
			Description: d.Description,
			Fields:      fields,
			ChildTables: children,
			Permissions: PermissionsJSON{
				CanRead:   anyGrant(d, "read"),
				CanWrite:  anyGrant(d, "write"),
				CanCreate: anyGrant(d, "create"),
				CanDelete: anyGrant(d, "delete"),
				CanSubmit: anyGrant(d, "submit"),
			},
		})
	}
	return out, nil
}

func fieldToJSON(f schema.Field) FieldJSON {
	return FieldJSON{
		Name:       f.Name,
		Column:     f.DBColumn,
		Type:       string(f.Type),
		Label:      f.Label,
		Required:   f.Required,
		Options:    f.Options,
		Link:       f.LinkTarget,
		Hidden:     f.Hidden,
		Permission: f.PermissionRole,
		ReadOnly:   f.ReadOnly,
	}
}

// anyGrant reports whether any declared DocPermission grants verb. A DocType
// with no explicit permission entries is treated as permissive.
func anyGrant(d *schema.CompiledDoc, verb string) bool {
	if len(d.Permissions) == 0 {
		return true
	}
	for _, p := range d.Permissions {
		switch verb {
		case "read":
			if p.Read {
				return true
			}
		case "write":
			if p.Write {
				return true
			}
		case "create":
			if p.Create {
				return true
			}
		case "delete":
			if p.Delete {
				return true
			}
		case "submit":
			if p.Submit {
				return true
			}
		}
	}
	return false
}
