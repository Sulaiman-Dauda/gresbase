package auth_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gresbase/gresbase/internal/auth"
	"github.com/gresbase/gresbase/internal/config"
)

func hs256TestConfig() *config.Config {
	return &config.Config{
		JWTSecret:          "test-jwt-security-secret",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenExpiry: 7 * 24 * time.Hour,
		AdminTokenExpiry:   24 * time.Hour,
		LogLevel:           "error",
	}
}

func TestValidateTokenRejectsTamperedPayload(t *testing.T) {
	svc := auth.NewService(nil, hs256TestConfig())

	access, _, err := svc.GenerateTokens("admin-1", "user@test.com", "admin", "default")
	if err != nil {
		t.Fatal(err)
	}

	parts := strings.Split(access, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 JWT segments, got %d", len(parts))
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	// Attempt privilege escalation by editing the role claim.
	payload["role"] = "super_admin"
	tampered, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	parts[1] = base64.RawURLEncoding.EncodeToString(tampered)

	if _, err := svc.ValidateToken(strings.Join(parts, ".")); err == nil {
		t.Fatal("token with tampered role claim must be rejected")
	}
}

func TestValidateTokenRejectsTamperedSignature(t *testing.T) {
	svc := auth.NewService(nil, hs256TestConfig())

	access, _, err := svc.GenerateTokens("admin-1", "user@test.com", "admin", "default")
	if err != nil {
		t.Fatal(err)
	}

	// Flip a character in the middle of the signature segment (the final
	// base64 character only carries padding bits).
	sigStart := strings.LastIndex(access, ".") + 1
	mid := sigStart + (len(access)-sigStart)/2
	flipped := byte('A')
	if access[mid] == 'A' {
		flipped = 'B'
	}
	tampered := access[:mid] + string(flipped) + access[mid+1:]

	if _, err := svc.ValidateToken(tampered); err == nil {
		t.Fatal("token with modified signature must be rejected")
	}
}

func TestValidateTokenRejectsAlgNone(t *testing.T) {
	cfg := hs256TestConfig()
	svc := auth.NewService(nil, cfg)

	now := time.Now()
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "admin-1",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			Issuer:    "gresbase",
		},
		AdminID: "admin-1",
		Role:    "super_admin",
		Type:    auth.AccessToken,
	}

	noneToken, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("craft alg=none token: %v", err)
	}

	if _, err := svc.ValidateToken(noneToken); err == nil {
		t.Fatal("alg=none token must be rejected")
	}
}

func TestValidateTokenRejectsAlgConfusion(t *testing.T) {
	// ES256-configured service must reject an HS256 token signed with the
	// public key PEM as the HMAC secret (classic key-confusion attack).
	privateKeyPEM, publicKeyPEM := generateES256TestKeys(t)
	cfg := &config.Config{
		JWTAlgorithm:       "ES256",
		JWTKeyID:           "confusion-key",
		JWTPrivateKey:      privateKeyPEM,
		JWTPublicKey:       publicKeyPEM,
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenExpiry: time.Hour,
		LogLevel:           "error",
	}
	svc := auth.NewService(nil, cfg)

	now := time.Now()
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "attacker",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			Issuer:    "gresbase",
		},
		AdminID: "attacker",
		Role:    "super_admin",
		Type:    auth.AccessToken,
	}

	forged, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).
		SignedString([]byte(publicKeyPEM))
	if err != nil {
		t.Fatalf("craft HS256 token: %v", err)
	}

	if _, err := svc.ValidateToken(forged); err == nil {
		t.Fatal("HS256 token must be rejected by ES256-configured service")
	}
}

func TestValidateTokenRejectsForeignES256Key(t *testing.T) {
	// Token signed with key A must not validate against a service using key B.
	privateA, publicA := generateES256TestKeys(t)
	privateB, publicB := generateES256TestKeys(t)

	cfgA := &config.Config{
		JWTAlgorithm:      "ES256",
		JWTKeyID:          "key-a",
		JWTPrivateKey:     privateA,
		JWTPublicKey:      publicA,
		AccessTokenExpiry: 15 * time.Minute, RefreshTokenExpiry: time.Hour,
		LogLevel: "error",
	}
	cfgB := &config.Config{
		JWTAlgorithm:      "ES256",
		JWTKeyID:          "key-b",
		JWTPrivateKey:     privateB,
		JWTPublicKey:      publicB,
		AccessTokenExpiry: 15 * time.Minute, RefreshTokenExpiry: time.Hour,
		LogLevel: "error",
	}

	svcA := auth.NewService(nil, cfgA)
	svcB := auth.NewService(nil, cfgB)

	access, _, err := svcA.GenerateTokens("admin-1", "a@b.com", "admin", "default")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svcA.ValidateToken(access); err != nil {
		t.Fatalf("token must validate against its own key: %v", err)
	}
	if _, err := svcB.ValidateToken(access); err == nil {
		t.Fatal("token signed with foreign ES256 key must be rejected")
	}
}

func TestValidateTokenRejectsMismatchedPublicKey(t *testing.T) {
	// When an explicit JWT_PUBLIC_KEY is configured that does not match the
	// private key, verification must use the configured public key and fail.
	privateA, _ := generateES256TestKeys(t)
	_, publicB := generateES256TestKeys(t)

	cfg := &config.Config{
		JWTAlgorithm:      "ES256",
		JWTKeyID:          "mismatch-key",
		JWTPrivateKey:     privateA,
		JWTPublicKey:      publicB,
		AccessTokenExpiry: 15 * time.Minute, RefreshTokenExpiry: time.Hour,
		LogLevel: "error",
	}
	svc := auth.NewService(nil, cfg)

	access, _, err := svc.GenerateTokens("admin-1", "a@b.com", "admin", "default")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.ValidateToken(access); err == nil {
		t.Fatal("token must not validate against a mismatched public key")
	}
}

