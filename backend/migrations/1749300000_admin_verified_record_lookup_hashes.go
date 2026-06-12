package migrations

import "github.com/gresbase/gresbase/internal/database"

// Migration005 adds missing admin verification state and fast lookup hashes for record auth tokens.
var Migration005 = &database.Migration{
	Name: "1749300001_admin_verified_record_lookup_hashes",
	Up: `
ALTER TABLE _admins ADD COLUMN IF NOT EXISTS verified BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE _record_password_resets ADD COLUMN IF NOT EXISTS lookup_hash TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_record_pwreset_lookup ON _record_password_resets(lookup_hash);

ALTER TABLE _record_verifications ADD COLUMN IF NOT EXISTS lookup_hash TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_record_verify_lookup ON _record_verifications(lookup_hash);

ALTER TABLE _record_email_changes ADD COLUMN IF NOT EXISTS lookup_hash TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_record_emailchange_lookup ON _record_email_changes(lookup_hash);
`,
	Down: `
DROP INDEX IF EXISTS idx_record_emailchange_lookup;
ALTER TABLE _record_email_changes DROP COLUMN IF EXISTS lookup_hash;

DROP INDEX IF EXISTS idx_record_verify_lookup;
ALTER TABLE _record_verifications DROP COLUMN IF EXISTS lookup_hash;

DROP INDEX IF EXISTS idx_record_pwreset_lookup;
ALTER TABLE _record_password_resets DROP COLUMN IF EXISTS lookup_hash;

ALTER TABLE _admins DROP COLUMN IF EXISTS verified;
`,
}
