package migrations

import "github.com/gresbase/gresbase/internal/database"

// Migration002 adds jobs, job runs, OAuth states, and settings tables.
var Migration002 = &database.Migration{
	Name: "1749100000_extended_schema",
	Up: `
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

CREATE TABLE IF NOT EXISTS _job_runs (
    id          BIGSERIAL PRIMARY KEY,
    job_id      TEXT REFERENCES _jobs(id) ON DELETE CASCADE,
    status      TEXT DEFAULT 'running',
    output      TEXT DEFAULT '',
    started_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ,
    error       TEXT DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_job_runs_job_id ON _job_runs(job_id);

CREATE TABLE IF NOT EXISTS _oauth_states (
    id          TEXT PRIMARY KEY,
    provider    TEXT NOT NULL,
    state       TEXT NOT NULL,
    data        JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_oauth_states_provider_state ON _oauth_states(provider, state);

CREATE TABLE IF NOT EXISTS _settings (
    id          TEXT PRIMARY KEY DEFAULT 'app',
    data        JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO _settings (id, data) VALUES ('app', '{}') ON CONFLICT DO NOTHING;
`,
	Down: `
DROP TABLE IF EXISTS _settings CASCADE;
DROP TABLE IF EXISTS _oauth_states CASCADE;
DROP TABLE IF EXISTS _job_runs CASCADE;
DROP TABLE IF EXISTS _jobs CASCADE;
`,
}
