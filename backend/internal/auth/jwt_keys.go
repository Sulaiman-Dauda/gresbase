package auth

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gresbase/gresbase/internal/config"
)

type jwtKeyMaterial struct {
	algorithm    string
	keyID        string
	hmacSecret   []byte
	ecdsaPrivate *ecdsa.PrivateKey
	ecdsaPublic  *ecdsa.PublicKey
}

var jwtKeyCache sync.Map

func signClaims(cfg *config.Config, claims jwt.Claims) (string, error) {
	material, err := loadJWTKeyMaterial(cfg)
	if err != nil {
		return "", err
	}

	var method jwt.SigningMethod
	var signingKey any
	switch material.algorithm {
	case "ES256":
		method = jwt.SigningMethodES256
		signingKey = material.ecdsaPrivate
	default:
		method = jwt.SigningMethodHS256
		signingKey = material.hmacSecret
	}

	token := jwt.NewWithClaims(method, claims)
	if material.keyID != "" {
		token.Header["kid"] = material.keyID
	}
	return token.SignedString(signingKey)
}

func jwtVerificationKeyFunc(cfg *config.Config) jwt.Keyfunc {
	return func(token *jwt.Token) (any, error) {
		material, err := loadJWTKeyMaterial(cfg)
		if err != nil {
			return nil, err
		}
		if token.Method.Alg() != material.algorithm {
			return nil, fmt.Errorf("unexpected signing method: %s", token.Method.Alg())
		}
		switch material.algorithm {
		case "ES256":
			return material.ecdsaPublic, nil
		default:
			return material.hmacSecret, nil
		}
	}
}

// PublicJWKS returns the JSON Web Key Set for asymmetric JWT verification.
// HS256 deployments intentionally expose an empty key set because the shared
// secret must never be published.
func PublicJWKS(cfg *config.Config) (map[string]any, error) {
	material, err := loadJWTKeyMaterial(cfg)
	if err != nil {
		return nil, err
	}
	if material.algorithm != "ES256" {
		return map[string]any{"keys": []any{}}, nil
	}

	publicKey := material.ecdsaPublic
	return map[string]any{
		"keys": []map[string]any{
			{
				"kty": "EC",
				"use": "sig",
				"kid": material.keyID,
				"alg": "ES256",
				"crv": "P-256",
				"x":   base64.RawURLEncoding.EncodeToString(paddedBigInt(publicKey.X, 32)),
				"y":   base64.RawURLEncoding.EncodeToString(paddedBigInt(publicKey.Y, 32)),
			},
		},
	}, nil
}

func loadJWTKeyMaterial(cfg *config.Config) (*jwtKeyMaterial, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	algorithm := strings.ToUpper(strings.TrimSpace(cfg.JWTAlgorithm))
	if algorithm == "" {
		algorithm = "HS256"
	}
	keyID := strings.TrimSpace(cfg.JWTKeyID)
	if keyID == "" {
		keyID = "gresbase-default"
	}

	cacheKey := strings.Join([]string{algorithm, keyID, cfg.JWTSecret, cfg.JWTPrivateKey, cfg.JWTPublicKey}, "\x00")
	if cached, ok := jwtKeyCache.Load(cacheKey); ok {
		if material, ok := cached.(*jwtKeyMaterial); ok {
			return material, nil
		}
	}

	material := &jwtKeyMaterial{algorithm: algorithm, keyID: keyID}
	switch algorithm {
	case "HS256":
		if strings.TrimSpace(cfg.JWTSecret) == "" {
			return nil, fmt.Errorf("JWT_SECRET is required")
		}
		material.hmacSecret = []byte(cfg.JWTSecret)
	case "ES256":
		privateKey, err := parseECDSAPrivateKey(cfg.JWTPrivateKey)
		if err != nil {
			return nil, err
		}
		publicKey := &privateKey.PublicKey
		if strings.TrimSpace(cfg.JWTPublicKey) != "" {
			parsedPublicKey, err := parseECDSAPublicKey(cfg.JWTPublicKey)
			if err != nil {
				return nil, err
			}
			publicKey = parsedPublicKey
		}
		material.ecdsaPrivate = privateKey
		material.ecdsaPublic = publicKey
	default:
		return nil, fmt.Errorf("unsupported JWT algorithm %q", algorithm)
	}

	jwtKeyCache.Store(cacheKey, material)
	return material, nil
}

func parseECDSAPrivateKey(value string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(normalizePEM(value)))
	if block == nil {
		return nil, fmt.Errorf("JWT_PRIVATE_KEY must be PEM encoded")
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsedKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse JWT_PRIVATE_KEY: %w", err)
	}
	privateKey, ok := parsedKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("JWT_PRIVATE_KEY must be an ECDSA P-256 key")
	}
	return privateKey, nil
}

func parseECDSAPublicKey(value string) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(normalizePEM(value)))
	if block == nil {
		return nil, fmt.Errorf("JWT_PUBLIC_KEY must be PEM encoded")
	}
	parsedKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse JWT_PUBLIC_KEY: %w", err)
	}
	publicKey, ok := parsedKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("JWT_PUBLIC_KEY must be an ECDSA P-256 key")
	}
	return publicKey, nil
}

func normalizePEM(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), `\n`, "\n")
}

func paddedBigInt(value *big.Int, size int) []byte {
	bytesValue := value.Bytes()
	if len(bytesValue) >= size {
		return bytesValue
	}
	padded := make([]byte, size)
	copy(padded[size-len(bytesValue):], bytesValue)
	return padded
}
