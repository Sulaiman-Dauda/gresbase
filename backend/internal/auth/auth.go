package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	stdsql "database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gresbase/gresbase/internal/config"
	"github.com/gresbase/gresbase/internal/database"
	"github.com/gresbase/gresbase/internal/netutil"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/hkdf"
)

// TokenType represents the type of token.
type TokenType string

const (
	AccessToken  TokenType = "access"
	RefreshToken TokenType = "refresh"
	AdminToken   TokenType = "admin"
	APIKeyToken  TokenType = "api_key"
)

// Claims represents JWT claims for Gresbase.
type Claims struct {
	jwt.RegisteredClaims
	AdminID string    `json:"admin_id,omitempty"`
	Email   string    `json:"email,omitempty"`
	Role    string    `json:"role,omitempty"`
	Type    TokenType `json:"type"`
}

// Service handles authentication operations.
type Service struct {
	db  *database.DB
	cfg *config.Config
}

// NewService creates a new auth service.
func NewService(db *database.DB, cfg *config.Config) *Service {
	return &Service{db: db, cfg: cfg}
}

func (s *Service) exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return s.db.ExecResult(ctx, sql, args...)
}

func (s *Service) query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return s.db.Query(ctx, sql, args...)
}

func (s *Service) queryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return s.db.QueryRow(ctx, sql, args...)
}

// HashPassword hashes a password using bcrypt at the configured cost.
func (s *Service) HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.bcryptCost())
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}
	return string(hash), nil
}

// bcryptCost returns the configured bcrypt work factor, defaulting to
// bcrypt.DefaultCost when unset or out of range.
func (s *Service) bcryptCost() int {
	if s != nil && s.cfg != nil {
		c := s.cfg.BCryptCost
		if c >= bcrypt.MinCost && c <= bcrypt.MaxCost {
			return c
		}
	}
	return bcrypt.DefaultCost
}

// dummyPasswordHash is a fixed, valid bcrypt hash (of a random string) used
// for constant-time anti-enumeration comparisons when an account is not found.
// It matches no real password.
var dummyPasswordHash = []byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")

// VerifyPassword checks a password against a bcrypt hash.
func (s *Service) VerifyPassword(hash, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// fastHash creates a deterministic SHA-256 HMAC for fast indexed lookup.
// Used alongside bcrypt for token tables to enable O(1) lookups.
//
// The HMAC key is derived from the configured JWT secret via HKDF, so the
// lookup hashes are unguessable without the server secret. IMPORTANT: rotating
// JWT_SECRET changes the derived key and therefore invalidates any outstanding
// magic-link / password-reset / email-verification / OTP lookup tokens — users
// must request fresh ones after a secret rotation.
func (s *Service) fastHash(token string) string {
	h := hmac.New(sha256.New, s.tokenLookupKey())
	h.Write([]byte(token))
	return hex.EncodeToString(h.Sum(nil))
}

// tokenLookupKey derives a stable 32-byte HMAC key from the JWT secret using
// HKDF-SHA256. The info label namespaces the derivation for token lookups.
func (s *Service) tokenLookupKey() []byte {
	var secret []byte
	if s != nil && s.cfg != nil {
		secret = []byte(s.cfg.JWTSecret)
	}
	key := make([]byte, 32)
	r := hkdf.New(sha256.New, secret, nil, []byte("gresbase-token-lookup-v1"))
	if _, err := io.ReadFull(r, key); err != nil {
		// HKDF over SHA-256 never fails for a 32-byte output; fall back defensively.
		h := hmac.New(sha256.New, secret)
		h.Write([]byte("gresbase-token-lookup-v1"))
		return h.Sum(nil)
	}
	return key
}

// GenerateTokens creates an access and refresh token pair.
func (s *Service) GenerateTokens(adminID, email, role string) (string, string, error) {
	accessToken, err := s.generateToken(adminID, email, role, AccessToken, s.cfg.AccessTokenExpiry)
	if err != nil {
		return "", "", err
	}

	refreshToken, err := s.generateToken(adminID, email, role, RefreshToken, s.cfg.RefreshTokenExpiry)
	if err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}

// GenerateAdminToken creates an admin-level token.
func (s *Service) GenerateAdminToken(adminID, email, role string) (string, error) {
	return s.generateToken(adminID, email, role, AdminToken, s.cfg.AdminTokenExpiry)
}

// ValidateToken validates a JWT and returns the claims.
func (s *Service) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, jwtVerificationKeyFunc(s.cfg))

	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	return claims, nil
}

