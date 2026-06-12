package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/gresbase/gresbase/internal/config"
	"github.com/gresbase/gresbase/internal/database"
	"github.com/gresbase/gresbase/internal/settings"
)

// passkeySessionTTL bounds how long a begin/finish ceremony round-trip may
// take. Sessions are persisted in _webauthn_sessions so they survive
// multi-node deployments and are consumed (deleted) on finish.
const passkeySessionTTL = 5 * time.Minute

// Sentinel errors so the HTTP layer can map service failures to the right
// status codes without string matching.
var (
	// ErrPasskeysDisabled is returned when the auth collection has not opted
	// in via the allowPasskeys option. Locked by default, exactly like
	// allowAnonymous.
	ErrPasskeysDisabled = errors.New("passkey authentication is not enabled for this collection")
	// ErrPasskeyNotFound is returned for unknown (or not owned) passkey ids.
	ErrPasskeyNotFound = errors.New("passkey not found")
	// ErrPasskeySessionInvalid is returned when a ceremony session id is
	// unknown, expired, or bound to a different purpose/collection/record.
	ErrPasskeySessionInvalid = errors.New("invalid or expired passkey session")
)

// PasskeyService implements WebAuthn (passkey) authentication for auth
// collection records: register-begin/finish and login-begin/finish ceremonies
// plus passkey management (list/delete).
//
// MFA note: a passkey is already a multi-factor credential (possession of the
// authenticator + on-device biometric/PIN user verification), so a successful
// passkey login mints record tokens directly via the same
// RecordAuthService.generateRecordTokens path that auth-with-password uses
// and deliberately does NOT require any additional MFA second step. (Today
// Gresbase MFA/TOTP is admin-only — _mfa_secrets is keyed by admin_id — so
// there is no record-level second step to skip; if record MFA ever lands,
// passkey logins must keep bypassing it.)
type PasskeyService struct {
	db         *database.DB
	cfg        *config.Config
	recordAuth *RecordAuthService
	// settingsFn lazily resolves the settings service (it is created during
	// app bootstrap, possibly after this service is constructed).
	settingsFn func() *settings.Service
}

// NewPasskeyService creates the passkey (WebAuthn) service. settingsFn may be
// nil; the relying party then falls back to the domain config.
func NewPasskeyService(db *database.DB, cfg *config.Config, recordAuth *RecordAuthService, settingsFn func() *settings.Service) *PasskeyService {
	return &PasskeyService{db: db, cfg: cfg, recordAuth: recordAuth, settingsFn: settingsFn}
}

