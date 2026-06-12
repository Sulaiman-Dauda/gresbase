package auth_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/gresbase/gresbase/internal/auth"
	"github.com/gresbase/gresbase/internal/config"
)

func TestHashAndVerifyPassword(t *testing.T) {
	cfg := &config.Config{JWTSecret: "test-secret", LogLevel: "error"}
	svc := auth.NewService(nil, cfg)

	hash, err := svc.HashPassword("secure-password-123")
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	if !svc.VerifyPassword(hash, "secure-password-123") {
		t.Error("Password verification failed for correct password")
	}

	if svc.VerifyPassword(hash, "wrong-password") {
		t.Error("Password verification should fail for incorrect password")
	}
}

func TestGenerateTokens(t *testing.T) {
	cfg := &config.Config{
		JWTSecret:          "test-jwt-secret-key",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenExpiry: 7 * 24 * time.Hour,
		AdminTokenExpiry:   24 * time.Hour,
		LogLevel:           "error",
	}
	svc := auth.NewService(nil, cfg)

	access, refresh, err := svc.GenerateTokens("admin-1", "test@gresbase.com", "admin", "default")
	if err != nil {
		t.Fatalf("Failed to generate tokens: %v", err)
	}

	if access == "" {
		t.Error("Access token should not be empty")
	}
	if refresh == "" {
		t.Error("Refresh token should not be empty")
	}

	// Validate access token
	claims, err := svc.ValidateToken(access)
	if err != nil {
		t.Fatalf("Failed to validate access token: %v", err)
	}

	if claims.AdminID != "admin-1" {
		t.Errorf("Expected admin-1, got %s", claims.AdminID)
	}
	if claims.Email != "test@gresbase.com" {
		t.Errorf("Expected test@gresbase.com, got %s", claims.Email)
	}
	if claims.Role != "admin" {
		t.Errorf("Expected admin role, got %s", claims.Role)
	}
	if claims.Type != auth.AccessToken {
		t.Errorf("Expected AccessToken type, got %s", claims.Type)
	}
}

func TestGenerateTokensES256AndJWKS(t *testing.T) {
	privateKeyPEM, publicKeyPEM := generateES256TestKeys(t)
	cfg := &config.Config{
		JWTAlgorithm:       "ES256",
		JWTKeyID:           "test-es256-key",
		JWTPrivateKey:      privateKeyPEM,
		JWTPublicKey:       publicKeyPEM,
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenExpiry: 7 * 24 * time.Hour,
		AdminTokenExpiry:   24 * time.Hour,
		LogLevel:           "error",
	}
	svc := auth.NewService(nil, cfg)

	access, _, err := svc.GenerateTokens("admin-1", "test@gresbase.com", "admin", "default")
	if err != nil {
		t.Fatalf("failed to generate ES256 tokens: %v", err)
	}

	claims, err := svc.ValidateToken(access)
	if err != nil {
		t.Fatalf("failed to validate ES256 token: %v", err)
	}
	if claims.AdminID != "admin-1" {
		t.Fatalf("expected admin-1, got %s", claims.AdminID)
	}

	jwks, err := auth.PublicJWKS(cfg)
	if err != nil {
		t.Fatalf("failed to build JWKS: %v", err)
	}
	keys, ok := jwks["keys"].([]map[string]any)
	if !ok {
		t.Fatalf("unexpected JWKS keys type: %T", jwks["keys"])
	}
	if len(keys) != 1 {
		t.Fatalf("expected one JWKS key, got %d", len(keys))
	}
	if keys[0]["kid"] != "test-es256-key" || keys[0]["alg"] != "ES256" {
		t.Fatalf("unexpected JWKS key metadata: %v", keys[0])
	}
}

func TestValidateTokenInvalidSignature(t *testing.T) {
	cfg := &config.Config{
		JWTSecret:          "real-secret",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenExpiry: 7 * 24 * time.Hour,
		LogLevel:           "error",
	}
	svc := auth.NewService(nil, cfg)

	access, _, err := svc.GenerateTokens("admin-1", "t@t.com", "admin", "default")
	if err != nil {
		t.Fatal(err)
	}

	// Validate with wrong secret
	cfg2 := &config.Config{
		JWTSecret:         "wrong-secret",
		AccessTokenExpiry: 15 * time.Minute,
		LogLevel:          "error",
	}
	svc2 := auth.NewService(nil, cfg2)

	_, err = svc2.ValidateToken(access)
	if err == nil {
		t.Error("Expected validation to fail with wrong secret")
	}
}

