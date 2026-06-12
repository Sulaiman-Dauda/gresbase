package api

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
)

// Auth cookie names. The access/refresh cookies are HttpOnly so JavaScript
// (and therefore XSS payloads) cannot read them; the CSRF cookie is readable so
// the SPA can echo it back in the X-CSRF-Token header (double-submit pattern).
const (
	cookieAccess  = "gb_access"
	cookieRefresh = "gb_refresh"
	cookieCSRF    = "gb_csrf"
)

// setAuthCookies issues the access, refresh and CSRF cookies after a successful
// admin login or token refresh. The Secure flag is set unless running in dev
// mode (so http://localhost development still works). It returns the CSRF token
// it generated, or empty on failure (in which case no cookies are set).
func (h *Handlers) setAuthCookies(w http.ResponseWriter, accessToken, refreshToken string) string {
	secure := true
	if cfg := h.app.Config(); cfg != nil && cfg.DevMode {
		secure = false
	}

	csrf, err := generateCSRFToken()
	if err != nil {
		// Without a CSRF token we must not set the auth cookies, otherwise
		// cookie-authenticated mutations would be impossible. The JSON token
		// response still lets clients authenticate via the Authorization header.
		return ""
	}

	http.SetCookie(w, &http.Cookie{
		Name:     cookieAccess,
		Value:    accessToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     cookieRefresh,
		Value:    refreshToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     cookieCSRF,
		Value:    csrf,
		Path:     "/",
		HttpOnly: false, // must be readable by the SPA for double-submit
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	return csrf
}

// clearAuthCookies expires all three auth cookies on logout.
func (h *Handlers) clearAuthCookies(w http.ResponseWriter) {
	secure := true
	if cfg := h.app.Config(); cfg != nil && cfg.DevMode {
		secure = false
	}
	for _, name := range []string{cookieAccess, cookieRefresh, cookieCSRF} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			HttpOnly: name != cookieCSRF,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   -1,
		})
	}
}

// generateCSRFToken returns a random 32-byte token, base64 (raw URL) encoded.
func generateCSRFToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
