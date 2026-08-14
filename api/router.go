package api

import (
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/orjanda-framework/orjanda/agent/runtime"
	apimiddleware "github.com/orjanda-framework/orjanda/api/middleware"
	"github.com/orjanda-framework/orjanda/api/rest"
	"github.com/orjanda-framework/orjanda/api/rpc"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/cache"
	"github.com/orjanda-framework/orjanda/dal"
	"github.com/orjanda-framework/orjanda/document"
	"github.com/orjanda-framework/orjanda/perm"
	"github.com/orjanda-framework/orjanda/schema"
	"github.com/orjanda-framework/orjanda/ui"
)

// RouterOptions holds dependencies required for mounting API routes.
type RouterOptions struct {
	CORSOrigins  []string
	AuthProvider auth.Provider
	RateLimit    int
	RateWindow   time.Duration
	Cache        cache.Store
	PermEngine   perm.Engine
	Registry     schema.Registry
	DocEngine    *document.Engine
	// Database is required to mount the built-in login/refresh routes
	// (PRD §15.1); when nil the routes are skipped.
	Database dal.Database
	// Pages carries ui.Page registrations surfaced by GET /api/v1/meta/pages
	// for the Admin UI sidebar (PRD §18.3). Nil skips the route.
	Pages ui.Registry
	// AgentRuntime carries the shared runtime options for the agent chat
	// WebSocket; when nil the /api/v1/agent/stream route is not mounted.
	// Sink and Approvals are per-connection and need not be set here.
	AgentRuntime *runtime.Options
}

// NewRouter constructs a Chi HTTP router with the middleware chain in PRD §12.2 order:
// CORS → Auth → Rate Limit → Permission → Handler
func NewRouter(opts RouterOptions) *chi.Mux {
	r := chi.NewRouter()

	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.Recoverer)

	// PRD §12.2 Middleware order: CORS -> Auth -> RateLimit -> Permission -> Handler
	r.Use(apimiddleware.CORS(opts.CORSOrigins))
	r.Use(apimiddleware.Auth(opts.AuthProvider))
	r.Use(apimiddleware.RateLimit(opts.RateLimit, opts.RateWindow, opts.Cache))

	restHandler := rest.NewHandler(opts.DocEngine)

	r.Route("/api/v1", func(r chi.Router) {
		// Built-in email/password login (PRD §15.1). Mounted outside the
		// permission middleware: the caller has no identity yet.
		if opts.Database != nil {
			authHandler := NewAuthHandler(opts.Database, opts.Registry, opts.AuthProvider)
			if authHandler.enabled {
				r.Post("/auth/login", authHandler.Login)
				r.Post("/auth/refresh", authHandler.Refresh)
			}
		}

		// Metadata API
		if opts.Registry != nil {
			metaHandler := NewMetaHandler(opts.Registry, opts.PermEngine)
			r.Get("/meta", metaHandler.ListDocTypes)
			r.Get("/meta/{doctype}", metaHandler.GetDocMeta)
			r.Get("/meta/{doctype}/links", metaHandler.GetLinks)
		}
		if opts.Pages != nil {
			r.Get("/meta/pages", NewPagesHandler(opts.Pages).List)
		}

		// Agent Chat WebSocket (TAD §6.2, §12.3)
		if opts.AgentRuntime != nil {
			r.Get("/agent/stream", (&AgentHandler{
				Base:           *opts.AgentRuntime,
				AllowedOrigins: opts.CORSOrigins,
			}).Stream)
		}

		// RPC Custom Methods
		r.HandleFunc("/method/{app}.{module}.{method}", rpc.DispatchHandler(opts.PermEngine))
		r.HandleFunc("/method/*", rpc.DispatchHandler(opts.PermEngine))

		// REST Document Endpoints (permission enforced)
		r.Group(func(r chi.Router) {
			r.Use(apimiddleware.Permission(opts.PermEngine))

			r.Get("/document/{doctype}", restHandler.List)
			r.Post("/document/{doctype}", restHandler.Create)
			r.Get("/document/{doctype}/{id}", restHandler.Read)
			r.Patch("/document/{doctype}/{id}", restHandler.Update)
			r.Put("/document/{doctype}/{id}", restHandler.Update)
			r.Delete("/document/{doctype}/{id}", restHandler.Delete)
		})
	})

	return r
}
