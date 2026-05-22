package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/gresbase/gresbase/internal/database"
	"github.com/gresbase/gresbase/internal/mailer"
	"github.com/rs/zerolog/log"
)

// VerificationService handles email verification, password reset, and email change flows.
type VerificationService struct {
	db     *database.DB
	mailer *mailer.Service
}

// NewVerificationService creates a verification service.
func NewVerificationService(db *database.DB, mailer *mailer.Service) *VerificationService {
	return &VerificationService{db: db, mailer: mailer}
}

// ---------------------------------------------------------------------------
// Email Verification
// ---------------------------------------------------------------------------

// RequestEmailVerification sends a verification email to the user.
func (s *VerificationService) RequestEmailVerification(ctx context.Context, adminID, email string) error {
	token := generateSecureToken()
	tokenHash := hashToken(token)
	expiresAt := time.Now().Add(24 * time.Hour)

	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO _verifications (id, admin_id, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		generateID16(), adminID, tokenHash, expiresAt, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("store verification: %w", err)
	}

	// Send email
	if s.mailer != nil {
		go func() {
			link := fmt.Sprintf("%s/_/verify-email?token=%s", s.mailer.BaseURL(), token)
			if err := s.mailer.SendVerificationEmail(email, link); err != nil {
				log.Error().Err(err).Str("email", email).Msg("Failed to send verification email")
			}
		}()
	}

	log.Info().Str("admin_id", adminID).Msg("Email verification requested")
	return nil
}

// ConfirmEmailVerification confirms a verification token and marks the email as verified.
func (s *VerificationService) ConfirmEmailVerification(ctx context.Context, token string) error {
	tokenHash := hashToken(token)

	result, err := s.db.Pool.Exec(ctx, `
		DELETE FROM _verifications WHERE token_hash = $1 AND expires_at > $2`,
		tokenHash, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("verify token: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("invalid or expired verification token")
	}

	// Mark admin as verified
	var adminID string
	err = s.db.Pool.QueryRow(ctx, `
		UPDATE _admins SET verified = TRUE, updated_at = $1
		WHERE id = (SELECT admin_id FROM _verifications WHERE token_hash = $2)
		RETURNING id`,
		time.Now(), tokenHash,
	).Scan(&adminID)

	if err != nil {
		// Fallback: try to find by token
		return fmt.Errorf("mark verified: %w", err)
	}

	log.Info().Str("admin_id", adminID).Msg("Email verified")
	return nil
}

// IsVerified checks if a user's email is verified.
func (s *VerificationService) IsVerified(ctx context.Context, adminID string) bool {
	var verified bool
	err := s.db.Pool.QueryRow(ctx,
		"SELECT COALESCE(verified, FALSE) FROM _admins WHERE id = $1", adminID,
	).Scan(&verified)
	return err == nil && verified
}

// ---------------------------------------------------------------------------
// Password Reset
// ---------------------------------------------------------------------------

// RequestPasswordReset sends a password reset email.
func (s *VerificationService) RequestPasswordReset(ctx context.Context, email string) error {
	// Find admin by email (don't reveal if user doesn't exist for security)
	var adminID string
	var adminEmail string
	err := s.db.Pool.QueryRow(ctx,
		"SELECT id, email FROM _admins WHERE email = $1", email,
	).Scan(&adminID, &adminEmail)
	if err != nil {
		// Return success even if email not found (prevent enumeration)
		log.Debug().Str("email", email).Msg("Password reset requested for unknown email")
		return nil
	}

	token := generateSecureToken()
	tokenHash := hashToken(token)
	expiresAt := time.Now().Add(1 * time.Hour)

	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO _password_resets (id, admin_id, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		generateID16(), adminID, tokenHash, expiresAt, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("store password reset: %w", err)
	}

	// Send email
	if s.mailer != nil {
		go func() {
			link := fmt.Sprintf("%s/_/reset-password?token=%s", s.mailer.BaseURL(), token)
			if err := s.mailer.SendPasswordResetEmail(adminEmail, link); err != nil {
				log.Error().Err(err).Str("email", adminEmail).Msg("Failed to send password reset email")
			}
		}()
	}

	log.Info().Str("admin_id", adminID).Msg("Password reset requested")
	return nil
}

// ConfirmPasswordReset confirms the reset token and sets a new password.
func (s *VerificationService) ConfirmPasswordReset(ctx context.Context, token, newPassword string, authService *Service) error {
	tokenHash := hashToken(token)

	var adminID string
	err := s.db.Pool.QueryRow(ctx, `
		SELECT admin_id FROM _password_resets
		WHERE token_hash = $1 AND expires_at > $2`,
		tokenHash, time.Now(),
	).Scan(&adminID)

	if err != nil {
		return fmt.Errorf("invalid or expired reset token")
	}

	// Hash new password
	passwordHash, err := authService.HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	// Update password and clean up token
	_, err = s.db.Pool.Exec(ctx, `
		UPDATE _admins SET password_hash = $1, updated_at = $2 WHERE id = $3`,
		passwordHash, time.Now(), adminID,
	)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}

	// Delete used token
	s.db.Pool.Exec(ctx, "DELETE FROM _password_resets WHERE token_hash = $1", tokenHash)

	log.Info().Str("admin_id", adminID).Msg("Password reset confirmed")
	return nil
}

