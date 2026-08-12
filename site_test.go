package orjanda_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/oklog/ulid/v2"
	"github.com/orjanda-framework/orjanda"
	"github.com/orjanda-framework/orjanda/app"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/config"
	"github.com/orjanda-framework/orjanda/schema"
	"github.com/stretchr/testify/require"
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
		Auth: config.AuthConfig{
			JWTSecret: "site-test-jwt-secret-0123456789-0123456789",
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

// forgeAccessToken signs an admin access token with the given secret, exactly
// the way an attacker who knows the signing key would.
func forgeAccessToken(t *testing.T, secret []byte) string {
	t.Helper()
	now := time.Now()
	claims := auth.JWTClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "01JFORGED0000000000000000",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			ID:        ulid.Make().String(),
		},
		Roles: []string{"System Administrator"},
		Type:  "access",
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString(secret)
	require.NoError(t, err)
	return s
}

// newCompiledSite builds a compiled Site bound to the given JWT secret.
func newCompiledSite(t *testing.T, cfg config.Config) *orjanda.Site {
	t.Helper()
	site, err := orjanda.NewSite(cfg)
	require.NoError(t, err)
	site.Install(app.Definition{Name: "test_app", Modules: []app.Module{{Name: "core"}}})
	require.NoError(t, site.Registry.Register("test_app", &SiteTestDoc{}))
	require.NoError(t, site.Compile())
	return site
}

// TestNewSite_RequiresJWTSecret is a regression test for REVIEW-2026-08-12
// finding 1: the framework must refuse to start with a missing or weak signing
// secret instead of silently deriving one from cfg.Server.Host.
func TestNewSite_RequiresJWTSecret(t *testing.T) {
	_, err := orjanda.NewSite(config.Config{})
	require.Error(t, err, "NewSite must fail without a configured JWT secret")

	_, err = orjanda.NewSite(config.Config{Auth: config.AuthConfig{JWTSecret: "too-short"}})
	require.Error(t, err, "NewSite must reject a secret shorter than %d chars", config.MinJWTSecretLength)

	_, err = orjanda.NewSite(config.Config{Auth: config.AuthConfig{JWTSecret: "a-strong-jwt-secret-0123456789-0123456789"}})
	require.NoError(t, err)
}

// TestSite_RejectsForgedTokens is a regression test for REVIEW-2026-08-12
// finding 1: tokens signed with the historical host-derived secrets ("0.0.0.0"
// and the old hardcoded fallback) must be rejected, while tokens signed with
// the configured secret are accepted.
func TestSite_RejectsForgedTokens(t *testing.T) {
	cfg := config.Config{
		Auth: config.AuthConfig{JWTSecret: "a-strong-jwt-secret-0123456789-0123456789"},
		Server: config.ServerConfig{
			// The bind host is deliberately the old default: it must no longer
			// double as the signing key.
			Host: "0.0.0.0",
			Port: 8080,
		},
	}
	site := newCompiledSite(t, cfg)

	for _, weak := range []string{
		"0.0.0.0",                                // old host-derived default
		"orjanda-secret-key-default-development", // old hardcoded fallback
	} {
		forged := forgeAccessToken(t, []byte(weak))
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/meta", http.NoBody)
		req.Header.Set("Authorization", "Bearer "+forged)
		w := httptest.NewRecorder()
		site.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("forged token signed with %q: expected 401, got %d: %s", weak, w.Code, w.Body.String())
		}
	}

	// Control: a token signed by the site's own provider is accepted.
	provider, ok := site.Auth.(*auth.JWTProvider)
	require.True(t, ok, "site.Auth must be a *auth.JWTProvider")
	access, _, err := provider.GenerateTokenPair(auth.Identity{
		UserID: "01JREAL0000000000000000000",
		Email:  "admin@localhost",
		Roles:  []string{"System Administrator"},
		Source: "local",
	})
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/meta", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+access)
	w := httptest.NewRecorder()
	site.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("token signed with the configured secret: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