// generateToken creates a signed JWT.
func (s *Service) generateToken(adminID, email, role string, tokenType TokenType, expiry time.Duration) (string, error) {
	now := time.Now()

	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(),
			Subject:   adminID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(expiry)),
			Issuer:    "gresbase",
		},
		AdminID: adminID,
		Email:   email,
		Role:    role,
		Type:    tokenType,
	}

	tokenString, err := signClaims(s.cfg, claims)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}

// Login authenticates an admin and returns tokens.
func (s *Service) Login(ctx context.Context, email, password string) (string, string, *AdminUser, error) {
	var admin *AdminUser
	var accessToken, refreshToken string

	err := s.db.RunInTransactionContext(ctx, func(txCtx context.Context, tx database.Tx) error {
		var err error
		admin, err = s.FindAdminByEmail(txCtx, email)
		if err != nil {
			// Anti-enumeration: perform a dummy bcrypt comparison so the
			// response timing does not reveal whether the email exists.
			bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(password))
			return fmt.Errorf("invalid credentials")
		}

		if !s.VerifyPassword(admin.PasswordHash, password) {
			return fmt.Errorf("invalid credentials")
		}

		accessToken, refreshToken, err = s.GenerateTokens(admin.ID, admin.Email, admin.Role)
		if err != nil {
			return err
		}

		if _, err = s.exec(txCtx, "UPDATE _admins SET last_login_at = NOW() WHERE id = $1", admin.ID); err != nil {
			return fmt.Errorf("update last login: %w", err)
		}

		sessionID := uuid.New().String()
		if _, err = s.exec(txCtx, `
			INSERT INTO _sessions (id, admin_id, token, refresh_token, expires_at)
			VALUES ($1, $2, $3, $4, $5)`,
			sessionID, admin.ID, accessToken, refreshToken,
			time.Now().Add(s.cfg.RefreshTokenExpiry)); err != nil {
			return fmt.Errorf("failed to create session: %w", err)
		}

		return nil
	})
	if err != nil {
		return "", "", nil, err
	}

	return accessToken, refreshToken, admin, nil
}

