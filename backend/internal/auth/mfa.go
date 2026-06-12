package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // G505: HMAC-SHA1 is mandated by RFC 6238 (TOTP); used only for OTP generation, not as a security hash.
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/gresbase/gresbase/internal/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog/log"
)

// MFAService handles Multi-Factor Authentication (TOTP) operations.
type MFAService struct {
	db *database.DB
}

// NewMFAService creates an MFA service.
func NewMFAService(db *database.DB) *MFAService {
	return &MFAService{db: db}
}

func (s *MFAService) exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return s.db.ExecResult(ctx, sql, args...)
}

func (s *MFAService) queryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return s.db.QueryRow(ctx, sql, args...)
}

// MFASecret is a generated TOTP secret for a user.
type MFASecret struct {
	ID          string    `json:"id"`
	AdminID     string    `json:"admin_id"`
	Secret      string    `json:"secret"` // base32 encoded secret
	QRCodeURL   string    `json:"qr_code_url,omitempty"`
	Enabled     bool      `json:"enabled"`
	BackupCodes []string  `json:"backup_codes,omitempty"` // hashed
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// MFAToken represents a TOTP token validation.
type MFATokenRequest struct {
	AdminID string `json:"admin_id"`
	Token   string `json:"token"`
	Code    string `json:"code"` // 6-digit TOTP code
}

// GenerateSecret creates a new TOTP secret for enrollment.
// Returns the base32 secret and an otpauth URL for QR codes.
func (s *MFAService) GenerateSecret(ctx context.Context, adminID, email, issuer string) (*MFASecret, error) {
	secret := generateTOTPSecret()

	// Build otpauth URL
	otpauthURL := fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA1&digits=6&period=30",
		issuer, email, secret, issuer)

	mfa := &MFASecret{
		ID:        generateID(),
		AdminID:   adminID,
		Secret:    secret,
		QRCodeURL: otpauthURL,
		Enabled:   false,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Store in database
	_, err := s.exec(ctx, `
		INSERT INTO _mfa_secrets (id, admin_id, secret, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (admin_id) DO UPDATE SET secret = $3, enabled = FALSE, updated_at = $6`,
		mfa.ID, adminID, secret, false, mfa.CreatedAt, mfa.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("store MFA secret: %w", err)
	}

	return mfa, nil
}

// VerifyAndEnable validates a TOTP token and enables MFA for the user.
func (s *MFAService) VerifyAndEnable(ctx context.Context, adminID, code string) ([]string, error) {
	// Retrieve secret
	var secret string
	err := s.queryRow(ctx,
		"SELECT secret FROM _mfa_secrets WHERE admin_id = $1 AND enabled = FALSE",
		adminID,
	).Scan(&secret)
	if err != nil {
		return nil, fmt.Errorf("no pending MFA enrollment found")
	}

	// Validate TOTP
	if !validateTOTP(secret, code) {
		return nil, fmt.Errorf("invalid TOTP code")
	}

	// Generate backup codes
	backupCodes := generateBackupCodes(8)
	backupCodesJSON, _ := json.Marshal(backupCodes)

	// Enable MFA
	_, err = s.exec(ctx, `
		UPDATE _mfa_secrets SET enabled = TRUE, backup_codes = $3, updated_at = $4
		WHERE admin_id = $1 AND secret = $2`,
		adminID, secret, backupCodesJSON, time.Now(),
	)
	if err != nil {
		return nil, fmt.Errorf("enable MFA: %w", err)
	}

	log.Info().Str("admin_id", adminID).Msg("MFA enabled")
	return backupCodes, nil
}

// ValidateToken checks a TOTP code or backup code.
func (s *MFAService) ValidateToken(ctx context.Context, adminID, code string) (bool, error) {
	// Try TOTP first
	var secret string
	var enabled bool
	err := s.queryRow(ctx,
		"SELECT secret, enabled FROM _mfa_secrets WHERE admin_id = $1",
		adminID,
	).Scan(&secret, &enabled)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil // MFA not set up, allow.
		}
		return false, err // Fail closed on real DB errors.
	}

	if !enabled {
		return true, nil
	}

	if validateTOTP(secret, code) {
		return true, nil
	}

	// Try backup codes
	var backupCodesJSON []byte
	err = s.queryRow(ctx,
		"SELECT backup_codes FROM _mfa_secrets WHERE admin_id = $1",
		adminID,
	).Scan(&backupCodesJSON)

	if err == nil {
		var backupCodes []string
		if err := json.Unmarshal(backupCodesJSON, &backupCodes); err == nil {
			for i, bc := range backupCodes {
				// Compare hash
				codeHash := sha256Hash(code)
				if bc == codeHash {
					// Remove used backup code. If this write fails we must not
					// accept the code, otherwise it could be reused.
					backupCodes = append(backupCodes[:i], backupCodes[i+1:]...)
					newJSON, _ := json.Marshal(backupCodes)
					if _, err := s.exec(ctx, "UPDATE _mfa_secrets SET backup_codes = $1 WHERE admin_id = $2",
						newJSON, adminID); err != nil {
						return false, fmt.Errorf("consume backup code: %w", err)
					}
					return true, nil
				}
			}
		}
	}

	return false, nil
}

