package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gresbase/gresbase/internal/acme"
	"github.com/gresbase/gresbase/internal/api/middleware"
	"github.com/gresbase/gresbase/internal/app"
	"github.com/gresbase/gresbase/internal/auth"
	"github.com/gresbase/gresbase/internal/collection"
	"github.com/gresbase/gresbase/internal/query"
	"github.com/gresbase/gresbase/internal/realtime"
	"github.com/gresbase/gresbase/internal/settings"
	"github.com/gresbase/gresbase/internal/storage"
	"github.com/gresbase/gresbase/internal/tools/search"
	"github.com/rs/zerolog/log"
)

// Context key aliases for readability
var (
	contextKeyAdminID   = middleware.CtxAdminID
	contextKeyAdminRole = middleware.CtxAdminRole
	contextKeyTenantID  = middleware.CtxTenantID
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
		recordAuth: auth.NewRecordAuthService(application.DB(), application.Config(), application.Auth()),
	}
}

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
		"version":   "0.1.0",
		"database":  dbOk,
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

func (h *Handlers) Metrics(w http.ResponseWriter, r *http.Request) {
	report := map[string]any{
		"status":    "healthy",
		"version":   "0.3.0",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"database": map[string]any{
			"status": "connected",
			"mode":   "embedded",
		},
		"realtime": map[string]any{
			"clients": h.app.Realtime().ClientCount(),
		},
		"system": map[string]any{
			"go_version":    "1.22+",
			"num_goroutines": 0,
		},
	}

	if h.app.DB() != nil {
		stats := h.app.DB().Pool.Stat()
		report["database"] = map[string]any{
			"status":           "connected",
			"mode":             map[bool]string{true: "embedded", false: "external"}[h.app.DB().IsEmbedded()],
			"open_connections": stats.TotalConns(),
			"idle_connections": stats.IdleConns(),
			"max_connections":  stats.MaxConns(),
		}
	}

	writeJSON(w, http.StatusOK, report)
}

func (h *Handlers) generateOpenAPISpec(r *http.Request) map[string]any {
	baseURL := getBaseURL(r)
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "Gresbase API",
			"version":     "0.3.0",
			"description": "Auto-generated OpenAPI specification for the Gresbase REST API.",
		},
		"servers": []map[string]any{
			{"url": baseURL + "/api/v1", "description": "Local server"},
		},
		"paths": map[string]any{
			"/health": map[string]any{
				"get": map[string]any{
					"summary":     "Health check",
					"operationId": "healthCheck",
					"tags":        []string{"Health"},
					"responses": map[string]any{
						"200": map[string]any{"description": "Server is healthy"},
					},
				},
			},
			"/auth/login": map[string]any{
				"post": map[string]any{
					"summary":     "Admin login",
					"operationId": "authLogin",
					"tags":        []string{"Auth"},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"email":    map[string]any{"type": "string", "format": "email"},
										"password": map[string]any{"type": "string", "format": "password"},
									},
								},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Login successful"},
						"401": map[string]any{"description": "Invalid credentials"},
					},
				},
			},
			"/auth/register": map[string]any{
				"post": map[string]any{
					"summary":     "Register admin",
					"operationId": "authRegister",
					"tags":        []string{"Auth"},
				},
			},
			"/auth/refresh": map[string]any{
				"post": map[string]any{
					"summary":     "Refresh token",
					"operationId": "authRefresh",
					"tags":        []string{"Auth"},
				},
			},
			"/auth/otp/request": map[string]any{
				"post": map[string]any{
					"summary":     "Request OTP",
					"operationId": "authOtpRequest",
					"tags":        []string{"Auth"},
				},
			},
			"/auth/otp/verify": map[string]any{
				"post": map[string]any{
					"summary":     "Verify OTP",
					"operationId": "authOtpVerify",
					"tags":        []string{"Auth"},
				},
			},
			"/auth/magic-link": map[string]any{
				"post": map[string]any{
					"summary":     "Send magic link",
					"operationId": "authMagicLink",
					"tags":        []string{"Auth"},
				},
			},
			"/collections": map[string]any{
				"get": map[string]any{
					"summary":     "List collections",
					"operationId": "collectionsList",
					"tags":        []string{"Collections"},
					"security":    []map[string]any{{"bearerAuth": []any{}}},
				},
				"post": map[string]any{
					"summary":     "Create collection",
					"operationId": "collectionsCreate",
					"tags":        []string{"Collections"},
					"security":    []map[string]any{{"bearerAuth": []any{}}},
				},
			},
			"/collections/{id}": map[string]any{
				"get": map[string]any{
					"summary":     "Get collection",
					"operationId": "collectionsGet",
					"tags":        []string{"Collections"},
					"parameters":  []map[string]any{{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}}},
				},
				"put": map[string]any{
					"summary":     "Update collection",
					"operationId": "collectionsUpdate",
					"tags":        []string{"Collections"},
				},
				"delete": map[string]any{
					"summary":     "Delete collection",
					"operationId": "collectionsDelete",
					"tags":        []string{"Collections"},
				},
			},
			"/records/{collection}": map[string]any{
				"get": map[string]any{
					"summary":     "List records",
					"operationId": "recordsList",
					"tags":        []string{"Records"},
					"parameters": []map[string]any{
						{"name": "collection", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
						{"name": "page", "in": "query", "schema": map[string]any{"type": "integer", "default": 1}},
						{"name": "perPage", "in": "query", "schema": map[string]any{"type": "integer", "default": 30}},
						{"name": "sort", "in": "query", "schema": map[string]any{"type": "string"}},
						{"name": "filter", "in": "query", "schema": map[string]any{"type": "string"}},
						{"name": "expand", "in": "query", "schema": map[string]any{"type": "string"}},
						{"name": "fields", "in": "query", "schema": map[string]any{"type": "string"}},
					},
				},
				"post": map[string]any{
					"summary":     "Create record",
					"operationId": "recordsCreate",
					"tags":        []string{"Records"},
				},
			},
			"/records/{collection}/{recordId}": map[string]any{
				"get": map[string]any{
					"summary":     "Get record",
					"operationId": "recordsGet",
					"tags":        []string{"Records"},
				},
				"put": map[string]any{
					"summary":     "Update record",
					"operationId": "recordsUpdate",
					"tags":        []string{"Records"},
				},
				"delete": map[string]any{
					"summary":     "Delete record",
					"operationId": "recordsDelete",
					"tags":        []string{"Records"},
				},
			},
			"/collections/{collection}/auth-methods": map[string]any{
				"get": map[string]any{
					"summary":     "List auth methods",
					"operationId": "recordAuthMethods",
					"tags":        []string{"Record Auth"},
				},
			},
			"/collections/{collection}/auth/auth-with-password": map[string]any{
				"post": map[string]any{
					"summary":     "Password auth for records",
					"operationId": "recordAuthPassword",
					"tags":        []string{"Record Auth"},
				},
			},
			"/realtime": map[string]any{
				"get": map[string]any{
					"summary":     "Realtime subscription (SSE/WebSocket)",
					"operationId": "realtimeConnect",
					"tags":        []string{"Realtime"},
				},
			},
			"/files/{collection}/{recordId}/{filename}": map[string]any{
				"get": map[string]any{
					"summary":     "Download file",
					"operationId": "filesDownload",
					"tags":        []string{"Files"},
				},
			},
			"/certificates": map[string]any{
				"get": map[string]any{
					"summary":     "List certificates",
					"operationId": "certificatesList",
					"tags":        []string{"Certificates"},
				},
				"post": map[string]any{
					"summary":     "Issue certificate",
					"operationId": "certificatesIssue",
					"tags":        []string{"Certificates"},
				},
			},
			"/api-keys": map[string]any{
				"get": map[string]any{
					"summary":     "List API keys",
					"operationId": "apiKeysList",
					"tags":        []string{"API Keys"},
				},
				"post": map[string]any{
					"summary":     "Create API key",
					"operationId": "apiKeysCreate",
					"tags":        []string{"API Keys"},
				},
			},
			"/logs": map[string]any{
				"get": map[string]any{
					"summary":     "View audit logs",
					"operationId": "logsList",
					"tags":        []string{"Logs"},
					"parameters": []map[string]any{
						{"name": "page", "in": "query", "schema": map[string]any{"type": "integer"}},
						{"name": "perPage", "in": "query", "schema": map[string]any{"type": "integer"}},
						{"name": "action", "in": "query", "schema": map[string]any{"type": "string"}},
						{"name": "resource", "in": "query", "schema": map[string]any{"type": "string"}},
					},
				},
			},
			"/settings": map[string]any{
				"get": map[string]any{
					"summary":     "Get settings",
					"operationId": "settingsGet",
					"tags":        []string{"Settings"},
				},
				"put": map[string]any{
					"summary":     "Update settings",
					"operationId": "settingsUpdate",
					"tags":        []string{"Settings"},
				},
			},
			"/admin/users": map[string]any{
				"get": map[string]any{
					"summary":     "List admins",
					"operationId": "adminList",
					"tags":        []string{"Admin"},
				},
				"post": map[string]any{
					"summary":     "Create admin",
					"operationId": "adminCreate",
					"tags":        []string{"Admin"},
				},
			},
			"/backups": map[string]any{
				"get": map[string]any{
					"summary":     "List backups",
					"operationId": "backupsList",
					"tags":        []string{"Backups"},
				},
				"post": map[string]any{
					"summary":     "Create backup",
					"operationId": "backupsCreate",
					"tags":        []string{"Backups"},
				},
			},
			"/backups/{name}/download": map[string]any{
				"get": map[string]any{
					"summary":     "Download backup",
					"operationId": "backupsDownload",
					"tags":        []string{"Backups"},
				},
			},
			"/backups/{name}/restore": map[string]any{
				"post": map[string]any{
					"summary":     "Restore backup",
					"operationId": "backupsRestore",
					"tags":        []string{"Backups"},
				},
			},
			"/jobs": map[string]any{
				"get": map[string]any{
					"summary":     "List cron jobs",
					"operationId": "jobsList",
					"tags":        []string{"Jobs"},
				},
				"post": map[string]any{
					"summary":     "Create cron job",
					"operationId": "jobsCreate",
					"tags":        []string{"Jobs"},
				},
			},
		},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "JWT",
				},
				"apiKey": map[string]any{
					"type": "apiKey",
					"in":   "header",
					"name": "Authorization",
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if body.Email == "" || body.Password == "" {
		writeError(w, 400, "Email and password are required")
		return
	}

	token, refreshToken, admin, err := h.app.Auth().Login(r.Context(), body.Email, body.Password)
	if err != nil {
		h.app.Auth().RecordAudit(r.Context(), "", "auth.login.failed", "_admins", "", map[string]any{"email": body.Email}, r)
		writeError(w, 401, "Invalid credentials")
		return
	}

	h.app.Auth().RecordAudit(r.Context(), admin.ID, "auth.login", "_admins", admin.ID, nil, r)
	writeOK(w, map[string]any{
		"token":        token,
		"refreshToken": refreshToken,
		"admin":        sanitizeAdmin(admin),
	})
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

	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if body.Email == "" || body.Password == "" || len(body.Password) < 8 {
		writeError(w, 400, "Email and password (min 8 chars) required")
		return
	}

	admin, err := h.app.Auth().CreateAdmin(r.Context(), body.Email, body.Password, "super_admin", "default")
	if err != nil {
		writeError(w, 409, "Failed to create admin: "+err.Error())
		return
	}

	token, refreshToken, _ := h.app.Auth().GenerateTokens(admin.ID, admin.Email, admin.Role, admin.TenantID)
	h.app.Auth().RecordAudit(r.Context(), admin.ID, "setup.first_admin", "_admins", admin.ID, nil, r)

	log.Info().Str("email", admin.Email).Str("id", admin.ID).Msg("🎉 First admin created — Gresbase is ready!")

	writeJSON(w, 201, map[string]any{
		"token":        token,
		"refreshToken": refreshToken,
		"admin":        sanitizeAdmin(admin),
		"message":      "Setup complete! Welcome to Gresbase.",
	})
}

