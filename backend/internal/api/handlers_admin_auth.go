package api

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/gresbase/gresbase/internal/auth"
	"github.com/gresbase/gresbase/internal/events"
	"github.com/gresbase/gresbase/internal/forms"
	"github.com/rs/zerolog/log"
)

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var form forms.LoginForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.AdminAuthRequestEvent{
		App:      h.app,
		Request:  r,
		Info:     toEventRequestInfoWithBody(r, form),
		Action:   "login",
		Provider: "password",
		Email:    form.Email,
	}
	var token, refreshToken string
	var admin *auth.AdminUser
	if err := h.app.OnAdminAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		token, refreshToken, admin, err = h.app.Auth().Login(r.Context(), form.Email, form.Password)
		return err
	}); err != nil {
		h.app.Auth().RecordAudit(r.Context(), "", "auth.login.failed", "_admins", "", map[string]any{"email": form.Email}, r)
		writeError(w, 401, "Invalid credentials")
		return
	}

	_ = h.app.OnAuthLogin().Trigger(&events.AuthEvent{
		App:          h.app,
		UserID:       admin.ID,
		Provider:     "password",
		Token:        token,
		RefreshToken: refreshToken,
	}, func(e events.Event) error { return e.Next() })

	h.app.Auth().RecordAudit(r.Context(), admin.ID, "auth.login", "_admins", admin.ID, nil, r)
	h.setAuthCookies(w, token, refreshToken)
	writeOK(w, map[string]any{
		"token":        token,
		"refreshToken": refreshToken,
		"admin":        sanitizeAdmin(admin),
	})
}

func (h *Handlers) Register(w http.ResponseWriter, r *http.Request) {
	var form forms.RegisterForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	// Check if this is the first admin (setup mode) or an existing admin adding another
	adminCount, _ := h.app.Auth().CountAdmins(r.Context())
	role := "admin"
	if adminCount == 0 {
		role = "super_admin"
		log.Info().Msg("First admin registration — granting super_admin role")
	}

	event := &events.AdminAuthRequestEvent{
		App:      h.app,
		Request:  r,
		Info:     toEventRequestInfoWithBody(r, form),
		Action:   "register",
		Provider: "password",
		Email:    form.Email,
	}
	var admin *auth.AdminUser
	if err := h.app.OnAdminAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		admin, err = h.app.Auth().CreateAdmin(r.Context(), form.Email, form.Password, role)
		return err
	}); err != nil {
		writeError(w, 409, "Email already in use")
		return
	}

	token, refreshToken, _ := h.app.Auth().GenerateTokens(admin.ID, admin.Email, admin.Role)
	_ = h.app.OnAuthLogin().Trigger(&events.AuthEvent{App: h.app, UserID: admin.ID, Provider: "register", Token: token, RefreshToken: refreshToken}, func(e events.Event) error { return e.Next() })
	h.app.Auth().RecordAudit(r.Context(), admin.ID, "auth.register", "_admins", admin.ID, nil, r)

	h.setAuthCookies(w, token, refreshToken)
	writeJSON(w, 201, map[string]any{
		"token":        token,
		"refreshToken": refreshToken,
		"admin":        sanitizeAdmin(admin),
	})
}

func (h *Handlers) RefreshToken(w http.ResponseWriter, r *http.Request) {
	var form forms.RefreshTokenForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	// Browser sessions carry the refresh token in the HttpOnly gb_refresh cookie
	// rather than the JSON body.
	if form.RefreshToken == "" {
		if c, err := r.Cookie(cookieRefresh); err == nil {
			form.RefreshToken = c.Value
		}
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.AdminAuthRequestEvent{
		App:          h.app,
		Request:      r,
		Info:         toEventRequestInfoWithBody(r, form),
		Action:       "refresh",
		RefreshToken: form.RefreshToken,
	}
	var token, refreshToken string
	if err := h.app.OnAdminAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		token, refreshToken, err = h.app.Auth().RefreshToken(r.Context(), form.RefreshToken)
		return err
	}); err != nil {
		writeError(w, 401, "Invalid or expired refresh token")
		return
	}

	if claims, claimErr := h.app.Auth().ValidateToken(token); claimErr == nil {
		_ = h.app.OnAuthRefresh().Trigger(&events.AuthEvent{App: h.app, UserID: claims.AdminID, Provider: "refresh", Token: token, RefreshToken: refreshToken}, func(e events.Event) error { return e.Next() })
	}
	h.setAuthCookies(w, token, refreshToken)
	writeOK(w, map[string]any{"token": token, "refreshToken": refreshToken})
}

