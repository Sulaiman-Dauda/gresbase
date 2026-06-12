package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/gresbase/gresbase/internal/auth"
	"github.com/gresbase/gresbase/internal/events"
	"github.com/gresbase/gresbase/internal/forms"
)

// ---------------------------------------------------------------------------
// Record Auth (end-user authentication for auth collections)
// ---------------------------------------------------------------------------

// OAuth2ProvidersMeta returns the global PocketBase-style OAuth provider catalog.
func (h *Handlers) OAuth2ProvidersMeta(w http.ResponseWriter, r *http.Request) {
	providers := []auth.OAuthProviderCatalogItem{}
	if h.app.OAuth() != nil {
		providers = h.app.OAuth().ProviderCatalog(false)
	}
	writeOK(w, providers)
}

// RecordAuthMethods returns the available authentication methods for an auth collection.
// PocketBase-compatible: GET /api/v1/collections/{collection}/auth-methods
func (h *Handlers) RecordAuthMethods(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	coll, err := h.app.Collections().GetCollectionByName(r.Context(), collName)
	if err != nil {
		writeError(w, 404, "Collection not found")
		return
	}

	event := &events.RecordAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "methods", CollectionID: coll.ID, CollectionName: coll.Name}

	// Build auth methods response (PocketBase-compatible format)
	methods := map[string]any{
		"usernamePassword": false,
		"emailPassword":    false,
		"onlyVerified":     false,
		"oauth2": map[string]any{
			"enabled":   false,
			"providers": []any{},
		},
		"mfa": map[string]any{
			"enabled":  false,
			"duration": 0,
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
		redirectURL := fmt.Sprintf("%s/api/oauth2-redirect", getBaseURL(r))
		providers := h.app.OAuth().AuthMethodProviders(r.Context(), redirectURL)
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

	if err := h.app.OnRecordAuthRequest().Trigger(event, func(e events.Event) error { return nil }); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeOK(w, methods)
}

// RecordAuthPassword authenticates a record (end user) with email/password.
func (h *Handlers) RecordAuthPassword(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	var form forms.RecordAuthPasswordForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.RecordAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "password", CollectionName: collName, Identity: form.Identity}
	var result *auth.RecordAuthResult
	var recordID string
	if err := h.app.OnRecordAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		result, recordID, err = h.recordAuth.PasswordAuth(r.Context(), collName, form.Identity, form.Password)
		return err
	}); err != nil {
		writeError(w, 401, "Invalid credentials")
		return
	}

	_ = h.app.OnAuthLogin().Trigger(&events.AuthEvent{App: h.app, UserID: recordID, Provider: "record:password", Token: result.Token, RefreshToken: result.RefreshToken}, func(e events.Event) error { return e.Next() })
	h.app.Auth().RecordAudit(r.Context(), recordID, "auth.record.password", collName, recordID, nil, r)
	writeOK(w, result)
}

// RecordAuthAnonymous signs in as a freshly created anonymous record. Only
// available when the auth collection sets the allowAnonymous option.
func (h *Handlers) RecordAuthAnonymous(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")

	event := &events.RecordAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "anonymous", CollectionName: collName}
	var result *auth.RecordAuthResult
	var recordID string
	if err := h.app.OnRecordAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		result, recordID, err = h.recordAuth.AnonymousAuth(r.Context(), collName)
		return err
	}); err != nil {
		writeError(w, 403, "Anonymous authentication is not available for this collection")
		return
	}

	_ = h.app.OnAuthLogin().Trigger(&events.AuthEvent{App: h.app, UserID: recordID, Provider: "record:anonymous", Token: result.Token, RefreshToken: result.RefreshToken}, func(e events.Event) error { return e.Next() })
	h.app.Auth().RecordAudit(r.Context(), recordID, "auth.record.anonymous", collName, recordID, nil, r)
	writeOK(w, result)
}

// RecordAuthOTPRequest sends an OTP code to a record's email.
func (h *Handlers) RecordAuthOTPRequest(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	var form forms.EmailForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.RecordAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "otp_request", CollectionName: collName, Email: form.Email}
	var otpID, code string
	if err := h.app.OnRecordAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		otpID, code, err = h.recordAuth.OTPRequest(r.Context(), collName, form.Email)
		return err
	}); err != nil {
		writeError(w, 400, err.Error())
		return
	}

	if h.app.Mailer() != nil {
		h.app.Mailer().SendOTP(form.Email, code)
	}

	writeOK(w, map[string]any{"otpId": otpID, "message": "OTP sent"})
}

