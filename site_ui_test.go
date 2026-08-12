package orjanda_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/orjanda-framework/orjanda"
	"github.com/orjanda-framework/orjanda/app"
	"github.com/orjanda-framework/orjanda/config"
	"github.com/orjanda-framework/orjanda/dal/dialect/sqlite"
	core "github.com/orjanda-framework/orjanda/orjanda-core"
	"github.com/orjanda-framework/orjanda/ui"
)

// newAdminSite builds a compiled Site with the core application, a bootstrapped
// admin account, and an LLM provider configured (so the agent WebSocket is
// mounted), mirroring a Phase 9 production composition.
func newAdminSite(t *testing.T) (*orjanda.Site, string) {
	t.Helper()
	cfg := config.Config{
		Database: config.DatabaseConfig{Driver: "sqlite", DSN: ":memory:"},
		Server:   config.ServerConfig{Host: "127.0.0.1", Port: 8080, CORSOrigins: []string{"*"}},
		Auth:     config.AuthConfig{JWTSecret: "site-ui-jwt-secret-0123456789-0123456789"},
		LLM: config.LLMConfig{
			DefaultProvider: "openai",
			Providers: map[string]config.LLMProviderConfig{
				"openai": {APIKey: "test-key", Model: "gpt-4o", MaxTokens: 100},
			},
		},
	}

	site, err := orjanda.NewSite(cfg)
	if err != nil {
		t.Fatalf("NewSite: %v", err)
	}
	site.Install(app.Definition{Name: "core", Modules: []app.Module{{Name: "Core"}}})
	if err := site.Registry.Register("core", &core.User{}); err != nil {
		t.Fatalf("register User: %v", err)
	}
	if err := site.Registry.Register("core", &core.Role{}); err != nil {
		t.Fatalf("register Role: %v", err)
	}
	if err := site.Registry.Register("core", &core.RolePermission{}); err != nil {
		t.Fatalf("register RolePermission: %v", err)
	}

	// site.Compile compiles the Registry exactly once (the Registry is
	// immutable post-compile, TAD §3.1), wires the engines, and mounts routes.
	if err := site.Compile(); err != nil {
		t.Fatalf("site Compile: %v", err)
	}

	// Create tables and seed the admin account after compilation.
	db := site.DB.(*sqlite.DB)
	db.RegisterDocs(site.Registry.List())
	for _, doc := range site.Registry.List() {
		for _, child := range doc.ChildTables {
			db.RegisterDoc(child.TypeName, child.DocType+"s")
		}
	}
	if err := db.CreateTables(site.Registry.List()); err != nil {
		t.Fatalf("CreateTables: %v", err)
	}

	password, err := core.Bootstrap(context.Background(), db, site.Registry)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	site.RegisterPage(ui.Page{Path: "/app/hr/org-chart", Title: "Org Chart", Component: "hr/OrgChart", Icon: "sitemap", Menu: "HR"})

	return site, password
}

func TestSite_AdminLoginAndAgentRoutes(t *testing.T) {
	site, password := newAdminSite(t)

	// Login through the composed site handler.
	body, _ := json.Marshal(map[string]any{"email": core.AdminEmail, "password": password})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	site.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 login via site, got %d: %s", w.Code, w.Body.String())
	}

	// With an LLM provider configured the agent WebSocket route is mounted.
	wsReq := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/agent/stream", http.NoBody)
	wsReq.Header.Set("Connection", "Upgrade")
	wsReq.Header.Set("Upgrade", "websocket")
	wsW := httptest.NewRecorder()
	site.ServeHTTP(wsW, wsReq)
	if wsW.Code == http.StatusNotFound {
		t.Fatalf("expected agent WebSocket route mounted, got 404")
	}

	// Custom page surfaces via the pages endpoint.
	pgReq := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/meta/pages", http.NoBody)
	pgW := httptest.NewRecorder()
	site.ServeHTTP(pgW, pgReq)
	if pgW.Code != http.StatusOK {
		t.Fatalf("expected 200 pages, got %d: %s", pgW.Code, pgW.Body.String())
	}
	if !bytes.Contains(pgW.Body.Bytes(), []byte(`"/app/hr/org-chart"`)) {
		t.Errorf("expected registered page in /api/v1/meta/pages: %s", pgW.Body.String())
	}
}

func TestSite_ServesEmbeddedUI(t *testing.T) {
	site, _ := newAdminSite(t)

	// Root serves the embedded index.html placeholder.
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", http.NoBody)
	w := httptest.NewRecorder()
	site.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for /, got %d", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`id="root"`)) {
		t.Errorf("expected embedded index.html body, got %s", w.Body.String())
	}

	// SPA fallback serves index.html for a client-side route.
	req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/agent", http.NoBody)
	w = httptest.NewRecorder()
	site.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(`id="root"`)) {
		t.Errorf("expected SPA fallback for /agent, got %d", w.Code)
	}

	// Production bundle assets resolve from the embedded dist.
	assetReq := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/assets/index.css", http.NoBody)
	assetW := httptest.NewRecorder()
	site.ServeHTTP(assetW, assetReq)
	if assetW.Code == http.StatusNotFound {
		t.Error("expected embedded asset to resolve (or 200 index); got 404")
	}

	// API paths still route to the API router, not the SPA.
	req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/meta", http.NoBody)
	w = httptest.NewRecorder()
	site.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 meta through composed handler, got %d", w.Code)
	}
}
