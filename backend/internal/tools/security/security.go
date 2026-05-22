// Package tools/security provides security-related utilities.
package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// HashPassword hashes a password using SHA-256 with a random salt.
// The result is format: $sha256$<salt>$<hash>
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", err
	}

	h := sha256.New()
	h.Write(salt)
	h.Write([]byte(password))
	hash := h.Sum(nil)

	return fmt.Sprintf("$sha256$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifyPassword checks a password against a hashed version.
func VerifyPassword(hashed, password string) bool {
	parts := strings.Split(hashed, "$")
	if len(parts) != 4 || parts[1] != "sha256" {
		return false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}

	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}

	h := sha256.New()
	h.Write(salt)
	h.Write([]byte(password))
	actualHash := h.Sum(nil)

	return subtle.ConstantTimeCompare(expectedHash, actualHash) == 1
}

// RandomString generates a cryptographically random string of specified length.
func RandomString(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes)[:length], nil
}

// RandomBytes generates cryptographically random bytes.
func RandomBytes(length int) ([]byte, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return nil, err
	}
	return bytes, nil
}

// EncryptAES encrypts data using AES-256-GCM with a 32-byte key.
func EncryptAES(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	// nonce + ciphertext
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// DecryptAES decrypts data encrypted with EncryptAES.
func DecryptAES(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	return plaintext, nil
}

// HMACSHA256 computes an HMAC-SHA256 signature.
func HMACSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

// SHA256Hash computes a SHA-256 hash and returns the hex string.
func SHA256Hash(data string) string {
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

// ConstantTimeCompare performs a constant-time comparison of two strings.
func ConstantTimeCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// SanitizeHTML strips potentially dangerous HTML tags and attributes.
func SanitizeHTML(html string) string {
	// Remove script tags
	scriptRe := regexp.MustCompile(`(?i)<script[^>]*>.*?</script>`)
	html = scriptRe.ReplaceAllString(html, "")

	// Remove event handlers
	eventRe := regexp.MustCompile(`(?i)\s+on\w+\s*=\s*"[^"]*"`)
	html = eventRe.ReplaceAllString(html, "")

	eventRe2 := regexp.MustCompile(`(?i)\s+on\w+\s*=\s*'[^']*'`)
	html = eventRe2.ReplaceAllString(html, "")

	// Remove javascript: URLs
	jsRe := regexp.MustCompile(`(?i)href\s*=\s*"javascript:[^"]*"`)
	html = jsRe.ReplaceAllString(html, `href="#"`)

	return html
}

// IsValidDomain checks if a string is a valid domain name.
func IsValidDomain(domain string) bool {
	re := regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$`)
	return re.MatchString(domain)
}

// IsValidEmail performs a basic email format validation.
func IsValidEmail(email string) bool {
	re := regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	return re.MatchString(email)
}

// MaskString masks a portion of a string (useful for API key display).
func MaskString(s string, visible int) string {
	if len(s) <= visible {
		return s
	}
	masked := len(s) - visible
	return s[:visible] + strings.Repeat("*", masked)
}

// TokenPrefix returns a prefix for display (e.g., "gb_abc123...").
func TokenPrefix(token string, length int) string {
	if len(token) <= length {
		return token
	}
	return token[:length] + "..."
}

// ValidatePasswordStrength checks password strength.
// Returns a score 0-4 and a list of issues.
func ValidatePasswordStrength(password string) (int, []string) {
	var issues []string
	score := 0

	if len(password) >= 8 {
		score++
	} else {
		issues = append(issues, "at least 8 characters")
	}

	if regexp.MustCompile(`[A-Z]`).MatchString(password) {
		score++
	} else {
		issues = append(issues, "uppercase letter")
	}

	if regexp.MustCompile(`[a-z]`).MatchString(password) {
		score++
	} else {
		issues = append(issues, "lowercase letter")
	}

	if regexp.MustCompile(`[0-9]`).MatchString(password) {
		score++
	} else {
		issues = append(issues, "digit")
	}

	if regexp.MustCompile(`[!@#$%^&*(),.?":{}|<>]`).MatchString(password) {
		score++
	} else {
		issues = append(issues, "special character")
	}

	return score, issues
}