func (h *Handlers) Register(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if body.Email == "" || body.Password == "" || len(body.Password) < 8 {
		writeError(w, 400, "Email and password (min 8 chars) required")
		return
	}

	// Check if this is the first admin (setup mode) or an existing admin adding another
	adminCount, _ := h.app.Auth().CountAdmins(r.Context())
	role := "admin"
	if adminCount == 0 {
		role = "super_admin"
		log.Info().Msg("First admin registration — granting super_admin role")
	}

	admin, err := h.app.Auth().CreateAdmin(r.Context(), body.Email, body.Password, role, "default")
	if err != nil {
		writeError(w, 409, "Email already in use")
		return
	}

	token, refreshToken, _ := h.app.Auth().GenerateTokens(admin.ID, admin.Email, admin.Role, admin.TenantID)
	h.app.Auth().RecordAudit(r.Context(), admin.ID, "auth.register", "_admins", admin.ID, nil, r)

	writeJSON(w, 201, map[string]any{
		"token":        token,
		"refreshToken": refreshToken,
		"admin":        sanitizeAdmin(admin),
	})
}

func (h *Handlers) RefreshToken(w http.ResponseWriter, r *http.Request) {
	var body struct{ RefreshToken string `json:"refreshToken"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if body.RefreshToken == "" {
		writeError(w, 400, "refreshToken is required")
		return
	}

	token, refreshToken, err := h.app.Auth().RefreshToken(r.Context(), body.RefreshToken)
	if err != nil {
		writeError(w, 401, "Invalid or expired refresh token")
		return
	}

	writeOK(w, map[string]any{"token": token, "refreshToken": refreshToken})
}

func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Authorization")
	if len(token) > 7 {
		token = token[7:]
	}
	_ = h.app.Auth().Logout(r.Context(), token)
	writeOK(w, map[string]any{"message": "Logged out"})
}

func (h *Handlers) OTPRequest(w http.ResponseWriter, r *http.Request) {
	var body struct{ Email string `json:"email"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" {
		writeError(w, 400, "Email is required")
		return
	}

	otp, err := h.app.Auth().CreateOTP(r.Context(), body.Email)
	if err != nil {
		writeError(w, 500, "Failed to create OTP")
		return
	}

	// Send OTP via email
	if h.app.Mailer() != nil {
		h.app.Mailer().SendOTP(body.Email, otp.Code)
	}

	writeOK(w, map[string]any{"message": "OTP sent", "otpId": otp.ID})
}

func (h *Handlers) OTPVerify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OTPID string `json:"otpId"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "Invalid body")
		return
	}

	admin, err := h.app.Auth().VerifyOTP(r.Context(), body.OTPID, body.Code)
	if err != nil {
		writeError(w, 401, "Invalid OTP code")
		return
	}

	token, refreshToken, _ := h.app.Auth().GenerateTokens(admin.ID, admin.Email, admin.Role, admin.TenantID)
	h.app.Auth().RecordAudit(r.Context(), admin.ID, "auth.otp", "_admins", admin.ID, nil, r)

	writeOK(w, map[string]any{
		"token":        token,
		"refreshToken": refreshToken,
		"admin":        sanitizeAdmin(admin),
	})
}

func (h *Handlers) MagicLink(w http.ResponseWriter, r *http.Request) {
	var body struct{ Email string `json:"email"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" {
		writeError(w, 400, "Email required")
		return
	}

	token, err := h.app.Auth().CreateMagicLink(r.Context(), body.Email)
	if err != nil {
		writeError(w, 500, "Failed to create magic link")
		return
	}

	if h.app.Mailer() != nil {
		h.app.Mailer().SendMagicLink(body.Email, token)
	}

	writeOK(w, map[string]any{"message": "Magic link sent"})
}

func (h *Handlers) MagicLinkVerify(w http.ResponseWriter, r *http.Request) {
	var body struct{ Token string `json:"token"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Token == "" {
		writeError(w, 400, "Token required")
		return
	}

	admin, err := h.app.Auth().VerifyMagicLink(r.Context(), body.Token)
	if err != nil {
		writeError(w, 401, "Invalid or expired magic link")
		return
	}

	token, refreshToken, _ := h.app.Auth().GenerateTokens(admin.ID, admin.Email, admin.Role, admin.TenantID)
	h.app.Auth().RecordAudit(r.Context(), admin.ID, "auth.magiclink", "_admins", admin.ID, nil, r)

	writeOK(w, map[string]any{
		"token":        token,
		"refreshToken": refreshToken,
		"admin":        sanitizeAdmin(admin),
	})
}

// ---------------------------------------------------------------------------
// Password Reset
// ---------------------------------------------------------------------------

func (h *Handlers) PasswordResetRequest(w http.ResponseWriter, r *http.Request) {
	var body struct{ Email string `json:"email"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" {
		writeError(w, 400, "Email required")
		return
	}

	token, err := h.app.Auth().CreatePasswordResetToken(r.Context(), body.Email)
	if err != nil {
		// Don't reveal whether email exists
		writeOK(w, map[string]any{"message": "If the email exists, a reset link has been sent"})
		return
	}

	if h.app.Mailer() != nil {
		h.app.Mailer().SendPasswordReset(body.Email, token)
	}

	writeOK(w, map[string]any{"message": "If the email exists, a reset link has been sent"})
}

