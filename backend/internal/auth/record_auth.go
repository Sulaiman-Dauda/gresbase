// Package auth provides record-level authentication.
//
// PocketBase's killer feature: users are just records in an "auth" collection.
// This module enables:
//   - Password authentication for records (users)
//   - OTP authentication for records
//   - OAuth2 authentication for records
//   - Email verification, password reset, email change for records
//   - Token generation with record-level claims
//   - Impersonation support
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gresbase/gresbase/internal/config"
	"github.com/gresbase/gresbase/internal/database"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

// RecordAuthService handles authentication for auth collection records (end users).
// This is separate from the admin auth service and provides PocketBase-compatible
// record-level authentication.
type RecordAuthService struct {
	db         *database.DB
	cfg        *config.Config
	authSvc    *Service // reference to base auth for shared utilities
}

// NewRecordAuthService creates a new record auth service.
func NewRecordAuthService(db *database.DB, cfg *config.Config, authSvc *Service) *RecordAuthService {
	return &RecordAuthService{
		db:      db,
		cfg:     cfg,
		authSvc: authSvc,
	}
}

// RecordAuthClaims represents JWT claims for record (end-user) authentication.
type RecordAuthClaims struct {
	jwt.RegisteredClaims
	RecordID     string `json:"record_id"`
	CollectionID string `json:"collection_id"`
	Email        string `json:"email,omitempty"`
	Username     string `json:"username,omitempty"`
	Verified     bool   `json:"verified"`
	Type         string `json:"type"` // "record_auth"
}

// RecordAuthResult is returned on successful authentication.
type RecordAuthResult struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	Record       any    `json:"record"`
}