// RefreshToken refreshes an access token using a refresh token.
func (s *Service) RefreshToken(ctx context.Context, refreshTokenStr string) (string, string, error) {
	claims, err := s.ValidateToken(refreshTokenStr)
	if err != nil {
		return "", "", fmt.Errorf("invalid refresh token")
	}
	if claims.Type != RefreshToken {
		return "", "", fmt.Errorf("not a refresh token")
	}

	var accessToken, refreshToken string
	err = s.db.RunInTransactionContext(ctx, func(txCtx context.Context, tx database.Tx) error {
		var sessionID string
		if err := s.queryRow(txCtx, `
			SELECT id FROM _sessions
			WHERE refresh_token = $1 AND expires_at > NOW()`, refreshTokenStr).Scan(&sessionID); err != nil {
			return fmt.Errorf("session not found or expired")
		}

		if _, err := s.exec(txCtx, "DELETE FROM _sessions WHERE id = $1", sessionID); err != nil {
			return err
		}

		var err error
		accessToken, refreshToken, err = s.GenerateTokens(claims.AdminID, claims.Email, claims.Role)
		if err != nil {
			return err
		}

		newSessionID := uuid.New().String()
		if _, err = s.exec(txCtx, `
			INSERT INTO _sessions (id, admin_id, token, refresh_token, expires_at)
			VALUES ($1, $2, $3, $4, $5)`,
			newSessionID, claims.AdminID, accessToken, refreshToken,
			time.Now().Add(s.cfg.RefreshTokenExpiry)); err != nil {
			return fmt.Errorf("failed to create new session: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}

// Logout invalidates a session.
func (s *Service) Logout(ctx context.Context, token string) error {
	_, err := s.exec(ctx,
		"DELETE FROM _sessions WHERE token = $1 OR refresh_token = $1", token)
	return err
}

// CreateAdmin creates a new admin user.
func (s *Service) CreateAdmin(ctx context.Context, email, password, role string) (*AdminUser, error) {
	hash, err := s.HashPassword(password)
	if err != nil {
		return nil, err
	}

	admin := &AdminUser{
		ID:           uuid.New().String(),
		Email:        email,
		PasswordHash: hash,
		Role:         role,
	}

	_, err = s.exec(ctx, `
		INSERT INTO _admins (id, email, password_hash, role)
		VALUES ($1, $2, $3, $4)`,
		admin.ID, admin.Email, admin.PasswordHash, admin.Role)
	if err != nil {
		return nil, fmt.Errorf("failed to create admin: %w", err)
	}

	return admin, nil
}

// FindAdminByEmail finds an admin by email.
func (s *Service) FindAdminByEmail(ctx context.Context, email string) (*AdminUser, error) {
	admin := &AdminUser{}
	err := s.queryRow(ctx, `
		SELECT id, email, password_hash, role, avatar,
		       COALESCE(verified, FALSE) as verified,
		       COALESCE(last_login_at, TIMESTAMP 'epoch') as last_login_at,
		       created_at, updated_at
		FROM _admins WHERE email = $1`, email).Scan(
		&admin.ID, &admin.Email, &admin.PasswordHash,
		&admin.Role, &admin.Avatar, &admin.Verified, &admin.LastLoginAt,
		&admin.CreatedAt, &admin.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("admin not found: %w", err)
	}
	return admin, nil
}

// FindAdminByID finds an admin by ID.
func (s *Service) FindAdminByID(ctx context.Context, id string) (*AdminUser, error) {
	admin := &AdminUser{}
	err := s.queryRow(ctx, `
		SELECT id, email, password_hash, role, avatar,
		       COALESCE(verified, FALSE) as verified,
		       COALESCE(last_login_at, TIMESTAMP 'epoch') as last_login_at,
		       created_at, updated_at
		FROM _admins WHERE id = $1`, id).Scan(
		&admin.ID, &admin.Email, &admin.PasswordHash,
		&admin.Role, &admin.Avatar, &admin.Verified, &admin.LastLoginAt,
		&admin.CreatedAt, &admin.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("admin not found: %w", err)
	}
	return admin, nil
}

// CountAdmins returns the total number of admin users.
// Used to determine if first-time setup is required.
func (s *Service) CountAdmins(ctx context.Context) (int, error) {
	var count int
	err := s.queryRow(ctx, `SELECT COUNT(*) FROM _admins`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count admins: %w", err)
	}
	return count, nil
}

// ListAdmins lists all admins.
func (s *Service) ListAdmins(ctx context.Context) ([]*AdminUser, error) {
	rows, err := s.query(ctx, `
		SELECT id, email, password_hash, role, avatar,
		       COALESCE(verified, FALSE) as verified,
		       COALESCE(last_login_at, TIMESTAMP 'epoch') as last_login_at,
		       created_at, updated_at
		FROM _admins
		ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var admins []*AdminUser
	for rows.Next() {
		admin := &AdminUser{}
		if err := rows.Scan(
			&admin.ID, &admin.Email, &admin.PasswordHash,
			&admin.Role, &admin.Avatar, &admin.Verified, &admin.LastLoginAt,
			&admin.CreatedAt, &admin.UpdatedAt); err != nil {
			return nil, err
		}
		admins = append(admins, admin)
	}
	return admins, nil
}

// ListAdminEmails returns the email of every superuser in the _admins table.
// Used for instance-level operational alerts (e.g. scheduled backup failures)
// that every superuser should hear about.
func (s *Service) ListAdminEmails(ctx context.Context) ([]string, error) {
	rows, err := s.query(ctx, `SELECT email FROM _admins ORDER BY email`)
	if err != nil {
		return nil, fmt.Errorf("list admin emails: %w", err)
	}
	defer rows.Close()

	var emails []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, err
		}
		emails = append(emails, email)
	}
	return emails, rows.Err()
}

// DeleteAdmin deletes an admin.
func (s *Service) DeleteAdmin(ctx context.Context, id string) error {
	_, err := s.exec(ctx, "DELETE FROM _admins WHERE id = $1", id)
	return err
}

// UpdateAdmin updates an admin's fields.
func (s *Service) UpdateAdmin(ctx context.Context, id string, updates map[string]any) error {
	allowed := map[string]struct{}{
		"email":         {},
		"role":          {},
		"avatar":        {},
		"password_hash": {},
		"verified":      {},
	}

	setClauses := []string{}
	args := []any{id}
	i := 1
	for k, v := range updates {
		if _, ok := allowed[k]; !ok {
			return fmt.Errorf("unsupported admin field %q", k)
		}
		i++
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", database.QuoteIdent(k), i))
		args = append(args, v)
	}
	if len(setClauses) == 0 {
		return nil
	}
	args = append(args, time.Now())
	i++
	setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", i))

	sql := fmt.Sprintf("UPDATE _admins SET %s WHERE id = $1", strings.Join(setClauses, ", "))
	_, err := s.exec(ctx, sql, args...)
	return err
}

// RecordAudit records an audit log entry.
func (s *Service) RecordAudit(ctx context.Context, adminID, action, resource, resourceID string, data map[string]any, r *http.Request) {
	dataJSON, _ := json.Marshal(data)
	var trusted []string
	if s != nil && s.cfg != nil {
		trusted = s.cfg.TrustedProxies
	}
	ip := extractIP(r, trusted)
	userAgent := ""
	if r != nil {
		userAgent = r.Header.Get("User-Agent")
	}
	if _, err := s.exec(ctx,
		`INSERT INTO _audit_logs (admin_id, action, resource, resource_id, data, ip, user_agent)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		adminID, action, resource, resourceID, dataJSON, ip, userAgent); err != nil {
		log.Warn().Err(err).Str("action", action).Str("resource", resource).Msg("Failed to write audit log")
	}
}

// extractIP resolves the client IP address. It only honors the
// X-Forwarded-For / X-Real-IP headers when the immediate peer (RemoteAddr) is
// within one of the trusted proxy CIDRs; otherwise the direct socket address is
// used so clients cannot spoof their IP. When no trusted proxies are configured
// the direct socket address is always returned.
// extractIP returns the real client IP, honoring X-Forwarded-For/X-Real-IP only
// when the request arrives from a configured trusted proxy.
func extractIP(r *http.Request, trustedProxies []string) string {
	if r == nil {
		return ""
	}
	return netutil.ClientIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"), r.Header.Get("X-Real-IP"), trustedProxies)
}

// CreateOTP generates and stores an OTP code.
func (s *Service) CreateOTP(ctx context.Context, email string) (*OTPRecord, error) {
	admin, err := s.FindAdminByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("admin not found")
	}

	code, _ := GenerateOTP(6)
	record := &OTPRecord{
		ID:        uuid.New().String(),
		AdminID:   admin.ID,
		Code:      code,
		ExpiresAt: time.Now().Add(5 * time.Minute),
		CreatedAt: time.Now(),
	}

	codeHash, _ := s.HashPassword(code)
	_, err = s.exec(ctx,
		`INSERT INTO _otp (id, admin_id, code_hash, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		record.ID, record.AdminID, codeHash, record.ExpiresAt, record.CreatedAt)
	if err != nil {
		return nil, err
	}

	return record, nil
}

// VerifyOTP validates an OTP code and returns the authenticated admin.
func (s *Service) VerifyOTP(ctx context.Context, otpID, code string) (*AdminUser, error) {
	var adminID, codeHash string
	var expiresAt time.Time
	err := s.queryRow(ctx,
		`SELECT admin_id, code_hash, expires_at FROM _otp WHERE id = $1`, otpID).
		Scan(&adminID, &codeHash, &expiresAt)
	if err != nil {
		return nil, fmt.Errorf("invalid OTP")
	}

	if time.Now().After(expiresAt) {
		s.exec(ctx, "DELETE FROM _otp WHERE id = $1", otpID)
		return nil, fmt.Errorf("OTP expired")
	}

	if !s.VerifyPassword(codeHash, code) {
		return nil, fmt.Errorf("invalid OTP code")
	}

	// Delete used OTP
	s.exec(ctx, "DELETE FROM _otp WHERE id = $1", otpID)

	return s.FindAdminByID(ctx, adminID)
}

// CreateMagicLink creates a magic link token with dual-hash storage.
func (s *Service) CreateMagicLink(ctx context.Context, email string) (string, error) {
	admin, err := s.FindAdminByEmail(ctx, email)
	if err != nil {
		return "", fmt.Errorf("admin not found")
	}

	token := generateRandomString(64)
	tokenHash, _ := s.HashPassword(token)
	lookupHash := s.fastHash(token)

	_, err = s.exec(ctx,
		`INSERT INTO _magic_links (id, admin_id, token_hash, lookup_hash, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		uuid.New().String(), admin.ID, tokenHash, lookupHash, time.Now().Add(15*time.Minute), time.Now())

	return token, err
}

// VerifyMagicLink validates a magic link token using O(1) lookup.
// Uses a fast SHA-256 HMAC for indexed lookup, then bcrypt for verification.
func (s *Service) VerifyMagicLink(ctx context.Context, token string) (*AdminUser, error) {
	if token == "" || len(token) < 32 {
		return nil, fmt.Errorf("invalid token format")
	}

	lookup := s.fastHash(token)

	// Fast O(1) lookup by indexed lookup_hash column, then bcrypt verify
	var id, adminID, tokenHash string
	var expiresAt time.Time
	err := s.queryRow(ctx,
		`SELECT id, admin_id, token_hash, expires_at
		 FROM _magic_links
		 WHERE lookup_hash = $1 AND expires_at > NOW()
		 LIMIT 1`, lookup).Scan(&id, &adminID, &tokenHash, &expiresAt)
	if err != nil {
		return nil, fmt.Errorf("invalid or expired magic link")
	}

	if !s.VerifyPassword(tokenHash, token) {
		return nil, fmt.Errorf("invalid magic link")
	}

	// Delete all tokens for this admin (single-use + cleanup)
	s.exec(ctx, "DELETE FROM _magic_links WHERE admin_id = $1", adminID)
	return s.FindAdminByID(ctx, adminID)
}

// CreatePasswordResetToken creates a password reset token with dual-hash.
func (s *Service) CreatePasswordResetToken(ctx context.Context, email string) (string, error) {
	admin, err := s.FindAdminByEmail(ctx, email)
	if err != nil {
		return "", err
	}

	token := generateRandomString(64)
	tokenHash, _ := s.HashPassword(token)
	lookupHash := s.fastHash(token)

	_, err = s.exec(ctx,
		`INSERT INTO _password_resets (id, admin_id, token_hash, lookup_hash, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		uuid.New().String(), admin.ID, tokenHash, lookupHash, time.Now().Add(1*time.Hour), time.Now())

	return token, err
}

// ConfirmPasswordReset validates a reset token using O(1) lookup and updates password.
func (s *Service) ConfirmPasswordReset(ctx context.Context, token, newPassword string) error {
	if token == "" || len(token) < 32 {
		return fmt.Errorf("invalid token format")
	}

	lookup := s.fastHash(token)

	return s.db.RunInTransactionContext(ctx, func(txCtx context.Context, tx database.Tx) error {
		var adminID, tokenHash string
		err := s.queryRow(txCtx,
			`SELECT admin_id, token_hash FROM _password_resets
			 WHERE lookup_hash = $1 AND expires_at > NOW()
			 LIMIT 1`, lookup).Scan(&adminID, &tokenHash)
		if err != nil {
			return fmt.Errorf("invalid or expired reset token")
		}

		if !s.VerifyPassword(tokenHash, token) {
			return fmt.Errorf("invalid reset token")
		}

		hash, _ := s.HashPassword(newPassword)
		if _, err := s.exec(txCtx, "UPDATE _admins SET password_hash = $1 WHERE id = $2", hash, adminID); err != nil {
			return err
		}
		if _, err := s.exec(txCtx, "DELETE FROM _password_resets WHERE admin_id = $1", adminID); err != nil {
			return err
		}
		return nil
	})
}

// CreateVerificationToken creates an email verification token with dual-hash.
func (s *Service) CreateVerificationToken(ctx context.Context, email string) (string, error) {
	admin, err := s.FindAdminByEmail(ctx, email)
	if err != nil {
		return "", err
	}

	token := generateRandomString(64)
	tokenHash, _ := s.HashPassword(token)
	lookupHash := s.fastHash(token)

	_, err = s.exec(ctx,
		`INSERT INTO _verifications (id, admin_id, token_hash, lookup_hash, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		uuid.New().String(), admin.ID, tokenHash, lookupHash, time.Now().Add(24*time.Hour), time.Now())

	return token, err
}

// ConfirmVerification validates a verification token using O(1) lookup.
func (s *Service) ConfirmVerification(ctx context.Context, token string) error {
	if token == "" || len(token) < 32 {
		return fmt.Errorf("invalid token format")
	}

	lookup := s.fastHash(token)

	return s.db.RunInTransactionContext(ctx, func(txCtx context.Context, tx database.Tx) error {
		var adminID, tokenHash string
		err := s.queryRow(txCtx,
			`SELECT admin_id, token_hash FROM _verifications
			 WHERE lookup_hash = $1 AND expires_at > NOW()
			 LIMIT 1`, lookup).Scan(&adminID, &tokenHash)
		if err != nil {
			return fmt.Errorf("invalid or expired verification token")
		}

		if !s.VerifyPassword(tokenHash, token) {
			return fmt.Errorf("invalid verification token")
		}

		if _, err := s.exec(txCtx, "UPDATE _admins SET verified = TRUE WHERE id = $1", adminID); err != nil {
			return err
		}
		if _, err := s.exec(txCtx, "DELETE FROM _verifications WHERE admin_id = $1", adminID); err != nil {
			return err
		}
		return nil
	})
}

// CreateEmailChangeToken creates an email change confirmation token with dual-hash.
func (s *Service) CreateEmailChangeToken(ctx context.Context, adminID, newEmail string) (string, error) {
	token := generateRandomString(64)
	tokenHash, _ := s.HashPassword(token)
	lookupHash := s.fastHash(token)

	_, err := s.exec(ctx,
		`INSERT INTO _email_changes (id, admin_id, new_email, token_hash, lookup_hash, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.New().String(), adminID, newEmail, tokenHash, lookupHash, time.Now().Add(1*time.Hour), time.Now())

	return token, err
}

// ConfirmEmailChange validates and applies an email change using O(1) lookup.
func (s *Service) ConfirmEmailChange(ctx context.Context, token string) error {
	if token == "" || len(token) < 32 {
		return fmt.Errorf("invalid token format")
	}

	lookup := s.fastHash(token)

	return s.db.RunInTransactionContext(ctx, func(txCtx context.Context, tx database.Tx) error {
		var adminID, newEmail, tokenHash string
		err := s.queryRow(txCtx,
			`SELECT admin_id, new_email, token_hash FROM _email_changes
			 WHERE lookup_hash = $1 AND expires_at > NOW()
			 LIMIT 1`, lookup).Scan(&adminID, &newEmail, &tokenHash)
		if err != nil {
			return fmt.Errorf("invalid or expired email change token")
		}

		if !s.VerifyPassword(tokenHash, token) {
			return fmt.Errorf("invalid email change token")
		}

		if _, err := s.exec(txCtx, "UPDATE _admins SET email = $1 WHERE id = $2", newEmail, adminID); err != nil {
			return err
		}
		if _, err := s.exec(txCtx, "DELETE FROM _email_changes WHERE admin_id = $1", adminID); err != nil {
			return err
		}
		return nil
	})
}

// generateRandomString creates a random string of the given length.
func generateRandomString(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)[:length]
}

// FindOrCreateByOAuth finds an existing admin by OAuth provider ID or creates a new one.
func (s *Service) FindOrCreateByOAuth(ctx context.Context, oauthUser *OAuthUserInfo) (*AdminUser, error) {
	// Try to find by provider + provider_id in external_auths table
	var adminID string
	err := s.queryRow(ctx,
		`SELECT admin_id FROM _external_auths WHERE provider = $1 AND provider_id = $2`,
		oauthUser.Provider, oauthUser.ProviderID).Scan(&adminID)

	if err == nil {
		return s.FindAdminByID(ctx, adminID)
	}

	// If OAuth email matches an existing admin, link them
	if oauthUser.Email != "" {
		admin, err := s.FindAdminByEmail(ctx, oauthUser.Email)
		if err == nil {
			// Link OAuth to existing admin
			s.exec(ctx,
				`INSERT INTO _external_auths (admin_id, provider, provider_id, data) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
				admin.ID, oauthUser.Provider, oauthUser.ProviderID, string(oauthUser.RawJSON))
			return admin, nil
		}
	}

	// Create new admin from OAuth
	return s.CreateAdmin(ctx, oauthUser.Email, "", "admin")
}

// GenerateAPIKey creates a new API key.
func (s *Service) GenerateAPIKey(ctx context.Context, adminID, name string, permissions []string) (string, *APIKey, error) {
	if _, err := s.FindAdminByID(ctx, adminID); err != nil {
		return "", nil, fmt.Errorf("api key owner not found: %w", err)
	}

	permissions = NormalizeAPIKeyPermissions(permissions)

	// Generate a cryptographically random API key
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", nil, fmt.Errorf("failed to generate API key: %w", err)
	}
	keyStr := "gb_" + base64.RawURLEncoding.EncodeToString(key)

	// Hash the key for storage
	hashed, err := s.HashPassword(keyStr)
	if err != nil {
		return "", nil, err
	}

	permissionsJSON, err := json.Marshal(permissions)
	if err != nil {
		return "", nil, fmt.Errorf("failed to encode api key permissions: %w", err)
	}

	now := time.Now().UTC()
	prefix := keyStr[:10]
	apiKey := &APIKey{
		ID:          uuid.New().String(),
		AdminID:     adminID,
		Name:        name,
		KeyHash:     hashed,
		Prefix:      prefix,
		Permissions: permissions,
		CreatedAt:   now,
	}

	_, err = s.exec(ctx, `
		INSERT INTO _api_keys (id, admin_id, name, key_hash, prefix, permissions, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)`,
		apiKey.ID, apiKey.AdminID, apiKey.Name, apiKey.KeyHash, apiKey.Prefix, permissionsJSON, now)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create API key: %w", err)
	}

	return keyStr, apiKey, nil
}

// NormalizeAPIKeyPermissions normalizes, deduplicates and sorts API key scopes.
func NormalizeAPIKeyPermissions(permissions []string) []string {
	if len(permissions) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(permissions))
	result := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		permission = normalizeAPIKeyPermission(permission)
		if permission == "" {
			continue
		}
		if _, ok := seen[permission]; ok {
			continue
		}
		seen[permission] = struct{}{}
		result = append(result, permission)
	}
	sort.Strings(result)
	return result
}

