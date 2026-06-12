package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/gresbase/gresbase/internal/auth"
	"github.com/gresbase/gresbase/internal/ctxkeys"
	"github.com/gresbase/gresbase/internal/events"
)

// ---------------------------------------------------------------------------
// Passkey (WebAuthn) routes for record auth
//
// All endpoints are gated by the collection's allowPasskeys option (default
// false, fail-closed) exactly like auth-with-anonymous is gated by
// allowAnonymous: disabled collections answer 403.
// ---------------------------------------------------------------------------

// mountPasskeyRoutes registers the passkey sub-routes inside the
// /collections/{collection}/auth route group.
func (s *Server) mountPasskeyRoutes(r chi.Router, authRate func(http.Handler) http.Handler) {
	mw := s.mw
	h := s.h

	r.Route("/passkey", func(r chi.Router) {
		// Registration requires a valid record auth token for the collection.
		r.With(authRate, mw.OptionalAuth).Post("/register-begin", h.PasskeyRegisterBegin)
		r.With(authRate, mw.OptionalAuth).Post("/register-finish", h.PasskeyRegisterFinish)
		// Login is unauthenticated (it IS the authentication).
		r.With(authRate).Post("/login-begin", h.PasskeyLoginBegin)
		r.With(authRate).Post("/login-finish", h.PasskeyLoginFinish)
	})

	// Passkey management: list/delete own passkeys (record auth required).
	r.With(mw.OptionalAuth).Get("/passkeys", h.PasskeysList)
	r.With(mw.OptionalAuth).Delete("/passkeys/{passkeyId}", h.PasskeyDelete)
}

// requireRecordAuthForCollection extracts the record auth identity injected by
// OptionalAuth and verifies the token belongs to the URL collection. Returns
// ok=false after writing the error response.
func (h *Handlers) requireRecordAuthForCollection(w http.ResponseWriter, r *http.Request, collName string) (string, bool) {
	recordID := getRecordIDFromContext(r)
	tokenCollectionID, _ := r.Context().Value(ctxkeys.CollectionID).(string)
	if recordID == "" || tokenCollectionID == "" {
		writeError(w, http.StatusUnauthorized, "Record authentication required")
		return "", false
	}
	coll, err := h.app.Collections().GetCollectionByName(r.Context(), collName)
	if err != nil {
		writeError(w, http.StatusNotFound, "Collection not found")
		return "", false
	}
	if coll.ID != tokenCollectionID {
		writeError(w, http.StatusForbidden, "Token does not belong to this collection")
		return "", false
	}
	return recordID, true
}

// writePasskeyError maps service errors onto the same status codes the other
// record auth endpoints use (403 for the disabled opt-in, like anonymous).
func writePasskeyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrPasskeysDisabled):
		writeError(w, http.StatusForbidden, "Passkey authentication is not available for this collection")
	case errors.Is(err, auth.ErrPasskeyNotFound):
		writeError(w, http.StatusNotFound, "Passkey not found")
	case errors.Is(err, auth.ErrPasskeySessionInvalid):
		writeError(w, http.StatusBadRequest, "Invalid or expired passkey session")
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

// PasskeyRegisterBegin starts passkey registration for the authenticated
// record and returns the CredentialCreation options plus a server session id.
func (h *Handlers) PasskeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	recordID, ok := h.requireRecordAuthForCollection(w, r, collName)
	if !ok {
		return
	}

	event := &events.RecordAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "passkey_register_begin", CollectionName: collName, RecordID: recordID}
	var options any
	var sessionID string
	if err := h.app.OnRecordAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		options, sessionID, err = h.app.Passkeys().BeginRegistration(r.Context(), collName, recordID)
		return err
	}); err != nil {
		writePasskeyError(w, err)
		return
	}

	writeOK(w, map[string]any{"sessionId": sessionID, "options": options})
}

