package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	apimiddleware "github.com/orjanda-framework/orjanda/api/middleware"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/cache"
)

func TestCORS(t *testing.T) {
	mw := apimiddleware.CORS([]string{"http://localhost:3000"})
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Preflight OPTIONS request
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/document/Task", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()

	mw(dummyHandler).ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204 No Content for OPTIONS, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Errorf("expected CORS origin header, got %s", w.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestAuthMiddleware_Unauthenticated(t *testing.T) {
	jwtProvider := auth.NewJWTProvider([]byte("secret"), 15*time.Minute, 7*24*time.Hour)
	mw := apimiddleware.Auth(jwtProvider)

	var extractedID auth.Identity
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		extractedID = auth.FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/document/Task", nil)
	w := httptest.NewRecorder()

	mw(dummyHandler).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK for unauthenticated request, got %d", w.Code)
	}
	if extractedID.UserID != "" {
		t.Errorf("expected zero UserID for unauthenticated request, got %s", extractedID.UserID)
	}
}

func TestRateLimit(t *testing.T) {
	store := cache.NewLRUStore(100)
	mw := apimiddleware.RateLimit(2, time.Minute, store)

	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h := mw(dummyHandler)

	// Call 1 (OK)
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.RemoteAddr = "127.0.0.1:12345"
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Errorf("call 1: expected 200, got %d", w1.Code)
	}

	// Call 2 (OK)
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "127.0.0.1:12345"
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("call 2: expected 200, got %d", w2.Code)
	}

	// Call 3 (Rate Limited - 400 Bad Request / Validation error)
	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	req3.RemoteAddr = "127.0.0.1:12345"
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, req3)
	if w3.Code != http.StatusBadRequest {
		t.Errorf("call 3: expected 400 Validation Error for rate limit, got %d", w3.Code)
	}
}