// PasskeyInfo is the public descriptor of a stored passkey. It never exposes
// credential material.
type PasskeyInfo struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Created    time.Time  `json:"created"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

// ---------------------------------------------------------------------------
// Relying party derivation
// ---------------------------------------------------------------------------

// DeriveRelyingParty derives the WebAuthn RPID (effective domain) and allowed
// origin from an absolute application URL, e.g.
// "https://app.example.com:8443" -> ("app.example.com", ["https://app.example.com:8443"]).
func DeriveRelyingParty(appURL string) (rpID string, origins []string, err error) {
	raw := strings.TrimSpace(appURL)
	if raw == "" {
		return "", nil, fmt.Errorf("app URL is empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", nil, fmt.Errorf("invalid app URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", nil, fmt.Errorf("app URL %q must be absolute with an http(s) scheme", raw)
	}
	host := u.Hostname()
	if host == "" {
		return "", nil, fmt.Errorf("app URL %q has no hostname", raw)
	}
	return host, []string{u.Scheme + "://" + u.Host}, nil
}

// appURLSource resolves the application's public URL the same way the
// mailer/magic-link flow does: the admin-editable Settings.AppURL first
// (defaults to http://localhost:8080), then the domain/enable_tls config that
// mailer.Service.BaseURL uses.
func (ps *PasskeyService) appURLSource(ctx context.Context) (appURL, appName string) {
	appName = "Gresbase"
	if ps.settingsFn != nil {
		if svc := ps.settingsFn(); svc != nil {
			if st, err := svc.Get(ctx); err == nil && st != nil {
				if strings.TrimSpace(st.AppName) != "" {
					appName = st.AppName
				}
				if strings.TrimSpace(st.AppURL) != "" {
					return st.AppURL, appName
				}
			}
		}
	}
	if ps.cfg != nil && strings.TrimSpace(ps.cfg.Domain) != "" {
		domain := ps.cfg.Domain
		if !strings.HasPrefix(domain, "http") {
			if ps.cfg.EnableTLS {
				domain = "https://" + domain
			} else {
				domain = "http://" + domain
			}
		}
		return domain, appName
	}
	return "", appName
}

// relyingParty builds the go-webauthn instance for the current app URL. It
// fails at ceremony time (never at boot) with an actionable error when no RP
// can be derived.
func (ps *PasskeyService) relyingParty(ctx context.Context) (*webauthn.WebAuthn, error) {
	appURL, appName := ps.appURLSource(ctx)
	rpID, origins, err := DeriveRelyingParty(appURL)
	if err != nil {
		return nil, fmt.Errorf("cannot derive WebAuthn relying party: %w (set the Application URL in dashboard settings or the domain config key)", err)
	}
	return webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: appName,
		RPOrigins:     origins,
	})
}

// ---------------------------------------------------------------------------
// webauthn.User adapter
// ---------------------------------------------------------------------------

// passkeyUser adapts an auth collection record to the webauthn.User
// interface. WebAuthnID is the record id, which authenticators store as the
// userHandle — discoverable logins resolve it back to the record.
type passkeyUser struct {
	id          string
	name        string
	credentials []webauthn.Credential
}

func (u *passkeyUser) WebAuthnID() []byte                         { return []byte(u.id) }
func (u *passkeyUser) WebAuthnName() string                       { return u.name }
func (u *passkeyUser) WebAuthnDisplayName() string                { return u.name }
func (u *passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

// ---------------------------------------------------------------------------
// Collection gating + record/credential loading
// ---------------------------------------------------------------------------

// requirePasskeyCollection loads the auth collection and enforces the
// fail-closed allowPasskeys option (default false), mirroring allowAnonymous.
func (ps *PasskeyService) requirePasskeyCollection(ctx context.Context, collectionName string) (*authCollectionInfo, error) {
	coll, err := ps.recordAuth.findAuthCollection(ctx, collectionName)
	if err != nil {
		return nil, err
	}
	if allowed, _ := coll.Options["allowPasskeys"].(bool); !allowed {
		return nil, ErrPasskeysDisabled
	}
	return coll, nil
}

// loadRecord fetches the full record row as a map (or nil error/row when the
// record does not exist).
func (ps *PasskeyService) loadRecord(ctx context.Context, collectionName, recordID string) (map[string]any, error) {
	tableName := ps.recordAuth.quoteIdent(collectionName)
	rows, err := ps.db.Query(ctx, fmt.Sprintf(`SELECT * FROM %s WHERE id = $1`, tableName), recordID)
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, fmt.Errorf("record not found")
	}
	values, err := rows.Values()
	if err != nil {
		return nil, fmt.Errorf("failed to read record: %w", err)
	}
	return ps.recordAuth.rowToMap(rows.FieldDescriptions(), values), nil
}

// loadUser builds the webauthn.User for a record, including its stored
// credentials.
func (ps *PasskeyService) loadUser(ctx context.Context, coll *authCollectionInfo, collectionName, recordID string) (*passkeyUser, error) {
	record, err := ps.loadRecord(ctx, collectionName, recordID)
	if err != nil {
		return nil, err
	}
	name := recordID
	if identityField := ps.recordAuth.findIdentityField(coll.Schema); identityField != "" {
		if v, _ := record[identityField].(string); v != "" {
			name = v
		}
	}
	creds, err := ps.credentialsFor(ctx, coll.ID, recordID)
	if err != nil {
		return nil, err
	}
	return &passkeyUser{id: recordID, name: name, credentials: creds}, nil
}

// credentialsFor returns the deserialized webauthn credentials of a record.
func (ps *PasskeyService) credentialsFor(ctx context.Context, collectionID, recordID string) ([]webauthn.Credential, error) {
	rows, err := ps.db.Query(ctx, `
		SELECT credential FROM _record_passkeys
		WHERE collection_id = $1 AND record_id = $2
		ORDER BY created`, collectionID, recordID)
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}
	defer rows.Close()

	var creds []webauthn.Credential
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var cred webauthn.Credential
		if err := json.Unmarshal(raw, &cred); err != nil {
			continue // skip rows that no longer deserialize instead of bricking login
		}
		creds = append(creds, cred)
	}
	return creds, rows.Err()
}

// ---------------------------------------------------------------------------
// Ceremony session persistence (_webauthn_sessions)
// ---------------------------------------------------------------------------

func (ps *PasskeyService) storeSession(ctx context.Context, purpose, collectionID, recordID string, session *webauthn.SessionData) (string, error) {
	// Opportunistic cleanup of expired ceremony rows.
	_, _ = ps.db.ExecResult(ctx, `DELETE FROM _webauthn_sessions WHERE expires < NOW()`)

	data, err := json.Marshal(session)
	if err != nil {
		return "", fmt.Errorf("failed to serialize webauthn session: %w", err)
	}
	id := uuid.New().String()
	var recordIDArg any
	if recordID != "" {
		recordIDArg = recordID
	}
	if _, err := ps.db.ExecResult(ctx, `
		INSERT INTO _webauthn_sessions (id, purpose, collection_id, record_id, data, expires)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		id, purpose, collectionID, recordIDArg, data, time.Now().Add(passkeySessionTTL)); err != nil {
		return "", fmt.Errorf("failed to store webauthn session: %w", err)
	}
	return id, nil
}

