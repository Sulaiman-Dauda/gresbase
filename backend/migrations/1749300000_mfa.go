package migrations

import "github.com/gresbase/gresbase/internal/database"

// Migration004 adds MFA (TOTP) support tables for both admins and records.
var Migration004 = &database.Migration{
	Name: "1749300000_mfa_support",
	Up: `
CREATE TABLE IF NOT EXISTS _mfa_secrets (
    id           TEXT PRIMARY KEY,
    admin_id     TEXT REFERENCES _admins(id) ON DELETE CASCADE,
    secret       TEXT NOT NULL,
    enabled      BOOLEAN DEFAULT FALSE,
    backup_codes JSONB DEFAULT '[]',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(admin_id)
);
CREATE INDEX IF NOT EXISTS idx_mfa_admin ON _mfa_secrets(admin_id);

CREATE TABLE IF NOT EXISTS _record_mfa (
    id            TEXT PRIMARY KEY,
    record_id     TEXT NOT NULL,
    collection_id TEXT NOT NULL,
    secret        TEXT NOT NULL,
    enabled       BOOLEAN DEFAULT FALSE,
    backup_codes  JSONB DEFAULT '[]',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_record_mfa_record ON _record_mfa(record_id, collection_id);

CREATE TABLE IF NOT EXISTS _migrations_tracking (
    name       VARCHAR(255) PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    checksum   VARCHAR(64)
);
`,
	Down: `
DROP TABLE IF EXISTS _migrations_tracking CASCADE;
DROP TABLE IF EXISTS _record_mfa CASCADE;
DROP TABLE IF EXISTS _mfa_secrets CASCADE;
`,
}