// ---------------------------------------------------------------------------
// Email Change
// ---------------------------------------------------------------------------

// RequestEmailChange sends a confirmation email for an email address change.
func (s *VerificationService) RequestEmailChange(ctx context.Context, adminID, newEmail string) error {
	token := generateSecureToken()
	tokenHash := hashToken(token)
	expiresAt := time.Now().Add(1 * time.Hour)

	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO _email_changes (id, admin_id, new_email, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		generateID16(), adminID, newEmail, tokenHash, expiresAt, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("store email change: %w", err)
	}

	// Send confirmation to NEW email
	if s.mailer != nil {
		go func() {
			link := fmt.Sprintf("%s/_/confirm-email-change?token=%s", s.mailer.BaseURL(), token)
			if err := s.mailer.SendEmailChangeConfirmation(newEmail, link); err != nil {
				log.Error().Err(err).Str("email", newEmail).Msg("Failed to send email change confirmation")
			}
		}()
	}

	log.Info().Str("admin_id", adminID).Str("new_email", newEmail).Msg("Email change requested")
	return nil
}

// ConfirmEmailChange confirms the email change token and updates the email.
func (s *VerificationService) ConfirmEmailChange(ctx context.Context, token string) error {
	tokenHash := hashToken(token)

	var adminID, newEmail string
	err := s.db.Pool.QueryRow(ctx, `
		SELECT admin_id, new_email FROM _email_changes
		WHERE token_hash = $1 AND expires_at > $2`,
		tokenHash, time.Now(),
	).Scan(&adminID, &newEmail)

	if err != nil {
		return fmt.Errorf("invalid or expired email change token")
	}

	// Update email
	_, err = s.db.Pool.Exec(ctx,
		"UPDATE _admins SET email = $1, updated_at = $2 WHERE id = $3",
		newEmail, time.Now(), adminID,
	)
	if err != nil {
		return fmt.Errorf("update email: %w", err)
	}

	// Clean up
	s.db.Pool.Exec(ctx, "DELETE FROM _email_changes WHERE admin_id = $1", adminID)

	log.Info().Str("admin_id", adminID).Str("new_email", newEmail).Msg("Email change confirmed")
	return nil
}

// ---------------------------------------------------------------------------
// Magic Link (OTP-less login)
// ---------------------------------------------------------------------------