func (h *Handlers) PasswordResetConfirm(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "Invalid body")
		return
	}
	if body.Token == "" || len(body.Password) < 8 {
		writeError(w, 400, "Token and password (min 8 chars) required")
		return
	}

	if err := h.app.Auth().ConfirmPasswordReset(r.Context(), body.Token, body.Password); err != nil {
		writeError(w, 400, "Invalid or expired reset token")
		return
	}

	writeOK(w, map[string]any{"message": "Password reset successfully"})
}

// ---------------------------------------------------------------------------
// Email Verification
// ---------------------------------------------------------------------------

func (h *Handlers) VerificationRequest(w http.ResponseWriter, r *http.Request) {
	var body struct{ Email string `json:"email"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" {
		writeError(w, 400, "Email required")
		return
	}

	token, err := h.app.Auth().CreateVerificationToken(r.Context(), body.Email)
	if err != nil {
		writeError(w, 500, "Failed to create verification")
		return
	}

	if h.app.Mailer() != nil {
		h.app.Mailer().SendVerification(body.Email, token)
	}

	writeOK(w, map[string]any{"message": "Verification email sent"})
}

func (h *Handlers) VerificationConfirm(w http.ResponseWriter, r *http.Request) {
	var body struct{ Token string `json:"token"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Token == "" {
		writeError(w, 400, "Token required")
		return
	}

	if err := h.app.Auth().ConfirmVerification(r.Context(), body.Token); err != nil {
		writeError(w, 400, "Invalid or expired verification token")
		return
	}

	writeOK(w, map[string]any{"message": "Email verified"})
}

// ---------------------------------------------------------------------------
// Email Change
// ---------------------------------------------------------------------------

func (h *Handlers) EmailChangeRequest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NewEmail string `json:"newEmail"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.NewEmail == "" {
		writeError(w, 400, "newEmail required")
		return
	}

	adminID := r.Context().Value(contextKeyAdminID).(string)
	token, err := h.app.Auth().CreateEmailChangeToken(r.Context(), adminID, body.NewEmail)
	if err != nil {
		writeError(w, 500, "Failed to create email change")
		return
	}

	if h.app.Mailer() != nil {
		h.app.Mailer().SendEmailChange(body.NewEmail, token)
	}

	writeOK(w, map[string]any{"message": "Confirmation email sent to new address"})
}

func (h *Handlers) EmailChangeConfirm(w http.ResponseWriter, r *http.Request) {
	var body struct{ Token string `json:"token"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Token == "" {
		writeError(w, 400, "Token required")
		return
	}

	if err := h.app.Auth().ConfirmEmailChange(r.Context(), body.Token); err != nil {
		writeError(w, 400, "Invalid or expired token")
		return
	}

	writeOK(w, map[string]any{"message": "Email changed successfully"})
}

// ---------------------------------------------------------------------------
// Admin users
// ---------------------------------------------------------------------------

func (h *Handlers) AdminMe(w http.ResponseWriter, r *http.Request) {
	adminID := r.Context().Value(contextKeyAdminID).(string)
	admin, err := h.app.Auth().FindAdminByID(r.Context(), adminID)
	if err != nil {
		writeError(w, 404, "Admin not found")
		return
	}
	writeOK(w, sanitizeAdmin(admin))
}

func (h *Handlers) AdminUpdateMe(w http.ResponseWriter, r *http.Request) {
	adminID := r.Context().Value(contextKeyAdminID).(string)
	var body struct {
		Email  string `json:"email"`
		Avatar string `json:"avatar"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "Invalid body")
		return
	}

	updates := make(map[string]any)
	if body.Email != "" {
		updates["email"] = body.Email
	}
	if body.Avatar != "" {
		updates["avatar"] = body.Avatar
	}

	if err := h.app.Auth().UpdateAdmin(r.Context(), adminID, updates); err != nil {
		writeError(w, 500, "Failed to update: "+err.Error())
		return
	}

	admin, _ := h.app.Auth().FindAdminByID(r.Context(), adminID)
	writeOK(w, sanitizeAdmin(admin))
}

func (h *Handlers) AdminList(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Context().Value(contextKeyTenantID).(string)
	admins, err := h.app.Auth().ListAdmins(r.Context(), tenantID)
	if err != nil {
		writeError(w, 500, "Failed to list admins")
		return
	}
	result := make([]map[string]any, len(admins))
	for i, a := range admins {
		result[i] = sanitizeAdmin(a)
	}
	writeOK(w, result)
}

func (h *Handlers) AdminCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "Invalid body")
		return
	}
	if body.Role == "" {
		body.Role = "admin"
	}
	tenantID := r.Context().Value(contextKeyTenantID).(string)
	admin, err := h.app.Auth().CreateAdmin(r.Context(), body.Email, body.Password, body.Role, tenantID)
	if err != nil {
		writeError(w, 409, "Failed to create admin: "+err.Error())
		return
	}
	adminID := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "admin.create", "_admins", admin.ID, map[string]any{"email": admin.Email, "role": admin.Role}, r)
	writeJSON(w, 201, sanitizeAdmin(admin))
}

func (h *Handlers) AdminGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	admin, err := h.app.Auth().FindAdminByID(r.Context(), id)
	if err != nil {
		writeError(w, 404, "Admin not found")
		return
	}
	writeOK(w, sanitizeAdmin(admin))
}

func (h *Handlers) AdminUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Email  string `json:"email"`
		Role   string `json:"role"`
		Avatar string `json:"avatar"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "Invalid body")
		return
	}

	updates := make(map[string]any)
	if body.Email != "" {
		updates["email"] = body.Email
	}
	if body.Role != "" {
		updates["role"] = body.Role
	}
	if body.Avatar != "" {
		updates["avatar"] = body.Avatar
	}

	if err := h.app.Auth().UpdateAdmin(r.Context(), id, updates); err != nil {
		writeError(w, 500, "Failed to update: "+err.Error())
		return
	}

	admin, _ := h.app.Auth().FindAdminByID(r.Context(), id)
	writeOK(w, sanitizeAdmin(admin))
}

func (h *Handlers) AdminDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.app.Auth().DeleteAdmin(r.Context(), id); err != nil {
		writeError(w, 500, "Failed to delete admin")
		return
	}
	writeOK(w, map[string]any{"deleted": id})
}

// ---------------------------------------------------------------------------
// Collections
// ---------------------------------------------------------------------------

func (h *Handlers) CollectionsList(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Context().Value(contextKeyTenantID).(string)
	colls, err := h.app.Collections().ListCollections(r.Context(), tenantID)
	if err != nil {
		writeError(w, 500, "Failed to list collections")
		return
	}
	writeOK(w, colls)
}

func (h *Handlers) CollectionsCreate(w http.ResponseWriter, r *http.Request) {
	var coll collection.Collection
	if err := json.NewDecoder(r.Body).Decode(&coll); err != nil {
		writeError(w, 400, "Invalid collection data")
		return
	}
	tenantID := r.Context().Value(contextKeyTenantID).(string)
	coll.TenantID = tenantID

	if err := h.app.Collections().CreateCollection(r.Context(), &coll); err != nil {
		writeError(w, 500, "Failed to create collection: "+err.Error())
		return
	}

	adminID := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "collection.create", "_collections", coll.ID, map[string]any{"name": coll.Name}, r)
	writeJSON(w, 201, coll)
}

func (h *Handlers) CollectionsGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	coll, err := h.app.Collections().GetCollection(r.Context(), id)
	if err != nil {
		writeError(w, 404, "Collection not found")
		return
	}
	writeOK(w, coll)
}

func (h *Handlers) CollectionsUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var coll collection.Collection
	if err := json.NewDecoder(r.Body).Decode(&coll); err != nil {
		writeError(w, 400, "Invalid collection data")
		return
	}
	coll.ID = id
	if err := h.app.Collections().UpdateCollection(r.Context(), &coll); err != nil {
		writeError(w, 500, "Failed to update collection: "+err.Error())
		return
	}
	writeOK(w, coll)
}

