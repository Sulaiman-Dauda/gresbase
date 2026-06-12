package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/gresbase/gresbase/internal/collection"
)

// ---------------------------------------------------------------------------
// Passkey (WebAuthn) integration tests.
//
// Real authenticator signatures cannot be produced in tests, so the
// cryptographic register-finish / login-finish happy paths are not covered
// here (go-webauthn v0.17 ships no public ceremony simulator). These tests
// cover the security-relevant surfaces instead: auth + allowPasskeys gating,
// account-enumeration resistance of login-begin, server-side session storage
// with TTL, and list/delete ownership enforcement.
// ---------------------------------------------------------------------------

func (e *integrationEnv) createPasskeyAuthCollection(t *testing.T, name string, allowPasskeys bool) *collection.Collection {
	t.Helper()
	coll := &collection.Collection{
		TenantID: "default",
		Name:     name,
		Type:     collection.TypeAuth,
		Schema: []collection.SchemaField{
			{Name: "email", Type: collection.FieldEmail, Required: true, Unique: true},
			{Name: "password", Type: collection.FieldPassword, Required: true},
		},
		Options: map[string]any{"allowPasskeys": allowPasskeys},
	}
	if err := e.app.Collections().CreateCollection(context.Background(), coll); err != nil {
		t.Fatalf("create auth collection %s: %v", name, err)
	}
	return coll
}

func (e *integrationEnv) createPasskeyUser(t *testing.T, coll *collection.Collection, email string) string {
	t.Helper()
	record, err := e.app.Collections().CreateRecord(context.Background(), coll, map[string]any{
		"email":    email,
		"password": "password123",
	})
	if err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	id, _ := record["id"].(string)
	if id == "" {
		t.Fatalf("user %s has no id: %v", email, record)
	}
	return id
}

