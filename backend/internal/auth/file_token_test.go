package auth_test

import (
	"strings"
	"testing"
	"time"

	"github.com/gresbase/gresbase/internal/auth"
)

func TestGenerateAndValidateFileTokenAdmin(t *testing.T) {
	svc := auth.NewService(nil, hs256TestConfig())

	token, err := svc.GenerateFileToken(auth.FileTokenClaims{
		AdminID:  "admin-1",
		Role:     "admin",
		Email:    "a@b.com",
		TenantID: "default",
	})
	if err != nil {
		t.Fatalf("generate file token: %v", err)
	}

	claims, err := svc.ValidateFileToken(token)
	if err != nil {
		t.Fatalf("validate file token: %v", err)
	}

	if claims.Type != auth.FileToken {
		t.Errorf("type = %s, want %s", claims.Type, auth.FileToken)
	}
	if !claims.IsAdmin {
		t.Error("IsAdmin must be true when AdminID is set")
	}
	if claims.Subject != "admin-1" {
		t.Errorf("subject = %s, want admin-1", claims.Subject)
	}
	if claims.Issuer != "gresbase" {
		t.Errorf("issuer = %s, want gresbase", claims.Issuer)
	}

	// File tokens travel in URLs; their lifetime must stay short.
	wantExpiry := time.Now().Add(auth.FileTokenExpiry)
	if d := claims.ExpiresAt.Time.Sub(wantExpiry); d < -time.Minute || d > time.Minute {
		t.Errorf("file token expiry %v not within a minute of %v", claims.ExpiresAt.Time, wantExpiry)
	}
}

func TestGenerateFileTokenRecordIdentity(t *testing.T) {
	svc := auth.NewService(nil, hs256TestConfig())

	token, err := svc.GenerateFileToken(auth.FileTokenClaims{
		RecordID:     "rec-1",
		CollectionID: "coll-1",
		Verified:     true,
	})
	if err != nil {
		t.Fatal(err)
	}

	claims, err := svc.ValidateFileToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.IsAdmin {
		t.Error("IsAdmin must be false for record identities")
	}
	if claims.Subject != "rec-1" {
		t.Errorf("subject = %s, want rec-1", claims.Subject)
	}
	if claims.RecordID != "rec-1" || claims.CollectionID != "coll-1" {
		t.Errorf("record claims did not round-trip: %+v", claims)
	}
}

func TestValidateFileTokenRejectsOtherTokenTypes(t *testing.T) {
	// Access/refresh tokens leaked into URLs must not be replayable as file
	// tokens (they have far longer lifetimes).
	svc := auth.NewService(nil, hs256TestConfig())

	access, refresh, err := svc.GenerateTokens("admin-1", "a@b.com", "admin", "default")
	if err != nil {
		t.Fatal(err)
	}

	for _, tok := range []string{access, refresh} {
		if _, err := svc.ValidateFileToken(tok); err == nil {
			t.Error("non-file token must be rejected by ValidateFileToken")
		}
	}
}

func TestValidateFileTokenRejectsTamperedAndForeign(t *testing.T) {
	svc := auth.NewService(nil, hs256TestConfig())

	token, err := svc.GenerateFileToken(auth.FileTokenClaims{AdminID: "admin-1"})
	if err != nil {
		t.Fatal(err)
	}

	// Tamper with the middle of the signature.
	sigStart := strings.LastIndex(token, ".") + 1
	mid := sigStart + (len(token)-sigStart)/2
	flipped := byte('A')
	if token[mid] == 'A' {
		flipped = 'B'
	}
	if _, err := svc.ValidateFileToken(token[:mid] + string(flipped) + token[mid+1:]); err == nil {
		t.Error("tampered file token must be rejected")
	}

	// Token from a service with a different secret must be rejected.
	otherCfg := hs256TestConfig()
	otherCfg.JWTSecret = "different-secret-entirely"
	otherSvc := auth.NewService(nil, otherCfg)
	foreign, err := otherSvc.GenerateFileToken(auth.FileTokenClaims{AdminID: "admin-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ValidateFileToken(foreign); err == nil {
		t.Error("file token signed with a different secret must be rejected")
	}

	if _, err := svc.ValidateFileToken("garbage"); err == nil {
		t.Error("garbage file token must be rejected")
	}
}
