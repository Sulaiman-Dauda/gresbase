package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/gresbase/gresbase/internal/app"
)

// Context keys shared with handlers.
const (
	CtxAdminID    = "admin_id"
	CtxAdminRole  = "admin_role"
	CtxAdminEmail = "admin_email"
	CtxTenantID   = "tenant_id"
	CtxRequestID  = "request_id"
)

// Middleware holds a reference to the app for auth validation.
type Middleware struct {
	app *app.App
}

// NewMiddleware creates a new middleware set.
func NewMiddleware(application *app.App) *Middleware {
	return &Middleware{app: application}
}

// SecurityHeaders adds OWASP-recommended security headers.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https:; connect-src 'self' ws: wss:;")
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
		}
		next.ServeHTTP(w, r)
	})
}

// RequestIDMiddleware extracts or creates a request ID and sets the X-Request-ID header.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = "unknown"
		}
		w.Header().Set("X-Request-ID", requestID)
		ctx := context.WithValue(r.Context(), CtxRequestID, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAuth is a middleware that validates the JWT token and injects admin
// info into the request context. Returns 401 if the token is missing or invalid.
func (mw *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractBearer(r)
		if token == "" {
			http.Error(w, `{"code":401,"message":"Missing authorization header"}`, http.StatusUnauthorized)
			w.Header().Set("Content-Type", "application/json")
			return
		}

		claims, err := mw.app.Auth().ValidateToken(token)
		if err != nil {
			http.Error(w, `{"code":401,"message":"Invalid or expired token"}`, http.StatusUnauthorized)
			w.Header().Set("Content-Type", "application/json")
			return
		}

		ctx := r.Context()
		ctx = context.WithValue(ctx, CtxAdminID, claims.AdminID)
		ctx = context.WithValue(ctx, CtxAdminRole, claims.Role)
		ctx = context.WithValue(ctx, CtxAdminEmail, claims.Email)
		ctx = context.WithValue(ctx, CtxTenantID, claims.TenantID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// OptionalAuth tries to parse a token but does NOT reject unauthenticated requests.
// Supports both admin tokens and record auth tokens.
func (mw *Middleware) OptionalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractBearer(r)
		if token == "" {
			// Also check query param (used by SSE/WebSocket)
			token = r.URL.Query().Get("token")
		}
		if token != "" {
			ctx := r.Context()

			// Try admin token first
			claims, err := mw.app.Auth().ValidateToken(token)
			if err == nil {
				ctx = context.WithValue(ctx, CtxAdminID, claims.AdminID)
				ctx = context.WithValue(ctx, CtxAdminRole, claims.Role)
				ctx = context.WithValue(ctx, CtxAdminEmail, claims.Email)
				ctx = context.WithValue(ctx, CtxTenantID, claims.TenantID)
				r = r.WithContext(ctx)
				next.ServeHTTP(w, r)
				return
			}

			// Try record auth token (end-user auth)
			recordAuthSvc := mw.app.RecordAuth()
			if recordAuthSvc != nil {
				recordClaims, err := recordAuthSvc.ValidateRecordToken(token)
				if err == nil {
					ctx = context.WithValue(ctx, "record_id", recordClaims.RecordID)
					ctx = context.WithValue(ctx, "collection_id", recordClaims.CollectionID)
					ctx = context.WithValue(ctx, "email", recordClaims.Email)
					ctx = context.WithValue(ctx, "verified", recordClaims.Verified)
					ctx = context.WithValue(ctx, CtxTenantID, "default") // records share tenant
					r = r.WithContext(ctx)
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// extractBearer pulls the token from Authorization: Bearer <token>.
func extractBearer(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		// Also check query param (useful for WebSocket)
		auth = r.URL.Query().Get("token")
		if auth != "" {
			return auth
		}
		return ""
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return parts[1]
}
