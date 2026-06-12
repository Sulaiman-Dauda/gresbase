package middleware

import (
	"net/http"
	"strings"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// IsLongLivedRequest reports whether the request is expected to stay open for a
// long time and therefore should not inherit the standard API timeout.
func IsLongLivedRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	// TUS resumable uploads stream large bodies; tusd applies its own network
	// timeouts, so the blanket API timeout must not cut uploads short.
	if strings.HasPrefix(r.URL.Path, "/api/v1/files/tus") {
		return true
	}
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/api/v1/realtime", "/api/v1/sse":
		return true
	default:
		return false
	}
}

// TimeoutExcept applies the standard chi timeout middleware to all requests
// except the ones explicitly skipped by skipper.
func TimeoutExcept(timeout time.Duration, skipper func(*http.Request) bool) func(http.Handler) http.Handler {
	base := chimw.Timeout(timeout)
	return func(next http.Handler) http.Handler {
		timed := base(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if skipper != nil && skipper(r) {
				next.ServeHTTP(w, r)
				return
			}
			timed.ServeHTTP(w, r)
		})
	}
}
