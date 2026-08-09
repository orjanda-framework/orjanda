// Package orjanda provides the central Site composition root and Application registration.
package orjanda

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
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
	"github.com/orjanda-framework/orjanda/event"
	"github.com/orjanda-framework/orjanda/perm"
	"github.com/orjanda-framework/orjanda/schema"
	"github.com/orjanda-framework/orjanda/workflow"
)

// Site is the central composition root wiring database, schema registry, permission engine,
// event bus, audit log, cache, auth provider, document engine, workflow engine, agent tool
// registry, and HTTP router. See TAD §12.4.
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
	Router      *chi.Mux
	apps        []app.Definition
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

	secretKey := []byte(cfg.Server.Host)
	if len(secretKey) == 0 {
		secretKey = []byte("orjanda-secret-key-default-development")
	}
	jwtProvider := auth.NewJWTProvider(secretKey, 15*time.Minute, 7*24*time.Hour)

	site := &Site{
		Config:   cfg,
		Registry: reg,
		DB:       db,
		EventBus: eventBus,
		AuditLog: auditLog,
		Cache:    cacheStore,
		Auth:     jwtProvider,
		apps:     make([]app.Definition, 0),
	}

	return site, nil
}

// Install registers an Application definition into the site before compilation.
func (s *Site) Install(appDef app.Definition) {
	s.apps = append(s.apps, appDef)
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
	})

	return nil
}

// ServeHTTP implements http.Handler, delegating to the inner Chi router.
func (s *Site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.Router != nil {
		s.Router.ServeHTTP(w, r)
	} else {
		http.NotFound(w, r)
	}
}