// PasskeyRegisterFinish verifies the attestation response and stores the
// passkey, returning its public descriptor.
func (h *Handlers) PasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	recordID, ok := h.requireRecordAuthForCollection(w, r, collName)
	if !ok {
		return
	}

	var form struct {
		SessionID  string          `json:"sessionId"`
		Name       string          `json:"name"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if form.SessionID == "" || len(form.Credential) == 0 {
		writeError(w, http.StatusBadRequest, "sessionId and credential are required")
		return
	}

	event := &events.RecordAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "passkey_register_finish", CollectionName: collName, RecordID: recordID}
	var info *auth.PasskeyInfo
	if err := h.app.OnRecordAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		info, err = h.app.Passkeys().FinishRegistration(r.Context(), collName, recordID, form.SessionID, form.Name, form.Credential)
		return err
	}); err != nil {
		writePasskeyError(w, err)
		return
	}

	h.app.Auth().RecordAudit(r.Context(), recordID, "auth.record.passkey_register", collName, recordID, map[string]any{"passkey_id": info.ID}, r)
	writeOK(w, info)
}

// PasskeyLoginBegin returns CredentialAssertion options. No auth required.
// An optional {"email": ...} body scopes allowCredentials to that user's
// passkeys; unknown emails get the same discoverable-login options (empty
// allowCredentials) so the endpoint never leaks account existence.
func (h *Handlers) PasskeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")

	var form struct {
		Email string `json:"email"`
	}
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	event := &events.RecordAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "passkey_login_begin", CollectionName: collName, Email: form.Email}
	var options any
	var sessionID string
	if err := h.app.OnRecordAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		options, sessionID, err = h.app.Passkeys().BeginLogin(r.Context(), collName, form.Email)
		return err
	}); err != nil {
		writePasskeyError(w, err)
		return
	}

	writeOK(w, map[string]any{"sessionId": sessionID, "options": options})
}

// PasskeyLoginFinish verifies the assertion and mints the same record auth
// tokens + response shape as auth-with-password (no MFA second step: a
// passkey is already possession + user verification).
func (h *Handlers) PasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")

	var form struct {
		SessionID  string          `json:"sessionId"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if form.SessionID == "" || len(form.Credential) == 0 {
		writeError(w, http.StatusBadRequest, "sessionId and credential are required")
		return
	}

	event := &events.RecordAuthRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "passkey_login", CollectionName: collName}
	var result *auth.RecordAuthResult
	var recordID string
	if err := h.app.OnRecordAuthRequest().Trigger(event, func(e events.Event) error {
		var err error
		result, recordID, err = h.app.Passkeys().FinishLogin(r.Context(), collName, form.SessionID, form.Credential)
		return err
	}); err != nil {
		if errors.Is(err, auth.ErrPasskeysDisabled) || errors.Is(err, auth.ErrPasskeySessionInvalid) {
			writePasskeyError(w, err)
			return
		}
		writeError(w, http.StatusUnauthorized, "Invalid credentials")
		return
	}

	_ = h.app.OnAuthLogin().Trigger(&events.AuthEvent{App: h.app, UserID: recordID, Provider: "record:passkey", Token: result.Token, RefreshToken: result.RefreshToken}, func(e events.Event) error { return e.Next() })
	h.app.Auth().RecordAudit(r.Context(), recordID, "auth.record.passkey", collName, recordID, nil, r)
	writeOK(w, result)
}

// PasskeysList returns the caller's own passkey descriptors (id, name,
// created, lastUsedAt — never credential material).
func (h *Handlers) PasskeysList(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	recordID, ok := h.requireRecordAuthForCollection(w, r, collName)
	if !ok {
		return
	}

	items, err := h.app.Passkeys().ListPasskeys(r.Context(), collName, recordID)
	if err != nil {
		writePasskeyError(w, err)
		return
	}
	writeOK(w, map[string]any{"items": items})
}

// PasskeyDelete removes one of the caller's own passkeys.
func (h *Handlers) PasskeyDelete(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	recordID, ok := h.requireRecordAuthForCollection(w, r, collName)
	if !ok {
		return
	}

	passkeyID := chi.URLParam(r, "passkeyId")
	if err := h.app.Passkeys().DeletePasskey(r.Context(), collName, recordID, passkeyID); err != nil {
		writePasskeyError(w, err)
		return
	}
	h.app.Auth().RecordAudit(r.Context(), recordID, "auth.record.passkey_delete", collName, recordID, map[string]any{"passkey_id": passkeyID}, r)
	writeOK(w, map[string]any{"message": "Passkey deleted"})
}
