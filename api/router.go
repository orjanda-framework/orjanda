package api

import (
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	apimiddleware "github.com/orjanda-framework/orjanda/api/middleware"
	"github.com/orjanda-framework/orjanda/api/rest"
	"github.com/orjanda-framework/orjanda/api/rpc"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/cache"
	"github.com/orjanda-framework/orjanda/document"
	"github.com/orjanda-framework/orjanda/perm"
	"github.com/orjanda-framework/orjanda/schema"
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
		// Metadata API
		if opts.Registry != nil {
			metaHandler := NewMetaHandler(opts.Registry, opts.PermEngine)
			r.Get("/meta", metaHandler.ListDocTypes)
			r.Get("/meta/{doctype}", metaHandler.GetDocMeta)
			r.Get("/meta/{doctype}/links", metaHandler.GetLinks)
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