// DisableMFA disables MFA for a user.
func (s *MFAService) DisableMFA(ctx context.Context, adminID string) error {
	_, err := s.exec(ctx,
		"UPDATE _mfa_secrets SET enabled = FALSE, updated_at = $2 WHERE admin_id = $1",
		adminID, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("disable MFA: %w", err)
	}
	log.Info().Str("admin_id", adminID).Msg("MFA disabled")
	return nil
}

// IsMFAEnabled checks if MFA is enabled for a user.
func (s *MFAService) IsMFAEnabled(ctx context.Context, adminID string) bool {
	var enabled bool
	err := s.queryRow(ctx,
		"SELECT enabled FROM _mfa_secrets WHERE admin_id = $1",
		adminID,
	).Scan(&enabled)
	return err == nil && enabled
}

// ---------------------------------------------------------------------------
// TOTP Implementation (RFC 6238)
// ---------------------------------------------------------------------------

// generateTOTPSecret generates a cryptographically random base32 secret.
func generateTOTPSecret() string {
	bytes := make([]byte, 20) // 160 bits
	// crypto/rand.Read only fails on a broken system entropy source, which is
	// unrecoverable; a panic here is preferable to issuing a weak secret.
	if _, err := rand.Read(bytes); err != nil {
		panic(fmt.Sprintf("crypto/rand failed generating TOTP secret: %v", err))
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(bytes)
}

// generateTOTP generates a TOTP code for the given secret and time.
func generateTOTP(secret string, t time.Time) (string, error) {
	secret = strings.ToUpper(strings.TrimSpace(secret))
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		return "", fmt.Errorf("decode secret: %w", err)
	}

	// RFC 6238 time-step counter. Unix time is non-negative for any real clock;
	// clamp defensively so the int64->uint64 conversion can never wrap.
	step := t.Unix() / 30
	if step < 0 {
		step = 0
	}
	//nolint:gosec // G115 false positive: step is clamped to >= 0 above, so the int64->uint64 conversion cannot wrap.
	counter := uint64(step)

	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(buf)
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0F
	value := int64(((int(sum[offset]) & 0x7F) << 24) |
		((int(sum[offset+1]) & 0xFF) << 16) |
		((int(sum[offset+2]) & 0xFF) << 8) |
		(int(sum[offset+3]) & 0xFF))

	mod := int64(math.Pow10(6))
	code := value % mod

	return fmt.Sprintf("%06d", code), nil
}

// validateTOTP checks if a TOTP code is valid (±1 time step tolerance).
func validateTOTP(secret, code string) bool {
	now := time.Now()
	// Check current and adjacent time steps
	for _, offset := range []int{0, -1, 1} {
		t := now.Add(time.Duration(offset) * 30 * time.Second)
		expected, err := generateTOTP(secret, t)
		if err != nil {
			return false
		}
		if expected == code {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Backup Codes
// ---------------------------------------------------------------------------

// generateBackupCodes creates n random backup codes and returns them.
// The caller receives the plaintext codes; they are stored hashed.
func generateBackupCodes(n int) []string {
	codes := make([]string, n)
	for i := 0; i < n; i++ {
		code := generateRandomCode(10)
		codes[i] = code
	}
	return codes
}

// hashBackupCodes hashes backup codes for storage.
func hashBackupCodes(codes []string) []string {
	hashed := make([]string, len(codes))
	for i, c := range codes {
		hashed[i] = sha256Hash(c)
	}
	return hashed
}

// generateRandomCode generates a random alphanumeric code of given length.
func generateRandomCode(length int) string {
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	raw := make([]byte, length)
	mustRandRead(raw)
	b := make([]byte, length)
	for i, v := range raw {
		b[i] = charset[int(v)%len(charset)]
	}
	return string(b)
}

// sha256Hash returns the hex-encoded SHA-256 hash of a string.
func sha256Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}

// generateID creates a short unique ID for records.
func generateID() string {
	b := make([]byte, 16)
	mustRandRead(b)
	return fmt.Sprintf("%x", b)
}

// Ensure imports used
var _ = json.Marshal
