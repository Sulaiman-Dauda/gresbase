package middleware

import (
	"net/http"

	"github.com/gresbase/gresbase/internal/netutil"
)

// RealIP rewrites r.RemoteAddr to the resolved client IP so downstream
// middleware (rate limiting, request logging) and handlers see the real client.
//
// Unlike chi's middleware.RealIP, it does NOT blindly trust X-Forwarded-For /
// X-Real-IP: those headers are honored only when the request's immediate peer
// is within a configured trusted-proxy CIDR. With no trusted proxies set, the
// socket peer address is always used — the only safe default, since forwarding
// headers are trivially spoofable by any direct client.
func RealIP(trustedProxies []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ip := netutil.ClientIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"), r.Header.Get("X-Real-IP"), trustedProxies); ip != "" {
				r.RemoteAddr = ip
			}
			next.ServeHTTP(w, r)
		})
	}
}