// consumeSession atomically loads and deletes a ceremony session. recordID is
// only matched for the register purpose (login sessions may be anonymous).
func (ps *PasskeyService) consumeSession(ctx context.Context, sessionID, purpose, collectionID, recordID string) (*webauthn.SessionData, error) {
	query := `
		DELETE FROM _webauthn_sessions
		WHERE id = $1 AND purpose = $2 AND collection_id = $3 AND expires > NOW()`
	args := []any{sessionID, purpose, collectionID}
	if recordID != "" {
		query += ` AND record_id = $4`
		args = append(args, recordID)
	}
	query += ` RETURNING data`

	var raw []byte
	if err := ps.db.QueryRow(ctx, query, args...).Scan(&raw); err != nil {
		return nil, ErrPasskeySessionInvalid
	}
	var session webauthn.SessionData
	if err := json.Unmarshal(raw, &session); err != nil {
		return nil, ErrPasskeySessionInvalid
	}
	return &session, nil
}

// ---------------------------------------------------------------------------
// Registration ceremony
// ---------------------------------------------------------------------------

// BeginRegistration starts a passkey registration ceremony for an already
// authenticated record. Resident key is requested as "preferred" so the
// resulting credential is discoverable (usernameless login capable).
func (ps *PasskeyService) BeginRegistration(ctx context.Context, collectionName, recordID string) (*protocol.CredentialCreation, string, error) {
	coll, err := ps.requirePasskeyCollection(ctx, collectionName)
	if err != nil {
		return nil, "", err
	}
	wa, err := ps.relyingParty(ctx)
	if err != nil {
		return nil, "", err
	}
	user, err := ps.loadUser(ctx, coll, collectionName, recordID)
	if err != nil {
		return nil, "", err
	}

	exclusions := make([]protocol.CredentialDescriptor, 0, len(user.credentials))
	for i := range user.credentials {
		exclusions = append(exclusions, user.credentials[i].Descriptor())
	}

	creation, session, err := wa.BeginRegistration(user,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			UserVerification: protocol.VerificationPreferred,
		}),
		// Sets residentKey=preferred and requireResidentKey=false so
		// authenticators create discoverable credentials when they can.
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementPreferred),
		webauthn.WithExclusions(exclusions),
	)
	if err != nil {
		return nil, "", fmt.Errorf("failed to begin passkey registration: %w", err)
	}

	sessionID, err := ps.storeSession(ctx, "register", coll.ID, recordID, session)
	if err != nil {
		return nil, "", err
	}
	return creation, sessionID, nil
}

