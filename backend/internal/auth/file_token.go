package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// FileToken is the token type for short-lived file access tokens. The token
// carries the caller's identity so protected file downloads can be rule-checked
// in contexts where an Authorization header cannot be sent (img/video tags).
const FileToken TokenType = "file"

// FileTokenExpiry is intentionally short: file tokens travel in URLs, which
// end up in logs and browser history.
const FileTokenExpiry = 3 * time.Minute

// FileTokenClaims captures the requester identity at mint time.
type FileTokenClaims struct {
	jwt.RegisteredClaims
	Type         TokenType `json:"type"`
	IsAdmin      bool      `json:"is_admin,omitempty"`
	AdminID      string    `json:"admin_id,omitempty"`
	Role         string    `json:"role,omitempty"`
	Email        string    `json:"email,omitempty"`
	RecordID     string    `json:"record_id,omitempty"`
	CollectionID string    `json:"collection_id,omitempty"`
	Verified     bool      `json:"verified,omitempty"`
}

// GenerateFileToken mints a short-lived file access token for the given
// identity. Exactly one of adminID or recordID is expected to be set.
func (s *Service) GenerateFileToken(claims FileTokenClaims) (string, error) {
	now := time.Now()
	subject := claims.AdminID
	if subject == "" {
		subject = claims.RecordID
	}
	claims.RegisteredClaims = jwt.RegisteredClaims{
		ID:        uuid.New().String(),
		Subject:   subject,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(FileTokenExpiry)),
		Issuer:    "gresbase",
	}
	claims.Type = FileToken
	claims.IsAdmin = claims.AdminID != ""

	token, err := signClaims(s.cfg, &claims)
	if err != nil {
		return "", fmt.Errorf("failed to sign file token: %w", err)
	}
	return token, nil
}

// ValidateFileToken validates a file access token and returns its claims.
// Tokens of any other type are rejected so access/refresh tokens leaked into
// URLs cannot be replayed here with their longer lifetimes.
func (s *Service) ValidateFileToken(tokenString string) (*FileTokenClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &FileTokenClaims{}, jwtVerificationKeyFunc(s.cfg))
	if err != nil {
		return nil, fmt.Errorf("invalid file token: %w", err)
	}
	claims, ok := token.Claims.(*FileTokenClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid file token claims")
	}
	if claims.Type != FileToken {
		return nil, fmt.Errorf("not a file token")
	}
	return claims, nil
}
