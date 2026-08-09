package orjanda_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/orjanda-framework/orjanda"
	"github.com/orjanda-framework/orjanda/app"
	"github.com/orjanda-framework/orjanda/config"
	"github.com/orjanda-framework/orjanda/schema"
)

type SiteTestDoc struct {
	schema.BaseDocument
	Name string `oj:"required"`
}

func (s *SiteTestDoc) DocMeta() schema.Meta {
	return schema.Meta{Name: "SiteTestDoc"}
}

func TestSite_Lifecycle(t *testing.T) {
	cfg := config.Config{
		Database: config.DatabaseConfig{
			Driver: "sqlite",
			DSN:    ":memory:",
		},
		Server: config.ServerConfig{
			Host:        "127.0.0.1",
			Port:        8080,
			CORSOrigins: []string{"*"},
		},
	}

	site, err := orjanda.NewSite(cfg)
	if err != nil {
		t.Fatalf("failed to create site: %v", err)
	}

	site.Install(app.Definition{
		Name: "test_app",
		Modules: []app.Module{
			{Name: "core"},
		},
	})

	if err := site.Registry.Register("test_app", &SiteTestDoc{}); err != nil {
		t.Fatalf("failed to register doc: %v", err)
	}

	if err := site.Compile(); err != nil {
		t.Fatalf("failed to compile site: %v", err)
	}

	// Test HTTP routing via site.ServeHTTP
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/meta", http.NoBody)
	w := httptest.NewRecorder()

	site.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK from /api/v1/meta, got %d: %s", w.Code, w.Body.String())
	}
}
