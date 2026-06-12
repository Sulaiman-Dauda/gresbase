package migrations

import "github.com/gresbase/gresbase/internal/database"

// Migration007 adds passkey (WebAuthn) authentication storage for auth
// collection records:
//   - _record_passkeys persists the serialized webauthn.Credential per record,
//     with a user-facing name and a base64url credential_id column so
//     discoverable (usernameless) logins can resolve the owning record.
//   - _webauthn_sessions stores the begin/finish ceremony state server-side so
//     the round-trip survives multi-node deployments. Rows are short-lived
//     (5 minute TTL) and cleaned opportunistically.
var Migration007 = &database.Migration{
	Name: "1749500000_passkeys",
	Up: `
CREATE TABLE IF NOT EXISTS _record_passkeys (
	id            TEXT PRIMARY KEY,
	collection_id TEXT NOT NULL,
	record_id     TEXT NOT NULL,
	name          TEXT NOT NULL DEFAULT 'Passkey',
	credential_id TEXT NOT NULL,
	credential    JSONB NOT NULL,
	created       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	last_used_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_record_passkeys_owner
	ON _record_passkeys (collection_id, record_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_record_passkeys_credential
	ON _record_passkeys (collection_id, credential_id);

CREATE TABLE IF NOT EXISTS _webauthn_sessions (
	id            TEXT PRIMARY KEY,
	purpose       TEXT NOT NULL,
	collection_id TEXT NOT NULL,
	record_id     TEXT,
	data          JSONB NOT NULL,
	expires       TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_webauthn_sessions_expires
	ON _webauthn_sessions (expires);
`,
	Down: `
DROP TABLE IF EXISTS _webauthn_sessions;
DROP TABLE IF EXISTS _record_passkeys;
`,
}