// RecordAuthOTPVerify verifies an OTP and returns auth tokens.
func (h *Handlers) RecordAuthOTPVerify(w http.ResponseWriter, r *http.Request) {
	var form forms.OTPVerifyForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.RecordAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "otp_verify", Provider: "otp", OTPID: form.OTPID, Code: form.Code}
	var result *auth.RecordAuthResult
	if err := h.app.OnRecordAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		result, err = h.recordAuth.OTPVerify(r.Context(), form.OTPID, form.Code)
		return err
	}); err != nil {
		writeError(w, 401, err.Error())
		return
	}

	if recordID := recordAuthResultID(result); recordID != "" {
		_ = h.app.OnAuthLogin().Trigger(&events.AuthEvent{App: h.app, UserID: recordID, Provider: "record:otp", Token: result.Token, RefreshToken: result.RefreshToken}, func(e events.Event) error { return e.Next() })
	}
	writeOK(w, result)
}

// RecordAuthOAuth2Redirect redirects to the OAuth2 provider for record auth.
func (h *Handlers) RecordAuthOAuth2Redirect(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	provider := chi.URLParam(r, "provider")
	redirectURL := fmt.Sprintf("%s/api/v1/collections/%s/auth/oauth2/%s/callback", getBaseURL(r), collName, provider)

	event := &events.RecordAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "oauth_redirect", CollectionName: collName, Provider: provider}
	var authURL string
	if err := h.app.OnRecordAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		authURL, _, err = h.app.OAuth().GetAuthURLContext(r.Context(), provider, redirectURL)
		return err
	}); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

func (h *Handlers) recordOAuth2ExchangeAndAuth(r *http.Request, action, collName, provider, code, state, redirectURL, codeVerifier string) (*auth.OAuthUserInfo, *auth.RecordAuthResult, error) {
	event := &events.RecordAuthRequestEvent{
		App:            h.app,
		Request:        r,
		Info:           toEventRequestInfoWithBody(r, map[string]any{"provider": provider, "code": code, "state": state, "redirectURL": redirectURL}),
		Action:         action,
		CollectionName: collName,
		Provider:       provider,
		Code:           code,
	}
	var userInfo *auth.OAuthUserInfo
	var result *auth.RecordAuthResult
	if err := h.app.OnRecordAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		userInfo, err = h.app.OAuth().ExchangeCodeWithVerifier(r.Context(), provider, code, state, redirectURL, codeVerifier)
		if err != nil {
			return err
		}
		result, err = h.recordAuth.OAuth2Auth(r.Context(), collName, provider, userInfo)
		return err
	}); err != nil {
		return nil, nil, err
	}
	return userInfo, result, nil
}

// RecordAuthOAuth2 handles PocketBase-style POST auth-with-oauth2 requests.
func (h *Handlers) RecordAuthOAuth2(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	var form forms.RecordAuthOAuth2Form
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	_, result, err := h.recordOAuth2ExchangeAndAuth(r, "oauth_auth", collName, form.Provider, form.Code, form.State, form.RedirectURL, form.CodeVerifier)
	if err != nil {
		writeError(w, 400, "OAuth auth failed: "+err.Error())
		return
	}
	if recordID := recordAuthResultID(result); recordID != "" {
		_ = h.app.OnAuthLogin().Trigger(&events.AuthEvent{App: h.app, UserID: recordID, Provider: "record:oauth:" + form.Provider, Token: result.Token, RefreshToken: result.RefreshToken}, func(e events.Event) error { return e.Next() })
	}
	writeOK(w, result)
}

// RecordAuthOAuth2Callback handles the OAuth2 callback for record auth.
func (h *Handlers) RecordAuthOAuth2Callback(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	provider := chi.URLParam(r, "provider")
	form := forms.OAuthCallbackForm{Code: r.URL.Query().Get("code"), State: r.URL.Query().Get("state")}
	redirectURL := fmt.Sprintf("%s/api/v1/collections/%s/auth/oauth2/%s/callback", getBaseURL(r), collName, provider)

	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	_, result, err := h.recordOAuth2ExchangeAndAuth(r, "oauth_callback", collName, provider, form.Code, form.State, redirectURL, "")
	if err != nil {
		writeInternalError(w, "OAuth auth failed", err)
		return
	}

	if recordID := recordAuthResultID(result); recordID != "" {
		_ = h.app.OnAuthLogin().Trigger(&events.AuthEvent{App: h.app, UserID: recordID, Provider: "record:oauth:" + provider, Token: result.Token, RefreshToken: result.RefreshToken}, func(e events.Event) error { return e.Next() })
	}
	writeOK(w, result)
}

