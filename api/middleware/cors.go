package middleware

import (
	"net/http"
	"strings"
)

// CORS handles Cross-Origin Resource Sharing based on configured allowed origins.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				allowed := false
				for _, o := range allowedOrigins {
					if o == "*" || strings.EqualFold(o, origin) {
						allowed = true
						break
					}
				}
				if allowed {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				} else if len(allowedOrigins) > 0 {
					w.Header().Set("Access-Control-Allow-Origin", allowedOrigins[0])
				}
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID, X-Tenant-ID")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