// FinishRegistration verifies the authenticator attestation response and
// persists the credential. name is optional; it defaults to a label derived
// from the authenticator AAGUID, or "Passkey".
func (ps *PasskeyService) FinishRegistration(ctx context.Context, collectionName, recordID, sessionID, name string, credentialJSON []byte) (*PasskeyInfo, error) {
	coll, err := ps.requirePasskeyCollection(ctx, collectionName)
	if err != nil {
		return nil, err
	}
	wa, err := ps.relyingParty(ctx)
	if err != nil {
		return nil, err
	}
	session, err := ps.consumeSession(ctx, sessionID, "register", coll.ID, recordID)
	if err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(credentialJSON))
	if err != nil {
		return nil, fmt.Errorf("invalid credential creation response: %w", err)
	}
	user, err := ps.loadUser(ctx, coll, collectionName, recordID)
	if err != nil {
		return nil, err
	}
	cred, err := wa.CreateCredential(user, *session, parsed)
	if err != nil {
		return nil, fmt.Errorf("passkey registration verification failed: %w", err)
	}

	name = strings.TrimSpace(name)
	if name == "" {
		name = defaultPasskeyName(cred.Authenticator.AAGUID)
	}

	raw, err := json.Marshal(cred)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize credential: %w", err)
	}

	id := uuid.New().String()
	now := time.Now()
	if _, err := ps.db.ExecResult(ctx, `
		INSERT INTO _record_passkeys (id, collection_id, record_id, name, credential_id, credential, created, updated)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)`,
		id, coll.ID, recordID, name, encodeCredentialID(cred.ID), raw, now); err != nil {
		return nil, fmt.Errorf("failed to store passkey: %w", err)
	}

	return &PasskeyInfo{ID: id, Name: name, Created: now}, nil
}

// defaultPasskeyName derives a stable default label from the authenticator
// AAGUID, falling back to "Passkey" for zero/unknown AAGUIDs.
func defaultPasskeyName(aaguid []byte) string {
	id, err := uuid.FromBytes(aaguid)
	if err != nil || id == uuid.Nil {
		return "Passkey"
	}
	return "Passkey " + id.String()[:8]
}

func encodeCredentialID(id []byte) string {
	return base64.RawURLEncoding.EncodeToString(id)
}

// ---------------------------------------------------------------------------
// Login ceremony
// ---------------------------------------------------------------------------

// BeginLogin starts a passkey assertion ceremony. With an empty email it
// returns discoverable-login options (empty allowCredentials). With an email
// it scopes allowCredentials to that user's passkeys — but if the email is
// unknown (or has no passkeys) it returns the same discoverable options
// instead of an error, so the endpoint never confirms whether an email exists.
func (ps *PasskeyService) BeginLogin(ctx context.Context, collectionName, email string) (*protocol.CredentialAssertion, string, error) {
	coll, err := ps.requirePasskeyCollection(ctx, collectionName)
	if err != nil {
		return nil, "", err
	}
	wa, err := ps.relyingParty(ctx)
	if err != nil {
		return nil, "", err
	}

	var user *passkeyUser
	if email = strings.TrimSpace(email); email != "" {
		if identityField := ps.recordAuth.findIdentityField(coll.Schema); identityField != "" {
			tableName := ps.recordAuth.quoteIdent(collectionName)
			var recordID string
			err := ps.db.QueryRow(ctx,
				fmt.Sprintf(`SELECT id FROM %s WHERE "%s" = $1`, tableName, identityField),
				email).Scan(&recordID)
			if err == nil {
				if candidate, err := ps.loadUser(ctx, coll, collectionName, recordID); err == nil && len(candidate.credentials) > 0 {
					user = candidate
				}
			}
			// Unknown email or no passkeys: fall through to the discoverable
			// branch so the response is indistinguishable from any other
			// passkey-less identity (no account enumeration).
		}
	}

	var (
		assertion *protocol.CredentialAssertion
		session   *webauthn.SessionData
		recordID  string
	)
	if user != nil {
		assertion, session, err = wa.BeginLogin(user, webauthn.WithUserVerification(protocol.VerificationPreferred))
		recordID = user.id
	} else {
		assertion, session, err = wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationPreferred))
	}
	if err != nil {
		return nil, "", fmt.Errorf("failed to begin passkey login: %w", err)
	}

	sessionID, err := ps.storeSession(ctx, "login", coll.ID, recordID, session)
	if err != nil {
		return nil, "", err
	}
	return assertion, sessionID, nil
}