// RecordAuthRefresh refreshes a record auth token.
func (h *Handlers) RecordAuthRefresh(w http.ResponseWriter, r *http.Request) {
	var form forms.RefreshTokenForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.RecordAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "refresh", RefreshToken: form.RefreshToken}
	var token, refreshToken string
	if err := h.app.OnRecordAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		token, refreshToken, err = h.recordAuth.RefreshRecordToken(r.Context(), form.RefreshToken)
		return err
	}); err != nil {
		writeError(w, 401, "Invalid or expired refresh token")
		return
	}

	if claims, err := h.recordAuth.ValidateRecordToken(token); err == nil {
		_ = h.app.OnAuthRefresh().Trigger(&events.AuthEvent{App: h.app, UserID: claims.RecordID, Provider: "record:refresh", Token: token, RefreshToken: refreshToken}, func(e events.Event) error { return e.Next() })
	}
	writeOK(w, map[string]any{"token": token, "refreshToken": refreshToken})
}

// RecordAuthImpersonate allows an admin to impersonate a record (generate tokens as that record).
func (h *Handlers) RecordAuthImpersonate(w http.ResponseWriter, r *http.Request) {
	adminID := r.Context().Value(contextKeyAdminID).(string)
	collName := chi.URLParam(r, "collection")
	recordID := chi.URLParam(r, "recordId")

	event := &events.RecordAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "impersonate", CollectionName: collName, RecordID: recordID}
	var result *auth.RecordAuthResult
	if err := h.app.OnRecordAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		result, err = h.recordAuth.Impersonate(r.Context(), collName, recordID)
		return err
	}); err != nil {
		writeError(w, 400, err.Error())
		return
	}

	_ = h.app.OnAuthLogin().Trigger(&events.AuthEvent{App: h.app, UserID: recordID, Provider: "record:impersonate", Token: result.Token, RefreshToken: result.RefreshToken}, func(e events.Event) error { return e.Next() })
	h.app.Auth().RecordAudit(r.Context(), adminID, "auth.record.impersonate", collName, recordID, nil, r)
	writeOK(w, result)
}

// RecordPasswordResetRequest creates a password reset token for a record.
func (h *Handlers) RecordPasswordResetRequest(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	var form forms.EmailForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.RecordAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "password_reset_request", CollectionName: collName, Email: form.Email}
	var token string
	if err := h.app.OnRecordAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		token, err = h.recordAuth.RequestRecordPasswordReset(r.Context(), collName, form.Email)
		return err
	}); err != nil {
		writeOK(w, map[string]any{"message": "If the email exists, a reset email has been sent"})
		return
	}

	if h.app.Mailer() != nil {
		h.app.Mailer().SendPasswordReset(form.Email, token)
	}

	writeOK(w, map[string]any{"message": "If the email exists, a reset email has been sent"})
}

// RecordPasswordResetConfirm confirms a password reset for a record.
func (h *Handlers) RecordPasswordResetConfirm(w http.ResponseWriter, r *http.Request) {
	var form forms.TokenPasswordForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.RecordAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "password_reset_confirm", Token: form.Token}
	if err := h.app.OnRecordAuthRequest().Trigger(event, func(e events.Event) error {
		return h.recordAuth.ConfirmRecordPasswordReset(r.Context(), form.Token, form.Password)
	}); err != nil {
		writeError(w, 400, err.Error())
		return
	}

	writeOK(w, map[string]any{"message": "Password reset successfully"})
}

// RecordVerificationRequest sends a verification email for a record.
func (h *Handlers) RecordVerificationRequest(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	var form forms.EmailForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	coll, err := h.app.Collections().GetCollectionByName(r.Context(), collName)
	if err != nil {
		writeError(w, 404, "Collection not found")
		return
	}

	event := &events.RecordAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "verification_request", CollectionName: collName, CollectionID: coll.ID, Email: form.Email}
	var token string
	if err := h.app.OnRecordAuthRequest().Trigger(event, func(e events.Event) error {
		records, _, err := h.app.Collections().ListRecords(r.Context(), coll, fmt.Sprintf("email = %q", form.Email), "", 1, 1)
		if err != nil || len(records) == 0 {
			return fmt.Errorf("not found")
		}
		recordID, _ := records[0]["id"].(string)
		event.RecordID = recordID
		token, err = h.recordAuth.RequestRecordVerification(r.Context(), collName, recordID)
		return err
	}); err != nil {
		writeOK(w, map[string]any{"message": "If the email exists, a verification has been sent"})
		return
	}

	if h.app.Mailer() != nil {
		h.app.Mailer().SendVerification(form.Email, token)
	}

	writeOK(w, map[string]any{"message": "Verification email sent"})
}