func (h *Handlers) CollectionsDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.app.Collections().DeleteCollection(r.Context(), id); err != nil {
		writeError(w, 500, "Failed to delete collection: "+err.Error())
		return
	}
	writeOK(w, map[string]any{"deleted": id})
}

func (h *Handlers) CollectionsImport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Collections   []collection.Collection `json:"collections"`
		DeleteMissing bool                    `json:"deleteMissing"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "Invalid import data")
		return
	}
	tenantID := r.Context().Value(contextKeyTenantID).(string)
	count := 0
	for _, c := range body.Collections {
		c.TenantID = tenantID
		if err := h.app.Collections().CreateCollection(r.Context(), &c); err != nil {
			log.Warn().Err(err).Str("collection", c.Name).Msg("Import failed")
			continue
		}
		count++
	}
	writeOK(w, map[string]any{"imported": count})
}

// ---------------------------------------------------------------------------
// Records (with validation and rules)
// ---------------------------------------------------------------------------

func (h *Handlers) RecordsList(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	coll, err := h.app.Collections().GetCollectionByName(r.Context(), collName)
	if err != nil {
		writeError(w, 404, "Collection not found: "+collName)
		return
	}

	// Check list rule
	if !h.checkRule(r, coll.ListRule) {
		writeError(w, 403, "Access denied")
		return
	}

	params := query.ParseParams(r.URL.RawQuery)

	// Build query with proper access rule WHERE clause
	qb := query.NewBuilder(h.app.DB(), coll).WithParams(params)

	// Apply access rule via the real evaluator (returns SQL WHERE clause)
	ruleWhere := h.evaluateRuleWhere(r, coll.ListRule)
	if ruleWhere != "" {
		qb.WithAccessRule(ruleWhere)
	}

	records, total, err := qb.List(r.Context())
	if err != nil {
		writeError(w, 500, "Failed to list records: "+err.Error())
		return
	}

	if records == nil {
		records = []map[string]any{}
	}

	// Apply field picking if ?fields= specified
	if params.Fields != "" && params.Fields != "*" {
		records = pickFieldsFromRecords(records, params.Fields)
	}

	writeOK(w, map[string]any{
		"items":      records,
		"page":       params.Page,
		"perPage":    params.PerPage,
		"totalItems": total,
		"totalPages": maxInt(1, (total+params.PerPage-1)/params.PerPage),
	})
}

func (h *Handlers) RecordsCreate(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	coll, err := h.app.Collections().GetCollectionByName(r.Context(), collName)
	if err != nil {
		writeError(w, 404, "Collection not found: "+collName)
		return
	}

	if !h.checkRule(r, coll.CreateRule) {
		writeError(w, 403, "Access denied")
		return
	}

	var data map[string]any
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		writeError(w, 400, "Invalid record data")
		return
	}

	// Validate record fields
	if err := h.app.Collections().ValidateRecord(coll, data); err != nil {
		writeError(w, 400, "Validation failed: "+err.Error())
		return
	}

	record, err := h.app.Collections().CreateRecord(r.Context(), coll, data)
	if err != nil {
		writeError(w, 500, "Failed to create record: "+err.Error())
		return
	}

	if rid, ok := record["id"].(string); ok {
		h.app.Realtime().BroadcastRecord("create", collName, rid, record)
	}

	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	if rid, ok := record["id"].(string); ok {
		h.app.Auth().RecordAudit(r.Context(), adminID, "record.create", collName, rid, nil, r)
	}

	writeJSON(w, 201, record)
}

func (h *Handlers) RecordsGet(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	recordID := chi.URLParam(r, "recordId")

	coll, err := h.app.Collections().GetCollectionByName(r.Context(), collName)
	if err != nil {
		writeError(w, 404, "Collection not found")
		return
	}

	if !h.checkRule(r, coll.ViewRule) {
		writeError(w, 403, "Access denied")
		return
	}

	record, err := h.app.Collections().GetRecord(r.Context(), coll, recordID)
	if err != nil {
		writeError(w, 404, "Record not found")
		return
	}

	// Expand relations if ?expand= specified
	expand := r.URL.Query().Get("expand")
	if expand != "" {
		h.app.Collections().ExpandRecord(r.Context(), coll, record, expand)
	}

	// Pick fields if ?fields= specified
	fields := r.URL.Query().Get("fields")
	if fields != "" && fields != "*" {
		record = pickSingleRecordFields(record, fields)
	}

	writeOK(w, record)
}

func (h *Handlers) RecordsUpdate(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	recordID := chi.URLParam(r, "recordId")

	coll, err := h.app.Collections().GetCollectionByName(r.Context(), collName)
	if err != nil {
		writeError(w, 404, "Collection not found")
		return
	}

	if !h.checkRule(r, coll.UpdateRule) {
		writeError(w, 403, "Access denied")
		return
	}

	var data map[string]any
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		writeError(w, 400, "Invalid body")
		return
	}

	if err := h.app.Collections().ValidateRecord(coll, data); err != nil {
		writeError(w, 400, "Validation failed: "+err.Error())
		return
	}

	if err := h.app.Collections().UpdateRecord(r.Context(), coll, recordID, data); err != nil {
		writeError(w, 500, "Failed to update: "+err.Error())
		return
	}

	record, _ := h.app.Collections().GetRecord(r.Context(), coll, recordID)
	h.app.Realtime().BroadcastRecord("update", collName, recordID, record)
	writeOK(w, record)
}

func (h *Handlers) RecordsDelete(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	recordID := chi.URLParam(r, "recordId")

	coll, err := h.app.Collections().GetCollectionByName(r.Context(), collName)
	if err != nil {
		writeError(w, 404, "Collection not found")
		return
	}

	if !h.checkRule(r, coll.DeleteRule) {
		writeError(w, 403, "Access denied")
		return
	}

	if err := h.app.Collections().DeleteRecord(r.Context(), coll, recordID); err != nil {
		writeError(w, 500, "Failed to delete: "+err.Error())
		return
	}

	h.app.Realtime().BroadcastRecord("delete", collName, recordID, map[string]any{"id": recordID})
	writeOK(w, map[string]any{"deleted": recordID})
}

// ---------------------------------------------------------------------------
// Batch
// ---------------------------------------------------------------------------

func (h *Handlers) Batch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Requests []struct {
			Method  string            `json:"method"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
			Body    json.RawMessage   `json:"body"`
		} `json:"requests"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "Invalid batch request")
		return
	}
	if len(body.Requests) > 100 {
		writeError(w, 400, "Maximum 100 requests per batch")
		return
	}

	results := make([]map[string]any, len(body.Requests))
	for i, req := range body.Requests {
		subReq, err := http.NewRequestWithContext(r.Context(), req.Method, req.URL, nil)
		if err != nil {
			results[i] = map[string]any{"status": 400, "body": map[string]any{"message": err.Error()}}
			continue
		}
		for k, v := range req.Headers {
			subReq.Header.Set(k, v)
		}
		if auth := r.Header.Get("Authorization"); auth != "" {
			subReq.Header.Set("Authorization", auth)
		}
		if len(req.Body) > 0 {
			subReq.Body = io.NopCloser(bytes.NewReader(req.Body))
			subReq.ContentLength = int64(len(req.Body))
		}

		rr := httptest.NewRecorder()
		h.app.ServeHTTP(rr, subReq)

		var respBody any
		json.Unmarshal(rr.Body.Bytes(), &respBody)
		results[i] = map[string]any{
			"status": rr.Code,
			"body":   respBody,
		}
	}

	writeOK(w, results)
}

// ---------------------------------------------------------------------------
// Files
// ---------------------------------------------------------------------------

func (h *Handlers) FileDownload(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	recordID := chi.URLParam(r, "recordId")
	filename := chi.URLParam(r, "filename")
	path := fmt.Sprintf("%s/%s/%s", collName, recordID, filename)

	// Check thumb parameter
	thumb := r.URL.Query().Get("thumb")

	data, info, err := h.app.Storage().Download(r.Context(), path)
	if err != nil {
		writeError(w, 404, "File not found")
		return
	}

	// Serve thumb if requested
	if thumb != "" {
		thumbData, _, err := h.app.ImageProcessor().Thumbnail(bytes.NewReader(data), thumb, "")
		if err == nil {
			data = thumbData
		}
	}

	w.Header().Set("Content-Type", info.MimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, info.OriginalName))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Write(data)
}

func (h *Handlers) FileUpload(w http.ResponseWriter, r *http.Request) {
	coll := r.URL.Query().Get("collection")
	recordID := r.URL.Query().Get("record")
	if coll == "" || recordID == "" {
		writeError(w, 400, "collection and record query params required")
		return
	}

	r.ParseMultipartForm(50 << 20)
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, 400, "No file provided")
		return
	}
	defer file.Close()

	info, err := h.app.Storage().Upload(r.Context(), coll, recordID, file, header)
	if err != nil {
		writeError(w, 500, "Upload failed: "+err.Error())
		return
	}
	writeJSON(w, 201, info)
}

func (h *Handlers) FileDelete(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	recordID := chi.URLParam(r, "recordId")
	filename := chi.URLParam(r, "filename")
	path := fmt.Sprintf("%s/%s/%s", collName, recordID, filename)

	if err := h.app.Storage().Delete(r.Context(), path); err != nil {
		writeError(w, 500, "Delete failed: "+err.Error())
		return
	}
	writeOK(w, map[string]any{"deleted": path})
}

// ---------------------------------------------------------------------------
// RealtimeConnect handles WebSocket and SSE connections via the realtime hub.
// WebSocket: ws://host/api/v1/realtime
// SSE: GET /api/v1/sse (primary), POST /api/v1/realtime (subscription management)
func (h *Handlers) RealtimeConnect(w http.ResponseWriter, r *http.Request) {
	// POST is for SSE subscription management (clients send clientId + subscriptions)
	if r.Method == http.MethodPost {
		h.app.Realtime().HandleSSESubscription(w, r)
		return
	}

	// SSE is the primary transport (GET with Accept: text/event-stream)
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		h.app.Realtime().HandleSSE(w, r)
		return
	}

	// Fallback to WebSocket
	h.app.Realtime().HandleWebSocket(w, r)
}

// RealtimeSSE is a dedicated SSE endpoint (GET /api/v1/sse).
func (h *Handlers) RealtimeSSE(w http.ResponseWriter, r *http.Request) {
	h.app.Realtime().HandleSSE(w, r)
}

// ---------------------------------------------------------------------------
// OAuth
// ---------------------------------------------------------------------------

func (h *Handlers) OAuthRedirect(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	redirectURL := fmt.Sprintf("%s/api/v1/oauth/%s/callback", getBaseURL(r), provider)

	authURL, _, err := h.app.OAuth().GetAuthURL(provider, redirectURL)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

func (h *Handlers) OAuthCallback(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	redirectURL := fmt.Sprintf("%s/api/v1/oauth/%s/callback", getBaseURL(r), provider)

	if code == "" || state == "" {
		writeError(w, 400, "Missing code or state")
		return
	}

	userInfo, err := h.app.OAuth().ExchangeCode(r.Context(), provider, code, state, redirectURL)
	if err != nil {
		writeError(w, 500, "OAuth exchange failed: "+err.Error())
		return
	}

	admin, err := h.app.Auth().FindOrCreateByOAuth(r.Context(), userInfo)
	if err != nil {
		writeError(w, 500, "Failed to authenticate: "+err.Error())
		return
	}

	token, refreshToken, _ := h.app.Auth().GenerateTokens(admin.ID, admin.Email, admin.Role, admin.TenantID)
	h.app.Auth().RecordAudit(r.Context(), admin.ID, "auth.oauth."+provider, "_admins", admin.ID, nil, r)

	writeOK(w, map[string]any{
		"token":        token,
		"refreshToken": refreshToken,
		"admin":        sanitizeAdmin(admin),
		"oauth":        userInfo,
	})
}

// ---------------------------------------------------------------------------
// ACME
// ---------------------------------------------------------------------------

func (h *Handlers) ACMEDirectory(w http.ResponseWriter, r *http.Request) {
	baseURL := getBaseURL(r) + "/api/v1"
	writeOK(w, h.app.ACME().Directory(baseURL))
}

func (h *Handlers) ACMENewAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Contact     []string `json:"contact"`
		TermsAgreed bool     `json:"termsOfServiceAgreed"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	account, err := h.app.ACME().CreateAccount(r.Context(), body.Contact, body.TermsAgreed)
	if err != nil {
		writeError(w, 500, "Failed to create account")
		return
	}
	writeJSON(w, 201, account)
}

