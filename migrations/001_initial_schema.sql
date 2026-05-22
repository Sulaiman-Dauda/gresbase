-- 001_initial_schema.sql
-- Initial Gresbase schema with all system tables.

-- Tenants
CREATE TABLE IF NOT EXISTS _tenants (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL UNIQUE,
    settings    JSONB DEFAULT '{}',
    active      BOOLEAN DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Admins
CREATE TABLE IF NOT EXISTS _admins (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT REFERENCES _tenants(id) ON DELETE CASCADE,
    email           TEXT NOT NULL UNIQUE,
    password_hash   TEXT NOT NULL,
    avatar          TEXT DEFAULT '',
    role            TEXT DEFAULT 'admin',
    verified        BOOLEAN DEFAULT FALSE,
    last_login_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_admins_tenant ON _admins(tenant_id);
CREATE INDEX IF NOT EXISTS idx_admins_email ON _admins(email);

-- Collections
CREATE TABLE IF NOT EXISTS _collections (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT REFERENCES _tenants(id) ON DELETE CASCADE,
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
CREATE UNIQUE INDEX IF NOT EXISTS idx_collections_tenant_name ON _collections(tenant_id, name);

-- API Keys
CREATE TABLE IF NOT EXISTS _api_keys (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT REFERENCES _tenants(id) ON DELETE CASCADE,
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
CREATE INDEX IF NOT EXISTS idx_apikeys_admin ON _api_keys(admin_id);

-- Audit Logs
CREATE TABLE IF NOT EXISTS _audit_logs (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   TEXT REFERENCES _tenants(id) ON DELETE CASCADE,
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
CREATE INDEX IF NOT EXISTS idx_audit_admin ON _audit_logs(admin_id);

-- Certificates
CREATE TABLE IF NOT EXISTS _certificates (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT REFERENCES _tenants(id) ON DELETE CASCADE,
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
    tenant_id     TEXT REFERENCES _tenants(id) ON DELETE CASCADE,
    token         TEXT NOT NULL UNIQUE,
    refresh_token TEXT,
    user_agent    TEXT,
    ip            TEXT,
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_sessions_token ON _sessions(token);
CREATE INDEX IF NOT EXISTS idx_sessions_admin ON _sessions(admin_id);

-- Rate Limits
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

-- External Auths
CREATE TABLE IF NOT EXISTS _external_auths (
    admin_id    TEXT REFERENCES _admins(id) ON DELETE CASCADE,
    provider    TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    data        JSONB DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (provider, provider_id)
);
CREATE INDEX IF NOT EXISTS idx_ext_auths_admin ON _external_auths(admin_id);

-- OAuth States
CREATE TABLE IF NOT EXISTS _oauth_states (
    id          TEXT PRIMARY KEY,
    provider    TEXT NOT NULL,
    state       TEXT NOT NULL,
    data        JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_oauth_states_provider_state ON _oauth_states(provider, state);

-- OTP
CREATE TABLE IF NOT EXISTS _otp (
    id          TEXT PRIMARY KEY,
    admin_id    TEXT REFERENCES _admins(id) ON DELETE CASCADE,
    code_hash   TEXT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_otp_admin ON _otp(admin_id);

-- Magic Links
CREATE TABLE IF NOT EXISTS _magic_links (
    id          TEXT PRIMARY KEY,
    admin_id    TEXT REFERENCES _admins(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_magic_links_admin ON _magic_links(admin_id);

-- Password Resets
CREATE TABLE IF NOT EXISTS _password_resets (
    id          TEXT PRIMARY KEY,
    admin_id    TEXT REFERENCES _admins(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_password_resets_admin ON _password_resets(admin_id);

-- Verifications
CREATE TABLE IF NOT EXISTS _verifications (
    id          TEXT PRIMARY KEY,
    admin_id    TEXT REFERENCES _admins(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_verifications_admin ON _verifications(admin_id);

-- Email Changes
CREATE TABLE IF NOT EXISTS _email_changes (
    id          TEXT PRIMARY KEY,
    admin_id    TEXT REFERENCES _admins(id) ON DELETE CASCADE,
    new_email   TEXT NOT NULL,
    token_hash  TEXT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_email_changes_admin ON _email_changes(admin_id);

-- Jobs
CREATE TABLE IF NOT EXISTS _jobs (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    cron_expr   TEXT NOT NULL,
    handler     TEXT NOT NULL,
    data        TEXT DEFAULT '',
    enabled     BOOLEAN DEFAULT TRUE,
    last_run_at TIMESTAMPTZ,
    next_run_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Migrations tracker
CREATE TABLE IF NOT EXISTS _gresbase_migrations (
    name       VARCHAR(255) PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    checksum   VARCHAR(64)
);

-- Insert default tenant
INSERT INTO _tenants (id, name, slug) VALUES ('default', 'Default', 'default')
ON CONFLICT DO NOTHING;
