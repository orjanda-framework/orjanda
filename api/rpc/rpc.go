package rpc

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/orjanda-framework/orjanda/api/render"
	"github.com/orjanda-framework/orjanda/auth"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/perm"
)

// MethodHandler handles custom RPC method requests per PRD §14.3 / TAD §9.2.
type MethodHandler func(ctx context.Context, args map[string]any) (any, error)

type MethodOpts struct {
	AllowedRoles []string
	HTTPMethod   string // "GET" | "POST"
}

type RegisteredMethod struct {
	Name    string
	Handler MethodHandler
	Opts    MethodOpts
}

var (
	mu      sync.RWMutex
	methods = make(map[string]RegisteredMethod)
)

// RegisterMethod registers a custom RPC method.
func RegisterMethod(name string, h MethodHandler, opts MethodOpts) {
	mu.Lock()
	defer mu.Unlock()
	if opts.HTTPMethod == "" {
		opts.HTTPMethod = http.MethodPost
	}
	methods[name] = RegisteredMethod{
		Name:    name,
		Handler: h,
		Opts:    opts,
	}
}

// GetMethod retrieves a registered RPC method by name.
func GetMethod(name string) (RegisteredMethod, bool) {
	mu.RLock()
	defer mu.RUnlock()
	m, ok := methods[name]
	return m, ok
}

// ResetRegistry clears all registered RPC methods (useful for testing).
func ResetRegistry() {
	mu.Lock()
	defer mu.Unlock()
	methods = make(map[string]RegisteredMethod)
}

// DispatchHandler handles RPC method dispatch for /api/v1/method/{app}.{module}.{method} or /api/v1/method/*
func DispatchHandler(permEngine perm.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		app := chi.URLParam(r, "app")
		module := chi.URLParam(r, "module")
		method := chi.URLParam(r, "method")

		var methodPath string
		if app != "" && module != "" && method != "" {
			methodPath = app + "." + module + "." + method
		} else {
			methodPath = chi.URLParam(r, "*")
		}
		methodPath = strings.TrimPrefix(methodPath, "/")

		m, ok := GetMethod(methodPath)
		if !ok {
			render.RespondError(w, orjerrors.NotFound("custom RPC method not found: "+methodPath))
			return
		}

		if m.Opts.HTTPMethod != "" && !strings.EqualFold(r.Method, m.Opts.HTTPMethod) {
			render.RespondError(w, orjerrors.Validation("method not allowed for this RPC endpoint", map[string]any{
				"allowed_method": m.Opts.HTTPMethod,
			}))
			return
		}

		// Role permission check: AllowedRoles must be held by caller identity
		id := auth.FromContext(r.Context())
		if len(m.Opts.AllowedRoles) > 0 {
			if !hasAnyRole(id, m.Opts.AllowedRoles) {
				render.RespondError(w, orjerrors.Permission("permission denied for method "+methodPath))
				return
			}
		}

		var args map[string]any
		if r.ContentLength > 0 && r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&args); err != nil && err.Error() != "EOF" {
				render.RespondError(w, orjerrors.Validation("invalid JSON request body", nil))
				return
			}
		}
		if args == nil {
			args = make(map[string]any)
		}

		for k, v := range r.URL.Query() {
			if len(v) > 0 {
				args[k] = v[0]
			}
		}

		res, err := m.Handler(r.Context(), args)
		if err != nil {
			render.RespondError(w, err)
			return
		}

		render.RespondJSON(w, http.StatusOK, res, nil)
	}
}

func hasAnyRole(id auth.Identity, allowedRoles []string) bool {
	for _, userRole := range id.Roles {
		if strings.EqualFold(userRole, "System Administrator") {
			return true
		}
		for _, allowedRole := range allowedRoles {
			if strings.EqualFold(userRole, allowedRole) {
				return true
			}
		}
	}
	return false
}