func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Authorization")
	if len(token) > 7 {
		token = token[7:]
	}
	if token == "" {
		if c, err := r.Cookie(cookieAccess); err == nil {
			token = c.Value
		}
	}
	event := &events.AdminAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "logout", Token: token}
	if err := h.app.OnAdminAuthRequest().Trigger(event, func(e events.Event) error {
		return h.app.Auth().Logout(r.Context(), token)
	}); err != nil {
		writeError(w, 500, "Failed to logout")
		return
	}
	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "auth.logout", "_admins", adminID, nil, r)
	h.clearAuthCookies(w)
	writeOK(w, map[string]any{"message": "Logged out"})
}

func (h *Handlers) OTPRequest(w http.ResponseWriter, r *http.Request) {
	var form forms.OTPRequestForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.AdminAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "otp_request", Provider: "otp", Email: form.Email}
	var otp *auth.OTPRecord
	if err := h.app.OnAdminAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		otp, err = h.app.Auth().CreateOTP(r.Context(), form.Email)
		return err
	}); err != nil {
		writeError(w, 500, "Failed to create OTP")
		return
	}

	// Send OTP via email
	if h.app.Mailer() != nil {
		h.app.Mailer().SendOTP(form.Email, otp.Code)
	}

	writeOK(w, map[string]any{"message": "OTP sent", "otpId": otp.ID})
}

func (h *Handlers) OTPVerify(w http.ResponseWriter, r *http.Request) {
	var form forms.OTPVerifyForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.AdminAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "otp_verify", Provider: "otp", OTPID: form.OTPID, Code: form.Code}
	var admin *auth.AdminUser
	if err := h.app.OnAdminAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		admin, err = h.app.Auth().VerifyOTP(r.Context(), form.OTPID, form.Code)
		return err
	}); err != nil {
		writeError(w, 401, "Invalid OTP code")
		return
	}

	token, refreshToken, _ := h.app.Auth().GenerateTokens(admin.ID, admin.Email, admin.Role)
	_ = h.app.OnAuthLogin().Trigger(&events.AuthEvent{App: h.app, UserID: admin.ID, Provider: "otp", Token: token, RefreshToken: refreshToken}, func(e events.Event) error { return e.Next() })
	h.app.Auth().RecordAudit(r.Context(), admin.ID, "auth.otp", "_admins", admin.ID, nil, r)

	h.setAuthCookies(w, token, refreshToken)
	writeOK(w, map[string]any{
		"token":        token,
		"refreshToken": refreshToken,
		"admin":        sanitizeAdmin(admin),
	})
}

func (h *Handlers) MagicLink(w http.ResponseWriter, r *http.Request) {
	var form forms.EmailForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.AdminAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "magic_link", Provider: "magiclink", Email: form.Email}
	var token string
	if err := h.app.OnAdminAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		token, err = h.app.Auth().CreateMagicLink(r.Context(), form.Email)
		return err
	}); err != nil {
		writeError(w, 500, "Failed to create magic link")
		return
	}

	if h.app.Mailer() != nil {
		h.app.Mailer().SendMagicLink(form.Email, token)
	}

	writeOK(w, map[string]any{"message": "Magic link sent"})
}