// PasswordAuth authenticates a record using email/password against an auth collection.
func (ras *RecordAuthService) PasswordAuth(ctx context.Context, collectionName string, identity, password string) (*RecordAuthResult, string, error) {
	// Find the auth collection
	coll, err := ras.findAuthCollection(ctx, collectionName)
	if err != nil {
		return nil, "", err
	}

	// Find the identity field (email or username) and password field from schema
	identityField := ras.findIdentityField(coll.Schema)
	passwordField := ras.findPasswordField(coll.Schema)
	if identityField == "" || passwordField == "" {
		return nil, "", fmt.Errorf("auth collection must have an identity field (email/username) and a password field")
	}

	// Query the record by identity
	tableName := ras.quoteIdent(collectionName)
	query := fmt.Sprintf(`SELECT * FROM %s WHERE "%s" = $1`, tableName, identityField)
	rows, err := ras.db.Pool.Query(ctx, query, identity)
	if err != nil {
		return nil, "", fmt.Errorf("database error: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, "", fmt.Errorf("invalid credentials")
	}

	values, err := rows.Values()
	if err != nil {
		return nil, "", fmt.Errorf("failed to read record: %w", err)
	}

	record := ras.rowToMap(rows.FieldDescriptions(), values)

	// Verify password
	storedHash, ok := record[passwordField].(string)
	if !ok {
		return nil, "", fmt.Errorf("invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password)); err != nil {
		return nil, "", fmt.Errorf("invalid credentials")
	}

	recordID, _ := record["id"].(string)

	// Check if email is verified
	verified := false
	if v, ok := record["verified"].(bool); ok {
		verified = v
	}

	email, _ := record[identityField].(string)

	// Generate tokens
	token, refreshToken, err := ras.generateRecordTokens(ctx, recordID, coll.ID, email, verified)
	if err != nil {
		return nil, "", err
	}

	// Remove sensitive fields
	delete(record, passwordField)
	delete(record, "tokenKey")

	return &RecordAuthResult{
		Token:        token,
		RefreshToken: refreshToken,
		Record:       record,
	}, recordID, nil
}

// OTPRequest creates a one-time password for a record.
func (ras *RecordAuthService) OTPRequest(ctx context.Context, collectionName, email string) (string, string, error) {
	coll, err := ras.findAuthCollection(ctx, collectionName)
	if err != nil {
		return "", "", err
	}

	identityField := ras.findIdentityField(coll.Schema)

	// Find the record
	tableName := ras.quoteIdent(collectionName)
	query := fmt.Sprintf(`SELECT id, "%s" FROM %s WHERE "%s" = $1`, identityField, tableName, identityField)
	var recordID, recordEmail string
	err = ras.db.Pool.QueryRow(ctx, query, email).Scan(&recordID, &recordEmail)
	if err != nil {
		return "", "", fmt.Errorf("no record found with this email")
	}

	// Generate OTP
	code, _ := GenerateOTP(6)
	otpID := uuid.New().String()
	codeHash, _ := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)

	_, err = ras.db.Pool.Exec(ctx, `
		INSERT INTO _record_otp (id, record_id, collection_id, code_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		otpID, recordID, coll.ID, string(codeHash),
		time.Now().Add(5*time.Minute), time.Now())
	if err != nil {
		return "", "", fmt.Errorf("failed to create OTP: %w", err)
	}

	return otpID, code, nil
}

// OTPVerify verifies an OTP and returns auth tokens for the record.
func (ras *RecordAuthService) OTPVerify(ctx context.Context, otpID, code string) (*RecordAuthResult, error) {
	var recordID, collectionID, codeHash string
	var expiresAt time.Time

	err := ras.db.Pool.QueryRow(ctx, `
		SELECT record_id, collection_id, code_hash, expires_at
		FROM _record_otp WHERE id = $1`, otpID,
	).Scan(&recordID, &collectionID, &codeHash, &expiresAt)
	if err != nil {
		return nil, fmt.Errorf("invalid OTP")
	}

	if time.Now().After(expiresAt) {
		ras.db.Pool.Exec(ctx, "DELETE FROM _record_otp WHERE id = $1", otpID)
		return nil, fmt.Errorf("OTP expired")
	}

	if bcrypt.CompareHashAndPassword([]byte(codeHash), []byte(code)) != nil {
		return nil, fmt.Errorf("invalid OTP code")
	}

	// Clean up
	ras.db.Pool.Exec(ctx, "DELETE FROM _record_otp WHERE id = $1", otpID)

	// Get collection info
	var collName string
	ras.db.Pool.QueryRow(ctx, "SELECT name FROM _collections WHERE id = $1", collectionID).Scan(&collName)

	// Generate tokens
	verified := true // OTP verification implies email ownership
	token, refreshToken, err := ras.generateRecordTokens(ctx, recordID, collectionID, "", verified)
	if err != nil {
		return nil, err
	}

	return &RecordAuthResult{
		Token:        token,
		RefreshToken: refreshToken,
		Record:       map[string]any{"id": recordID, "collection_id": collectionID},
	}, nil
}

// OAuth2Auth authenticates or creates a record via OAuth2.
func (ras *RecordAuthService) OAuth2Auth(ctx context.Context, collectionName, provider string, userInfo *OAuthUserInfo) (*RecordAuthResult, error) {
	coll, err := ras.findAuthCollection(ctx, collectionName)
	if err != nil {
		return nil, err
	}

	// Check if this OAuth user already has a linked record
	var recordID string
	tableName := ras.quoteIdent(collectionName)
	err = ras.db.Pool.QueryRow(ctx, `
		SELECT record_id FROM _record_external_auths
		WHERE collection_id = $1 AND provider = $2 AND provider_id = $3`,
		coll.ID, provider, userInfo.ProviderID,
	).Scan(&recordID)

	if err == nil {
		// Existing record — generate tokens
		verified := true
		token, refreshToken, err := ras.generateRecordTokens(ctx, recordID, coll.ID, userInfo.Email, verified)
		if err != nil {
			return nil, err
		}
		// Update last login
		ras.db.Pool.Exec(ctx, fmt.Sprintf(`UPDATE %s SET updated_at = NOW() WHERE id = $1`, tableName), recordID)
		return &RecordAuthResult{Token: token, RefreshToken: refreshToken, Record: map[string]any{"id": recordID}}, nil
	}

	// Try to find by email
	identityField := ras.findIdentityField(coll.Schema)
	if userInfo.Email != "" {
		var existingID string
		query := fmt.Sprintf(`SELECT id FROM %s WHERE "%s" = $1`, tableName, identityField)
		err = ras.db.Pool.QueryRow(ctx, query, userInfo.Email).Scan(&existingID)
		if err == nil {
			// Link OAuth to existing record
			ras.linkOAuthRecord(ctx, existingID, coll.ID, provider, userInfo)
			verified := true
			token, refreshToken, _ := ras.generateRecordTokens(ctx, existingID, coll.ID, userInfo.Email, verified)
			return &RecordAuthResult{Token: token, RefreshToken: refreshToken, Record: map[string]any{"id": existingID}}, nil
		}
	}

	// Create new record
	newID := uuid.New().String()
	insertSQL := fmt.Sprintf(`INSERT INTO %s (id, "%s", created_at, updated_at) VALUES ($1, $2, NOW(), NOW())`,
		tableName, identityField)
	ras.db.Pool.Exec(ctx, insertSQL, newID, userInfo.Email)

	// Link OAuth
	ras.linkOAuthRecord(ctx, newID, coll.ID, provider, userInfo)

	verified := true
	token, refreshToken, _ := ras.generateRecordTokens(ctx, newID, coll.ID, userInfo.Email, verified)
	return &RecordAuthResult{Token: token, RefreshToken: refreshToken, Record: map[string]any{"id": newID}}, nil
}

// RefreshRecordToken refreshes a record auth token.
func (ras *RecordAuthService) RefreshRecordToken(ctx context.Context, refreshTokenStr string) (string, string, error) {
	token, err := jwt.ParseWithClaims(refreshTokenStr, &RecordAuthClaims{},
		func(token *jwt.Token) (any, error) {
			return []byte(ras.cfg.JWTSecret), nil
		})
	if err != nil {
		return "", "", fmt.Errorf("invalid refresh token")
	}

	claims, ok := token.Claims.(*RecordAuthClaims)
	if !ok || claims.Type != "record_refresh" {
		return "", "", fmt.Errorf("invalid token type")
	}

	// Verify session
	var sessionID string
	err = ras.db.Pool.QueryRow(ctx, `
		SELECT id FROM _record_sessions
		WHERE refresh_token = $1 AND expires_at > NOW()`,
		refreshTokenStr).Scan(&sessionID)
	if err != nil {
		return "", "", fmt.Errorf("session expired")
	}

	// Delete old session
	ras.db.Pool.Exec(ctx, "DELETE FROM _record_sessions WHERE id = $1", sessionID)

	// Generate new tokens
	return ras.generateRecordTokens(ctx, claims.RecordID, claims.CollectionID, claims.Email, claims.Verified)
}

// ValidateRecordToken validates a record auth JWT token and returns claims.
func (ras *RecordAuthService) ValidateRecordToken(tokenString string) (*RecordAuthClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &RecordAuthClaims{},
		func(token *jwt.Token) (any, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method")
			}
			return []byte(ras.cfg.JWTSecret), nil
		})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*RecordAuthClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}

// Impersonate generates tokens for a record as if an admin is logging in as them.
func (ras *RecordAuthService) Impersonate(ctx context.Context, collectionName, recordID string) (*RecordAuthResult, error) {
	coll, err := ras.findAuthCollection(ctx, collectionName)
	if err != nil {
		return nil, err
	}

	// Get record email
	identityField := ras.findIdentityField(coll.Schema)
	tableName := ras.quoteIdent(collectionName)
	query := fmt.Sprintf(`SELECT "%s" FROM %s WHERE id = $1`, identityField, tableName)
	var email string
	err = ras.db.Pool.QueryRow(ctx, query, recordID).Scan(&email)
	if err != nil {
		return nil, fmt.Errorf("record not found")
	}

	token, refreshToken, err := ras.generateRecordTokens(ctx, recordID, coll.ID, email, true)
	if err != nil {
		return nil, err
	}
	return &RecordAuthResult{Token: token, RefreshToken: refreshToken, Record: map[string]any{"id": recordID}}, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

type authCollectionInfo struct {
	ID     string
	Schema []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
}

func (ras *RecordAuthService) findAuthCollection(ctx context.Context, name string) (*authCollectionInfo, error) {
	var id string
	var schemaJSON []byte
	err := ras.db.Pool.QueryRow(ctx,
		`SELECT id, schema FROM _collections WHERE (name = $1 OR id = $1) AND type = 'auth'`,
		name).Scan(&id, &schemaJSON)
	if err != nil {
		return nil, fmt.Errorf("auth collection not found: %s", name)
	}

	info := &authCollectionInfo{ID: id}
	json.Unmarshal(schemaJSON, &info.Schema)
	return info, nil
}

func (ras *RecordAuthService) findIdentityField(schema []struct {
	Name string `json:"name"`
	Type string `json:"type"`
}) string {
	for _, f := range schema {
		if f.Type == "email" {
			return f.Name
		}
	}
	// Fallback to username or a field named "email"
	for _, f := range schema {
		if f.Name == "email" || f.Name == "username" {
			return f.Name
		}
	}
	return ""
}

func (ras *RecordAuthService) findPasswordField(schema []struct {
	Name string `json:"name"`
	Type string `json:"type"`
}) string {
	for _, f := range schema {
		if f.Type == "password" {
			return f.Name
		}
	}
	return ""
}

func (ras *RecordAuthService) generateRecordTokens(ctx context.Context, recordID, collectionID, email string, verified bool) (string, string, error) {
	now := time.Now()

	accessClaims := &RecordAuthClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(),
			Subject:   recordID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ras.cfg.AccessTokenExpiry)),
			Issuer:    "gresbase",
		},
		RecordID:     recordID,
		CollectionID: collectionID,
		Email:        email,
		Verified:     verified,
		Type:         "record_auth",
	}

	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).
		SignedString([]byte(ras.cfg.JWTSecret))
	if err != nil {
		return "", "", err
	}

	refreshClaims := &RecordAuthClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(),
			Subject:   recordID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ras.cfg.RefreshTokenExpiry)),
			Issuer:    "gresbase",
		},
		RecordID:     recordID,
		CollectionID: collectionID,
		Email:        email,
		Verified:     verified,
		Type:         "record_refresh",
	}

	refreshToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).
		SignedString([]byte(ras.cfg.JWTSecret))
	if err != nil {
		return "", "", err
	}

	// Create session
	sessionID := uuid.New().String()
	ras.db.Pool.Exec(ctx, `
		INSERT INTO _record_sessions (id, record_id, collection_id, token, refresh_token, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		sessionID, recordID, collectionID, accessToken, refreshToken,
		time.Now().Add(ras.cfg.RefreshTokenExpiry))

	return accessToken, refreshToken, nil
}

func (ras *RecordAuthService) linkOAuthRecord(ctx context.Context, recordID, collectionID, provider string, userInfo *OAuthUserInfo) {
	dataJSON, _ := json.Marshal(userInfo)
	ras.db.Pool.Exec(ctx, `
		INSERT INTO _record_external_auths (record_id, collection_id, provider, provider_id, data, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (collection_id, provider, provider_id) DO NOTHING`,
		recordID, collectionID, provider, userInfo.ProviderID, string(dataJSON))
}

func (ras *RecordAuthService) rowToMap(fields []pgconn.FieldDescription, values []any) map[string]any {
	m := make(map[string]any, len(fields))
	for i, f := range fields {
		m[string(f.Name)] = values[i]
	}
	return m
}

func (ras *RecordAuthService) quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// ---------------------------------------------------------------------------
// Password management for records
// ---------------------------------------------------------------------------

// SetRecordPassword sets or changes the password for an auth record.
func (ras *RecordAuthService) SetRecordPassword(ctx context.Context, collectionName, recordID, newPassword string) error {
	coll, err := ras.findAuthCollection(ctx, collectionName)
	if err != nil {
		return err
	}
	passwordField := ras.findPasswordField(coll.Schema)
	if passwordField == "" {
		return fmt.Errorf("collection does not have a password field")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	tableName := ras.quoteIdent(collectionName)
	_, err = ras.db.Pool.Exec(ctx,
		fmt.Sprintf(`UPDATE %s SET "%s" = $1, updated_at = NOW() WHERE id = $2`, tableName, passwordField),
		string(hash), recordID)
	return err
}

// RequestRecordPasswordReset creates a password reset token for a record.
func (ras *RecordAuthService) RequestRecordPasswordReset(ctx context.Context, collectionName, email string) (string, error) {
	coll, err := ras.findAuthCollection(ctx, collectionName)
	if err != nil {
		return "", err
	}

	identityField := ras.findIdentityField(coll.Schema)
	tableName := ras.quoteIdent(collectionName)

	var recordID string
	query := fmt.Sprintf(`SELECT id FROM %s WHERE "%s" = $1`, tableName, identityField)
	err = ras.db.Pool.QueryRow(ctx, query, email).Scan(&recordID)
	if err != nil {
		return "", fmt.Errorf("no record found")
	}

	token := generateToken(64)
	tokenHash, _ := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)

	_, err = ras.db.Pool.Exec(ctx, `
		INSERT INTO _record_password_resets (id, record_id, collection_id, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())`,
		uuid.New().String(), recordID, coll.ID, string(tokenHash), time.Now().Add(1*time.Hour))
	return token, err
}

// ConfirmRecordPasswordReset validates a reset token and updates the password.
func (ras *RecordAuthService) ConfirmRecordPasswordReset(ctx context.Context, token, newPassword string) error {
	var recordID, collectionID, tokenHash string
	err := ras.db.Pool.QueryRow(ctx, `
		SELECT record_id, collection_id, token_hash FROM _record_password_resets
		WHERE expires_at > NOW()
		ORDER BY created_at DESC LIMIT 1`).Scan(&recordID, &collectionID, &tokenHash)
	if err != nil {
		return fmt.Errorf("invalid or expired reset token")
	}

	if bcrypt.CompareHashAndPassword([]byte(tokenHash), []byte(token)) != nil {
		return fmt.Errorf("invalid reset token")
	}

	// Get collection name
	var collName string
	ras.db.Pool.QueryRow(ctx, "SELECT name FROM _collections WHERE id = $1", collectionID).Scan(&collName)

	// Update password
	if err := ras.SetRecordPassword(ctx, collName, recordID, newPassword); err != nil {
		return err
	}

	// Cleanup
	ras.db.Pool.Exec(ctx, "DELETE FROM _record_password_resets WHERE record_id = $1", recordID)
	return nil
}

// RequestRecordVerification sends an email verification for a record.
func (ras *RecordAuthService) RequestRecordVerification(ctx context.Context, collectionName, recordID string) (string, error) {
	token := generateToken(64)
	tokenHash, _ := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)

	_, err := ras.db.Pool.Exec(ctx, `
		INSERT INTO _record_verifications (id, record_id, collection_id, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())`,
		uuid.New().String(), recordID, collectionName, string(tokenHash), time.Now().Add(24*time.Hour))
	return token, err
}

// ConfirmRecordVerification marks a record's email as verified.
func (ras *RecordAuthService) ConfirmRecordVerification(ctx context.Context, token string) error {
	var recordID, collectionID, tokenHash string
	err := ras.db.Pool.QueryRow(ctx, `
		SELECT record_id, collection_id, token_hash FROM _record_verifications
		WHERE expires_at > NOW()
		ORDER BY created_at DESC LIMIT 1`).Scan(&recordID, &collectionID, &tokenHash)
	if err != nil {
		return fmt.Errorf("invalid or expired verification token")
	}

	if bcrypt.CompareHashAndPassword([]byte(tokenHash), []byte(token)) != nil {
		return fmt.Errorf("invalid verification token")
	}

	// Get collection info
	var collName string
	ras.db.Pool.QueryRow(ctx, "SELECT name FROM _collections WHERE id = $1", collectionID).Scan(&collName)

	// Mark as verified
	tableName := ras.quoteIdent(collName)
	ras.db.Pool.Exec(ctx, fmt.Sprintf(`UPDATE %s SET verified = TRUE WHERE id = $1`, tableName), recordID)
	ras.db.Pool.Exec(ctx, "DELETE FROM _record_verifications WHERE record_id = $1", recordID)
	return nil
}

// ---------------------------------------------------------------------------
// Record Email Change
// ---------------------------------------------------------------------------

// RequestRecordEmailChange creates an email change token for a record.
func (ras *RecordAuthService) RequestRecordEmailChange(ctx context.Context, collectionName, recordID, newEmail string) (string, error) {
	coll, err := ras.findAuthCollection(ctx, collectionName)
	if err != nil {
		return "", err
	}

	token := generateToken(64)
	tokenHash, _ := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)

	_, err = ras.db.Pool.Exec(ctx, `
		INSERT INTO _record_email_changes (id, record_id, collection_id, new_email, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())`,
		uuid.New().String(), recordID, coll.ID, newEmail, string(tokenHash), time.Now().Add(1*time.Hour))
	return token, err
}

