package auth_test

import (
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
		JWTSecret:          "wrong-secret",
		AccessTokenExpiry:  15 * time.Minute,
		LogLevel:           "error",
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