func (h *Handlers) MagicLinkVerify(w http.ResponseWriter, r *http.Request) {
	var form forms.TokenForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.AdminAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "magic_link_verify", Provider: "magiclink", Token: form.Token}
	var admin *auth.AdminUser
	if err := h.app.OnAdminAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		admin, err = h.app.Auth().VerifyMagicLink(r.Context(), form.Token)
		return err
	}); err != nil {
		writeError(w, 401, "Invalid or expired magic link")
		return
	}

	token, refreshToken, _ := h.app.Auth().GenerateTokens(admin.ID, admin.Email, admin.Role)
	_ = h.app.OnAuthLogin().Trigger(&events.AuthEvent{App: h.app, UserID: admin.ID, Provider: "magiclink", Token: token, RefreshToken: refreshToken}, func(e events.Event) error { return e.Next() })
	h.app.Auth().RecordAudit(r.Context(), admin.ID, "auth.magiclink", "_admins", admin.ID, nil, r)

	h.setAuthCookies(w, token, refreshToken)
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
	var form forms.EmailForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.AdminAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "password_reset_request", Provider: "password", Email: form.Email}
	var token string
	if err := h.app.OnAdminAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		token, err = h.app.Auth().CreatePasswordResetToken(r.Context(), form.Email)
		return err
	}); err != nil {
		// Don't reveal whether email exists
		writeOK(w, map[string]any{"message": "If the email exists, a reset link has been sent"})
		return
	}

	if h.app.Mailer() != nil {
		h.app.Mailer().SendPasswordReset(form.Email, token)
	}

	writeOK(w, map[string]any{"message": "If the email exists, a reset link has been sent"})
}

func (h *Handlers) PasswordResetConfirm(w http.ResponseWriter, r *http.Request) {
	var form forms.TokenPasswordForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.AdminAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "password_reset_confirm", Provider: "password", Token: form.Token}
	if err := h.app.OnAdminAuthRequest().Trigger(event, func(e events.Event) error {
		return h.app.Auth().ConfirmPasswordReset(r.Context(), form.Token, form.Password)
	}); err != nil {
		writeError(w, 400, "Invalid or expired reset token")
		return
	}

	writeOK(w, map[string]any{"message": "Password reset successfully"})
}

// ---------------------------------------------------------------------------
// Email Verification
// ---------------------------------------------------------------------------

func (h *Handlers) VerificationRequest(w http.ResponseWriter, r *http.Request) {
	var form forms.EmailForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.AdminAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "verification_request", Provider: "password", Email: form.Email}
	var token string
	if err := h.app.OnAdminAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		token, err = h.app.Auth().CreateVerificationToken(r.Context(), form.Email)
		return err
	}); err != nil {
		writeError(w, 500, "Failed to create verification")
		return
	}

	if h.app.Mailer() != nil {
		h.app.Mailer().SendVerification(form.Email, token)
	}

	writeOK(w, map[string]any{"message": "Verification email sent"})
}

func (h *Handlers) VerificationConfirm(w http.ResponseWriter, r *http.Request) {
	var form forms.TokenForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.AdminAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "verification_confirm", Provider: "password", Token: form.Token}
	if err := h.app.OnAdminAuthRequest().Trigger(event, func(e events.Event) error {
		return h.app.Auth().ConfirmVerification(r.Context(), form.Token)
	}); err != nil {
		writeError(w, 400, "Invalid or expired verification token")
		return
	}

	writeOK(w, map[string]any{"message": "Email verified"})
}

// ---------------------------------------------------------------------------
// Email Change
// ---------------------------------------------------------------------------

func (h *Handlers) EmailChangeRequest(w http.ResponseWriter, r *http.Request) {
	var form forms.NewEmailForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	adminID := r.Context().Value(contextKeyAdminID).(string)
	event := &events.AdminAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "email_change_request", Provider: "password", NewEmail: form.NewEmail}
	var token string
	if err := h.app.OnAdminAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		token, err = h.app.Auth().CreateEmailChangeToken(r.Context(), adminID, form.NewEmail)
		return err
	}); err != nil {
		writeError(w, 500, "Failed to create email change")
		return
	}

	if h.app.Mailer() != nil {
		h.app.Mailer().SendEmailChange(form.NewEmail, token)
	}

	writeOK(w, map[string]any{"message": "Confirmation email sent to new address"})
}

func (h *Handlers) EmailChangeConfirm(w http.ResponseWriter, r *http.Request) {
	var form forms.TokenForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.AdminAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "email_change_confirm", Provider: "password", Token: form.Token}
	if err := h.app.OnAdminAuthRequest().Trigger(event, func(e events.Event) error {
		return h.app.Auth().ConfirmEmailChange(r.Context(), form.Token)
	}); err != nil {
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
	event := &events.AdminUserRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "me", TargetAdminID: adminID}
	var admin *auth.AdminUser
	if err := h.app.OnAdminUserRequest().Trigger(event, func(e events.Event) error {
		var err error
		admin, err = h.app.Auth().FindAdminByID(r.Context(), adminID)
		return err
	}); err != nil {
		writeError(w, 404, "Admin not found")
		return
	}
	writeOK(w, sanitizeAdmin(admin))
}