// RecordVerificationConfirm confirms a record's email verification.
func (h *Handlers) RecordVerificationConfirm(w http.ResponseWriter, r *http.Request) {
	var form forms.TokenForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.RecordAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "verification_confirm", Token: form.Token}
	if err := h.app.OnRecordAuthRequest().Trigger(event, func(e events.Event) error {
		return h.recordAuth.ConfirmRecordVerification(r.Context(), form.Token)
	}); err != nil {
		writeError(w, 400, err.Error())
		return
	}

	writeOK(w, map[string]any{"message": "Email verified successfully"})
}

// RecordEmailChangeRequest creates an email change token for a record.
func (h *Handlers) RecordEmailChangeRequest(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	var form forms.NewEmailForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	recordID := getRecordIDFromContext(r)
	if recordID == "" {
		writeError(w, 401, "Authentication required")
		return
	}

	event := &events.RecordAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "email_change_request", CollectionName: collName, RecordID: recordID, NewEmail: form.NewEmail}
	var token string
	if err := h.app.OnRecordAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		token, err = h.recordAuth.RequestRecordEmailChange(r.Context(), collName, recordID, form.NewEmail)
		return err
	}); err != nil {
		writeError(w, 400, err.Error())
		return
	}

	if h.app.Mailer() != nil {
		h.app.Mailer().SendEmailChange(form.NewEmail, token)
	}

	writeOK(w, map[string]any{"message": "Confirmation email sent to new address"})
}

// RecordEmailChangeConfirm confirms an email change for a record.
func (h *Handlers) RecordEmailChangeConfirm(w http.ResponseWriter, r *http.Request) {
	var form forms.TokenForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.RecordAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "email_change_confirm", Token: form.Token}
	if err := h.app.OnRecordAuthRequest().Trigger(event, func(e events.Event) error {
		return h.recordAuth.ConfirmRecordEmailChange(r.Context(), form.Token)
	}); err != nil {
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

func recordAuthResultID(result *auth.RecordAuthResult) string {
	if result == nil {
		return ""
	}
	record, ok := result.Record.(map[string]any)
	if !ok {
		return ""
	}
	id, _ := record["id"].(string)
	return id
}

// OAuth2RedirectBridge provides a PocketBase-style popup redirect endpoint that
// relays OAuth callback params back to the opener window.
func (h *Handlers) OAuth2RedirectBridge(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	payload := map[string]any{
		"code":             strings.TrimSpace(r.FormValue("code")),
		"state":            strings.TrimSpace(r.FormValue("state")),
		"error":            strings.TrimSpace(r.FormValue("error")),
		"errorDescription": strings.TrimSpace(r.FormValue("error_description")),
	}
	raw, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>OAuth redirect</title>
  <style>
    body { font-family: system-ui, sans-serif; background: #0b1020; color: #f8fafc; display: flex; min-height: 100vh; align-items: center; justify-content: center; margin: 0; }
    .card { max-width: 420px; padding: 24px; border-radius: 16px; background: rgba(15,23,42,.92); border: 1px solid rgba(148,163,184,.2); box-shadow: 0 20px 60px rgba(0,0,0,.35); }
    .muted { color: #94a3b8; font-size: 14px; }
    code { display: block; margin-top: 12px; padding: 10px 12px; border-radius: 10px; background: rgba(148,163,184,.12); color: #e2e8f0; overflow-wrap: anywhere; }
  </style>
</head>
<body>
  <div class="card">
    <h1 style="margin:0 0 8px; font-size: 20px;">OAuth redirect received</h1>
    <p class="muted">You can close this window and return to Gresbase.</p>
    <code id="payload"></code>
  </div>
  <script>
    const payload = ` + string(raw) + `;
    document.getElementById('payload').textContent = JSON.stringify(payload, null, 2);
    try { localStorage.setItem('gresbase_oauth2_redirect_result', JSON.stringify(payload)); } catch (err) {}
    try {
      const message = { type: 'gresbase:oauth2-redirect', payload };
      if (window.opener && !window.opener.closed) {
        window.opener.postMessage(message, window.location.origin);
      }
      if (window.parent && window.parent !== window) {
        window.parent.postMessage(message, window.location.origin);
      }
    } catch (err) {}
    setTimeout(() => {
      try { window.close(); } catch (err) {}
    }, 250);
  </script>
</body>
</html>`))
}
