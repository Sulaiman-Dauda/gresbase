package api

import (
	"encoding/json"
	"net/http"

	"github.com/gresbase/gresbase/internal/api/middleware"
	"github.com/gresbase/gresbase/internal/app"
	"github.com/gresbase/gresbase/internal/auth"
	"github.com/gresbase/gresbase/internal/forms"
	"github.com/rs/zerolog/log"
)

// Context key aliases for readability
var (
	contextKeyAdminID   = middleware.CtxAdminID
	contextKeyAdminRole = middleware.CtxAdminRole
)

type Handlers struct {
	app        *app.App
	rules      *RuleEvaluator
	recordAuth *auth.RecordAuthService
}

func NewHandlers(application *app.App) *Handlers {
	return &Handlers{
		app:        application,
		rules:      NewRuleEvaluator(),
		recordAuth: application.RecordAuth(),
	}
}

func requireAPIKeyPermission(w http.ResponseWriter, r *http.Request, permission string) bool {
	if middleware.HasAPIKeyPermission(r.Context(), permission) {
		return true
	}
	writeError(w, 403, "API key lacks permission: "+permission)
	return false
}

// ---------------------------------------------------------------------------
// Response helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeOK(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"code":    status,
		"message": message,
	})
}

// writeInternalError logs the full underlying error server-side (so operators
// can debug) and returns a generic message to the client, never leaking
// internal details (SQL, stack traces, file paths) in HTTP 5xx responses.
// context is a short server-side label identifying the failing operation.
func writeInternalError(w http.ResponseWriter, context string, err error) {
	log.Error().Err(err).Str("op", context).Msg("internal server error")
	writeError(w, http.StatusInternalServerError, "Internal server error")
}

func writeValidationError(w http.ResponseWriter, err error) {
	if verr, ok := err.(forms.Errors); ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"code":    http.StatusBadRequest,
			"message": "validation failed",
			"errors":  verr,
		})
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

func getBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func sanitizeAdmin(a *auth.AdminUser) map[string]any {
	return map[string]any{
		"id":            a.ID,
		"email":         a.Email,
		"avatar":        a.Avatar,
		"role":          a.Role,
		"verified":      a.Verified,
		"last_login_at": a.LastLoginAt,
		"created_at":    a.CreatedAt,
		"updated_at":    a.UpdatedAt,
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
