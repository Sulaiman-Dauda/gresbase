package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gresbase/gresbase/internal/config"
)

func newNilDBService(t *testing.T) *Service {
	t.Helper()
	cfg := &config.Config{
		JWTSecret:         "logic-test-secret",
		AccessTokenExpiry: 15 * time.Minute, RefreshTokenExpiry: time.Hour,
		LogLevel: "error",
	}
	return NewService(nil, cfg)
}

func TestExtractIP(t *testing.T) {
	if got := extractIP(nil); got != "" {
		t.Errorf("nil request: got %q, want empty", got)
	}

	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:54321"
	if got := extractIP(r); got != "10.0.0.1" {
		t.Errorf("RemoteAddr: got %q, want 10.0.0.1", got)
	}

	r.Header.Set("X-Real-IP", "192.168.1.5")
	if got := extractIP(r); got != "192.168.1.5" {
		t.Errorf("X-Real-IP: got %q, want 192.168.1.5", got)
	}

	// X-Forwarded-For takes priority over X-Real-IP and RemoteAddr.
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	if got := extractIP(r); got != "203.0.113.7" {
		t.Errorf("X-Forwarded-For: got %q, want 203.0.113.7", got)
	}
}

func TestFastHash(t *testing.T) {
	h1 := fastHash("token-a")
	h2 := fastHash("token-a")
	h3 := fastHash("token-b")

	if h1 != h2 {
		t.Error("fastHash must be deterministic")
	}
	if h1 == h3 {
		t.Error("different inputs must produce different hashes")
	}
	if len(h1) != 64 {
		t.Errorf("fastHash should be 64 hex chars, got %d", len(h1))
	}
	for _, c := range h1 {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Errorf("non-hex character in hash: %c", c)
		}
	}
}

func TestGenerateRandomString(t *testing.T) {
	s1 := generateRandomString(64)
	s2 := generateRandomString(64)

	if len(s1) != 64 {
		t.Errorf("length = %d, want 64", len(s1))
	}
	if s1 == s2 {
		t.Error("random strings must be unique")
	}
	const urlSafe = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	for _, c := range s1 {
		if !strings.ContainsRune(urlSafe, c) {
			t.Errorf("non URL-safe character: %c", c)
		}
	}
}

func TestDecodeAPIKeyPermissions(t *testing.T) {
	if got := decodeAPIKeyPermissions(nil); got != nil {
		t.Errorf("nil input: got %v, want nil", got)
	}
	if got := decodeAPIKeyPermissions([]byte("not-json")); got != nil {
		t.Errorf("invalid JSON: got %v, want nil", got)
	}

	got := decodeAPIKeyPermissions([]byte(`[" Records.Read ", "all", "records.read"]`))
	want := []string{"*", "records.read"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestNormalizeAPIKeyPermissionEdgeCases(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{" ALL ", "*"},
		{"..records.read.", "records.read"},
		{"Collections.WRITE", "collections.write"},
		{"...", ""},
		{"   ", ""},
		{".*", "*"},
	}
	for _, tt := range tests {
		if got := normalizeAPIKeyPermission(tt.in); got != tt.want {
			t.Errorf("normalizeAPIKeyPermission(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestAPIKeyPermissionAllowedEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		granted   []string
		required  string
		wantAllow bool
	}{
		{name: "empty required always allowed", granted: []string{"records.read"}, required: "", wantAllow: true},
		{name: "prefix wildcard matches bare prefix", granted: []string{"records.*"}, required: "records", wantAllow: true},
		{name: "prefix wildcard requires dot boundary", granted: []string{"records.*"}, required: "recordsfoo.read", wantAllow: false},
		{name: "wildcard does not leak across resources", granted: []string{"records.*"}, required: "collections.read", wantAllow: false},
		{name: "dot-star normalizes to global", granted: []string{".*"}, required: "anything.at.all", wantAllow: true},
		{name: "case-insensitive match", granted: []string{"Records.Read"}, required: "RECORDS.READ", wantAllow: true},
	}
	for _, tt := range tests {
		if got := APIKeyPermissionAllowed(tt.granted, tt.required); got != tt.wantAllow {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.wantAllow)
		}
	}
}

func TestValidateAPIKeyGuards(t *testing.T) {
	ctx := context.Background()

	var nilSvc *Service
	if _, _, err := nilSvc.ValidateAPIKey(ctx, "gb_anything"); err == nil {
		t.Error("nil service must return an error, not panic")
	}

	svc := newNilDBService(t)
	if _, _, err := svc.ValidateAPIKey(ctx, "gb_anything"); err == nil {
		t.Error("service without database must return an error")
	}
}

func TestTokenConfirmationRejectsShortTokens(t *testing.T) {
	// All confirmation flows must reject empty/short tokens before touching
	// the database (the service here has a nil DB: reaching the DB panics).
	svc := newNilDBService(t)
	ctx := context.Background()

	shortTokens := []string{"", "short", strings.Repeat("a", 31)}
	for _, tok := range shortTokens {
		if _, err := svc.VerifyMagicLink(ctx, tok); err == nil {
			t.Errorf("VerifyMagicLink(%q) must fail", tok)
		}
		if err := svc.ConfirmPasswordReset(ctx, tok, "new-password"); err == nil {
			t.Errorf("ConfirmPasswordReset(%q) must fail", tok)
		}
		if err := svc.ConfirmVerification(ctx, tok); err == nil {
			t.Errorf("ConfirmVerification(%q) must fail", tok)
		}
		if err := svc.ConfirmEmailChange(ctx, tok); err == nil {
			t.Errorf("ConfirmEmailChange(%q) must fail", tok)
		}
	}
}

func TestUpdateAdminNoFieldsIsNoop(t *testing.T) {
	svc := newNilDBService(t)
	// Empty update must short-circuit before any database access.
	if err := svc.UpdateAdmin(context.Background(), "admin-1", map[string]any{}); err != nil {
		t.Fatalf("empty update should be a no-op, got: %v", err)
	}
}

func TestAdminUserJSONRedactsPasswordHash(t *testing.T) {
	admin := &AdminUser{
		ID:           "admin-1",
		Email:        "a@b.com",
		PasswordHash: "super-secret-bcrypt-hash",
	}
	out, err := json.Marshal(admin)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "super-secret-bcrypt-hash") {
		t.Error("AdminUser JSON must never contain the password hash")
	}
}

func TestAPIKeyJSONRedactsKeyHash(t *testing.T) {
	key := &APIKey{
		ID:      "key-1",
		KeyHash: "secret-key-hash-value",
		Prefix:  "gb_abcdefg",
	}
	out, err := json.Marshal(key)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "secret-key-hash-value") {
		t.Error("APIKey JSON must never contain the key hash")
	}
}

func TestConstantTimeCompareEmpty(t *testing.T) {
	if !ConstantTimeCompare("", "") {
		t.Error("two empty strings must compare equal")
	}
	if ConstantTimeCompare("", "a") {
		t.Error("empty vs non-empty must not compare equal")
	}
}

func TestGenerateOTPUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	duplicates := 0
	for i := 0; i < 50; i++ {
		code, err := GenerateOTP(6)
		if err != nil {
			t.Fatal(err)
		}
		if seen[code] {
			duplicates++
		}
		seen[code] = true
	}
	// With 50 draws from 10^6 the chance of >2 collisions is negligible.
	if duplicates > 2 {
		t.Errorf("too many duplicate OTPs: %d", duplicates)
	}
}
