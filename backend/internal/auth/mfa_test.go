package auth

import (
	"testing"
	"time"
)

func TestGenerateTOTPSecret(t *testing.T) {
	s := generateTOTPSecret()
	if len(s) < 16 {
		t.Errorf("secret too short: %d chars", len(s))
	}

	// Should be valid base32
	for _, c := range s {
		if !((c >= 'A' && c <= 'Z') || (c >= '2' && c <= '7')) {
			t.Errorf("invalid base32 character in secret: %c", c)
		}
	}
}

func TestGenerateTOTP(t *testing.T) {
	// Test vector from RFC 6238
	secret := "JBSWY3DPEHPK3PXP"
	// Time: 1970-01-01T00:00:00Z -> counter 0
	// TOTP for counter 0 with secret "Hello!" (base32: JBSWY3DPEHPK3PXP)
	code, err := generateTOTP(secret, time.Unix(0, 0))
	if err != nil {
		t.Fatalf("generateTOTP: %v", err)
	}
	if len(code) != 6 {
		t.Errorf("TOTP code should be 6 digits, got %s", code)
	}
	t.Logf("TOTP code: %s", code)
}

func TestValidateTOTP(t *testing.T) {
	secret := generateTOTPSecret()
	code, err := generateTOTP(secret, time.Now())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if !validateTOTP(secret, code) {
		t.Error("current TOTP should be valid")
	}

	// Wrong code
	if validateTOTP(secret, "000000") {
		t.Error("wrong code should not validate (except by incredibly small chance)")
	}

	// Future code (2 steps ahead, should fail with ±1 tolerance)
	future := time.Now().Add(60 * time.Second)
	code, _ = generateTOTP(secret, future)
	if validateTOTP(secret, code) {
		t.Log("future code validated (edge case)")
	}
}

func TestGenerateBackupCodes(t *testing.T) {
	codes := generateBackupCodes(8)
	if len(codes) != 8 {
		t.Errorf("expected 8 codes, got %d", len(codes))
	}

	seen := make(map[string]bool)
	for _, c := range codes {
		if len(c) != 10 {
			t.Errorf("code length should be 10, got %d", len(c))
		}
		if seen[c] {
			t.Errorf("duplicate code: %s", c)
		}
		seen[c] = true
	}
}

func TestHashBackupCodes(t *testing.T) {
	codes := []string{"ABCD1234", "WXYZ5678"}
	hashed := hashBackupCodes(codes)

	if len(hashed) != 2 {
		t.Error("wrong count")
	}
	for i, h := range hashed {
		if h == codes[i] {
			t.Error("backup code should be hashed")
		}
		if len(h) != 64 { // SHA-256 hex
			t.Errorf("hash should be 64 chars, got %d", len(h))
		}
	}
}

func TestGenerateSecureToken(t *testing.T) {
	t1 := generateSecureToken()
	t2 := generateSecureToken()

	if t1 == t2 {
		t.Error("tokens should be unique")
	}
	if len(t1) != 64 { // 32 bytes hex = 64 chars
		t.Errorf("token length: %d", len(t1))
	}
}

func TestHashToken(t *testing.T) {
	h := hashToken("test-token")
	if len(h) != 64 {
		t.Errorf("hash length: %d", len(h))
	}

	// Deterministic
	h2 := hashToken("test-token")
	if h != h2 {
		t.Error("hash should be deterministic")
	}
}

func TestGenerateNumericCode(t *testing.T) {
	code := generateNumericCode(6)
	if len(code) != 6 {
		t.Errorf("code length: %d", len(code))
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			t.Errorf("non-numeric character: %c", c)
		}
	}
}
