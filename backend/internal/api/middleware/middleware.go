package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gresbase/gresbase/internal/app"
	"github.com/gresbase/gresbase/internal/auth"
)

// Context keys shared with handlers.
const (
	CtxAdminID           = "admin_id"
	CtxAdminRole         = "admin_role"
	CtxAdminEmail        = "admin_email"
	CtxTenantID          = "tenant_id"
	CtxRequestID         = "request_id"
	CtxAdminAuthMethod   = "admin_auth_method"
	CtxAPIKeyID          = "api_key_id"
	CtxAPIKeyPermissions = "api_key_permissions"
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

// RequireAuth validates admin JWT or API key credentials and injects admin
// context into the request. Returns 401 if the credential is missing or invalid.
func (mw *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, ok := mw.authenticateAdminRequest(r)
		if !ok {
			writeAuthError(w, http.StatusUnauthorized, "Missing or invalid authorization")
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireMinRole ensures that the authenticated admin role satisfies the
// provided minimum role in the hierarchy: viewer < editor < admin < super_admin.
func (mw *Middleware) RequireMinRole(minRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			adminID, _ := r.Context().Value(CtxAdminID).(string)
			role, _ := r.Context().Value(CtxAdminRole).(string)
			if adminID == "" {
				writeAuthError(w, http.StatusUnauthorized, "Authentication required")
				return
			}
			if !roleAtLeast(role, minRole) {
				writeAuthError(w, http.StatusForbidden, "Insufficient role")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequirePermission enforces an API key scope when the current request is
// authenticated with an API key. JWT-authenticated admins bypass scope checks.
func (mw *Middleware) RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !HasAPIKeyPermission(r.Context(), permission) {
				writeAuthError(w, http.StatusForbidden, "API key lacks permission: "+permission)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// OptionalAuth tries to parse admin JWT/API key or record auth token but does
// not reject unauthenticated requests.
func (mw *Middleware) OptionalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ctx, ok := mw.authenticateAdminRequest(r); ok {
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		token := extractCredential(r)
		if token != "" {
			ctx := r.Context()
			recordAuthSvc := mw.app.RecordAuth()
			if recordAuthSvc != nil {
				recordClaims, err := recordAuthSvc.ValidateRecordToken(token)
				if err == nil {
					ctx = context.WithValue(ctx, "record_id", recordClaims.RecordID)
					ctx = context.WithValue(ctx, "collection_id", recordClaims.CollectionID)
					ctx = context.WithValue(ctx, "email", recordClaims.Email)
					ctx = context.WithValue(ctx, "verified", recordClaims.Verified)
					ctx = context.WithValue(ctx, "anonymous", recordClaims.Anonymous)
					ctx = context.WithValue(ctx, CtxTenantID, "default")
					r = r.WithContext(ctx)
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (mw *Middleware) authenticateAdminRequest(r *http.Request) (context.Context, bool) {
	credential := extractCredential(r)
	if credential == "" {
		return nil, false
	}

	if claims, err := mw.app.Auth().ValidateToken(credential); err == nil && isAllowedAdminTokenType(claims.Type) {
		ctx := withAdminContext(r.Context(), claims.AdminID, claims.Role, claims.Email, claims.TenantID)
		ctx = context.WithValue(ctx, CtxAdminAuthMethod, "jwt")
		return ctx, true
	}

	if apiKey, admin, err := mw.app.Auth().ValidateAPIKey(r.Context(), credential); err == nil && admin != nil {
		ctx := withAdminContext(r.Context(), admin.ID, admin.Role, admin.Email, admin.TenantID)
		ctx = context.WithValue(ctx, CtxAdminAuthMethod, "api_key")
		ctx = context.WithValue(ctx, CtxAPIKeyID, apiKey.ID)
		ctx = context.WithValue(ctx, CtxAPIKeyPermissions, auth.NormalizeAPIKeyPermissions(apiKey.Permissions))
		return ctx, true
	}

	return nil, false
}

func withAdminContext(ctx context.Context, adminID, role, email, tenantID string) context.Context {
	ctx = context.WithValue(ctx, CtxAdminID, adminID)
	ctx = context.WithValue(ctx, CtxAdminRole, role)
	ctx = context.WithValue(ctx, CtxAdminEmail, email)
	ctx = context.WithValue(ctx, CtxTenantID, tenantID)
	return ctx
}

// IsAPIKeyAuth reports whether the current request was authenticated via API key.
func IsAPIKeyAuth(ctx context.Context) bool {
	method, _ := ctx.Value(CtxAdminAuthMethod).(string)
	return strings.EqualFold(strings.TrimSpace(method), "api_key")
}

// APIKeyPermissions returns the normalized API key permissions from the context.
func APIKeyPermissions(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	if permissions, ok := ctx.Value(CtxAPIKeyPermissions).([]string); ok {
		return auth.NormalizeAPIKeyPermissions(permissions)
	}
	if raw, ok := ctx.Value(CtxAPIKeyPermissions).([]any); ok {
		converted := make([]string, 0, len(raw))
		for _, item := range raw {
			converted = append(converted, fmt.Sprint(item))
		}
		return auth.NormalizeAPIKeyPermissions(converted)
	}
	return nil
}

// HasAPIKeyPermission reports whether the current API key allows the required
// permission. Non-API-key requests always return true.
func HasAPIKeyPermission(ctx context.Context, permission string) bool {
	if !IsAPIKeyAuth(ctx) {
		return true
	}
	return auth.APIKeyPermissionAllowed(APIKeyPermissions(ctx), permission)
}

func isAllowedAdminTokenType(tokenType interface{}) bool {
	switch strings.TrimSpace(strings.ToLower(fmt.Sprint(tokenType))) {
	case "access", "admin":
		return true
	default:
		return false
	}
}

func roleAtLeast(role, minRole string) bool {
	return rolePriority(role) >= rolePriority(minRole)
}

func rolePriority(role string) int {
	switch strings.TrimSpace(strings.ToLower(role)) {
	case "super_admin":
		return 40
	case "admin":
		return 30
	case "editor":
		return 20
	case "viewer":
		return 10
	default:
		return 0
	}
}

func writeAuthError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"code":` + asStatus(code) + `,"message":"` + message + `"}`))
}

func asStatus(code int) string {
	switch code {
	case http.StatusUnauthorized:
		return "401"
	case http.StatusForbidden:
		return "403"
	default:
		return "400"
	}
}

// extractCredential pulls a bearer token, raw API key, or query token.
func extractCredential(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == "" {
		if token := strings.TrimSpace(r.URL.Query().Get("token")); token != "" {
			return token
		}
		return ""
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
		return strings.TrimSpace(parts[1])
	}
	if strings.HasPrefix(auth, "gb_") {
		return auth
	}
	return ""
}
