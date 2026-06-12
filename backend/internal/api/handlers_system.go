package api

import (
	"net/http"
	"runtime"
	"time"

	"github.com/gresbase/gresbase/internal/auth"
	"github.com/gresbase/gresbase/internal/buildinfo"
	"github.com/gresbase/gresbase/internal/events"
	"github.com/gresbase/gresbase/internal/forms"
	"github.com/gresbase/gresbase/internal/openapi"
	"github.com/rs/zerolog/log"
)

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	dbOk := true
	if h.app.DB() != nil {
		if err := h.app.DB().Ping(r.Context()); err != nil {
			dbOk = false
		}
	}
	status := http.StatusOK
	statusText := "healthy"
	if !dbOk {
		status = http.StatusServiceUnavailable
		statusText = "degraded"
	}
	writeJSON(w, status, map[string]any{
		"status":    statusText,
		"version":   buildinfo.Version,
		"database":  dbOk,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

func (h *Handlers) Ready(w http.ResponseWriter, r *http.Request) {
	checks := map[string]any{
		"app": h.app != nil && h.app.IsReady(),
	}
	ready := h.app != nil && h.app.IsReady()

	dbReady := false
	if ready && h.app.DB() != nil {
		dbReady = h.app.DB().Ping(r.Context()) == nil
	}
	checks["database"] = dbReady
	ready = ready && dbReady

	status := http.StatusOK
	statusText := "ready"
	if !ready {
		status = http.StatusServiceUnavailable
		statusText = "not_ready"
	}

	writeJSON(w, status, map[string]any{
		"status":    statusText,
		"checks":    checks,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// ---------------------------------------------------------------------------
// OpenAPI / Metrics
// ---------------------------------------------------------------------------

func (h *Handlers) OpenAPI(w http.ResponseWriter, r *http.Request) {
	spec := h.generateOpenAPISpec(r)
	writeJSON(w, http.StatusOK, spec)
}

func (h *Handlers) JWKS(w http.ResponseWriter, r *http.Request) {
	jwks, err := auth.PublicJWKS(h.app.Config())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load JWKS")
		return
	}
	writeJSON(w, http.StatusOK, jwks)
}

func (h *Handlers) Metrics(w http.ResponseWriter, r *http.Request) {
	report := map[string]any{
		"status":    "healthy",
		"version":   buildinfo.Version,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"database": map[string]any{
			"status": "connected",
			"mode":   "embedded",
		},
		"realtime": map[string]any{
			"clients": h.app.Realtime().ClientCount(),
		},
		"system": map[string]any{
			"go_version":     runtime.Version(),
			"num_goroutines": runtime.NumGoroutine(),
		},
	}

	if h.app.DB() != nil {
		stats := h.app.DB().Pool.Stat()
		report["database"] = map[string]any{
			"status":                   "connected",
			"mode":                     map[bool]string{true: "embedded", false: "external"}[h.app.DB().IsEmbedded()],
			"open_connections":         stats.TotalConns(),
			"acquired_connections":     stats.AcquiredConns(),
			"idle_connections":         stats.IdleConns(),
			"constructing_connections": stats.ConstructingConns(),
			"max_connections":          stats.MaxConns(),
			"acquire_count":            stats.AcquireCount(),
			"acquire_duration_ms":      float64(stats.AcquireDuration().Microseconds()) / 1000,
			"empty_acquire_count":      stats.EmptyAcquireCount(),
			"canceled_acquire_count":   stats.CanceledAcquireCount(),
		}
	}

	writeJSON(w, http.StatusOK, report)
}

func (h *Handlers) generateOpenAPISpec(r *http.Request) any {
	baseURL := getBaseURL(r)
	if provider, ok := h.app.APIRouter().(interface{ OpenAPISpec(string) *openapi.Spec }); ok {
		return provider.OpenAPISpec(baseURL)
	}
	return openapi.Generate(buildinfo.Version, baseURL, nil)
}

// SetupStatus checks whether the system needs first-time setup (no admin exists).
func (h *Handlers) SetupStatus(w http.ResponseWriter, r *http.Request) {
	adminCount, err := h.app.Auth().CountAdmins(r.Context())
	if err != nil {
		writeError(w, 500, "Failed to check admin status")
		return
	}
	writeOK(w, map[string]any{
		"setup_required": adminCount == 0,
		"admin_count":    adminCount,
	})
}

// SetupCreate creates the FIRST admin when no admin exists yet.
// Once any admin exists, this endpoint becomes unavailable.
func (h *Handlers) SetupCreate(w http.ResponseWriter, r *http.Request) {
	// Only allow setup when no admin exists
	adminCount, err := h.app.Auth().CountAdmins(r.Context())
	if err != nil {
		writeError(w, 500, "Failed to check admin status")
		return
	}
	if adminCount > 0 {
		writeError(w, 403, "Setup already completed. Use /auth/login to sign in.")
		return
	}

	var form forms.SetupForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.AdminAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "setup", Provider: "setup", Email: form.Email}
	var admin *auth.AdminUser
	if err := h.app.OnAdminAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		admin, err = h.app.Auth().CreateAdmin(r.Context(), form.Email, form.Password, "super_admin")
		return err
	}); err != nil {
		writeError(w, 409, "Failed to create admin: "+err.Error())
		return
	}

	token, refreshToken, _ := h.app.Auth().GenerateTokens(admin.ID, admin.Email, admin.Role)
	_ = h.app.OnAuthLogin().Trigger(&events.AuthEvent{App: h.app, UserID: admin.ID, Provider: "setup", Token: token, RefreshToken: refreshToken}, func(e events.Event) error { return e.Next() })
	h.app.Auth().RecordAudit(r.Context(), admin.ID, "setup.first_admin", "_admins", admin.ID, nil, r)

	log.Info().Str("email", admin.Email).Str("id", admin.ID).Msg("🎉 First admin created — Gresbase is ready!")

	writeJSON(w, 201, map[string]any{
		"token":        token,
		"refreshToken": refreshToken,
		"admin":        sanitizeAdmin(admin),
		"message":      "Setup complete! Welcome to Gresbase.",
	})
}