// FinishLogin verifies the assertion, updates the stored credential (sign
// count) and last_used_at, and mints the exact same record auth tokens +
// response shape as auth-with-password. See the PasskeyService doc comment
// for why no MFA second step applies.
func (ps *PasskeyService) FinishLogin(ctx context.Context, collectionName, sessionID string, credentialJSON []byte) (*RecordAuthResult, string, error) {
	coll, err := ps.requirePasskeyCollection(ctx, collectionName)
	if err != nil {
		return nil, "", err
	}
	wa, err := ps.relyingParty(ctx)
	if err != nil {
		return nil, "", err
	}
	session, err := ps.consumeSession(ctx, sessionID, "login", coll.ID, "")
	if err != nil {
		return nil, "", err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(credentialJSON))
	if err != nil {
		return nil, "", fmt.Errorf("invalid credential assertion response: %w", err)
	}

	var (
		user *passkeyUser
		cred *webauthn.Credential
	)
	if len(session.UserID) > 0 {
		// Email-scoped login: the session is bound to a specific record.
		user, err = ps.loadUser(ctx, coll, collectionName, string(session.UserID))
		if err != nil {
			return nil, "", fmt.Errorf("invalid credentials")
		}
		cred, err = wa.ValidateLogin(user, *session, parsed)
	} else {
		// Discoverable login: the authenticator's userHandle is the record id.
		handler := func(rawID, userHandle []byte) (webauthn.User, error) {
			candidate, err := ps.loadUser(ctx, coll, collectionName, string(userHandle))
			if err != nil {
				return nil, fmt.Errorf("unknown user handle")
			}
			return candidate, nil
		}
		var waUser webauthn.User
		waUser, cred, err = wa.ValidatePasskeyLogin(handler, *session, parsed)
		if err == nil {
			user, _ = waUser.(*passkeyUser)
		}
	}
	if err != nil || user == nil || cred == nil {
		return nil, "", fmt.Errorf("invalid credentials")
	}

	// Persist the updated sign count and stamp last_used_at.
	if raw, err := json.Marshal(cred); err == nil {
		_, _ = ps.db.ExecResult(ctx, `
			UPDATE _record_passkeys
			SET credential = $1, updated = NOW(), last_used_at = NOW()
			WHERE collection_id = $2 AND credential_id = $3`,
			raw, coll.ID, encodeCredentialID(cred.ID))
	}

	// Mint tokens through the exact same path as auth-with-password.
	record, err := ps.loadRecord(ctx, collectionName, user.id)
	if err != nil {
		return nil, "", fmt.Errorf("invalid credentials")
	}
	verified := false
	if v, ok := record["verified"].(bool); ok {
		verified = v
	}
	email := ""
	if identityField := ps.recordAuth.findIdentityField(coll.Schema); identityField != "" {
		email, _ = record[identityField].(string)
	}
	token, refreshToken, err := ps.recordAuth.generateRecordTokens(ctx, user.id, coll.ID, email, verified)
	if err != nil {
		return nil, "", err
	}
	if passwordField := ps.recordAuth.findPasswordField(coll.Schema); passwordField != "" {
		delete(record, passwordField)
	}
	delete(record, "tokenKey")

	return &RecordAuthResult{
		Token:        token,
		RefreshToken: refreshToken,
		Record:       record,
	}, user.id, nil
}

// ---------------------------------------------------------------------------
// Passkey management
// ---------------------------------------------------------------------------

// ListPasskeys returns the caller's own passkey descriptors (never the
// credential material).
func (ps *PasskeyService) ListPasskeys(ctx context.Context, collectionName, recordID string) ([]PasskeyInfo, error) {
	coll, err := ps.requirePasskeyCollection(ctx, collectionName)
	if err != nil {
		return nil, err
	}
	rows, err := ps.db.Query(ctx, `
		SELECT id, name, created, last_used_at FROM _record_passkeys
		WHERE collection_id = $1 AND record_id = $2
		ORDER BY created`, coll.ID, recordID)
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}
	defer rows.Close()

	items := []PasskeyInfo{}
	for rows.Next() {
		var info PasskeyInfo
		if err := rows.Scan(&info.ID, &info.Name, &info.Created, &info.LastUsedAt); err != nil {
			return nil, err
		}
		items = append(items, info)
	}
	return items, rows.Err()
}

// DeletePasskey removes one of the caller's own passkeys. Ownership is
// enforced in the WHERE clause: deleting someone else's passkey id reports
// ErrPasskeyNotFound.
func (ps *PasskeyService) DeletePasskey(ctx context.Context, collectionName, recordID, passkeyID string) error {
	coll, err := ps.requirePasskeyCollection(ctx, collectionName)
	if err != nil {
		return err
	}
	tag, err := ps.db.ExecResult(ctx, `
		DELETE FROM _record_passkeys
		WHERE id = $1 AND collection_id = $2 AND record_id = $3`,
		passkeyID, coll.ID, recordID)
	if err != nil {
		return fmt.Errorf("database error: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPasskeyNotFound
	}
	return nil
}
