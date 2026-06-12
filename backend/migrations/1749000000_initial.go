package migrations

import "github.com/gresbase/gresbase/internal/database"

// Migration001 creates the initial Gresbase system tables:
// admins, collections, api_keys, audit_logs,
// sessions, rate_limits, external_auths, OTP, magic links,
// password resets, verifications, and email changes.
var Migration001 = &database.Migration{
	Name: "1749000000_initial_schema",
	Up: `
CREATE TABLE IF NOT EXISTS _admins (
    id              TEXT PRIMARY KEY,
    email           TEXT NOT NULL UNIQUE,
    password_hash   TEXT NOT NULL,
    avatar          TEXT DEFAULT '',
    role            TEXT DEFAULT 'admin',
    last_login_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_admins_email ON _admins(email);

CREATE TABLE IF NOT EXISTS _collections (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    type          TEXT NOT NULL DEFAULT 'base',
    schema        JSONB NOT NULL DEFAULT '[]',
    list_rule     TEXT DEFAULT '',
    view_rule     TEXT DEFAULT '',
    create_rule   TEXT DEFAULT '',
    update_rule   TEXT DEFAULT '',
    delete_rule   TEXT DEFAULT '',
    view_query    TEXT DEFAULT '',
    indexes       JSONB DEFAULT '[]',
    options       JSONB DEFAULT '{}',
    system        BOOLEAN DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_collections_name ON _collections(name);

CREATE TABLE IF NOT EXISTS _api_keys (
    id            TEXT PRIMARY KEY,
    admin_id      TEXT REFERENCES _admins(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    key_hash      TEXT NOT NULL,
    prefix        TEXT NOT NULL,
    permissions   JSONB DEFAULT '[]',
    last_used_at  TIMESTAMPTZ,
    expires_at    TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_apikeys_key_hash ON _api_keys(key_hash);

CREATE TABLE IF NOT EXISTS _audit_logs (
    id          BIGSERIAL PRIMARY KEY,
    admin_id    TEXT,
    action      TEXT NOT NULL,
    resource    TEXT NOT NULL,
    resource_id TEXT,
    data        JSONB DEFAULT '{}',
    ip          TEXT,
    user_agent  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_audit_created ON _audit_logs(created_at);

CREATE TABLE IF NOT EXISTS _sessions (
    id            TEXT PRIMARY KEY,
    admin_id      TEXT REFERENCES _admins(id) ON DELETE CASCADE,
    token         TEXT NOT NULL UNIQUE,
    refresh_token TEXT,
    user_agent    TEXT,
    ip            TEXT,
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_sessions_token ON _sessions(token);

CREATE TABLE IF NOT EXISTS _rate_limits (
    id          BIGSERIAL PRIMARY KEY,
    key         TEXT NOT NULL,
    hits        INTEGER NOT NULL DEFAULT 1,
    window_start TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_ratelimit_key ON _rate_limits(key);

CREATE TABLE IF NOT EXISTS _external_auths (
    admin_id    TEXT REFERENCES _admins(id) ON DELETE CASCADE,
    provider    TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    data        JSONB DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (provider, provider_id)
);
CREATE INDEX IF NOT EXISTS idx_ext_auths_admin ON _external_auths(admin_id);

CREATE TABLE IF NOT EXISTS _otp (
    id          TEXT PRIMARY KEY,
    admin_id    TEXT REFERENCES _admins(id) ON DELETE CASCADE,
    code_hash   TEXT NOT NULL,
    lookup_hash TEXT NOT NULL DEFAULT '',
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_otp_lookup ON _otp(lookup_hash);

CREATE TABLE IF NOT EXISTS _magic_links (
    id          TEXT PRIMARY KEY,
    admin_id    TEXT REFERENCES _admins(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL,
    lookup_hash TEXT NOT NULL DEFAULT '',
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_magic_links_lookup ON _magic_links(lookup_hash);

CREATE TABLE IF NOT EXISTS _password_resets (
    id          TEXT PRIMARY KEY,
    admin_id    TEXT REFERENCES _admins(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL,
    lookup_hash TEXT NOT NULL DEFAULT '',
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_password_resets_lookup ON _password_resets(lookup_hash);

CREATE TABLE IF NOT EXISTS _verifications (
    id          TEXT PRIMARY KEY,
    admin_id    TEXT REFERENCES _admins(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL,
    lookup_hash TEXT NOT NULL DEFAULT '',
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_verifications_lookup ON _verifications(lookup_hash);

CREATE TABLE IF NOT EXISTS _email_changes (
    id          TEXT PRIMARY KEY,
    admin_id    TEXT REFERENCES _admins(id) ON DELETE CASCADE,
    new_email   TEXT NOT NULL,
    token_hash  TEXT NOT NULL,
    lookup_hash TEXT NOT NULL DEFAULT '',
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_email_changes_lookup ON _email_changes(lookup_hash);
`,
	Down: `
DROP TABLE IF EXISTS _rate_limits CASCADE;
DROP TABLE IF EXISTS _sessions CASCADE;
DROP TABLE IF EXISTS _email_changes CASCADE;
DROP TABLE IF EXISTS _verifications CASCADE;
DROP TABLE IF EXISTS _password_resets CASCADE;
DROP TABLE IF EXISTS _magic_links CASCADE;
DROP TABLE IF EXISTS _otp CASCADE;
DROP TABLE IF EXISTS _external_auths CASCADE;
DROP TABLE IF EXISTS _audit_logs CASCADE;
DROP TABLE IF EXISTS _api_keys CASCADE;
DROP TABLE IF EXISTS _collections CASCADE;
DROP TABLE IF EXISTS _admins CASCADE;
`,
}
