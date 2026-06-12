package auth

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
)

func newECDSAKeyPEMs(t *testing.T) (privateSEC1, privatePKCS8, publicPKIX string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ecdsa key: %v", err)
	}

	sec1, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}

	privateSEC1 = string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: sec1}))
	privatePKCS8 = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}))
	publicPKIX = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub}))
	return
}

func newEd25519PEMs(t *testing.T) (privatePKCS8, publicPKIX string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	pubBytes, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	privatePKCS8 = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}))
	publicPKIX = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes}))
	return
}

func TestParseECDSAPrivateKeyFormats(t *testing.T) {
	sec1, pkcs8, _ := newECDSAKeyPEMs(t)

	if _, err := parseECDSAPrivateKey(sec1); err != nil {
		t.Errorf("SEC1 EC private key must parse: %v", err)
	}
	if _, err := parseECDSAPrivateKey(pkcs8); err != nil {
		t.Errorf("PKCS8 EC private key must parse: %v", err)
	}

	// Env vars often carry literal \n escapes instead of newlines.
	escaped := strings.ReplaceAll(sec1, "\n", `\n`)
	if _, err := parseECDSAPrivateKey(escaped); err != nil {
		t.Errorf("PEM with escaped newlines must parse: %v", err)
	}
}

func TestParseECDSAPrivateKeyRejectsInvalid(t *testing.T) {
	if _, err := parseECDSAPrivateKey("not pem at all"); err == nil {
		t.Error("non-PEM input must be rejected")
	}

	// A valid PKCS8 key of the wrong type must be rejected.
	ed25519Priv, _ := newEd25519PEMs(t)
	if _, err := parseECDSAPrivateKey(ed25519Priv); err == nil {
		t.Error("non-ECDSA PKCS8 private key must be rejected")
	}
}

func TestParseECDSAPublicKeyRejectsInvalid(t *testing.T) {
	_, _, ecPub := newECDSAKeyPEMs(t)
	if _, err := parseECDSAPublicKey(ecPub); err != nil {
		t.Errorf("ECDSA public key must parse: %v", err)
	}

	if _, err := parseECDSAPublicKey("garbage"); err == nil {
		t.Error("non-PEM public key must be rejected")
	}

	_, ed25519Pub := newEd25519PEMs(t)
	if _, err := parseECDSAPublicKey(ed25519Pub); err == nil {
		t.Error("non-ECDSA public key must be rejected")
	}
}

func TestPaddedBigInt(t *testing.T) {
	// Small values must be left-padded with zeros to the requested size so
	// JWKS coordinates are always exactly 32 bytes.
	small := paddedBigInt(big.NewInt(0x01ff), 32)
	if len(small) != 32 {
		t.Fatalf("padded length = %d, want 32", len(small))
	}
	if small[30] != 0x01 || small[31] != 0xff {
		t.Errorf("unexpected padded value tail: %x", small[28:])
	}
	for _, b := range small[:30] {
		if b != 0 {
			t.Errorf("padding must be zero bytes, got %x", small)
			break
		}
	}

	// Values already at (or above) the size are returned unchanged.
	full := new(big.Int).Lsh(big.NewInt(1), 255) // 32-byte value
	if got := paddedBigInt(full, 32); len(got) != 32 {
		t.Errorf("full-size value length = %d, want 32", len(got))
	}
}

func TestHashPasswordRejectsOverlongPassword(t *testing.T) {
	svc := newNilDBService(t)

	// bcrypt only consumes 72 bytes; silently truncating would weaken long
	// passphrases, so longer inputs must be rejected outright.
	if _, err := svc.HashPassword(strings.Repeat("a", 100)); err == nil {
		t.Error("passwords longer than 72 bytes must be rejected by bcrypt")
	}
}
