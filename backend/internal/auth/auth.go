package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gresbase/gresbase/internal/config"
	"github.com/gresbase/gresbase/internal/database"
	"golang.org/x/crypto/bcrypt"
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
	AdminID  string    `json:"admin_id,omitempty"`
	Email    string    `json:"email,omitempty"`
	Role     string    `json:"role,omitempty"`
	TenantID string    `json:"tenant_id,omitempty"`
	Type     TokenType `json:"type"`
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

// HashPassword hashes a password using bcrypt.
func (s *Service) HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}
	return string(hash), nil
}

// VerifyPassword checks a password against a bcrypt hash.
func (s *Service) VerifyPassword(hash, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// fastHash creates a deterministic SHA-256 HMAC for fast indexed lookup.
// Used alongside bcrypt for token tables to enable O(1) lookups.
func fastHash(token string) string {
	h := hmac.New(sha256.New, []byte("gresbase-token-lookup-v1"))
	h.Write([]byte(token))
	return hex.EncodeToString(h.Sum(nil))
}

// GenerateTokens creates an access and refresh token pair.
func (s *Service) GenerateTokens(adminID, email, role, tenantID string) (string, string, error) {
	accessToken, err := s.generateToken(adminID, email, role, tenantID, AccessToken, s.cfg.AccessTokenExpiry)
	if err != nil {
		return "", "", err
	}

	refreshToken, err := s.generateToken(adminID, email, role, tenantID, RefreshToken, s.cfg.RefreshTokenExpiry)
	if err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}

// GenerateAdminToken creates an admin-level token.
func (s *Service) GenerateAdminToken(adminID, email, role, tenantID string) (string, error) {
	return s.generateToken(adminID, email, role, tenantID, AdminToken, s.cfg.AdminTokenExpiry)
}

// ValidateToken validates a JWT and returns the claims.
func (s *Service) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.cfg.JWTSecret), nil
	})

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
func (s *Service) generateToken(adminID, email, role, tenantID string, tokenType TokenType, expiry time.Duration) (string, error) {
	now := time.Now()

	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(),
			Subject:   adminID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(expiry)),
			Issuer:    "gresbase",
		},
		AdminID:  adminID,
		Email:    email,
		Role:     role,
		TenantID: tenantID,
		Type:     tokenType,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(s.cfg.JWTSecret))
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}

// Login authenticates an admin and returns tokens.
func (s *Service) Login(ctx context.Context, email, password string) (string, string, *AdminUser, error) {
	admin, err := s.FindAdminByEmail(ctx, email)
	if err != nil {
		return "", "", nil, fmt.Errorf("invalid credentials")
	}

	if !s.VerifyPassword(admin.PasswordHash, password) {
		return "", "", nil, fmt.Errorf("invalid credentials")
	}

	accessToken, refreshToken, err := s.GenerateTokens(admin.ID, admin.Email, admin.Role, admin.TenantID)
	if err != nil {
		return "", "", nil, err
	}

	// Update last login
	_, err = s.db.Pool.Exec(ctx,
		"UPDATE _admins SET last_login_at = NOW() WHERE id = $1", admin.ID)
	if err != nil {
		// Non-fatal
	}

	// Create session
	sessionID := uuid.New().String()
	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO _sessions (id, admin_id, tenant_id, token, refresh_token, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		sessionID, admin.ID, admin.TenantID, accessToken, refreshToken,
		time.Now().Add(s.cfg.RefreshTokenExpiry))
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to create session: %w", err)
	}

	return accessToken, refreshToken, admin, nil
}