func (h *Handlers) AdminUpdateMe(w http.ResponseWriter, r *http.Request) {
	adminID := r.Context().Value(contextKeyAdminID).(string)
	var form forms.AdminUpsertForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid body")
		return
	}
	if err := form.ValidateUpdate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.AdminUserRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "update_me", TargetAdminID: adminID, Data: bodyToMap(form)}
	var admin *auth.AdminUser
	if err := h.app.OnAdminUserRequest().Trigger(event, func(e events.Event) error {
		var updatedForm forms.AdminUpsertForm
		if err := remarshalInto(event.Data, &updatedForm); err != nil {
			return err
		}
		if err := updatedForm.ValidateUpdate(); err != nil {
			return err
		}

		updates := make(map[string]any)
		if updatedForm.Email != "" {
			updates["email"] = updatedForm.Email
		}
		if updatedForm.Avatar != "" {
			updates["avatar"] = updatedForm.Avatar
		}
		if updatedForm.Password != "" {
			hash, err := h.app.Auth().HashPassword(updatedForm.Password)
			if err != nil {
				return err
			}
			updates["password_hash"] = hash
		}

		if err := h.app.Auth().UpdateAdmin(r.Context(), adminID, updates); err != nil {
			return err
		}
		admin, _ = h.app.Auth().FindAdminByID(r.Context(), adminID)
		return nil
	}); err != nil {
		if _, ok := err.(forms.Errors); ok {
			writeValidationError(w, err)
			return
		}
		writeInternalError(w, "Failed to update", err)
		return
	}

	h.app.Auth().RecordAudit(r.Context(), adminID, "admin.update_me", "_admins", adminID, nil, r)
	writeOK(w, sanitizeAdmin(admin))
}

func (h *Handlers) AdminList(w http.ResponseWriter, r *http.Request) {
	event := &events.AdminUserRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "list"}
	var admins []*auth.AdminUser
	if err := h.app.OnAdminUserRequest().Trigger(event, func(e events.Event) error {
		var err error
		admins, err = h.app.Auth().ListAdmins(r.Context())
		return err
	}); err != nil {
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
	var form forms.AdminUpsertForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid body")
		return
	}
	if form.Role == "" {
		form.Role = "admin"
	}
	if err := form.ValidateCreate(); err != nil {
		writeValidationError(w, err)
		return
	}
	event := &events.AdminUserRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "create", Data: bodyToMap(form)}
	var admin *auth.AdminUser
	if err := h.app.OnAdminUserRequest().Trigger(event, func(e events.Event) error {
		var createForm forms.AdminUpsertForm
		if err := remarshalInto(event.Data, &createForm); err != nil {
			return err
		}
		if createForm.Role == "" {
			createForm.Role = "admin"
		}
		if err := createForm.ValidateCreate(); err != nil {
			return err
		}
		var err error
		admin, err = h.app.Auth().CreateAdmin(r.Context(), createForm.Email, createForm.Password, createForm.Role)
		return err
	}); err != nil {
		if _, ok := err.(forms.Errors); ok {
			writeValidationError(w, err)
			return
		}
		writeError(w, 409, "Failed to create admin: "+err.Error())
		return
	}
	adminID := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "admin.create", "_admins", admin.ID, map[string]any{"email": admin.Email, "role": admin.Role}, r)
	writeJSON(w, 201, sanitizeAdmin(admin))
}

func (h *Handlers) AdminGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	event := &events.AdminUserRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "view", TargetAdminID: id}
	var admin *auth.AdminUser
	if err := h.app.OnAdminUserRequest().Trigger(event, func(e events.Event) error {
		var err error
		admin, err = h.app.Auth().FindAdminByID(r.Context(), id)
		return err
	}); err != nil {
		writeError(w, 404, "Admin not found")
		return
	}
	writeOK(w, sanitizeAdmin(admin))
}

