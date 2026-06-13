package middleware

import (
	"net/http"
	"time"

	"github.com/go-chi/httprate"
	"github.com/gresbase/gresbase/internal/netutil"
)

// RateLimitByIP returns an IP-based rate-limit middleware (a thin wrapper over
// httprate) that exempts any client IP in the configured RateLimitExcludeIPs
// allowlist. Matching is CIDR-aware; excluded requests skip the limiter window
// entirely instead of just getting a higher quota.
//
// Use this in place of httprate.LimitByIP so operators can carve out trusted
// networks (internal services, uptime probes, an office egress IP) without
// disabling rate limiting for everyone. The client IP is read from
// r.RemoteAddr, which RealIP has already resolved in a trusted-proxy-aware way.
func (mw *Middleware) RateLimitByIP(requests int, window time.Duration) func(http.Handler) http.Handler {
	limiter := httprate.LimitByIP(requests, window)

	return func(next http.Handler) http.Handler {
		limited := limiter(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if mw.rateLimitExcluded(r) {
				next.ServeHTTP(w, r)
				return
			}
			limited.ServeHTTP(w, r)
		})
	}
}

// rateLimitExcluded reports whether the request's resolved client IP is exempt
// from rate limiting per the configured RateLimitExcludeIPs allowlist. Read at
// request time so the list stays consistent with live config.
func (mw *Middleware) rateLimitExcluded(r *http.Request) bool {
	if mw.app == nil || mw.app.Config() == nil {
		return false
	}
	exclude := mw.app.Config().RateLimitExcludeIPs
	if len(exclude) == 0 {
		return false
	}
	return netutil.IPInCIDRs(netutil.HostOnly(r.RemoteAddr), exclude)
}
