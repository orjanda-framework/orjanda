package rest

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/orjanda-framework/orjanda/api/render"
	"github.com/orjanda-framework/orjanda/document"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
)

// Handler serves REST operations for Documents per PRD §14.2.
type Handler struct {
	engine *document.Engine
}

// NewHandler constructs a new REST Handler.
func NewHandler(engine *document.Engine) *Handler {
	return &Handler{engine: engine}
}

// List handles GET /api/v1/document/{doctype}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	docType := chi.URLParam(r, "doctype")
	q := r.URL.Query()

	opts := document.ListOpts{}

	if limitStr := q.Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l >= 0 {
			opts.Limit = l
		}
	}
	if offsetStr := q.Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			opts.Offset = o
		}
	}
	opts.OrderBy = q.Get("order_by")

	filters := make(map[string]any)
	if filtersStr := q.Get("filters"); filtersStr != "" {
		if strings.HasPrefix(filtersStr, "{") {
			_ = json.Unmarshal([]byte(filtersStr), &filters)
		}
	}
	for k, v := range q {
		switch k {
		case "fields", "filters", "order_by", "limit", "offset", "q":
			continue
		default:
			if len(v) > 0 {
				filters[k] = v[0]
			}
		}
	}
	opts.Filters = filters

	if searchQ := q.Get("q"); searchQ != "" {
		opts.Filters["q"] = searchQ
	}

	records, err := h.engine.List(r.Context(), docType, opts)
	if err != nil {
		render.RespondError(w, err)
		return
	}

	meta := &render.MetaDetails{
		TotalCount: len(records),
		Limit:      opts.Limit,
		Offset:     opts.Offset,
	}

	render.RespondJSON(w, http.StatusOK, records, meta)
}

// Read handles GET /api/v1/document/{doctype}/{id}
func (h *Handler) Read(w http.ResponseWriter, r *http.Request) {
	docType := chi.URLParam(r, "doctype")
	id := chi.URLParam(r, "id")

	record, err := h.engine.Read(r.Context(), docType, id)
	if err != nil {
		render.RespondError(w, err)
		return
	}

	render.RespondJSON(w, http.StatusOK, record, nil)
}

// Create handles POST /api/v1/document/{doctype}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	docType := chi.URLParam(r, "doctype")

	var data map[string]any
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		render.RespondError(w, orjerrors.Validation("invalid JSON request body", nil))
		return
	}

	id, err := h.engine.Create(r.Context(), docType, data)
	if err != nil {
		render.RespondError(w, err)
		return
	}

	record, err := h.engine.Read(r.Context(), docType, id)
	if err != nil {
		render.RespondJSON(w, http.StatusCreated, map[string]any{"id": id}, nil)
		return
	}

	render.RespondJSON(w, http.StatusCreated, record, nil)
}

// Update handles PATCH /api/v1/document/{doctype}/{id}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	docType := chi.URLParam(r, "doctype")
	id := chi.URLParam(r, "id")

	var data map[string]any
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		render.RespondError(w, orjerrors.Validation("invalid JSON request body", nil))
		return
	}

	if err := h.engine.Update(r.Context(), docType, id, data); err != nil {
		render.RespondError(w, err)
		return
	}

	record, err := h.engine.Read(r.Context(), docType, id)
	if err != nil {
		render.RespondJSON(w, http.StatusOK, map[string]any{"id": id}, nil)
		return
	}

	render.RespondJSON(w, http.StatusOK, record, nil)
}

// Delete handles DELETE /api/v1/document/{doctype}/{id}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	docType := chi.URLParam(r, "doctype")
	id := chi.URLParam(r, "id")

	if err := h.engine.Delete(r.Context(), docType, id); err != nil {
		render.RespondError(w, err)
		return
	}

	render.RespondJSON(w, http.StatusOK, map[string]any{"id": id, "success": true}, nil)
}