func (h *Handlers) ACMENewOrder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Identifiers []struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"identifiers"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	var idents []acme.Identifier
	for _, i := range body.Identifiers {
		idents = append(idents, acme.Identifier{Type: i.Type, Value: i.Value})
	}
	order, err := h.app.ACME().CreateOrder(r.Context(), idents)
	if err != nil {
		writeError(w, 500, "Failed to create order")
		return
	}
	writeJSON(w, 201, order)
}

func (h *Handlers) ACMEChallenge(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	challenge, err := h.app.ACME().ValidateChallenge(r.Context(), id)
	if err != nil {
		writeError(w, 404, "Challenge not found")
		return
	}
	writeOK(w, challenge)
}

func (h *Handlers) ACMEFinalize(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct{ CSR string `json:"csr"` }
	json.NewDecoder(r.Body).Decode(&body)
	cert, err := h.app.ACME().FinalizeOrder(r.Context(), id, []byte(body.CSR))
	if err != nil {
		writeError(w, 500, "Failed to finalize: "+err.Error())
		return
	}
	writeOK(w, cert)
}

func (h *Handlers) ACMECertificate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cert, err := h.app.ACME().GetCertificate(r.Context(), id)
	if err != nil {
		writeError(w, 404, "Certificate not found")
		return
	}
	w.Header().Set("Content-Type", "application/pem-certificate-chain")
	w.Write([]byte(cert.Raw))
}

func (h *Handlers) ACMERevoke(w http.ResponseWriter, r *http.Request) {
	var body struct{ CertificateID string `json:"certificateId"` }
	json.NewDecoder(r.Body).Decode(&body)
	if err := h.app.ACME().RevokeCertificate(r.Context(), body.CertificateID); err != nil {
		writeError(w, 500, "Revocation failed")
		return
	}
	writeOK(w, map[string]any{"revoked": true})
}

func (h *Handlers) ACMEHTTPChallenge(w http.ResponseWriter, r *http.Request) {
	h.app.ACME().HTTPChallengeHandler(w, r)
}

// ---------------------------------------------------------------------------
// Certificates
// ---------------------------------------------------------------------------

func (h *Handlers) CertificatesList(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Context().Value(contextKeyTenantID).(string)
	certs, err := h.app.ACME().ListCertificates(r.Context(), tenantID)
	if err != nil {
		writeError(w, 500, "Failed to list certificates")
		return
	}
	writeOK(w, certs)
}

