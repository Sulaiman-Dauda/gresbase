package security

import (
	"testing"
)

func TestHashPassword(t *testing.T) {
	hash, err := HashPassword("my-secret-password")
	if err != nil {
		t.Fatalf("hash failed: %v", err)
	}
	if hash == "" {
		t.Error("hash should not be empty")
	}
	if hash == "my-secret-password" {
		t.Error("hash should not equal original password")
	}
	if !stringsHasPrefix(hash, "$sha256$") {
		t.Error("hash should have $sha256$ prefix")
	}
}

func TestVerifyPassword(t *testing.T) {
	hash, _ := HashPassword("correct-horse-battery-staple")

	if !VerifyPassword(hash, "correct-horse-battery-staple") {
		t.Error("password should match")
	}
	if VerifyPassword(hash, "wrong-password") {
		t.Error("wrong password should not match")
	}
}

func TestRandomString(t *testing.T) {
	s1, err := RandomString(32)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := RandomString(32)
	if err != nil {
		t.Fatal(err)
	}

	if s1 == "" {
		t.Error("string should not be empty")
	}
	if len(s1) != 32 {
		t.Errorf("expected length 32, got %d", len(s1))
	}
	if s1 == s2 {
		t.Error("random strings should differ")
	}
}

func TestRandomBytes(t *testing.T) {
	b, err := RandomBytes(16)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 16 {
		t.Errorf("expected 16 bytes, got %d", len(b))
	}
}

func TestAESEncryptDecrypt(t *testing.T) {
	key, _ := RandomBytes(32)
	plaintext := []byte("secret message that needs encryption")

	ciphertext, err := EncryptAES(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}
	if string(ciphertext) == string(plaintext) {
		t.Error("ciphertext should differ from plaintext")
	}

	decrypted, err := DecryptAES(key, ciphertext)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Errorf("decrypted mismatch: got %q, want %q", string(decrypted), string(plaintext))
	}
}

func TestHMACSHA256(t *testing.T) {
	mac := HMACSHA256([]byte("key"), []byte("data"))
	if len(mac) != 32 {
		t.Errorf("expected 32-byte HMAC, got %d", len(mac))
	}

	// Same inputs produce same MAC
	mac2 := HMACSHA256([]byte("key"), []byte("data"))
	if string(mac) != string(mac2) {
		t.Error("HMAC should be deterministic")
	}

	// Different inputs produce different MAC
	mac3 := HMACSHA256([]byte("key"), []byte("different"))
	if string(mac) == string(mac3) {
		t.Error("different data should produce different HMAC")
	}
}

func TestSHA256Hash(t *testing.T) {
	h := SHA256Hash("hello")
	if len(h) != 64 {
		t.Errorf("expected 64-char hex hash, got %d", len(h))
	}

	// Deterministic
	h2 := SHA256Hash("hello")
	if h != h2 {
		t.Error("SHA256 should be deterministic")
	}
}

func TestConstantTimeCompare(t *testing.T) {
	if !ConstantTimeCompare("abc", "abc") {
		t.Error("identical strings should match")
	}
	if ConstantTimeCompare("abc", "abd") {
		t.Error("different strings should not match")
	}
	if ConstantTimeCompare("abc", "ab") {
		t.Error("different length strings should not match")
	}
}

func TestSanitizeHTML(t *testing.T) {
	input := `<p>Hello</p><script>alert('xss')</script><a href="javascript:evil()">click</a>`
	result := SanitizeHTML(input)

	if contains(result, "<script>") {
		t.Error("script tags should be removed")
	}
	if contains(result, "javascript:") {
		t.Error("javascript: URLs should be removed")
	}
	if !contains(result, "<p>Hello</p>") {
		t.Error("safe HTML should be preserved")
	}
}

func TestIsValidDomain(t *testing.T) {
	valid := []string{"example.com", "sub.example.com", "test.co.uk"}
	for _, d := range valid {
		if !IsValidDomain(d) {
			t.Errorf("expected valid domain: %s", d)
		}
	}

	invalid := []string{"", "not a domain", ".com", "example"}
	for _, d := range invalid {
		if IsValidDomain(d) {
			t.Errorf("expected invalid domain: %s", d)
		}
	}
}

func TestIsValidEmail(t *testing.T) {
	valid := []string{"test@example.com", "user+tag@domain.co.uk"}
	for _, e := range valid {
		if !IsValidEmail(e) {
			t.Errorf("expected valid email: %s", e)
		}
	}

	invalid := []string{"", "not-an-email", "@domain.com", "user@", "user @domain.com"}
	for _, e := range invalid {
		if IsValidEmail(e) {
			t.Errorf("expected invalid email: %s", e)
		}
	}
}

func TestMaskString(t *testing.T) {
	result := MaskString("gb_abcdef1234567890", 7)
	// gb_abcd + 14 asterisks if original is 21 chars, or fewer if shorter
	if len(result) < 7 {
		t.Errorf("expected at least 7 chars, got %d", len(result))
	}
	if result[:7] != "gb_abcd" {
		t.Errorf("expected prefix 'gb_abcd', got %q", result[:7])
	}
}

func TestTokenPrefix(t *testing.T) {
	result := TokenPrefix("gb_abcdef1234567890", 10)
	if result != "gb_abcdef1..." {
		t.Errorf("expected 'gb_abcdef1...', got %q", result)
	}

	// Short token
	result = TokenPrefix("ab", 10)
	if result != "ab" {
		t.Errorf("expected 'ab', got %q", result)
	}
}

func TestValidatePasswordStrength(t *testing.T) {
	score, issues := ValidatePasswordStrength("weak")
	if score >= 3 {
		t.Errorf("weak password got score %d, expected < 3", score)
	}
	if len(issues) == 0 {
		t.Error("weak password should have issues")
	}

	score2, issues2 := ValidatePasswordStrength("Str0ng!Pass")
	if score2 < 4 {
		t.Errorf("strong password got score %d, expected >= 4 (issues: %v)", score2, issues2)
	}
}

func stringsHasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