// RequestMagicLink sends a magic link for passwordless login.
func (s *VerificationService) RequestMagicLink(ctx context.Context, email string) error {
	var adminID, adminEmail string
	err := s.db.Pool.QueryRow(ctx,
		"SELECT id, email FROM _admins WHERE email = $1", email,
	).Scan(&adminID, &adminEmail)
	if err != nil {
		// Don't reveal user doesn't exist
		return nil
	}

	token := generateSecureToken()
	tokenHash := hashToken(token)
	expiresAt := time.Now().Add(15 * time.Minute)

	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO _magic_links (id, admin_id, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		generateID16(), adminID, tokenHash, expiresAt, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("store magic link: %w", err)
	}

	if s.mailer != nil {
		go func() {
			link := fmt.Sprintf("%s/_/magic-link?token=%s", s.mailer.BaseURL(), token)
			if err := s.mailer.SendMagicLinkEmail(adminEmail, link); err != nil {
				log.Error().Err(err).Str("email", adminEmail).Msg("Failed to send magic link")
			}
		}()
	}

	log.Info().Str("admin_id", adminID).Msg("Magic link requested")
	return nil
}

// VerifyMagicLink validates a magic link token and returns the admin ID.
func (s *VerificationService) VerifyMagicLink(ctx context.Context, token string) (string, error) {
	tokenHash := hashToken(token)

	var adminID string
	err := s.db.Pool.QueryRow(ctx, `
		SELECT admin_id FROM _magic_links
		WHERE token_hash = $1 AND expires_at > $2`,
		tokenHash, time.Now(),
	).Scan(&adminID)

	if err != nil {
		return "", fmt.Errorf("invalid or expired magic link")
	}

	// Delete used token
	s.db.Pool.Exec(ctx, "DELETE FROM _magic_links WHERE token_hash = $1", tokenHash)

	return adminID, nil
}

// ---------------------------------------------------------------------------
// OTP (One-Time Password)
// ---------------------------------------------------------------------------

// RequestOTP sends a one-time password to the user's email.
func (s *VerificationService) RequestOTP(ctx context.Context, email string) error {
	var adminID string
	err := s.db.Pool.QueryRow(ctx,
		"SELECT id FROM _admins WHERE email = $1", email,
	).Scan(&adminID)
	if err != nil {
		// Don't reveal
		return nil
	}

	code := generateNumericCode(6)
	codeHash := hashToken(code)
	expiresAt := time.Now().Add(5 * time.Minute)

	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO _otp (id, admin_id, code_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		generateID16(), adminID, codeHash, expiresAt, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("store OTP: %w", err)
	}

	if s.mailer != nil {
		go func() {
			if err := s.mailer.SendOTPEmail(email, code); err != nil {
				log.Error().Err(err).Str("email", email).Msg("Failed to send OTP email")
			}
		}()
	}

	log.Info().Str("admin_id", adminID).Msg("OTP requested")
	return nil
}

// VerifyOTP validates an OTP code and returns the admin ID.
func (s *VerificationService) VerifyOTP(ctx context.Context, email, code string) (string, error) {
	var adminID string
	err := s.db.Pool.QueryRow(ctx,
		"SELECT id FROM _admins WHERE email = $1", email,
	).Scan(&adminID)
	if err != nil {
		return "", fmt.Errorf("invalid credentials")
	}

	codeHash := hashToken(code)

	result, err := s.db.Pool.Exec(ctx, `
		DELETE FROM _otp WHERE admin_id = $1 AND code_hash = $2 AND expires_at > $3`,
		adminID, codeHash, time.Now(),
	)
	if err != nil || result.RowsAffected() == 0 {
		return "", fmt.Errorf("invalid or expired OTP code")
	}

	return adminID, nil
}

// ---------------------------------------------------------------------------
// Crypto Helpers
// ---------------------------------------------------------------------------

func generateSecureToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func generateID16() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func generateNumericCode(digits int) string {
	b := make([]byte, digits)
	const numbers = "0123456789"
	for i := range b {
		rb := make([]byte, 1)
		rand.Read(rb)
		b[i] = numbers[int(rb[0])%len(numbers)]
	}
	return string(b)
}