func (h *Handlers) CertificatesIssue(w http.ResponseWriter, r *http.Request) {
	var body struct{ Domain string `json:"domain"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Domain == "" {
		writeError(w, 400, "Domain is required")
		return
	}
	cert, err := h.app.ACME().IssueForDomain(r.Context(), body.Domain)
	if err != nil {
		writeError(w, 500, "Failed to issue: "+err.Error())
		return
	}
	writeJSON(w, 201, cert)
}

func (h *Handlers) CertificatesRevoke(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.app.ACME().RevokeCertificate(r.Context(), id); err != nil {
		writeError(w, 500, "Revocation failed")
		return
	}
	writeOK(w, map[string]any{"revoked": id})
}

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

func (h *Handlers) SettingsGet(w http.ResponseWriter, r *http.Request) {
	settings, err := h.app.Settings().Get(r.Context())
	if err != nil {
		writeError(w, 500, "Failed to load settings")
		return
	}
	// Mask sensitive fields
	settings.SMTP.Password = ""
	settings.S3.SecretKey = ""
	writeOK(w, settings)
}

func (h *Handlers) SettingsUpdate(w http.ResponseWriter, r *http.Request) {
	settings, err := h.app.Settings().Get(r.Context())
	if err != nil {
		writeError(w, 500, "Failed to load settings")
		return
	}

	if err := json.NewDecoder(r.Body).Decode(settings); err != nil {
		writeError(w, 400, "Invalid body")
		return
	}

	if err := h.app.Settings().Save(r.Context(), settings); err != nil {
		writeError(w, 500, "Failed to save settings")
		return
	}

	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "settings.update", "_settings", "", nil, r)
	writeOK(w, map[string]any{"message": "Settings saved"})
}

// ---------------------------------------------------------------------------
// Logs
// ---------------------------------------------------------------------------

func (h *Handlers) LogsList(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page <= 0 { page = 1 }
	perPage, _ := strconv.Atoi(r.URL.Query().Get("perPage"))
	if perPage <= 0 { perPage = 50 }

	params := settings.LogQueryParams{
		Action:   r.URL.Query().Get("action"),
		Resource: r.URL.Query().Get("resource"),
		DateFrom: r.URL.Query().Get("date_from"),
		DateTo:   r.URL.Query().Get("date_to"),
		Page:     page,
		PerPage:  perPage,
	}

	entries, total, err := h.app.Settings().ListLogs(r.Context(), params)
	if err != nil {
		writeError(w, 500, "Failed to fetch logs")
		return
	}

	writeOK(w, map[string]any{
		"page":       page,
		"perPage":    perPage,
		"totalItems": total,
		"totalPages": maxInt(1, (total+perPage-1)/perPage),
		"items":      entries,
	})
}

// ---------------------------------------------------------------------------
// API Keys
// ---------------------------------------------------------------------------

func (h *Handlers) APIKeysList(w http.ResponseWriter, r *http.Request) {
	adminID := r.Context().Value(contextKeyAdminID).(string)
	rows, err := h.app.DB().Pool.Query(r.Context(),
		`SELECT id, admin_id, name, prefix, created_at FROM _api_keys WHERE admin_id = $1 ORDER BY created_at DESC`, adminID)
	if err != nil {
		writeError(w, 500, "Failed to list API keys")
		return
	}
	defer rows.Close()

	var keys []map[string]any
	for rows.Next() {
		var id, aID, name, prefix string
		var createdAt time.Time
		rows.Scan(&id, &aID, &name, &prefix, &createdAt)
		keys = append(keys, map[string]any{
			"id": id, "name": name, "prefix": prefix, "created_at": createdAt,
		})
	}
	writeOK(w, keys)
}

func (h *Handlers) APIKeysCreate(w http.ResponseWriter, r *http.Request) {
	var body struct{ Name string `json:"name"` }
	json.NewDecoder(r.Body).Decode(&body)
	if body.Name == "" {
		body.Name = "API Key " + time.Now().Format("2006-01-02")
	}
	adminID := r.Context().Value(contextKeyAdminID).(string)
	key, apiKey, err := h.app.Auth().GenerateAPIKey(r.Context(), adminID, body.Name, nil)
	if err != nil {
		writeError(w, 500, "Failed to create API key")
		return
	}
	writeJSON(w, 201, map[string]any{"key": key, "apiKey": apiKey})
}

func (h *Handlers) APIKeysDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, err := h.app.DB().Pool.Exec(r.Context(), "DELETE FROM _api_keys WHERE id = $1", id)
	if err != nil {
		writeError(w, 500, "Failed to delete API key")
		return
	}
	writeOK(w, map[string]any{"deleted": id})
}

// ---------------------------------------------------------------------------
// Backups
// ---------------------------------------------------------------------------

func (h *Handlers) BackupsList(w http.ResponseWriter, r *http.Request) {
	backups, err := h.app.Backup().ListBackups()
	if err != nil {
		writeError(w, 500, "Failed to list backups")
		return
	}
	if backups == nil {
		backups = []*storage.BackupInfo{}
	}
	writeOK(w, backups)
}

func (h *Handlers) BackupsCreate(w http.ResponseWriter, r *http.Request) {
	var body struct{ Name string `json:"name"` }
	json.NewDecoder(r.Body).Decode(&body)

	info, err := h.app.Backup().CreateBackup(r.Context(), body.Name, false)
	if err != nil {
		writeError(w, 500, "Backup failed: "+err.Error())
		return
	}

	if h.app.Mailer() != nil {
		// Notify admins about backup completion
	}

	writeJSON(w, 201, info)
}

func (h *Handlers) BackupsRestore(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	backups, _ := h.app.Backup().ListBackups()
	var backupID string
	for _, b := range backups {
		if b.Name == name || b.ID == name {
			backupID = b.ID
			break
		}
	}
	if backupID == "" {
		writeError(w, 404, "Backup not found")
		return
	}

	if err := h.app.Backup().RestoreBackup(r.Context(), backupID); err != nil {
		writeError(w, 500, "Restore failed: "+err.Error())
		return
	}

	writeOK(w, map[string]any{"message": "Restore completed — restart the server to apply changes"})
}

func (h *Handlers) BackupsDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.app.Backup().DeleteBackup(name); err != nil {
		writeError(w, 500, "Delete failed: "+err.Error())
		return
	}
	writeOK(w, map[string]any{"deleted": name})
}

func (h *Handlers) BackupsDownload(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	path, err := h.app.Backup().GetBackupPath(name)
	if err != nil {
		writeError(w, 404, "Backup not found")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, name))
	http.ServeFile(w, r, path)
}

// ---------------------------------------------------------------------------
// Search (Full-Text Search)
// ---------------------------------------------------------------------------

func (h *Handlers) SearchRecords(w http.ResponseWriter, r *http.Request) {
	collection := chi.URLParam(r, "collection")
	var body struct {
		Query     string `json:"query"`
		Language  string `json:"language"`
		Page      int    `json:"page"`
		PerPage   int    `json:"per_page"`
		Highlight bool   `json:"highlight"`
		Rank      bool   `json:"rank"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.Query == "" {
		writeError(w, 400, "query is required")
		return
	}
	if body.PerPage <= 0 {
		body.PerPage = 30
	}
	if body.Page <= 0 {
		body.Page = 1
	}

	results, total, err := h.app.Search().Search(r.Context(), search.SearchQuery{
		Collection: collection,
		Query:      body.Query,
		Language:   body.Language,
		Page:       body.Page,
		PerPage:    body.PerPage,
		Highlight:  body.Highlight,
		Rank:       body.Rank,
	})
	if err != nil {
		writeError(w, 500, "Search failed: "+err.Error())
		return
	}

	writeOK(w, map[string]any{
		"items":      results,
		"page":       body.Page,
		"perPage":    body.PerPage,
		"totalItems": total,
		"totalPages": maxInt(1, int((total+int64(body.PerPage)-1)/int64(body.PerPage))),
	})
}

func (h *Handlers) FTSCreateIndex(w http.ResponseWriter, r *http.Request) {
	collection := chi.URLParam(r, "collection")
	var body struct {
		Fields   []string `json:"fields"`
		Language string   `json:"language"`
		Weight   string   `json:"weight"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if len(body.Fields) == 0 {
		writeError(w, 400, "fields is required")
		return
	}
	config := search.IndexConfig{
		Collection: collection,
		Fields:     body.Fields,
		Language:   body.Language,
		Weight:     body.Weight,
	}
	if err := h.app.Search().CreateIndex(r.Context(), config); err != nil {
		writeError(w, 500, "Failed to create index: "+err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"message": "FTS index created", "collection": collection})
}

func (h *Handlers) FTSRemoveIndex(w http.ResponseWriter, r *http.Request) {
	collection := chi.URLParam(r, "collection")
	if err := h.app.Search().RemoveIndex(r.Context(), collection); err != nil {
		writeError(w, 500, "Failed to remove index: "+err.Error())
		return
	}
	writeOK(w, map[string]any{"message": "FTS index removed", "collection": collection})
}

// ---------------------------------------------------------------------------
// Batch Record Operations (Transactional)
// ---------------------------------------------------------------------------

func (h *Handlers) BatchRecords(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	coll, err := h.app.Collections().GetCollectionByName(r.Context(), collName)
	if err != nil {
		writeError(w, 404, "Collection not found: "+collName)
		return
	}

	var body struct {
		Creates []map[string]any `json:"creates"`
		Updates map[string]map[string]any `json:"updates"`
		Deletes []string `json:"deletes"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	var created []collection.Record
	var updated, deleted int

	if len(body.Creates) > 0 {
		created, err = h.app.Collections().CreateBatch(r.Context(), coll, body.Creates)
		if err != nil {
			writeError(w, 500, "Batch create failed: "+err.Error())
			return
		}
		for _, rec := range created {
			if rid, ok := rec["id"].(string); ok {
				h.app.Realtime().BroadcastRecord("create", collName, rid, rec)
			}
		}
	}

	if len(body.Updates) > 0 {
		if err := h.app.Collections().UpdateBatch(r.Context(), coll, body.Updates); err != nil {
			writeError(w, 500, "Batch update failed: "+err.Error())
			return
		}
		updated = len(body.Updates)
	}

	if len(body.Deletes) > 0 {
		if err := h.app.Collections().DeleteBatch(r.Context(), coll, body.Deletes); err != nil {
			writeError(w, 500, "Batch delete failed: "+err.Error())
			return
		}
		deleted = len(body.Deletes)
	}

	writeOK(w, map[string]any{
		"created": created,
		"updated": updated,
		"deleted": deleted,
	})
}

// ---------------------------------------------------------------------------
// JS Plugins
// ---------------------------------------------------------------------------

func (h *Handlers) JSPluginsList(w http.ResponseWriter, r *http.Request) {
	plugins := h.app.JSRuntime().ListPlugins()
	writeOK(w, plugins)
}

func (h *Handlers) JSPluginCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Script   string `json:"script"`
		Priority int    `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "Invalid body")
		return
	}
	if body.Name == "" || body.Script == "" {
		writeError(w, 400, "name and script are required")
		return
	}
	if body.ID == "" {
		body.ID = "js-" + generateID()
	}

	plugin, err := h.app.JSRuntime().LoadPlugin(body.ID, body.Name, body.Script, body.Priority)
	if err != nil {
		writeError(w, 400, "Failed to load plugin: "+err.Error())
		return
	}

	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "jsplugin.create", "_js_plugins", body.ID, nil, r)

	writeJSON(w, 201, map[string]any{
		"id":   plugin.ID,
		"name": plugin.Name,
	})
}

func (h *Handlers) JSPluginGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	script, err := h.app.JSRuntime().GetPluginScript(id)
	if err != nil {
		writeError(w, 404, "Plugin not found")
		return
	}
	writeOK(w, map[string]any{"id": id, "script": script})
}

func (h *Handlers) JSPluginExecute(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct{ Expression string `json:"expression"` }
	json.NewDecoder(r.Body).Decode(&body)
	if body.Expression == "" {
		writeError(w, 400, "expression is required")
		return
	}

	result, err := h.app.JSRuntime().Execute(id, body.Expression)
	if err != nil {
		writeError(w, 500, "Execution failed: "+err.Error())
		return
	}
	writeOK(w, map[string]any{"result": result.Export()})
}

func (h *Handlers) JSPluginDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.app.JSRuntime().UnloadPlugin(id)
	writeOK(w, map[string]any{"deleted": id})
}

// ---------------------------------------------------------------------------
// Cron Jobs
// ---------------------------------------------------------------------------

func (h *Handlers) JobsList(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.app.Jobs().ListJobs(r.Context())
	if err != nil {
		writeError(w, 500, "Failed to list jobs")
		return
	}
	writeOK(w, jobs)
}

func (h *Handlers) JobsCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string         `json:"name"`
		CronExpr string         `json:"cron_expr"`
		Handler  string         `json:"handler"`
		Data     map[string]any `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "Invalid body")
		return
	}
	if body.Name == "" || body.CronExpr == "" {
		writeError(w, 400, "name and cron_expr are required")
		return
	}
	if err := h.app.Jobs().ValidateCron(body.CronExpr); err != nil {
		writeError(w, 400, "Invalid cron expression: "+err.Error())
		return
	}

	job, err := h.app.Jobs().AddJob(r.Context(), body.Name, body.CronExpr, body.Handler, body.Data)
	if err != nil {
		writeError(w, 500, "Failed to create job: "+err.Error())
		return
	}
	writeJSON(w, 201, job)
}

