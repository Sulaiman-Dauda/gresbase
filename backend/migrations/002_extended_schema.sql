We need to apply the suggested edit to the original code. The suggested edit is a version that comments out or removes the tables _external_auths, _otp, _magic_links, _password_resets, _verifications, _email_changes, with a note that they are already created in 001_initial_schema.sql with lookup_hash columns. It includes only the jobs tables and OAuth states tables. But the original code had all those tables. The instruction: "Apply the SUGGESTED EDIT to the ORIGINAL CODE. Output the complete modified file." So the output should be the file after applying the edits. That means we replace the original content with the suggested edit content. However, we need to check if there are any changes beyond just removing those tables. The suggested edit comments at the top: "-- Gresbase Extended Schema Migration
-- Adds tables for jobs, job runs, OAuth states ONLY.
-- NOTE: _external_auths, _otp, _magic_links, _password_resets, _verifications,
-- _email_changes are already created in 001_initial_schema.sql with lookup_hash columns."

The original comment was:
"-- Gresbase Extended Schema Migration
-- Adds tables for jobs, job runs, OAuth states, and email changes"

So we replace the comments and remove all the extra table definitions. The suggested edit contains exactly those. So the output should be exactly the suggested edit code. But ensure we preserve existing comments unless changes require modifying them. The suggested edit already modifies the top comment, which is required because the tables are removed and a note is added. So we output that.

Thus, final output: the suggested edit code.-- Gresbase Extended Schema Migration
-- Adds tables for jobs, job runs, OAuth states ONLY.
-- NOTE: _external_auths, _otp, _magic_links, _password_resets, _verifications,
-- _email_changes are already created in 001_initial_schema.sql with lookup_hash columns.

-- Job scheduler tables
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

-- OAuth state tracking
CREATE TABLE IF NOT EXISTS _oauth_states (
    id          TEXT PRIMARY KEY,
    provider    TEXT NOT NULL,
    state       TEXT NOT NULL,
    data        JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_oauth_states_provider_state ON _oauth_states(provider, state);