func TestValidateTokenRejectsGarbage(t *testing.T) {
	svc := auth.NewService(nil, hs256TestConfig())

	for _, tok := range []string{"", "not-a-jwt", "a.b", "a.b.c.d", "....."} {
		if _, err := svc.ValidateToken(tok); err == nil {
			t.Fatalf("garbage token %q must be rejected", tok)
		}
	}
}

func TestGenerateTokensFailsWithoutSecret(t *testing.T) {
	cfg := &config.Config{
		JWTSecret:         "",
		AccessTokenExpiry: 15 * time.Minute, RefreshTokenExpiry: time.Hour,
		LogLevel: "error",
	}
	svc := auth.NewService(nil, cfg)

	if _, _, err := svc.GenerateTokens("admin-1", "a@b.com", "admin", "default"); err == nil {
		t.Fatal("token generation must fail when JWT secret is empty")
	}
}

func TestUnsupportedJWTAlgorithmRejected(t *testing.T) {
	cfg := &config.Config{
		JWTAlgorithm:      "RS256",
		JWTSecret:         "irrelevant",
		AccessTokenExpiry: 15 * time.Minute, RefreshTokenExpiry: time.Hour,
		LogLevel: "error",
	}
	svc := auth.NewService(nil, cfg)

	if _, _, err := svc.GenerateTokens("admin-1", "a@b.com", "admin", "default"); err == nil {
		t.Fatal("unsupported JWT algorithm must be rejected")
	}
	if _, err := svc.ValidateToken("x.y.z"); err == nil {
		t.Fatal("validation must fail for unsupported JWT algorithm")
	}
}

func TestES256RejectsInvalidPrivateKeyPEM(t *testing.T) {
	cfg := &config.Config{
		JWTAlgorithm:      "ES256",
		JWTPrivateKey:     "not a pem block",
		AccessTokenExpiry: 15 * time.Minute, RefreshTokenExpiry: time.Hour,
		LogLevel: "error",
	}
	svc := auth.NewService(nil, cfg)

	if _, _, err := svc.GenerateTokens("admin-1", "a@b.com", "admin", "default"); err == nil {
		t.Fatal("invalid private key PEM must be rejected")
	}
}

func TestPublicJWKSEmptyForHS256(t *testing.T) {
	jwks, err := auth.PublicJWKS(hs256TestConfig())
	if err != nil {
		t.Fatalf("PublicJWKS: %v", err)
	}
	keys, ok := jwks["keys"].([]any)
	if !ok {
		t.Fatalf("unexpected keys type: %T", jwks["keys"])
	}
	if len(keys) != 0 {
		t.Fatalf("HS256 JWKS must never expose keys, got %d", len(keys))
	}
}

func TestPublicJWKSNilConfig(t *testing.T) {
	if _, err := auth.PublicJWKS(nil); err == nil {
		t.Fatal("PublicJWKS(nil) must return an error")
	}
}

func TestRefreshTokenClaimsContent(t *testing.T) {
	svc := auth.NewService(nil, hs256TestConfig())

	access, refresh, err := svc.GenerateTokens("admin-1", "a@b.com", "admin", "tenant-x")
	if err != nil {
		t.Fatal(err)
	}

	accessClaims, err := svc.ValidateToken(access)
	if err != nil {
		t.Fatal(err)
	}
	refreshClaims, err := svc.ValidateToken(refresh)
	if err != nil {
		t.Fatal(err)
	}

	if refreshClaims.Type != auth.RefreshToken {
		t.Errorf("refresh token type = %s, want %s", refreshClaims.Type, auth.RefreshToken)
	}
	if refreshClaims.TenantID != "tenant-x" {
		t.Errorf("tenant_id = %s, want tenant-x", refreshClaims.TenantID)
	}
	if refreshClaims.Issuer != "gresbase" {
		t.Errorf("issuer = %s, want gresbase", refreshClaims.Issuer)
	}
	if accessClaims.ID == "" || refreshClaims.ID == "" || accessClaims.ID == refreshClaims.ID {
		t.Errorf("access and refresh tokens must carry distinct non-empty JTIs (%q vs %q)",
			accessClaims.ID, refreshClaims.ID)
	}

	wantExpiry := time.Now().Add(7 * 24 * time.Hour)
	if d := refreshClaims.ExpiresAt.Time.Sub(wantExpiry); d < -time.Minute || d > time.Minute {
		t.Errorf("refresh expiry %v not within a minute of expected %v", refreshClaims.ExpiresAt.Time, wantExpiry)
	}
}

func TestRefreshTokenRejectsNonRefreshTokens(t *testing.T) {
	svc := auth.NewService(nil, hs256TestConfig())
	ctx := context.Background()

	access, _, err := svc.GenerateTokens("admin-1", "a@b.com", "admin", "default")
	if err != nil {
		t.Fatal(err)
	}

	// An access token must not be accepted as a refresh token. This check
	// happens before any database access.
	if _, _, err := svc.RefreshToken(ctx, access); err == nil {
		t.Fatal("access token must not be usable as a refresh token")
	} else if !strings.Contains(err.Error(), "not a refresh token") {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, _, err := svc.RefreshToken(ctx, "garbage-token"); err == nil {
		t.Fatal("garbage refresh token must be rejected")
	}
}