func (h *Handlers) JobsRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.app.Jobs().RunJob(r.Context(), id); err != nil {
		writeError(w, 500, "Job run failed: "+err.Error())
		return
	}
	writeOK(w, map[string]any{"message": "Job triggered", "id": id})
}

func (h *Handlers) JobsDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.app.Jobs().DeleteJob(r.Context(), id); err != nil {
		writeError(w, 500, "Delete failed: "+err.Error())
		return
	}
	writeOK(w, map[string]any{"deleted": id})
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
		"tenant_id":     a.TenantID,
		"email":         a.Email,
		"avatar":        a.Avatar,
		"role":          a.Role,
		"last_login_at": a.LastLoginAt,
		"created_at":    a.CreatedAt,
		"updated_at":    a.UpdatedAt,
	}
}

func realtimeMsg(event string, data any) realtime.RealtimeMessage {
	payload, _ := json.Marshal(data)
	return realtime.RealtimeMessage{
		Event:     event,
		Timestamp: time.Now().UnixMilli(),
		Data:      payload,
	}
}

// checkRule evaluates a collection access rule against the request context.
// Uses the real RuleEvaluator that parses filter expressions and resolves
// @request.auth.* macros instead of the previous placeholder.
func (h *Handlers) checkRule(r *http.Request, rule string) bool {
	if rule == "" {
		return true
	}
	rc := NewRuleContext(r)
	// Admins always bypass rules
	if rc.IsAdmin {
		return true
	}
	return h.rules.EvaluateRuleBool(r.Context(), rule, rc)
}

// evaluateRuleWhere returns a SQL WHERE clause from an access rule.
// Use this for list/view rules to filter rows the user is allowed to see.
func (h *Handlers) evaluateRuleWhere(r *http.Request, rule string) string {
	if rule == "" {
		return ""
	}
	rc := NewRuleContext(r)
	if rc.IsAdmin {
		return ""
	}
	return h.rules.EvaluateRuleWhere(r.Context(), rule, rc)
}

// ---------------------------------------------------------------------------
// Record Auth (end-user authentication for auth collections)
// ---------------------------------------------------------------------------

// RecordAuthMethods returns the available authentication methods for an auth collection.
// PocketBase-compatible: GET /api/v1/collections/{collection}/auth-methods
func (h *Handlers) RecordAuthMethods(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	coll, err := h.app.Collections().GetCollectionByName(r.Context(), collName)
	if err != nil {
		writeError(w, 404, "Collection not found")
		return
	}

	// Build auth methods response (PocketBase-compatible format)
	methods := map[string]any{
		"usernamePassword": false,
		"emailPassword":    false,
		"onlyVerified":     false,
		"oauth2": map[string]any{
			"enabled":    false,
			"providers":  []any{},
		},
		"mfa": map[string]any{
			"enabled":   false,
			"duration":  0,
		},
		"otp": map[string]any{
			"enabled":  false,
			"duration": 0,
		},
	}

	// Check schema for auth fields
	for _, field := range coll.Schema {
		switch field.Type {
		case "password":
			methods["emailPassword"] = true
			methods["usernamePassword"] = true
		case "email":
			methods["otp"] = map[string]any{
				"enabled":  true,
				"duration": 300,
			}
		}
	}

	// Check if OAuth2 is configured for this collection
	if h.app.OAuth() != nil {
		providers := h.app.OAuth().GetAvailableProviders()
		if len(providers) > 0 {
			methods["oauth2"] = map[string]any{
				"enabled":   true,
				"providers": providers,
			}
		}
	}

	// Check MFA availability
	if h.app.MFA() != nil {
		methods["mfa"] = map[string]any{
			"enabled":  true,
			"duration": 30,
		}
	}

	// Collection options override
	if coll.Options != nil {
		if o, ok := coll.Options["onlyVerified"].(bool); ok {
			methods["onlyVerified"] = o
		}
	}

	writeOK(w, methods)
}

// RecordAuthPassword authenticates a record (end user) with email/password.
func (h *Handlers) RecordAuthPassword(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	var body struct {
		Identity string `json:"identity"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if body.Identity == "" || body.Password == "" {
		writeError(w, 400, "identity and password are required")
		return
	}

	result, recordID, err := h.recordAuth.PasswordAuth(r.Context(), collName, body.Identity, body.Password)
	if err != nil {
		writeError(w, 401, "Invalid credentials")
		return
	}

	h.app.Auth().RecordAudit(r.Context(), recordID, "auth.record.password", collName, recordID, nil, r)
	writeOK(w, result)
}

// RecordAuthOTPRequest sends an OTP code to a record's email.
func (h *Handlers) RecordAuthOTPRequest(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	var body struct{ Email string `json:"email"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" {
		writeError(w, 400, "email is required")
		return
	}

	otpID, code, err := h.recordAuth.OTPRequest(r.Context(), collName, body.Email)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}

	// Send OTP via email if mailer is configured
	if h.app.Mailer() != nil {
		h.app.Mailer().SendOTP(body.Email, code)
	}

	writeOK(w, map[string]any{"otpId": otpID, "message": "OTP sent"})
}

