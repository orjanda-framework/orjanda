package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/orjanda-framework/orjanda/api/render"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/cache"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
)

type rateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	limit    int
	window   time.Duration
	cache    cache.Store
}

// RateLimit creates rate limiting middleware per caller identity or remote IP.
func RateLimit(limit int, window time.Duration, store cache.Store) func(http.Handler) http.Handler {
	rl := &rateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
		cache:    store,
	}
	if rl.limit <= 0 {
		rl.limit = 1000
	}
	if rl.window <= 0 {
		rl.window = time.Minute
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := auth.FromContext(r.Context())
			key := id.UserID
			if key == "" {
				key = r.RemoteAddr
			}

			now := time.Now()
			rl.mu.Lock()
			timestamps := rl.requests[key]
			cutoff := now.Add(-rl.window)

			valid := timestamps[:0]
			for _, t := range timestamps {
				if t.After(cutoff) {
					valid = append(valid, t)
				}
			}

			if len(valid) >= rl.limit {
				rl.requests[key] = valid
				rl.mu.Unlock()
				render.RespondError(w, orjerrors.Validation("rate limit exceeded", map[string]any{
					"limit":  rl.limit,
					"window": rl.window.String(),
				}))
				return
			}

			rl.requests[key] = append(valid, now)
			rl.mu.Unlock()

			next.ServeHTTP(w, r)
		})
	}
}
