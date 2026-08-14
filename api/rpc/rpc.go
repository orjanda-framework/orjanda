package rpc

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/orjanda-framework/orjanda/api/render"
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

// Methods returns a copy of every registered RPC method. Consumed by the
// agent ToolRegistry at compile time to emit one tool per role-gated method
// (TAD §10.1 step 8).
func Methods() []RegisteredMethod {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]RegisteredMethod, 0, len(methods))
	for _, m := range methods {
		out = append(out, m)
	}
	return out
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

		// Role check through the shared perm.Engine path (TAD §9.2): AllowedRoles
		// is enforced as a synthetic DocType "method:<name>", not a bespoke gate.
		if len(m.Opts.AllowedRoles) > 0 {
			if err := permEngine.CheckRoles(r.Context(), "method:"+m.Name, "call", m.Opts.AllowedRoles); err != nil {
				render.RespondError(w, err)
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
