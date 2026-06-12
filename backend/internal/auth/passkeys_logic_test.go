package auth

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

func TestDeriveRelyingParty(t *testing.T) {
	cases := []struct {
		name        string
		appURL      string
		wantRPID    string
		wantOrigins []string
		wantErr     bool
	}{
		{
			name:        "https URL",
			appURL:      "https://app.example.com",
			wantRPID:    "app.example.com",
			wantOrigins: []string{"https://app.example.com"},
		},
		{
			name:        "https URL with port keeps port in origin but not RPID",
			appURL:      "https://app.example.com:8443",
			wantRPID:    "app.example.com",
			wantOrigins: []string{"https://app.example.com:8443"},
		},
		{
			name:        "http localhost default",
			appURL:      "http://localhost:8080",
			wantRPID:    "localhost",
			wantOrigins: []string{"http://localhost:8080"},
		},
		{
			name:        "trailing path is ignored for RP purposes",
			appURL:      "https://example.com/app/",
			wantRPID:    "example.com",
			wantOrigins: []string{"https://example.com"},
		},
		{
			name:        "surrounding whitespace is trimmed",
			appURL:      "  https://example.com  ",
			wantRPID:    "example.com",
			wantOrigins: []string{"https://example.com"},
		},
		{name: "empty", appURL: "", wantErr: true},
		{name: "whitespace only", appURL: "   ", wantErr: true},
		{name: "no scheme", appURL: "example.com", wantErr: true},
		{name: "unsupported scheme", appURL: "ftp://example.com", wantErr: true},
		{name: "scheme only", appURL: "https://", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rpID, origins, err := DeriveRelyingParty(tc.appURL)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got rpID=%q origins=%v", tc.appURL, rpID, origins)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if rpID != tc.wantRPID {
				t.Errorf("rpID = %q, want %q", rpID, tc.wantRPID)
			}
			if !reflect.DeepEqual(origins, tc.wantOrigins) {
				t.Errorf("origins = %v, want %v", origins, tc.wantOrigins)
			}
		})
	}
}

// TestPasskeyCredentialSerializationRoundTrip ensures the exact JSON we store
// in _record_passkeys.credential deserializes back into an equivalent
// webauthn.Credential (the storage format the login ceremony depends on).
func TestPasskeyCredentialSerializationRoundTrip(t *testing.T) {
	original := webauthn.Credential{
		ID:                []byte("credential-id-bytes"),
		PublicKey:         []byte{0x01, 0x02, 0x03, 0x04},
		AttestationType:   "none",
		AttestationFormat: "packed",
		Transport: []protocol.AuthenticatorTransport{
			protocol.Internal, protocol.Hybrid,
		},
		Flags: webauthn.CredentialFlags{
			UserPresent:    true,
			UserVerified:   true,
			BackupEligible: true,
			BackupState:    true,
		},
		Authenticator: webauthn.Authenticator{
			AAGUID:    []byte{0xad, 0xce, 0x00, 0x02, 0x35, 0xbc, 0xc6, 0x0a, 0x64, 0x8b, 0x0b, 0x25, 0xf1, 0xf0, 0x55, 0x03},
			SignCount: 42,
		},
	}

	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var restored webauthn.Credential
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !reflect.DeepEqual(original.ID, restored.ID) {
		t.Errorf("ID round-trip mismatch: %v != %v", original.ID, restored.ID)
	}
	if !reflect.DeepEqual(original.PublicKey, restored.PublicKey) {
		t.Errorf("PublicKey round-trip mismatch")
	}
	if original.AttestationType != restored.AttestationType {
		t.Errorf("AttestationType = %q, want %q", restored.AttestationType, original.AttestationType)
	}
	if original.AttestationFormat != restored.AttestationFormat {
		t.Errorf("AttestationFormat = %q, want %q", restored.AttestationFormat, original.AttestationFormat)
	}
	if !reflect.DeepEqual(original.Transport, restored.Transport) {
		t.Errorf("Transport round-trip mismatch: %v != %v", original.Transport, restored.Transport)
	}
	if original.Flags != restored.Flags {
		t.Errorf("Flags round-trip mismatch: %+v != %+v", original.Flags, restored.Flags)
	}
	if !reflect.DeepEqual(original.Authenticator.AAGUID, restored.Authenticator.AAGUID) {
		t.Errorf("AAGUID round-trip mismatch")
	}
	if original.Authenticator.SignCount != restored.Authenticator.SignCount {
		t.Errorf("SignCount = %d, want %d", restored.Authenticator.SignCount, original.Authenticator.SignCount)
	}
}

func TestDefaultPasskeyName(t *testing.T) {
	if got := defaultPasskeyName(nil); got != "Passkey" {
		t.Errorf("nil AAGUID name = %q, want Passkey", got)
	}
	if got := defaultPasskeyName(make([]byte, 16)); got != "Passkey" {
		t.Errorf("zero AAGUID name = %q, want Passkey", got)
	}
	aaguid := []byte{0xad, 0xce, 0x00, 0x02, 0x35, 0xbc, 0xc6, 0x0a, 0x64, 0x8b, 0x0b, 0x25, 0xf1, 0xf0, 0x55, 0x03}
	got := defaultPasskeyName(aaguid)
	if got != "Passkey adce0002" {
		t.Errorf("AAGUID-derived name = %q, want %q", got, "Passkey adce0002")
	}
}

func TestEncodeCredentialID(t *testing.T) {
	// base64url without padding, so the value is safe in URLs and unique per
	// byte sequence (used for the (collection_id, credential_id) lookup).
	if got := encodeCredentialID([]byte{0xfb, 0xff, 0xfe}); got != "-__-" {
		t.Errorf("encodeCredentialID = %q, want %q", got, "-__-")
	}
}