// RecordAuthOTPVerify verifies an OTP and returns auth tokens.
func (h *Handlers) RecordAuthOTPVerify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OTPID string `json:"otpId"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "Invalid body")
		return
	}

	result, err := h.recordAuth.OTPVerify(r.Context(), body.OTPID, body.Code)
	if err != nil {
		writeError(w, 401, err.Error())
		return
	}

	writeOK(w, result)
}

// RecordAuthOAuth2Redirect redirects to the OAuth2 provider for record auth.
func (h *Handlers) RecordAuthOAuth2Redirect(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	provider := chi.URLParam(r, "provider")
	redirectURL := fmt.Sprintf("%s/api/v1/collections/%s/auth/oauth2/%s/callback", getBaseURL(r), collName, provider)

	authURL, _, err := h.app.OAuth().GetAuthURL(provider, redirectURL)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

// RecordAuthOAuth2Callback handles the OAuth2 callback for record auth.
func (h *Handlers) RecordAuthOAuth2Callback(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	provider := chi.URLParam(r, "provider")
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	redirectURL := fmt.Sprintf("%s/api/v1/collections/%s/auth/oauth2/%s/callback", getBaseURL(r), collName, provider)

	if code == "" || state == "" {
		writeError(w, 400, "Missing code or state")
		return
	}

	userInfo, err := h.app.OAuth().ExchangeCode(r.Context(), provider, code, state, redirectURL)
	if err != nil {
		writeError(w, 500, "OAuth exchange failed: "+err.Error())
		return
	}

	result, err := h.recordAuth.OAuth2Auth(r.Context(), collName, provider, userInfo)
	if err != nil {
		writeError(w, 500, "OAuth auth failed: "+err.Error())
		return
	}

	writeOK(w, result)
}

// RecordAuthRefresh refreshes a record auth token.
func (h *Handlers) RecordAuthRefresh(w http.ResponseWriter, r *http.Request) {
	var body struct{ RefreshToken string `json:"refreshToken"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RefreshToken == "" {
		writeError(w, 400, "refreshToken is required")
		return
	}

	token, refreshToken, err := h.recordAuth.RefreshRecordToken(r.Context(), body.RefreshToken)
	if err != nil {
		writeError(w, 401, "Invalid or expired refresh token")
		return
	}

	writeOK(w, map[string]any{"token": token, "refreshToken": refreshToken})
}

// RecordAuthImpersonate allows an admin to impersonate a record (generate tokens as that record).
func (h *Handlers) RecordAuthImpersonate(w http.ResponseWriter, r *http.Request) {
	adminID := r.Context().Value(contextKeyAdminID).(string)
	collName := chi.URLParam(r, "collection")
	recordID := chi.URLParam(r, "recordId")

	result, err := h.recordAuth.Impersonate(r.Context(), collName, recordID)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}

	h.app.Auth().RecordAudit(r.Context(), adminID, "auth.record.impersonate", collName, recordID, nil, r)
	writeOK(w, result)
}

// RecordPasswordResetRequest creates a password reset token for a record.
func (h *Handlers) RecordPasswordResetRequest(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	var body struct{ Email string `json:"email"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" {
		writeError(w, 400, "email is required")
		return
	}

	token, err := h.recordAuth.RequestRecordPasswordReset(r.Context(), collName, body.Email)
	if err != nil {
		// Don't reveal whether email exists
		writeOK(w, map[string]any{"message": "If the email exists, a reset email has been sent"})
		return
	}

	if h.app.Mailer() != nil {
		h.app.Mailer().SendPasswordReset(body.Email, token)
	}

	writeOK(w, map[string]any{"message": "If the email exists, a reset email has been sent"})
}

// RecordPasswordResetConfirm confirms a password reset for a record.
func (h *Handlers) RecordPasswordResetConfirm(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token       string `json:"token"`
		NewPassword string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "Invalid body")
		return
	}
	if body.Token == "" || len(body.NewPassword) < 8 {
		writeError(w, 400, "token and password (min 8 chars) required")
		return
	}

	if err := h.recordAuth.ConfirmRecordPasswordReset(r.Context(), body.Token, body.NewPassword); err != nil {
		writeError(w, 400, err.Error())
		return
	}

	writeOK(w, map[string]any{"message": "Password reset successfully"})
}

// RecordVerificationRequest sends a verification email for a record.
func (h *Handlers) RecordVerificationRequest(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	var body struct{ Email string `json:"email"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" {
		writeError(w, 400, "email is required")
		return
	}

	// Find record by email and send verification
	coll, err := h.app.Collections().GetCollectionByName(r.Context(), collName)
	if err != nil {
		writeError(w, 404, "Collection not found")
		return
	}

	records, _, err := h.app.Collections().ListRecords(r.Context(), coll,
		fmt.Sprintf("email = %q", body.Email), "", 1, 1)
	if err != nil || len(records) == 0 {
		writeOK(w, map[string]any{"message": "If the email exists, a verification has been sent"})
		return
	}

	recordID, _ := records[0]["id"].(string)
	token, err := h.recordAuth.RequestRecordVerification(r.Context(), collName, recordID)
	if err != nil {
		writeError(w, 500, "Failed to send verification")
		return
	}

	if h.app.Mailer() != nil {
		h.app.Mailer().SendVerification(body.Email, token)
	}

	writeOK(w, map[string]any{"message": "Verification email sent"})
}

// RecordVerificationConfirm confirms a record's email verification.
func (h *Handlers) RecordVerificationConfirm(w http.ResponseWriter, r *http.Request) {
	var body struct{ Token string `json:"token"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Token == "" {
		writeError(w, 400, "token is required")
		return
	}

	if err := h.recordAuth.ConfirmRecordVerification(r.Context(), body.Token); err != nil {
		writeError(w, 400, err.Error())
		return
	}

	writeOK(w, map[string]any{"message": "Email verified successfully"})
}

// RecordEmailChangeRequest creates an email change token for a record.
func (h *Handlers) RecordEmailChangeRequest(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	var body struct {
		NewEmail string `json:"newEmail"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.NewEmail == "" {
		writeError(w, 400, "newEmail is required")
		return
	}

	// Get record ID from auth context
	recordID := getRecordIDFromContext(r)
	if recordID == "" {
		writeError(w, 401, "Authentication required")
		return
	}

	token, err := h.recordAuth.RequestRecordEmailChange(r.Context(), collName, recordID, body.NewEmail)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}

	if h.app.Mailer() != nil {
		h.app.Mailer().SendEmailChange(body.NewEmail, token)
	}

	writeOK(w, map[string]any{"message": "Confirmation email sent to new address"})
}

// RecordEmailChangeConfirm confirms an email change for a record.
func (h *Handlers) RecordEmailChangeConfirm(w http.ResponseWriter, r *http.Request) {
	var body struct{ Token string `json:"token"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Token == "" {
		writeError(w, 400, "token is required")
		return
	}

	if err := h.recordAuth.ConfirmRecordEmailChange(r.Context(), body.Token); err != nil {
		writeError(w, 400, err.Error())
		return
	}

	writeOK(w, map[string]any{"message": "Email changed successfully"})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// getRecordIDFromContext extracts the record ID from the request context
// (set by middleware from the record auth JWT token).
func getRecordIDFromContext(r *http.Request) string {
	if rid, ok := r.Context().Value("record_id").(string); ok {
		return rid
	}
	return ""
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func generateID() string {
	return fmt.Sprintf("%x", time.Now().UnixNano())
}

// Field picking: reduce records to only specified fields (PocketBase-like ?fields=).
func pickFieldsFromRecords(records []map[string]any, fields string) []map[string]any {
	if fields == "*" || fields == "" {
		return records
	}
	fieldSet := make(map[string]bool)
	for _, f := range strings.Split(fields, ",") {
		f = strings.TrimSpace(f)
		if f != "" {
			fieldSet[f] = true
		}
	}
	// Always include id
	fieldSet["id"] = true
	fieldSet["@expand"] = true

	result := make([]map[string]any, len(records))
	for i, record := range records {
		filtered := make(map[string]any)
		for k, v := range record {
			if fieldSet[k] {
				filtered[k] = v
			}
		}
		result[i] = filtered
	}
	return result
}

func pickSingleRecordFields(record map[string]any, fields string) map[string]any {
	result := pickFieldsFromRecords([]map[string]any{record}, fields)
	return result[0]
}