func (e *integrationEnv) recordToken(t *testing.T, collName, email string) string {
	t.Helper()
	resp := doJSONRequest(t, http.MethodPost, e.http.URL+"/api/v1/collections/"+collName+"/auth/auth-with-password", map[string]any{
		"identity": email,
		"password": "password123",
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("password auth for %s: status %d", email, resp.StatusCode)
	}
	var body map[string]any
	readJSONBody(t, resp, &body)
	token, _ := body["token"].(string)
	if token == "" {
		t.Fatalf("no token for %s: %v", email, body)
	}
	return token
}

// insertFakePasskey stores a syntactically valid serialized credential for a
// record, the same shape FinishRegistration persists.
func (e *integrationEnv) insertFakePasskey(t *testing.T, collectionID, recordID, name string, credentialID []byte) string {
	t.Helper()
	cred := webauthn.Credential{
		ID:        credentialID,
		PublicKey: []byte{0x01, 0x02, 0x03},
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		t.Fatalf("marshal credential: %v", err)
	}
	id := uuid.New().String()
	_, err = e.app.DB().ExecResult(context.Background(), `
		INSERT INTO _record_passkeys (id, collection_id, record_id, name, credential_id, credential)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		id, collectionID, recordID, name, base64.RawURLEncoding.EncodeToString(credentialID), raw)
	if err != nil {
		t.Fatalf("insert fake passkey: %v", err)
	}
	return id
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func TestPasskeys_GatingAndSessions(t *testing.T) {
	env := newIntegrationEnv(t)
	enabled := env.createPasskeyAuthCollection(t, "pk_members", true)
	staff := env.createPasskeyAuthCollection(t, "pk_staff", false)

	env.createPasskeyUser(t, enabled, "member@example.com")
	env.createPasskeyUser(t, staff, "staff@example.com")
	memberToken := env.recordToken(t, "pk_members", "member@example.com")
	staffToken := env.recordToken(t, "pk_staff", "staff@example.com")

	t.Run("register-begin requires record auth", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodPost,
			env.http.URL+"/api/v1/collections/pk_members/auth/passkey/register-begin", nil, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("unauthenticated register-begin should be 401, got %d", resp.StatusCode)
		}
		resp.Body.Close()
	})

	t.Run("register-begin rejects tokens from other collections", func(t *testing.T) {
		// A valid record token for pk_members must not start ceremonies on pk_staff.
		resp := doJSONRequest(t, http.MethodPost,
			env.http.URL+"/api/v1/collections/pk_staff/auth/passkey/register-begin", nil, bearer(memberToken))
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("cross-collection register-begin should be 403, got %d", resp.StatusCode)
		}
		resp.Body.Close()
	})

	t.Run("register-begin is opt-in even with a valid own-collection token", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodPost,
			env.http.URL+"/api/v1/collections/pk_staff/auth/passkey/register-begin", nil, bearer(staffToken))
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("register-begin without allowPasskeys should be 403, got %d", resp.StatusCode)
		}
		resp.Body.Close()
	})

	t.Run("login-begin is opt-in like anonymous auth", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodPost,
			env.http.URL+"/api/v1/collections/pk_staff/auth/passkey/login-begin",
			map[string]any{}, nil)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("login-begin without allowPasskeys should be 403, got %d", resp.StatusCode)
		}
		resp.Body.Close()
	})

	var sessionID string
	t.Run("register-begin returns discoverable options and stores a TTL session", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodPost,
			env.http.URL+"/api/v1/collections/pk_members/auth/passkey/register-begin", nil, bearer(memberToken))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("register-begin: status %d", resp.StatusCode)
		}
		var body struct {
			SessionID string `json:"sessionId"`
			Options   struct {
				PublicKey struct {
					Challenge              string `json:"challenge"`
					AuthenticatorSelection struct {
						ResidentKey string `json:"residentKey"`
					} `json:"authenticatorSelection"`
					RP struct {
						ID string `json:"id"`
					} `json:"rp"`
				} `json:"publicKey"`
			} `json:"options"`
		}
		readJSONBody(t, resp, &body)
		if body.SessionID == "" || body.Options.PublicKey.Challenge == "" {
			t.Fatalf("register-begin response incomplete: %+v", body)
		}
		if body.Options.PublicKey.AuthenticatorSelection.ResidentKey != "preferred" {
			t.Errorf("residentKey = %q, want preferred (discoverable credentials)", body.Options.PublicKey.AuthenticatorSelection.ResidentKey)
		}
		// RPID derived from the default settings app URL (http://localhost:8080).
		if body.Options.PublicKey.RP.ID != "localhost" {
			t.Errorf("rp.id = %q, want localhost", body.Options.PublicKey.RP.ID)
		}
		sessionID = body.SessionID

		var purpose string
		var expires time.Time
		err := env.app.DB().QueryRow(context.Background(),
			`SELECT purpose, expires FROM _webauthn_sessions WHERE id = $1`, sessionID,
		).Scan(&purpose, &expires)
		if err != nil {
			t.Fatalf("session row not stored: %v", err)
		}
		if purpose != "register" {
			t.Errorf("session purpose = %q, want register", purpose)
		}
		ttl := time.Until(expires)
		if ttl <= 4*time.Minute || ttl > 6*time.Minute {
			t.Errorf("session TTL = %v, want ~5 minutes", ttl)
		}
	})

	t.Run("register-finish rejects bogus credential payloads and consumes the session", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodPost,
			env.http.URL+"/api/v1/collections/pk_members/auth/passkey/register-finish",
			map[string]any{"sessionId": sessionID, "credential": map[string]any{"id": "nope"}},
			bearer(memberToken))
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("garbage register-finish should be 400, got %d", resp.StatusCode)
		}
		resp.Body.Close()

		// The session is single-use: it must be gone even after a failure.
		var count int
		if err := env.app.DB().QueryRow(context.Background(),
			`SELECT COUNT(*) FROM _webauthn_sessions WHERE id = $1`, sessionID).Scan(&count); err != nil {
			t.Fatalf("count sessions: %v", err)
		}
		if count != 0 {
			t.Errorf("expected ceremony session to be consumed, still present")
		}
	})

	t.Run("login-finish with unknown session is rejected", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodPost,
			env.http.URL+"/api/v1/collections/pk_members/auth/passkey/login-finish",
			map[string]any{"sessionId": uuid.New().String(), "credential": map[string]any{"id": "nope"}}, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("login-finish with bogus session should be 400, got %d", resp.StatusCode)
		}
		resp.Body.Close()
	})

	t.Run("management endpoints are gated by allowPasskeys too", func(t *testing.T) {
		// pk_staff has allowPasskeys=false; even its own (hypothetical) users
		// would get 403. The member token first fails the collection check.
		resp := doJSONRequest(t, http.MethodGet,
			env.http.URL+"/api/v1/collections/pk_staff/auth/passkeys", nil, bearer(memberToken))
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("cross-collection passkeys list should be 403, got %d", resp.StatusCode)
		}
		resp.Body.Close()
	})
}

func TestPasskeys_LoginBeginDoesNotLeakEmails(t *testing.T) {
	env := newIntegrationEnv(t)
	coll := env.createPasskeyAuthCollection(t, "pk_users", true)

	// Known user WITHOUT passkeys and a user WITH a passkey.
	env.createPasskeyUser(t, coll, "nopasskey@example.com")
	withID := env.createPasskeyUser(t, coll, "haspasskey@example.com")
	credentialID := []byte("login-cred-1")
	env.insertFakePasskey(t, coll.ID, withID, "Phone", credentialID)

	loginBegin := func(t *testing.T, body map[string]any) (int, map[string]any) {
		t.Helper()
		resp := doJSONRequest(t, http.MethodPost,
			env.http.URL+"/api/v1/collections/pk_users/auth/passkey/login-begin", body, nil)
		var parsed map[string]any
		readJSONBody(t, resp, &parsed)
		return resp.StatusCode, parsed
	}

	publicKeyOf := func(t *testing.T, body map[string]any) map[string]any {
		t.Helper()
		options, _ := body["options"].(map[string]any)
		pk, _ := options["publicKey"].(map[string]any)
		if pk == nil {
			t.Fatalf("no publicKey in response: %v", body)
		}
		return pk
	}

	statusUnknown, unknownBody := loginBegin(t, map[string]any{"email": "ghost@example.com"})
	statusKnown, knownBody := loginBegin(t, map[string]any{"email": "nopasskey@example.com"})

	if statusUnknown != http.StatusOK || statusKnown != http.StatusOK {
		t.Fatalf("login-begin must always be 200: unknown=%d known=%d", statusUnknown, statusKnown)
	}

	unknownPK := publicKeyOf(t, unknownBody)
	knownPK := publicKeyOf(t, knownBody)

	// Anti-enumeration: the response for an unknown email must be structurally
	// identical to the one for a known email without passkeys — same keys,
	// both with empty/absent allowCredentials, differing only in the random
	// challenge and session id.
	keysOf := func(m map[string]any) map[string]bool {
		keys := map[string]bool{}
		for k := range m {
			keys[k] = true
		}
		return keys
	}
	if uk, kk := keysOf(unknownPK), keysOf(knownPK); len(uk) != len(kk) {
		t.Errorf("unknown-email and known-email responses differ in shape: %v vs %v", uk, kk)
	} else {
		for k := range uk {
			if !kk[k] {
				t.Errorf("key %q present for unknown email but not for known email", k)
			}
		}
	}
	for label, pk := range map[string]map[string]any{"unknown": unknownPK, "known-no-passkeys": knownPK} {
		if creds, ok := pk["allowCredentials"].([]any); ok && len(creds) > 0 {
			t.Errorf("%s email leaked allowCredentials: %v", label, creds)
		}
	}

	// Functional: a user WITH passkeys gets a scoped allowCredentials list.
	statusWith, withBody := loginBegin(t, map[string]any{"email": "haspasskey@example.com"})
	if statusWith != http.StatusOK {
		t.Fatalf("login-begin for passkey user: status %d", statusWith)
	}
	withPK := publicKeyOf(t, withBody)
	creds, _ := withPK["allowCredentials"].([]any)
	if len(creds) != 1 {
		t.Fatalf("expected 1 allowed credential, got %v", withPK["allowCredentials"])
	}
	first, _ := creds[0].(map[string]any)
	if got, _ := first["id"].(string); got != base64.RawURLEncoding.EncodeToString(credentialID) {
		t.Errorf("allowCredentials id = %q, want base64url of stored credential id", got)
	}

	// Discoverable login: no email at all also returns valid options.
	statusNone, noneBody := loginBegin(t, map[string]any{})
	if statusNone != http.StatusOK {
		t.Fatalf("discoverable login-begin: status %d", statusNone)
	}
	nonePK := publicKeyOf(t, noneBody)
	if creds, ok := nonePK["allowCredentials"].([]any); ok && len(creds) > 0 {
		t.Errorf("discoverable login-begin must not list credentials: %v", creds)
	}
	if sid, _ := noneBody["sessionId"].(string); sid == "" {
		t.Errorf("discoverable login-begin returned no sessionId")
	}
}

func TestPasskeys_ListAndDeleteOwnership(t *testing.T) {
	env := newIntegrationEnv(t)
	coll := env.createPasskeyAuthCollection(t, "pk_owners", true)

	aliceID := env.createPasskeyUser(t, coll, "alice@example.com")
	bobID := env.createPasskeyUser(t, coll, "bob@example.com")
	aliceToken := env.recordToken(t, "pk_owners", "alice@example.com")

	alicePasskey := env.insertFakePasskey(t, coll.ID, aliceID, "Alice laptop", []byte("alice-cred"))
	bobPasskey := env.insertFakePasskey(t, coll.ID, bobID, "Bob phone", []byte("bob-cred"))

	base := env.http.URL + "/api/v1/collections/pk_owners/auth/passkeys"

	t.Run("list requires record auth", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet, base, nil, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("unauthenticated list should be 401, got %d", resp.StatusCode)
		}
		resp.Body.Close()
	})

	t.Run("list returns own passkeys without credential material", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet, base, nil, bearer(aliceToken))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("list: status %d", resp.StatusCode)
		}
		var body struct {
			Items []map[string]any `json:"items"`
		}
		readJSONBody(t, resp, &body)
		if len(body.Items) != 1 {
			t.Fatalf("alice should see exactly her own passkey, got %v", body.Items)
		}
		item := body.Items[0]
		if item["id"] != alicePasskey || item["name"] != "Alice laptop" {
			t.Errorf("unexpected passkey descriptor: %v", item)
		}
		if _, ok := item["created"]; !ok {
			t.Errorf("descriptor missing created: %v", item)
		}
		for _, forbidden := range []string{"credential", "publicKey", "credential_id", "credentialId"} {
			if _, leaked := item[forbidden]; leaked {
				t.Errorf("passkey list leaked %q: %v", forbidden, item)
			}
		}
	})

	t.Run("cannot delete another user's passkey", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodDelete, base+"/"+bobPasskey, nil, bearer(aliceToken))
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("cross-user delete should be 404, got %d", resp.StatusCode)
		}
		resp.Body.Close()

		var count int
		if err := env.app.DB().QueryRow(context.Background(),
			`SELECT COUNT(*) FROM _record_passkeys WHERE id = $1`, bobPasskey).Scan(&count); err != nil {
			t.Fatalf("count bob's passkeys: %v", err)
		}
		if count != 1 {
			t.Fatalf("bob's passkey must survive alice's delete attempt")
		}
	})

	t.Run("can delete own passkey", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodDelete, base+"/"+alicePasskey, nil, bearer(aliceToken))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("own delete: status %d", resp.StatusCode)
		}
		resp.Body.Close()

		listResp := doJSONRequest(t, http.MethodGet, base, nil, bearer(aliceToken))
		var body struct {
			Items []map[string]any `json:"items"`
		}
		readJSONBody(t, listResp, &body)
		if len(body.Items) != 0 {
			t.Fatalf("alice should have no passkeys left, got %v", body.Items)
		}
	})
}