// RefreshToken refreshes an access token using a refresh token.
func (s *Service) RefreshToken(ctx context.Context, refreshTokenStr string) (string, string, error) {
	// Validate the refresh token
	claims, err := s.ValidateToken(refreshTokenStr)
	if err != nil {
		return "", "", fmt.Errorf("invalid refresh token")
	}

	if claims.Type != RefreshToken {
		return "", "", fmt.Errorf("not a refresh token")
	}

	// Verify session exists and is valid
	var sessionID string
	err = s.db.Pool.QueryRow(ctx, `
		SELECT id FROM _sessions
		WHERE refresh_token = $1 AND expires_at > NOW()`,
		refreshTokenStr).Scan(&sessionID)
	if err != nil {
		return "", "", fmt.Errorf("session not found or expired")
	}

	// Delete old session
	s.db.Pool.Exec(ctx, "DELETE FROM _sessions WHERE id = $1", sessionID)

	// Generate new tokens
	accessToken, refreshToken, err := s.GenerateTokens(
		claims.AdminID, claims.Email, claims.Role, claims.TenantID)
	if err != nil {
		return "", "", err
	}

	// Create new session
	newSessionID := uuid.New().String()
	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO _sessions (id, admin_id, tenant_id, token, refresh_token, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		newSessionID, claims.AdminID, claims.TenantID, accessToken, refreshToken,
		time.Now().Add(s.cfg.RefreshTokenExpiry))
	if err != nil {
		return "", "", fmt.Errorf("failed to create new session: %w", err)
	}

	return accessToken, refreshToken, nil
}

// Logout invalidates a session.
func (s *Service) Logout(ctx context.Context, token string) error {
	_, err := s.db.Pool.Exec(ctx,
		"DELETE FROM _sessions WHERE token = $1 OR refresh_token = $1", token)
	return err
}

// CreateAdmin creates a new admin user.
func (s *Service) CreateAdmin(ctx context.Context, email, password, role, tenantID string) (*AdminUser, error) {
	hash, err := s.HashPassword(password)
	if err != nil {
		return nil, err
	}

	admin := &AdminUser{
		ID:           uuid.New().String(),
		Email:        email,
		PasswordHash: hash,
		Role:         role,
		TenantID:     tenantID,
	}

	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO _admins (id, tenant_id, email, password_hash, role)
		VALUES ($1, $2, $3, $4, $5)`,
		admin.ID, admin.TenantID, admin.Email, admin.PasswordHash, admin.Role)
	if err != nil {
		return nil, fmt.Errorf("failed to create admin: %w", err)
	}

	return admin, nil
}

// FindAdminByEmail finds an admin by email.
func (s *Service) FindAdminByEmail(ctx context.Context, email string) (*AdminUser, error) {
	admin := &AdminUser{}
	err := s.db.Pool.QueryRow(ctx, `
		SELECT id, tenant_id, email, password_hash, role, avatar,
		       COALESCE(last_login_at, TIMESTAMP 'epoch') as last_login_at,
		       created_at, updated_at
		FROM _admins WHERE email = $1`, email).Scan(
		&admin.ID, &admin.TenantID, &admin.Email, &admin.PasswordHash,
		&admin.Role, &admin.Avatar, &admin.LastLoginAt,
		&admin.CreatedAt, &admin.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("admin not found: %w", err)
	}
	return admin, nil
}

// FindAdminByID finds an admin by ID.
func (s *Service) FindAdminByID(ctx context.Context, id string) (*AdminUser, error) {
	admin := &AdminUser{}
	err := s.db.Pool.QueryRow(ctx, `
		SELECT id, tenant_id, email, password_hash, role, avatar,
		       COALESCE(last_login_at, TIMESTAMP 'epoch') as last_login_at,
		       created_at, updated_at
		FROM _admins WHERE id = $1`, id).Scan(
		&admin.ID, &admin.TenantID, &admin.Email, &admin.PasswordHash,
		&admin.Role, &admin.Avatar, &admin.LastLoginAt,
		&admin.CreatedAt, &admin.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("admin not found: %w", err)
	}
	return admin, nil
}

// ListAdmins lists all admins for a tenant.
func (s *Service) ListAdmins(ctx context.Context, tenantID string) ([]*AdminUser, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, tenant_id, email, password_hash, role, avatar,
		       COALESCE(last_login_at, TIMESTAMP 'epoch') as last_login_at,
		       created_at, updated_at
		FROM _admins WHERE tenant_id = $1
		ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var admins []*AdminUser
	for rows.Next() {
		admin := &AdminUser{}
		if err := rows.Scan(
			&admin.ID, &admin.TenantID, &admin.Email, &admin.PasswordHash,
			&admin.Role, &admin.Avatar, &admin.LastLoginAt,
			&admin.CreatedAt, &admin.UpdatedAt); err != nil {
			return nil, err
		}
		admins = append(admins, admin)
	}
	return admins, nil
}

// DeleteAdmin deletes an admin.
func (s *Service) DeleteAdmin(ctx context.Context, id string) error {
	_, err := s.db.Pool.Exec(ctx, "DELETE FROM _admins WHERE id = $1", id)
	return err
}

// UpdateAdmin updates an admin's fields.
func (s *Service) UpdateAdmin(ctx context.Context, id string, updates map[string]any) error {
	setClauses := []string{}
	args := []any{id}
	i := 1
	for k, v := range updates {
		i++
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", k, i))
		args = append(args, v)
	}
	if len(setClauses) == 0 {
		return nil
	}
	args = append(args, time.Now())
	i++
	setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", i))

	sql := fmt.Sprintf("UPDATE _admins SET %s WHERE id = $1", strings.Join(setClauses, ", "))
	_, err := s.db.Pool.Exec(ctx, sql, args...)
	return err
}

// RecordAudit records an audit log entry.
func (s *Service) RecordAudit(ctx context.Context, adminID, action, resource, resourceID string, data map[string]any, r *http.Request) {
	dataJSON, _ := json.Marshal(data)
	ip := extractIP(r)
	userAgent := ""
	if r != nil {
		userAgent = r.Header.Get("User-Agent")
	}
	tenantID := "default"
	s.db.Pool.Exec(ctx,
		`INSERT INTO _audit_logs (tenant_id, admin_id, action, resource, resource_id, data, ip, user_agent)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		tenantID, adminID, action, resource, resourceID, dataJSON, ip, userAgent)
}

func extractIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
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
	_, err = s.db.Pool.Exec(ctx,
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
	err := s.db.Pool.QueryRow(ctx,
		`SELECT admin_id, code_hash, expires_at FROM _otp WHERE id = $1`, otpID).
		Scan(&adminID, &codeHash, &expiresAt)
	if err != nil {
		return nil, fmt.Errorf("invalid OTP")
	}

	if time.Now().After(expiresAt) {
		s.db.Pool.Exec(ctx, "DELETE FROM _otp WHERE id = $1", otpID)
		return nil, fmt.Errorf("OTP expired")
	}

	if !s.VerifyPassword(codeHash, code) {
		return nil, fmt.Errorf("invalid OTP code")
	}

	// Delete used OTP
	s.db.Pool.Exec(ctx, "DELETE FROM _otp WHERE id = $1", otpID)

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
	lookupHash := fastHash(token)

	_, err = s.db.Pool.Exec(ctx,
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

	lookup := fastHash(token)

	// Fast O(1) lookup by indexed lookup_hash column, then bcrypt verify
	var id, adminID, tokenHash string
	var expiresAt time.Time
	err := s.db.Pool.QueryRow(ctx,
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
	s.db.Pool.Exec(ctx, "DELETE FROM _magic_links WHERE admin_id = $1", adminID)
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
	lookupHash := fastHash(token)

	_, err = s.db.Pool.Exec(ctx,
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

	lookup := fastHash(token)

	var adminID, tokenHash string
	err := s.db.Pool.QueryRow(ctx,
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
	s.db.Pool.Exec(ctx, "UPDATE _admins SET password_hash = $1 WHERE id = $2", hash, adminID)
	s.db.Pool.Exec(ctx, "DELETE FROM _password_resets WHERE admin_id = $1", adminID)
	return nil
}

// CreateVerificationToken creates an email verification token with dual-hash.
func (s *Service) CreateVerificationToken(ctx context.Context, email string) (string, error) {
	admin, err := s.FindAdminByEmail(ctx, email)
	if err != nil {
		return "", err
	}

	token := generateRandomString(64)
	tokenHash, _ := s.HashPassword(token)
	lookupHash := fastHash(token)

	_, err = s.db.Pool.Exec(ctx,
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

	lookup := fastHash(token)

	var adminID, tokenHash string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT admin_id, token_hash FROM _verifications
		 WHERE lookup_hash = $1 AND expires_at > NOW()
		 LIMIT 1`, lookup).Scan(&adminID, &tokenHash)
	if err != nil {
		return fmt.Errorf("invalid or expired verification token")
	}

	if !s.VerifyPassword(tokenHash, token) {
		return fmt.Errorf("invalid verification token")
	}

	s.db.Pool.Exec(ctx, "UPDATE _admins SET verified = TRUE WHERE id = $1", adminID)
	s.db.Pool.Exec(ctx, "DELETE FROM _verifications WHERE admin_id = $1", adminID)
	return nil
}

// CreateEmailChangeToken creates an email change confirmation token with dual-hash.
func (s *Service) CreateEmailChangeToken(ctx context.Context, adminID, newEmail string) (string, error) {
	token := generateRandomString(64)
	tokenHash, _ := s.HashPassword(token)
	lookupHash := fastHash(token)

	_, err := s.db.Pool.Exec(ctx,
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

	lookup := fastHash(token)

	var adminID, newEmail, tokenHash string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT admin_id, new_email, token_hash FROM _email_changes
		 WHERE lookup_hash = $1 AND expires_at > NOW()
		 LIMIT 1`, lookup).Scan(&adminID, &newEmail, &tokenHash)
	if err != nil {
		return fmt.Errorf("invalid or expired email change token")
	}

	if !s.VerifyPassword(tokenHash, token) {
		return fmt.Errorf("invalid email change token")
	}

	s.db.Pool.Exec(ctx, "UPDATE _admins SET email = $1 WHERE id = $2", newEmail, adminID)
	s.db.Pool.Exec(ctx, "DELETE FROM _email_changes WHERE admin_id = $1", adminID)
	return nil
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
	err := s.db.Pool.QueryRow(ctx,
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
			s.db.Pool.Exec(ctx,
				`INSERT INTO _external_auths (admin_id, provider, provider_id, data) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
				admin.ID, oauthUser.Provider, oauthUser.ProviderID, string(oauthUser.RawJSON))
			return admin, nil
		}
	}

	// Create new admin from OAuth
	return s.CreateAdmin(ctx, oauthUser.Email, "", "admin", "default")
}

// GenerateAPIKey creates a new API key.
func (s *Service) GenerateAPIKey(ctx context.Context, adminID, name string, permissions []string) (string, *APIKey, error) {
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

	prefix := keyStr[:10]
	apiKey := &APIKey{
		ID:        uuid.New().String(),
		AdminID:   adminID,
		Name:      name,
		KeyHash:   hashed,
		Prefix:    prefix,
	}

	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO _api_keys (id, admin_id, name, key_hash, prefix)
		VALUES ($1, $2, $3, $4, $5)`,
		apiKey.ID, apiKey.AdminID, apiKey.Name, apiKey.KeyHash, apiKey.Prefix)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create API key: %w", err)
	}

	return keyStr, apiKey, nil
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
	TenantID     string    `json:"tenant_id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Avatar       string    `json:"avatar"`
	Role         string    `json:"role"`
	LastLoginAt  time.Time `json:"last_login_at"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// APIKey represents an API key.
type APIKey struct {
	ID        string    `json:"id"`
	AdminID   string    `json:"admin_id"`
	Name      string    `json:"name"`
	KeyHash   string    `json:"-"`
	Prefix    string    `json:"prefix"`
	CreatedAt time.Time `json:"created_at"`
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
	s.db.Pool.Exec(ctx, "DELETE FROM _magic_links WHERE admin_id = $1", adminID)
	s.db.Pool.Exec(ctx, "DELETE FROM _otp WHERE admin_id = $1", adminID)
	s.db.Pool.Exec(ctx, "DELETE FROM _password_resets WHERE admin_id = $1", adminID)
	s.db.Pool.Exec(ctx, "DELETE FROM _verifications WHERE admin_id = $1", adminID)
	s.db.Pool.Exec(ctx, "DELETE FROM _email_changes WHERE admin_id = $1", adminID)
}

// RateLimitAuthRequest checks if a request exceeds rate limits for auth endpoints.
// Returns nil if allowed, error if rate limited.
func (s *Service) RateLimitAuthRequest(ctx context.Context, action, identifier string, maxPerWindow int, window time.Duration) error {
	var count int
	err := s.db.Pool.QueryRow(ctx,
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
	s.db.Pool.Exec(ctx,
		`INSERT INTO _rate_limits (key, hits, window_start) VALUES ($1, 1, NOW())`,
		"auth:"+action+":"+identifier)

	return nil
}