func (h *Handlers) AdminUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var form forms.AdminUpsertForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid body")
		return
	}
	if err := form.ValidateUpdate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.AdminUserRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "update", TargetAdminID: id, Data: bodyToMap(form)}
	var admin *auth.AdminUser
	if err := h.app.OnAdminUserRequest().Trigger(event, func(e events.Event) error {
		var updateForm forms.AdminUpsertForm
		if err := remarshalInto(event.Data, &updateForm); err != nil {
			return err
		}
		if err := updateForm.ValidateUpdate(); err != nil {
			return err
		}

		updates := make(map[string]any)
		if updateForm.Email != "" {
			updates["email"] = updateForm.Email
		}
		if updateForm.Role != "" {
			updates["role"] = updateForm.Role
		}
		if updateForm.Avatar != "" {
			updates["avatar"] = updateForm.Avatar
		}
		if updateForm.Password != "" {
			hash, err := h.app.Auth().HashPassword(updateForm.Password)
			if err != nil {
				return err
			}
			updates["password_hash"] = hash
		}

		if err := h.app.Auth().UpdateAdmin(r.Context(), id, updates); err != nil {
			return err
		}
		admin, _ = h.app.Auth().FindAdminByID(r.Context(), id)
		return nil
	}); err != nil {
		if _, ok := err.(forms.Errors); ok {
			writeValidationError(w, err)
			return
		}
		writeInternalError(w, "Failed to update", err)
		return
	}

	actorID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), actorID, "admin.update", "_admins", id, nil, r)
	writeOK(w, sanitizeAdmin(admin))
}

func (h *Handlers) AdminDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	event := &events.AdminUserRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "delete", TargetAdminID: id}
	if err := h.app.OnAdminUserRequest().Trigger(event, func(e events.Event) error {
		return h.app.Auth().DeleteAdmin(r.Context(), id)
	}); err != nil {
		writeError(w, 500, "Failed to delete admin")
		return
	}
	actorID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), actorID, "admin.delete", "_admins", id, nil, r)
	writeOK(w, map[string]any{"deleted": id})
}

// ---------------------------------------------------------------------------
// OAuth
// ---------------------------------------------------------------------------

func (h *Handlers) OAuthRedirect(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	redirectURL := fmt.Sprintf("%s/api/v1/oauth/%s/callback", getBaseURL(r), provider)

	event := &events.AdminAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "oauth_redirect", Provider: provider}
	var authURL string
	if err := h.app.OnAdminAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		authURL, _, err = h.app.OAuth().GetAuthURLContext(r.Context(), provider, redirectURL)
		return err
	}); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

func (h *Handlers) OAuthCallback(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	form := forms.OAuthCallbackForm{Code: r.URL.Query().Get("code"), State: r.URL.Query().Get("state")}
	redirectURL := fmt.Sprintf("%s/api/v1/oauth/%s/callback", getBaseURL(r), provider)

	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.AdminAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "oauth_callback", Provider: provider, Code: form.Code}
	var userInfo *auth.OAuthUserInfo
	var admin *auth.AdminUser
	if err := h.app.OnAdminAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		userInfo, err = h.app.OAuth().ExchangeCode(r.Context(), provider, form.Code, form.State, redirectURL)
		if err != nil {
			return err
		}
		admin, err = h.app.Auth().FindOrCreateByOAuth(r.Context(), userInfo)
		return err
	}); err != nil {
		writeInternalError(w, "OAuth exchange failed", err)
		return
	}

	token, refreshToken, _ := h.app.Auth().GenerateTokens(admin.ID, admin.Email, admin.Role)
	_ = h.app.OnAuthLogin().Trigger(&events.AuthEvent{App: h.app, UserID: admin.ID, Provider: "oauth:" + provider, Token: token, RefreshToken: refreshToken}, func(e events.Event) error { return e.Next() })
	h.app.Auth().RecordAudit(r.Context(), admin.ID, "auth.oauth."+provider, "_admins", admin.ID, nil, r)

	h.setAuthCookies(w, token, refreshToken)
	writeOK(w, map[string]any{
		"token":        token,
		"refreshToken": refreshToken,
		"admin":        sanitizeAdmin(admin),
		"oauth":        userInfo,
	})
}
