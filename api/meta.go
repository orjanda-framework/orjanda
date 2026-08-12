package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/orjanda-framework/orjanda/api/render"
	"github.com/orjanda-framework/orjanda/perm"
	"github.com/orjanda-framework/orjanda/schema"
)

type MetaHandler struct {
	reg  schema.Registry
	perm perm.Engine
}

func NewMetaHandler(reg schema.Registry, permEngine perm.Engine) *MetaHandler {
	return &MetaHandler{reg: reg, perm: permEngine}
}

type PermissionsMeta struct {
	CanRead   bool `json:"can_read"`
	CanWrite  bool `json:"can_write"`
	CanCreate bool `json:"can_create"`
	CanDelete bool `json:"can_delete"`
}

type DocMetaResponse struct {
	Name        string          `json:"name"`
	TitleField  string          `json:"title_field"`
	Searchable  bool            `json:"searchable"`
	Submittable bool            `json:"submittable"`
	Icon        string          `json:"icon,omitempty"`
	Description string          `json:"description,omitempty"`
	Fields      []FieldMeta     `json:"fields"`
	Permissions PermissionsMeta `json:"permissions"`
}

type FieldMeta struct {
	Name       string   `json:"name"`
	Column     string   `json:"db_column"`
	Type       string   `json:"type"`
	Label      string   `json:"label"`
	Required   bool     `json:"required"`
	Options    []string `json:"options,omitempty"`
	LinkTarget string   `json:"link,omitempty"`
	Hidden     bool     `json:"hidden"`
	Permission string   `json:"permission,omitempty"`
	ReadOnly   bool     `json:"read_only,omitempty"`
}

// ListDocTypes handles GET /api/v1/meta
func (h *MetaHandler) ListDocTypes(w http.ResponseWriter, r *http.Request) {
	docs := h.reg.List()
	summaries := make([]map[string]any, 0, len(docs))
	for _, d := range docs {
		summaries = append(summaries, map[string]any{
			"name":        d.Name,
			"module":      d.Module,
			"title_field": d.TitleField,
			"searchable":  d.Searchable,
			"submittable": d.Submittable,
			"icon":        d.Icon,
			"description": d.Description,
		})
	}
	render.RespondJSON(w, http.StatusOK, summaries, nil)
}

// GetDocMeta handles GET /api/v1/meta/{doctype}
func (h *MetaHandler) GetDocMeta(w http.ResponseWriter, r *http.Request) {
	docType := chi.URLParam(r, "doctype")
	compiled, err := h.reg.Get(docType)
	if err != nil {
		render.RespondError(w, err)
		return
	}

	canRead := true
	canWrite := true
	canCreate := true
	canDelete := true

	if h.perm != nil {
		canRead = h.perm.CheckAction(r.Context(), docType, "read") == nil
		canWrite = h.perm.CheckAction(r.Context(), docType, "write") == nil
		canCreate = h.perm.CheckAction(r.Context(), docType, "create") == nil
		canDelete = h.perm.CheckAction(r.Context(), docType, "delete") == nil
	}

	// Fields the caller may access once field-level permission (oj:"permission=role")
	// is applied per identity (TAD §2.7, §6.1 note: metadata is pre-calculated
	// for the requesting user so the UI can hide gated fields immediately).
	allowed := map[string]bool{}
	if h.perm != nil {
		if names, err := h.perm.AllowedFields(r.Context(), docType, "write"); err == nil {
			for _, n := range names {
				allowed[n] = true
			}
		}
	}

	fieldsMeta := make([]FieldMeta, 0, len(compiled.Fields))
	for i := range compiled.Fields {
		f := &compiled.Fields[i]
		if f.Hidden {
			continue
		}
		if len(allowed) > 0 && f.PermissionRole != "" && !allowed[f.Name] {
			continue
		}
		fieldsMeta = append(fieldsMeta, FieldMeta{
			Name:       f.Name,
			Column:     f.DBColumn,
			Type:       string(f.Type),
			Label:      f.Label,
			Required:   f.Required,
			Options:    f.Options,
			LinkTarget: f.LinkTarget,
			Hidden:     f.Hidden,
			Permission: f.PermissionRole,
			ReadOnly:   f.ReadOnly,
		})
	}

	resp := DocMetaResponse{
		Name:        compiled.Name,
		TitleField:  compiled.TitleField,
		Searchable:  compiled.Searchable,
		Submittable: compiled.Submittable,
		Icon:        compiled.Icon,
		Description: compiled.Description,
		Fields:      fieldsMeta,
		Permissions: PermissionsMeta{
			CanRead:   canRead,
			CanWrite:  canWrite,
			CanCreate: canCreate,
			CanDelete: canDelete,
		},
	}

	render.RespondJSON(w, http.StatusOK, resp, nil)
}

// GetLinks handles GET /api/v1/meta/{doctype}/links
func (h *MetaHandler) GetLinks(w http.ResponseWriter, r *http.Request) {
	docType := chi.URLParam(r, "doctype")
	rel := h.reg.Relationships(docType)
	render.RespondJSON(w, http.StatusOK, rel, nil)
}