// ConfirmRecordEmailChange validates an email change token and updates the record.
func (ras *RecordAuthService) ConfirmRecordEmailChange(ctx context.Context, token string) error {
	var recordID, collectionID, newEmail, tokenHash string
	err := ras.db.Pool.QueryRow(ctx, `
		SELECT record_id, collection_id, new_email, token_hash FROM _record_email_changes
		WHERE expires_at > NOW()
		ORDER BY created_at DESC LIMIT 1`).Scan(&recordID, &collectionID, &newEmail, &tokenHash)
	if err != nil {
		return fmt.Errorf("invalid or expired email change token")
	}

	if bcrypt.CompareHashAndPassword([]byte(tokenHash), []byte(token)) != nil {
		return fmt.Errorf("invalid email change token")
	}

	// Get collection info
	var collName string
	ras.db.Pool.QueryRow(ctx, "SELECT name FROM _collections WHERE id = $1", collectionID).Scan(&collName)

	// Find identity field and update
	coll, _ := ras.findAuthCollection(ctx, collName)
	identityField := ras.findIdentityField(coll.Schema)
	tableName := ras.quoteIdent(collName)
	ras.db.Pool.Exec(ctx, fmt.Sprintf(`UPDATE %s SET "%s" = $1 WHERE id = $2`, tableName, identityField), newEmail, recordID)
	ras.db.Pool.Exec(ctx, "DELETE FROM _record_email_changes WHERE record_id = $1", recordID)
	return nil
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

func generateToken(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)[:length]
}
