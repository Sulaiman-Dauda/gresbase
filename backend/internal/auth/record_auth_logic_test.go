package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gresbase/gresbase/internal/config"
	"github.com/jackc/pgx/v5/pgconn"
)

type schemaField = struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

func newRecordAuthTestService(t *testing.T) *RecordAuthService {
	t.Helper()
	cfg := &config.Config{
		JWTSecret:         "record-auth-test-secret",
		AccessTokenExpiry: 15 * time.Minute, RefreshTokenExpiry: time.Hour,
		LogLevel: "error",
	}
	return NewRecordAuthService(nil, cfg, nil)
}

func signRecordClaims(t *testing.T, cfg *config.Config, claims *RecordAuthClaims) string {
	t.Helper()
	token, err := signClaims(cfg, claims)
	if err != nil {
		t.Fatalf("sign record claims: %v", err)
	}
	return token
}

func recordClaims(tokenType string, expiresIn time.Duration) *RecordAuthClaims {
	now := time.Now()
	return &RecordAuthClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        generateID(),
			Subject:   "rec-1",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(expiresIn)),
			Issuer:    "gresbase",
		},
		RecordID:     "rec-1",
		CollectionID: "coll-1",
		Email:        "user@example.com",
		Verified:     true,
		Type:         tokenType,
	}
}

func TestFindIdentityField(t *testing.T) {
	svc := newRecordAuthTestService(t)

	tests := []struct {
		name   string
		schema []schemaField
		want   string
	}{
		{
			name:   "email type takes priority",
			schema: []schemaField{{Name: "username", Type: "text"}, {Name: "contact", Type: "email"}},
			want:   "contact",
		},
		{
			name:   "fallback to field named email",
			schema: []schemaField{{Name: "email", Type: "text"}},
			want:   "email",
		},
		{
			name:   "fallback to field named username",
			schema: []schemaField{{Name: "username", Type: "text"}},
			want:   "username",
		},
		{
			name:   "no identity field",
			schema: []schemaField{{Name: "bio", Type: "text"}},
			want:   "",
		},
		{
			name:   "empty schema",
			schema: nil,
			want:   "",
		},
	}

	for _, tt := range tests {
		if got := svc.findIdentityField(tt.schema); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestFindPasswordField(t *testing.T) {
	svc := newRecordAuthTestService(t)

	schema := []schemaField{
		{Name: "email", Type: "email"},
		{Name: "secret", Type: "password"},
	}
	if got := svc.findPasswordField(schema); got != "secret" {
		t.Errorf("got %q, want secret", got)
	}

	// A field merely named "password" with a non-password type must not match.
	noMatch := []schemaField{{Name: "password", Type: "text"}}
	if got := svc.findPasswordField(noMatch); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestRowToMap(t *testing.T) {
	svc := newRecordAuthTestService(t)

	fields := []pgconn.FieldDescription{
		{Name: "id"},
		{Name: "email"},
	}
	values := []any{"rec-1", "user@example.com"}

	m := svc.rowToMap(fields, values)
	if m["id"] != "rec-1" || m["email"] != "user@example.com" {
		t.Errorf("unexpected map: %v", m)
	}
	if len(m) != 2 {
		t.Errorf("expected 2 entries, got %d", len(m))
	}
}

func TestValidateRecordToken(t *testing.T) {
	svc := newRecordAuthTestService(t)

	token := signRecordClaims(t, svc.cfg, recordClaims("record_auth", time.Hour))

	claims, err := svc.ValidateRecordToken(token)
	if err != nil {
		t.Fatalf("valid record token rejected: %v", err)
	}
	if claims.RecordID != "rec-1" || claims.CollectionID != "coll-1" {
		t.Errorf("unexpected claims: %+v", claims)
	}
	if !claims.Verified {
		t.Error("verified flag should round-trip")
	}
}

func TestValidateRecordTokenRejectsTampered(t *testing.T) {
	svc := newRecordAuthTestService(t)

	token := signRecordClaims(t, svc.cfg, recordClaims("record_auth", time.Hour))

	// Flip a character in the middle of the signature segment (the final
	// base64 character only carries padding bits).
	sigStart := strings.LastIndex(token, ".") + 1
	mid := sigStart + (len(token)-sigStart)/2
	flipped := byte('A')
	if token[mid] == 'A' {
		flipped = 'B'
	}
	tampered := token[:mid] + string(flipped) + token[mid+1:]
	if _, err := svc.ValidateRecordToken(tampered); err == nil {
		t.Fatal("tampered record token must be rejected")
	}
}

func TestValidateRecordTokenRejectsExpired(t *testing.T) {
	svc := newRecordAuthTestService(t)

	token := signRecordClaims(t, svc.cfg, recordClaims("record_auth", -time.Minute))
	if _, err := svc.ValidateRecordToken(token); err == nil {
		t.Fatal("expired record token must be rejected")
	}
}

func TestValidateRecordTokenRejectsAlgNone(t *testing.T) {
	svc := newRecordAuthTestService(t)

	noneToken, err := jwt.NewWithClaims(jwt.SigningMethodNone, recordClaims("record_auth", time.Hour)).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ValidateRecordToken(noneToken); err == nil {
		t.Fatal("alg=none record token must be rejected")
	}
}

func TestValidateRecordTokenRejectsWrongKey(t *testing.T) {
	svc := newRecordAuthTestService(t)

	otherCfg := &config.Config{
		JWTSecret:         "a-completely-different-secret",
		AccessTokenExpiry: 15 * time.Minute, RefreshTokenExpiry: time.Hour,
		LogLevel: "error",
	}
	token := signRecordClaims(t, otherCfg, recordClaims("record_auth", time.Hour))

	if _, err := svc.ValidateRecordToken(token); err == nil {
		t.Fatal("record token signed with a different secret must be rejected")
	}
}

func TestRefreshRecordTokenRejectsWrongType(t *testing.T) {
	svc := newRecordAuthTestService(t)
	ctx := context.Background()

	// An access ("record_auth") token must not be accepted for refresh.
	// The type check happens before any database access.
	accessToken := signRecordClaims(t, svc.cfg, recordClaims("record_auth", time.Hour))
	if _, _, err := svc.RefreshRecordToken(ctx, accessToken); err == nil {
		t.Fatal("record access token must not be usable as a refresh token")
	} else if !strings.Contains(err.Error(), "invalid token type") {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, _, err := svc.RefreshRecordToken(ctx, "garbage"); err == nil {
		t.Fatal("garbage refresh token must be rejected")
	}
}

func TestRecordConfirmationsRejectShortTokens(t *testing.T) {
	// All record confirmation flows must reject empty/short tokens before
	// touching the database (the service here has a nil DB).
	svc := NewRecordAuthService(nil, nil, nil)
	ctx := context.Background()

	for _, tok := range []string{"", "short", strings.Repeat("x", 31)} {
		if err := svc.ConfirmRecordPasswordReset(ctx, tok, "new-password"); err == nil {
			t.Errorf("ConfirmRecordPasswordReset(%q) must fail", tok)
		}
		if err := svc.ConfirmRecordVerification(ctx, tok); err == nil {
			t.Errorf("ConfirmRecordVerification(%q) must fail", tok)
		}
		if err := svc.ConfirmRecordEmailChange(ctx, tok); err == nil {
			t.Errorf("ConfirmRecordEmailChange(%q) must fail", tok)
		}
	}
}