func TestTokenExpiry(t *testing.T) {
	cfg := &config.Config{
		JWTSecret:          "test-secret",
		AccessTokenExpiry:  1 * time.Millisecond,
		RefreshTokenExpiry: 7 * 24 * time.Hour,
		LogLevel:           "error",
	}
	svc := auth.NewService(nil, cfg)

	access, _, err := svc.GenerateTokens("admin-1", "t@t.com", "admin", "default")
	if err != nil {
		t.Fatal(err)
	}

	// Wait for token to expire
	time.Sleep(10 * time.Millisecond)

	_, err = svc.ValidateToken(access)
	if err == nil {
		t.Error("Expected token to be expired")
	}
}

func TestAdminTokenGeneration(t *testing.T) {
	cfg := &config.Config{
		JWTSecret:        "test-secret",
		AdminTokenExpiry: 24 * time.Hour,
		LogLevel:         "error",
	}
	svc := auth.NewService(nil, cfg)

	token, err := svc.GenerateAdminToken("admin-1", "t@t.com", "admin", "default")
	if err != nil {
		t.Fatal(err)
	}

	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatal(err)
	}

	if claims.Type != auth.AdminToken {
		t.Errorf("Expected AdminToken type, got %s", claims.Type)
	}
}

func TestGenerateOTP(t *testing.T) {
	otp, err := auth.GenerateOTP(6)
	if err != nil {
		t.Fatal(err)
	}

	if len(otp) != 6 {
		t.Errorf("Expected OTP length 6, got %d", len(otp))
	}

	for _, ch := range otp {
		if ch < '0' || ch > '9' {
			t.Errorf("OTP contains non-digit character: %c", ch)
		}
	}
}

func TestConstantTimeCompare(t *testing.T) {
	if !auth.ConstantTimeCompare("abc", "abc") {
		t.Error("Equal strings should match")
	}

	if auth.ConstantTimeCompare("abc", "xyz") {
		t.Error("Different strings should not match")
	}

	if auth.ConstantTimeCompare("abc", "ab") {
		t.Error("Different length strings should not match")
	}
}

func TestUpdateAdminRejectsUnsupportedFields(t *testing.T) {
	cfg := &config.Config{JWTSecret: "secret", LogLevel: "error"}
	svc := auth.NewService(nil, cfg)

	err := svc.UpdateAdmin(context.Background(), "admin-1", map[string]any{"drop_table": true})
	if err == nil {
		t.Fatal("expected unsupported field error")
	}
	if !strings.Contains(err.Error(), "unsupported admin field") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeAPIKeyPermissions(t *testing.T) {
	got := auth.NormalizeAPIKeyPermissions([]string{" Records.Read ", "collections.*", "all", "records.read", ""})
	want := []string{"*", "collections.*", "records.read"}
	if len(got) != len(want) {
		t.Fatalf("expected %d permissions, got %d (%v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("permission[%d] = %q, want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}

func TestAPIKeyPermissionAllowed(t *testing.T) {
	tests := []struct {
		name      string
		granted   []string
		required  string
		wantAllow bool
	}{
		{name: "unrestricted empty", granted: nil, required: "collections.read", wantAllow: true},
		{name: "exact", granted: []string{"collections.read"}, required: "collections.read", wantAllow: true},
		{name: "wildcard", granted: []string{"collections.*"}, required: "collections.write", wantAllow: true},
		{name: "global", granted: []string{"*"}, required: "api_keys.write", wantAllow: true},
		{name: "denied", granted: []string{"collections.read"}, required: "collections.write", wantAllow: false},
	}

	for _, tt := range tests {
		if got := auth.APIKeyPermissionAllowed(tt.granted, tt.required); got != tt.wantAllow {
			t.Fatalf("%s: got %v want %v", tt.name, got, tt.wantAllow)
		}
	}
}

func generateES256TestKeys(t *testing.T) (string, string) {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate private key: %v", err)
	}

	privateBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	publicBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}

	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privateBytes})
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicBytes})
	return string(privatePEM), string(publicPEM)
}
