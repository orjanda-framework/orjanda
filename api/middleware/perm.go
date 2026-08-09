package middleware

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/orjanda-framework/orjanda/api/render"
	"github.com/orjanda-framework/orjanda/perm"
)

// Permission checks document-level CRUD permissions for REST endpoints using perm.Engine.
func Permission(permEngine perm.Engine) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			docType := chi.URLParam(r, "doctype")
			if docType == "" {
				next.ServeHTTP(w, r)
				return
			}

			action := mapHTTPMethodToAction(r.Method)
			if permEngine != nil {
				if err := permEngine.CheckAction(r.Context(), docType, action); err != nil {
					render.RespondError(w, err)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

func mapHTTPMethodToAction(method string) string {
	switch strings.ToUpper(method) {
	case http.MethodGet:
		return "read"
	case http.MethodPost:
		return "create"
	case http.MethodPatch, http.MethodPut:
		return "write"
	case http.MethodDelete:
		return "delete"
	default:
		return "read"
	}
}