// APIKeyPermissionAllowed reports whether the granted permission set allows the
// required permission. Empty granted permissions mean unrestricted owner-role access.
func APIKeyPermissionAllowed(granted []string, required string) bool {
	required = normalizeAPIKeyPermission(required)
	if required == "" {
		return true
	}

	granted = NormalizeAPIKeyPermissions(granted)
	if len(granted) == 0 {
		return true
	}

	for _, permission := range granted {
		switch {
		case permission == "*", permission == required:
			return true
		case strings.HasSuffix(permission, ".*"):
			prefix := strings.TrimSuffix(permission, ".*")
			if prefix != "" && (required == prefix || strings.HasPrefix(required, prefix+".")) {
				return true
			}
		}
	}

	return false
}

func normalizeAPIKeyPermission(permission string) string {
	permission = strings.ToLower(strings.TrimSpace(permission))
	permission = strings.Trim(permission, ".")
	if permission == "all" {
		return "*"
	}
	return permission
}

// ValidateAPIKey validates an API key and returns both the key metadata and its owning admin.
func (s *Service) ValidateAPIKey(ctx context.Context, token string) (*APIKey, *AdminUser, error) {
	if s == nil || s.db == nil {
		return nil, nil, fmt.Errorf("auth service not initialized")
	}

	token = strings.TrimSpace(token)
	if token == "" || !strings.HasPrefix(token, "gb_") {
		return nil, nil, fmt.Errorf("invalid api key")
	}

	prefix := token
	if len(prefix) > 10 {
		prefix = prefix[:10]
	}

	rows, err := s.query(ctx, `
		SELECT id, admin_id, name, key_hash, prefix, permissions, last_used_at, expires_at, created_at
		FROM _api_keys
		WHERE prefix = $1`, prefix)
	if err != nil {
		return nil, nil, fmt.Errorf("api key lookup failed: %w", err)
	}
	defer rows.Close()

	now := time.Now().UTC()
	for rows.Next() {
		apiKey := &APIKey{}
		var permissionsJSON []byte
		var lastUsedAt stdsql.NullTime
		var expiresAt stdsql.NullTime
		if err := rows.Scan(&apiKey.ID, &apiKey.AdminID, &apiKey.Name, &apiKey.KeyHash, &apiKey.Prefix, &permissionsJSON, &lastUsedAt, &expiresAt, &apiKey.CreatedAt); err != nil {
			continue
		}
		apiKey.Permissions = decodeAPIKeyPermissions(permissionsJSON)
		if expiresAt.Valid {
			expiresAtValue := expiresAt.Time.UTC()
			apiKey.ExpiresAt = &expiresAtValue
			if now.After(expiresAtValue) {
				continue
			}
		}
		if lastUsedAt.Valid {
			lastUsedAtValue := lastUsedAt.Time.UTC()
			apiKey.LastUsedAt = &lastUsedAtValue
		}
		if !s.VerifyPassword(apiKey.KeyHash, token) {
			continue
		}

		admin, err := s.FindAdminByID(ctx, apiKey.AdminID)
		if err != nil {
			return nil, nil, fmt.Errorf("api key owner not found: %w", err)
		}
		_, _ = s.exec(ctx, `UPDATE _api_keys SET last_used_at = $2, updated_at = $2 WHERE id = $1`, apiKey.ID, now)
		apiKey.LastUsedAt = &now
		return apiKey, admin, nil
	}

	return nil, nil, fmt.Errorf("invalid api key")
}

