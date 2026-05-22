package migrations

import "github.com/gresbase/gresbase/internal/database"

// Migration003 adds record-level authentication tables for auth collections.
var Migration003 = &database.Migration{
	Name: "1749200000_record_auth",
	Up: `
CREATE TABLE IF NOT EXISTS _record_otp (
    id            TEXT PRIMARY KEY,
    record_id     TEXT NOT NULL,
    collection_id TEXT NOT NULL,
    code_hash     TEXT NOT NULL,
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_record_otp_record ON _record_otp(record_id);

CREATE TABLE IF NOT EXISTS _record_sessions (
    id            TEXT PRIMARY KEY,
    record_id     TEXT NOT NULL,
    collection_id TEXT NOT NULL,
    token         TEXT NOT NULL,
    refresh_token TEXT,
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_record_sessions_token ON _record_sessions(token);
CREATE INDEX IF NOT EXISTS idx_record_sessions_record ON _record_sessions(record_id);

CREATE TABLE IF NOT EXISTS _record_external_auths (
    record_id     TEXT NOT NULL,
    collection_id TEXT NOT NULL,
    provider      TEXT NOT NULL,
    provider_id   TEXT NOT NULL,
    data          JSONB DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (collection_id, provider, provider_id)
);
CREATE INDEX IF NOT EXISTS idx_record_extauth_record ON _record_external_auths(record_id);

CREATE TABLE IF NOT EXISTS _record_password_resets (
    id            TEXT PRIMARY KEY,
    record_id     TEXT NOT NULL,
    collection_id TEXT NOT NULL,
    token_hash    TEXT NOT NULL,
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_record_pwreset_record ON _record_password_resets(record_id);

CREATE TABLE IF NOT EXISTS _record_verifications (
    id            TEXT PRIMARY KEY,
    record_id     TEXT NOT NULL,
    collection_id TEXT NOT NULL,
    token_hash    TEXT NOT NULL,
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_record_verify_record ON _record_verifications(record_id);

CREATE TABLE IF NOT EXISTS _record_email_changes (
    id            TEXT PRIMARY KEY,
    record_id     TEXT NOT NULL,
    collection_id TEXT NOT NULL,
    new_email     TEXT NOT NULL,
    token_hash    TEXT NOT NULL,
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_record_emailchange_record ON _record_email_changes(record_id);

CREATE TABLE IF NOT EXISTS _collection_rate_limits (
    id            TEXT PRIMARY KEY,
    collection_id TEXT NOT NULL,
    action        TEXT NOT NULL,
    max_per_sec   INTEGER DEFAULT 10,
    max_per_min   INTEGER DEFAULT 100,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_col_ratelimit ON _collection_rate_limits(collection_id, action);
`,
	Down: `
DROP TABLE IF EXISTS _collection_rate_limits CASCADE;
DROP TABLE IF EXISTS _record_email_changes CASCADE;
DROP TABLE IF EXISTS _record_verifications CASCADE;
DROP TABLE IF EXISTS _record_password_resets CASCADE;
DROP TABLE IF EXISTS _record_external_auths CASCADE;
DROP TABLE IF EXISTS _record_sessions CASCADE;
DROP TABLE IF EXISTS _record_otp CASCADE;
`,
}
