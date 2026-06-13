package api

import (
	"net/http"
	"testing"
)

// TestSuperuserIPAllowlist verifies that, when SuperuserIPs is set, the admin
// API is only reachable from allowlisted IPs/CIDRs, and that an empty list
// never locks anyone out (fail-open).
func TestSuperuserIPAllowlist(t *testing.T) {
	env := newIntegrationEnv(t)
	token := env.createAdminToken(t)
	auth := map[string]string{"Authorization": "Bearer " + token}
	url := env.http.URL + "/api/v1/admin/me"

	// Empty allowlist → admin reachable from anywhere (fail-open default).
	if resp := doJSONRequest(t, http.MethodGet, url, nil, auth); resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("empty allowlist: expected 200, got %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// Allowlist containing only a foreign address → loopback test client is
	// rejected with 403 even though the token is valid.
	env.app.Config().SuperuserIPs = []string{"10.99.99.99/32"}
	if resp := doJSONRequest(t, http.MethodGet, url, nil, auth); resp.StatusCode != http.StatusForbidden {
		resp.Body.Close()
		t.Fatalf("foreign-only allowlist: expected 403, got %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// Allowlist that includes loopback (both IPv4 and IPv6 forms) → allowed.
	env.app.Config().SuperuserIPs = []string{"127.0.0.0/8", "::1"}
	if resp := doJSONRequest(t, http.MethodGet, url, nil, auth); resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("loopback allowlist: expected 200, got %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// Clearing the list restores open access.
	env.app.Config().SuperuserIPs = nil
	if resp := doJSONRequest(t, http.MethodGet, url, nil, auth); resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("cleared allowlist: expected 200, got %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
}

// TestRateLimitExcludeIPs verifies that excluded IPs bypass the per-IP rate
// limiter entirely, while non-excluded IPs are still throttled.
func TestRateLimitExcludeIPs(t *testing.T) {
	env := newIntegrationEnv(t)
	url := env.http.URL + "/api/v1/auth/login"
	body := map[string]any{"email": "nobody@example.com", "password": "wrong-password"}

	hammer := func(n int) (got429 bool) {
		for i := 0; i < n; i++ {
			resp := doJSONRequest(t, http.MethodPost, url, body, nil)
			code := resp.StatusCode
			resp.Body.Close()
			if code == http.StatusTooManyRequests {
				got429 = true
			}
		}
		return got429
	}

	// With loopback excluded, no amount of attempts trips the limiter (login
	// is configured at 10/min; 20 attempts would normally 429).
	env.app.Config().RateLimitExcludeIPs = []string{"127.0.0.0/8", "::1"}
	if hammer(20) {
		t.Fatal("excluded IP should never be rate limited")
	}

	// Without exclusion, the same client gets throttled. The window starts
	// fresh because excluded requests never touched the limiter.
	env.app.Config().RateLimitExcludeIPs = nil
	if !hammer(20) {
		t.Fatal("non-excluded IP should be rate limited after exceeding the quota")
	}
}
