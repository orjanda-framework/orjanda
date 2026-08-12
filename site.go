// Package orjanda provides the central Site composition root and Application registration.
package orjanda

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/orjanda-framework/orjanda/agent/llm"
	"github.com/orjanda-framework/orjanda/agent/runtime"
	"github.com/orjanda-framework/orjanda/agent/safety"
	"github.com/orjanda-framework/orjanda/agent/tools"
	"github.com/orjanda-framework/orjanda/api"
	"github.com/orjanda-framework/orjanda/app"
	"github.com/orjanda-framework/orjanda/audit"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/cache"
	"github.com/orjanda-framework/orjanda/config"
	"github.com/orjanda-framework/orjanda/dal"
	"github.com/orjanda-framework/orjanda/dal/dialect/postgres"
	"github.com/orjanda-framework/orjanda/dal/dialect/sqlite"
	"github.com/orjanda-framework/orjanda/document"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/event"
	"github.com/orjanda-framework/orjanda/perm"
	"github.com/orjanda-framework/orjanda/schema"
	"github.com/orjanda-framework/orjanda/ui"
	"github.com/orjanda-framework/orjanda/workflow"
)

// Site is the central composition root wiring database, schema registry, permission engine,
// event bus, audit log, cache, auth provider, document engine, workflow engine, agent tool
// registry, admin UI page registry, and HTTP router. See TAD §12.4.
type Site struct {
	Config      config.Config
	Registry    schema.Registry
	DB          dal.Database
	Permissions perm.Engine
	EventBus    event.Bus
	AuditLog    audit.Log
	Cache       cache.Store
	Auth        auth.Provider
	DocEngine   *document.Engine
	Workflows   workflow.Engine
	Tools       tools.ToolRegistry
	Pages       ui.Registry
	Router      *chi.Mux
	apps        []app.Definition
	ui          http.Handler
}

// NewSite initializes a new Site with the provided configuration.
func NewSite(cfg config.Config) (*Site, error) {
	var db dal.Database
	if cfg.Database.DSN != "" {
		switch cfg.Database.Driver {
		case "postgres":
			pgDB, err := postgres.Open(cfg.Database.DSN)
			if err != nil {
				return nil, err
			}
			db = pgDB
		case "sqlite":
			sqDB, err := sqlite.Open(cfg.Database.DSN)
			if err != nil {
				return nil, err
			}
			db = sqDB
		}
	}

	eventBus := event.NewBus()
	auditLog := audit.NewInMemoryLog()
	cacheStore := cache.NewLRUStore(1000)
	reg := schema.NewRegistry()

	// The JWT signing key comes exclusively from auth.jwt_secret. There is no
	// fallback: a derived (host-based) or hardcoded default key would let
	// anyone forge administrator tokens (REVIEW-2026-08-12 finding 1).
	if err := config.ValidateJWTSecret(cfg.Auth.JWTSecret); err != nil {
		return nil, orjerrors.Validation("invalid auth.jwt_secret: "+err.Error(), nil)
	}
	jwtProvider := auth.NewJWTProvider([]byte(cfg.Auth.JWTSecret), 15*time.Minute, 7*24*time.Hour)

	site := &Site{
		Config:   cfg,
		Registry: reg,
		DB:       db,
		EventBus: eventBus,
		AuditLog: auditLog,
		Cache:    cacheStore,
		Auth:     jwtProvider,
		Pages:    ui.NewRegistry(),
		apps:     make([]app.Definition, 0),
	}

	return site, nil
}

// Install registers an Application definition into the site before compilation.
func (s *Site) Install(appDef app.Definition) {
	s.apps = append(s.apps, appDef)
}

// InstalledApps returns the Application definitions registered via Install.
// Used by the CLI's install/uninstall commands (TAD §16).
func (s *Site) InstalledApps() []app.Definition {
	out := make([]app.Definition, len(s.apps))
	copy(out, s.apps)
	return out
}

// Compile compiles the schema registry, wires engines, and mounts HTTP routes.
func (s *Site) Compile() error {
	if err := s.Registry.Compile(); err != nil {
		return err
	}

	s.Permissions = perm.NewEngine(s.Registry)
	if s.DB != nil {
		s.Permissions.SetDatabase(s.DB)
	}

	s.DocEngine = document.NewWithServices(
		s.DB,
		s.Registry,
		s.Permissions,
		s.EventBus,
		s.AuditLog,
	)

	// Initialization steps 8–9 (TAD §5.2): workflow Engine (layer 6 services)
	// then Agent Runtime init, which runs ToolRegistry.Compile immediately
	// after Registry.Compile (TAD §3.1 step 5, §5.3).
	s.Workflows = workflow.NewEngine(s.DB, s.Registry, s.Permissions, s.EventBus, s.AuditLog)
	s.Tools = tools.NewToolRegistry(s.Permissions, s.Workflows)
	if err := s.Tools.Compile(s.Registry); err != nil {
		return err
	}

	s.Router = api.NewRouter(api.RouterOptions{
		CORSOrigins:  s.Config.Server.CORSOrigins,
		AuthProvider: s.Auth,
		RateLimit:    1000,
		RateWindow:   time.Minute,
		Cache:        s.Cache,
		PermEngine:   s.Permissions,
		Registry:     s.Registry,
		DocEngine:    s.DocEngine,
		Database:     s.DB,
		Pages:        s.Pages,
		AgentRuntime: s.agentRuntime(),
	})

	s.ui = newSPAHandler(s.Router)
	return nil
}

// agentRuntime builds the Phase 8 runtime options shared across agent chat
// connections. The Agent Chat UI (TAD §6.2) requires this to be non-nil; when
// no LLM provider is resolvable the WebSocket route is simply not mounted and
// the chat panel reports the endpoint as unavailable.
func (s *Site) agentRuntime() *runtime.Options {
	if s.Config.LLM.Providers == nil {
		return nil
	}
	provider, err := llm.ProviderFromConfig(&s.Config, "")
	if err != nil {
		slog.Warn("site.agent.disabled", "error", err)
		return nil
	}

	policy := safety.SafetyPolicy{
		AutoApprove:       []string{"read", "search", "list"},
		MaxBulkOperations: s.Config.LLM.Safety.MaxBulkOperations,
		RateLimit:         safety.RateLimit{OperationsPerMinute: 60, Scope: "user"},
	}

	return &runtime.Options{
		Provider:   provider,
		Tools:      s.Tools,
		PermEngine: s.Permissions,
		Registry:   s.Registry,
		DocEngine:  s.DocEngine,
		Workflow:   s.Workflows,
		Safety:     safety.NewLayer(policy, s.Cache),
	}
}

// RegisterPage adds a custom Admin UI page (TAD §9.1 / PRD §18.3).
func (s *Site) RegisterPage(p ui.Page) {
	s.Pages.RegisterPage(p)
}

// HTTPHandler returns the composed request handler: API routes plus the
// embedded Admin UI single-page application (PRD §17.4). It is the handler the
// server package serves; ServeHTTP delegates to it.
func (s *Site) HTTPHandler() http.Handler {
	if s.ui != nil {
		return s.ui
	}
	if s.Router != nil {
		return s.Router
	}
	return http.NotFoundHandler()
}

// ServeHTTP implements http.Handler, delegating to the composed handler.
func (s *Site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.HTTPHandler().ServeHTTP(w, r)
}
