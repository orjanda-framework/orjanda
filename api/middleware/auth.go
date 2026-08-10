package middleware

import (
	"net/http"
	"strings"

	"github.com/orjanda-framework/orjanda/api/render"
	"github.com/orjanda-framework/orjanda/auth"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
)

// Auth middleware extracts authentication credentials from the Authorization
// header and injects auth.Identity into context.Context via auth.NewContext.
// The ?access_token= query parameter is accepted as a fallback so browsers can
// authenticate the agent WebSocket (TAD §6.2) without a custom header; it is
// never returned in URLs by the UI.
func Auth(provider auth.Provider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			token := ""
			if authHeader != "" {
				parts := strings.SplitN(authHeader, " ", 2)
				if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
					render.RespondError(w, orjerrors.Auth("invalid authorization header format"))
					return
				}
				token = parts[1]
			} else if q := r.URL.Query().Get("access_token"); q != "" {
				token = q
			}

			if token == "" {
				// Proceed unauthenticated (Identity will be empty zero value)
				next.ServeHTTP(w, r)
				return
			}

			if provider == nil {
				render.RespondError(w, orjerrors.Auth("authentication provider not configured"))
				return
			}

			identity, err := provider.ValidateToken(r.Context(), token)
			if err != nil || identity == nil {
				render.RespondError(w, orjerrors.Auth("invalid or expired token"))
				return
			}

			ctx := auth.NewContext(r.Context(), *identity)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
