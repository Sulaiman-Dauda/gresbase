package auth

import (
	"testing"
)

func TestRecordAuthClaims(t *testing.T) {
	claims := &RecordAuthClaims{
		RecordID:     "rec123",
		CollectionID: "coll456",
		Email:        "user@test.com",
		Verified:     true,
		Type:         "record_auth",
	}

	if claims.Type != "record_auth" {
		t.Errorf("expected type record_auth, got %s", claims.Type)
	}
	if claims.RecordID != "rec123" {
		t.Errorf("expected record_id rec123, got %s", claims.RecordID)
	}
	if !claims.Verified {
		t.Error("expected verified to be true")
	}
}

func TestRecordAuthService_NewService(t *testing.T) {
	svc := NewRecordAuthService(nil, nil, nil)
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestRecordAuthService_QuoteIdent(t *testing.T) {
	svc := NewRecordAuthService(nil, nil, nil)

	if result := svc.quoteIdent("test"); result != `"test"` {
		t.Errorf("expected \"test\", got %q", result)
	}

	if result := svc.quoteIdent("te\"st"); result != `"te""st"` {
		t.Errorf("expected \"te\"\"st\", got %q", result)
	}
}

func TestGenerateToken(t *testing.T) {
	token := generateToken(32)
	if len(token) != 32 {
		t.Errorf("expected length 32, got %d", len(token))
	}

	// Should generate unique tokens
	token2 := generateToken(32)
	if token == token2 {
		t.Error("expected unique tokens")
	}
}

func TestRecordAuthResult(t *testing.T) {
	result := &RecordAuthResult{
		Token:        "access-token",
		RefreshToken: "refresh-token",
		Record:       map[string]any{"id": "rec123", "email": "test@example.com"},
	}

	if result.Token == "" {
		t.Error("expected non-empty token")
	}
	if result.RefreshToken == "" {
		t.Error("expected non-empty refresh token")
	}
	if result.Record == nil {
		t.Error("expected non-nil record")
	}
}

func TestRecordAuthClaimsValidation(t *testing.T) {
	// Test that claims are properly structured
	claims := &RecordAuthClaims{
		Type: "record_refresh",
	}

	if claims.Type != "record_refresh" {
		t.Error("expected record_refresh type")
	}

	// Test that empty claims don't panic
	if claims.Verified {
		t.Error("expected verified to be false by default")
	}
	if claims.Email != "" {
		t.Error("expected empty email by default")
	}
}

func TestNewRecordAuthService_NilParams(t *testing.T) {
	// Should not panic with nil params
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("NewRecordAuthService panicked with nil params: %v", r)
		}
	}()
	svc := NewRecordAuthService(nil, nil, nil)
	if svc == nil {
		t.Error("expected non-nil service even with nil params")
	}
}