func decodeAPIKeyPermissions(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var permissions []string
	if err := json.Unmarshal(raw, &permissions); err != nil {
		return nil
	}
	return NormalizeAPIKeyPermissions(permissions)
}

// ----------- Password utilities (constant time) -----------

// ConstantTimeCompare compares two strings in constant time.
func ConstantTimeCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// GenerateOTP generates a cryptographically secure OTP code.
func GenerateOTP(length int) (string, error) {
	const digits = "0123456789"
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = digits[int(b[i])%len(digits)]
	}
	return string(b), nil
}

// ----------- Models -----------

// AdminUser represents an admin/superuser.
type AdminUser struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Avatar       string    `json:"avatar"`
	Role         string    `json:"role"`
	Verified     bool      `json:"verified"`
	LastLoginAt  time.Time `json:"last_login_at"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// APIKey represents an API key.
type APIKey struct {
	ID          string     `json:"id"`
	AdminID     string     `json:"admin_id"`
	Name        string     `json:"name"`
	KeyHash     string     `json:"-"`
	Prefix      string     `json:"prefix"`
	Permissions []string   `json:"permissions,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// OTPRecord represents a one-time password.
type OTPRecord struct {
	ID        string    `json:"id"`
	AdminID   string    `json:"admin_id"`
	Code      string    `json:"code,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Token invalidation helpers
// ---------------------------------------------------------------------------

// InvalidateAllTokens removes all active tokens for an admin (magic links, OTPs, resets, etc.)
func (s *Service) InvalidateAllTokens(ctx context.Context, adminID string) {
	s.exec(ctx, "DELETE FROM _magic_links WHERE admin_id = $1", adminID)
	s.exec(ctx, "DELETE FROM _otp WHERE admin_id = $1", adminID)
	s.exec(ctx, "DELETE FROM _password_resets WHERE admin_id = $1", adminID)
	s.exec(ctx, "DELETE FROM _verifications WHERE admin_id = $1", adminID)
	s.exec(ctx, "DELETE FROM _email_changes WHERE admin_id = $1", adminID)
}

// RateLimitAuthRequest checks if a request exceeds rate limits for auth endpoints.
// Returns nil if allowed, error if rate limited.
func (s *Service) RateLimitAuthRequest(ctx context.Context, action, identifier string, maxPerWindow int, window time.Duration) error {
	var count int
	err := s.queryRow(ctx,
		`SELECT COUNT(*) FROM _rate_limits
		 WHERE key = $1 AND window_start > $2`,
		"auth:"+action+":"+identifier, time.Now().Add(-window)).Scan(&count)
	if err != nil {
		return nil // Don't block on DB errors
	}

	if count >= maxPerWindow {
		return fmt.Errorf("rate limit exceeded for %s", action)
	}

	// Record this attempt
	s.exec(ctx,
		`INSERT INTO _rate_limits (key, hits, window_start) VALUES ($1, 1, NOW())`,
		"auth:"+action+":"+identifier)

	return nil
}
