package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/orjanda-framework/orjanda/api"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/cache"
	"github.com/orjanda-framework/orjanda/dal/dialect/sqlite"
	"github.com/orjanda-framework/orjanda/document"
	"github.com/orjanda-framework/orjanda/event"
	core "github.com/orjanda-framework/orjanda/orjanda-core"
	"github.com/orjanda-framework/orjanda/perm"
	"github.com/orjanda-framework/orjanda/schema"
	"github.com/orjanda-framework/orjanda/ui"
)

// setupAuthTestSite mirrors the Phase 5 test harness: core documents, child
// table name mappings, table creation, and a bootstrapped admin account.
func setupAuthTestSite(t *testing.T) (http.Handler, string) {
	t.Helper()

	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	reg := schema.NewRegistry()
	for _, d := range []schema.Document{&core.User{}, &core.Role{}, &core.RolePermission{}} {
		if err := reg.Register("core", d); err != nil {
			t.Fatalf("register core doc: %v", err)
		}
	}
	if err := reg.Compile(); err != nil {
		t.Fatalf("compile: %v", err)
	}

	db.RegisterDocs(reg.List())
	for _, doc := range reg.List() {
		for _, child := range doc.ChildTables {
			db.RegisterDoc(child.TypeName, child.TableName)
		}
	}
	if err := db.CreateTables(reg.List()); err != nil {
		t.Fatalf("create tables: %v", err)
	}

	password, err := core.Bootstrap(context.Background(), db, reg)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if password == "" {
		t.Fatalf("expected bootstrap to seed a fresh database")
	}

	permEngine := perm.NewEngine(reg)
	permEngine.SetDatabase(db)
	docEngine := document.NewWithServices(db, reg, permEngine, event.NewBus(), nil)
	jwtProvider := auth.NewJWTProvider([]byte("test-auth-secret-key-1234567"), 15*time.Minute, 7*24*time.Hour)

	pages := ui.NewRegistry()
	pages.RegisterPage(ui.Page{Path: "/app/hr/org-chart", Title: "Organization Chart", Component: "hr/OrgChart", Icon: "sitemap", Menu: "HR"})

	router := api.NewRouter(api.RouterOptions{
		CORSOrigins:  []string{"*"},
		AuthProvider: jwtProvider,
		RateLimit:    1000,
		RateWindow:   time.Minute,
		Cache:        cache.NewLRUStore(100),
		PermEngine:   permEngine,
		Registry:     reg,
		DocEngine:    docEngine,
		Database:     db,
		Pages:        pages,
	})

	return router, password
}

func postJSON(t *testing.T, h http.Handler, path string, body map[string]any, token string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w, out
}

// TestAuth_LoginRefreshRoundTrip covers the built-in email/password flow
// (PRD §15.1) against a bootstrapped admin account, then exercises the access
// token against the metadata API.
func TestAuth_LoginRefreshRoundTrip(t *testing.T) {
	handler, password := setupAuthTestSite(t)

	// Wrong password is rejected with a generic auth error.
	w, _ := postJSON(t, handler, "/api/v1/auth/login", map[string]any{"email": core.AdminEmail, "password": "wrong-password"}, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for bad password, got %d: %s", w.Code, w.Body.String())
	}

	// Correct credentials return a token pair.
	w, out := postJSON(t, handler, "/api/v1/auth/login", map[string]any{"email": core.AdminEmail, "password": password}, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 login, got %d: %s", w.Code, w.Body.String())
	}
	tokens := out["data"].(map[string]any)
	access, _ := tokens["access_token"].(string)
	refresh, _ := tokens["refresh_token"].(string)
	if access == "" || refresh == "" {
		t.Fatalf("expected access+refresh tokens, got %+v", tokens)
	}

	// The access token works against the metadata API with admin roles.
	metaReq := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/meta", nil)
	metaReq.Header.Set("Authorization", "Bearer "+access)
	metaW := httptest.NewRecorder()
	handler.ServeHTTP(metaW, metaReq)
	if metaW.Code != http.StatusOK {
		t.Fatalf("expected 200 meta with access token, got %d: %s", metaW.Code, metaW.Body.String())
	}

	// Refresh rotates the pair.
	w, out = postJSON(t, handler, "/api/v1/auth/refresh", map[string]any{"refresh_token": refresh}, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 refresh, got %d: %s", w.Code, w.Body.String())
	}
	newTokens := out["data"].(map[string]any)
	newAccess, _ := newTokens["access_token"].(string)
	if newAccess == "" {
		t.Fatalf("expected rotated access token, got %+v", newTokens)
	}
}

// TestMeta_DBColumnAndPages verifies the Phase 9 additions to the metadata
// surface: db_column on fields and the ui.Page sidebar payload.
func TestMeta_DBColumnAndPages(t *testing.T) {
	handler, password := setupAuthTestSite(t)

	_, out := postJSON(t, handler, "/api/v1/auth/login", map[string]any{"email": core.AdminEmail, "password": password}, "")
	access, _ := out["data"].(map[string]any)["access_token"].(string)

	get := func(path string) (*httptest.ResponseRecorder, map[string]any) {
		t.Helper()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+access)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w, out
	}

	// Pages endpoint returns the registered custom page for the sidebar.
	w, out := get("/api/v1/meta/pages")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 pages, got %d: %s", w.Code, w.Body.String())
	}
	data, _ := out["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("expected 1 registered page, got %d", len(data))
	}
	page := data[0].(map[string]any)
	if page["path"] != "/app/hr/org-chart" || page["component"] != "hr/OrgChart" || page["menu"] != "HR" {
		t.Errorf("page payload wrong: %+v", page)
	}

	// Field metadata carries db_column, the wire key for records.
	w, out = get("/api/v1/meta/User")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 meta/User, got %d: %s", w.Code, w.Body.String())
	}
	docData := out["data"].(map[string]any)
	fields, _ := docData["fields"].([]any)
	byCol := map[string]map[string]any{}
	for _, f := range fields {
		fm := f.(map[string]any)
		byCol[fm["db_column"].(string)] = fm
	}
	if fm := byCol["full_name"]; fm == nil {
		t.Errorf("expected full_name db_column in meta/User fields: %+v", fields)
	}
	if fm := byCol["email"]; fm == nil || fm["type"] != "string" {
		t.Errorf("expected email field typed string: %+v", fm)
	}
}
