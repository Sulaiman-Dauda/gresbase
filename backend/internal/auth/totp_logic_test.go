package auth

import (
	"strings"
	"testing"
	"time"
)

// rfc6238Secret is the base32 encoding of the ASCII seed "12345678901234567890"
// from RFC 6238 Appendix B.
const rfc6238Secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestGenerateTOTPRFC6238Vectors(t *testing.T) {
	// Expected values are the 6-digit truncations of the RFC 6238 SHA-1
	// reference vectors (which are listed as 8 digits).
	vectors := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	}

	for _, v := range vectors {
		got, err := generateTOTP(rfc6238Secret, time.Unix(v.unix, 0))
		if err != nil {
			t.Fatalf("generateTOTP(t=%d): %v", v.unix, err)
		}
		if got != v.want {
			t.Errorf("generateTOTP(t=%d) = %s, want %s", v.unix, got, v.want)
		}
	}
}

func TestValidateTOTPWindow(t *testing.T) {
	secret := generateTOTPSecret()

	// validateTOTP uses time.Now() internally; avoid flaking when the test
	// starts right before a 30s step boundary.
	if time.Now().Unix()%30 >= 27 {
		time.Sleep(time.Duration(30-time.Now().Unix()%30+1) * time.Second)
	}
	now := time.Now()

	codeAt := func(offset time.Duration) string {
		t.Helper()
		code, err := generateTOTP(secret, now.Add(offset))
		if err != nil {
			t.Fatalf("generateTOTP: %v", err)
		}
		return code
	}

	current := codeAt(0)
	prev := codeAt(-30 * time.Second)
	next := codeAt(30 * time.Second)
	tooOld := codeAt(-60 * time.Second)
	tooNew := codeAt(60 * time.Second)

	if !validateTOTP(secret, current) {
		t.Error("current step code must validate")
	}
	if !validateTOTP(secret, prev) {
		t.Error("previous step code must validate (clock skew tolerance)")
	}
	if !validateTOTP(secret, next) {
		t.Error("next step code must validate (clock skew tolerance)")
	}

	inWindow := map[string]bool{current: true, prev: true, next: true}
	if !inWindow[tooOld] && validateTOTP(secret, tooOld) {
		t.Error("code two steps in the past must be rejected")
	}
	if !inWindow[tooNew] && validateTOTP(secret, tooNew) {
		t.Error("code two steps in the future must be rejected")
	}

	if validateTOTP(secret, "") {
		t.Error("empty code must be rejected")
	}
	if validateTOTP(secret, "abcdef") {
		t.Error("non-numeric code must be rejected")
	}
}

func TestValidateTOTPWrongSecret(t *testing.T) {
	secretA := generateTOTPSecret()
	secretB := generateTOTPSecret()

	code, err := generateTOTP(secretA, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	otherCode, err := generateTOTP(secretB, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if code == otherCode {
		t.Skip("rare code collision between secrets")
	}
	if validateTOTP(secretB, code) {
		t.Error("code generated from a different secret must be rejected")
	}
}

func TestGenerateTOTPInvalidSecret(t *testing.T) {
	if _, err := generateTOTP("0189!", time.Now()); err == nil {
		t.Error("invalid base32 secret must return an error")
	}
	if validateTOTP("0189!", "123456") {
		t.Error("validateTOTP must reject invalid secrets")
	}
}

func TestGenerateTOTPNormalizesSecret(t *testing.T) {
	now := time.Now()
	upper, err := generateTOTP(rfc6238Secret, now)
	if err != nil {
		t.Fatal(err)
	}
	lower, err := generateTOTP("  "+strings.ToLower(rfc6238Secret)+" ", now)
	if err != nil {
		t.Fatalf("lowercase/padded secret should be accepted: %v", err)
	}
	if upper != lower {
		t.Errorf("normalized secrets must produce the same code: %s vs %s", upper, lower)
	}
}

func TestSha256HashKnownVector(t *testing.T) {
	got := sha256Hash("abc")
	want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got != want {
		t.Errorf("sha256Hash(abc) = %s, want %s", got, want)
	}
}

func TestGenerateIDFormat(t *testing.T) {
	id1 := generateID()
	id2 := generateID()
	if len(id1) != 32 {
		t.Errorf("generateID length = %d, want 32", len(id1))
	}
	if id1 == id2 {
		t.Error("IDs must be unique")
	}
}

func TestGenerateID16Format(t *testing.T) {
	id1 := generateID16()
	id2 := generateID16()
	if len(id1) != 32 { // 16 bytes hex-encoded
		t.Errorf("generateID16 length = %d, want 32", len(id1))
	}
	if id1 == id2 {
		t.Error("IDs must be unique")
	}
}

func TestGenerateRandomCodeCharset(t *testing.T) {
	// Charset deliberately excludes ambiguous characters (0, O, 1, I).
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	code := generateRandomCode(20)
	if len(code) != 20 {
		t.Errorf("length = %d, want 20", len(code))
	}
	for _, c := range code {
		if !strings.ContainsRune(charset, c) {
			t.Errorf("unexpected character %c in backup code", c)
		}
	}
}
