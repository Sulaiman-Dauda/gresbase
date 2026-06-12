-- Gresbase Initial Schema
-- Run automatically on first bootstrap

-- Admin users (superusers)
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

-- Collections (schema definitions)
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
    indexes       JSONB DEFAULT '[]',
    options       JSONB DEFAULT '{}',
    system        BOOLEAN DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_collections_name ON _collections(name);

-- API Keys
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

-- Audit Logs
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
CREATE INDEX IF NOT EXISTS idx_audit_action ON _audit_logs(action);
CREATE INDEX IF NOT EXISTS idx_audit_created ON _audit_logs(created_at);

-- Certificates
CREATE TABLE IF NOT EXISTS _certificates (
    id              TEXT PRIMARY KEY,
    domain          TEXT NOT NULL,
    certificate     TEXT NOT NULL,
    private_key     TEXT NOT NULL DEFAULT '',
    issuer          TEXT DEFAULT '',
    not_before      TIMESTAMPTZ,
    not_after       TIMESTAMPTZ,
    auto_renew      BOOLEAN DEFAULT TRUE,
    challenge_type  TEXT DEFAULT 'http-01',
    status          TEXT DEFAULT 'active',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_certs_domain ON _certificates(domain);

-- Sessions
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
CREATE INDEX IF NOT EXISTS idx_sessions_admin ON _sessions(admin_id);
CREATE INDEX IF NOT EXISTS idx_sessions_token ON _sessions(token);

-- Rate limits
CREATE TABLE IF NOT EXISTS _rate_limits (
    id          BIGSERIAL PRIMARY KEY,
    key         TEXT NOT NULL,
    hits        INTEGER NOT NULL DEFAULT 1,
    window_start TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_ratelimit_key ON _rate_limits(key);

-- ACME Accounts
CREATE TABLE IF NOT EXISTS _acme_accounts (
    id          TEXT PRIMARY KEY,
    contact     JSONB DEFAULT '[]',
    terms_agreed BOOLEAN DEFAULT FALSE,
    status      TEXT DEFAULT 'valid',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
