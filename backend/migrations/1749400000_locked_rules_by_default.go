package migrations

import "github.com/gresbase/gresbase/internal/database"

// Migration006 switches collection access rules to locked-by-default semantics:
// NULL = locked (superusers only), ” = public, expression = filtered.
// Existing empty-string rules are converted to NULL so that nothing is
// silently public unless it was explicitly configured with a rule.
var Migration006 = &database.Migration{
	Name: "1749400000_locked_rules_by_default",
	Up: `
ALTER TABLE _collections ALTER COLUMN list_rule DROP DEFAULT;
ALTER TABLE _collections ALTER COLUMN view_rule DROP DEFAULT;
ALTER TABLE _collections ALTER COLUMN create_rule DROP DEFAULT;
ALTER TABLE _collections ALTER COLUMN update_rule DROP DEFAULT;
ALTER TABLE _collections ALTER COLUMN delete_rule DROP DEFAULT;

UPDATE _collections SET list_rule   = NULL WHERE list_rule   = '';
UPDATE _collections SET view_rule   = NULL WHERE view_rule   = '';
UPDATE _collections SET create_rule = NULL WHERE create_rule = '';
UPDATE _collections SET update_rule = NULL WHERE update_rule = '';
UPDATE _collections SET delete_rule = NULL WHERE delete_rule = '';
`,
	Down: `
UPDATE _collections SET list_rule   = '' WHERE list_rule   IS NULL;
UPDATE _collections SET view_rule   = '' WHERE view_rule   IS NULL;
UPDATE _collections SET create_rule = '' WHERE create_rule IS NULL;
UPDATE _collections SET update_rule = '' WHERE update_rule IS NULL;
UPDATE _collections SET delete_rule = '' WHERE delete_rule IS NULL;

ALTER TABLE _collections ALTER COLUMN list_rule SET DEFAULT '';
ALTER TABLE _collections ALTER COLUMN view_rule SET DEFAULT '';
ALTER TABLE _collections ALTER COLUMN create_rule SET DEFAULT '';
ALTER TABLE _collections ALTER COLUMN update_rule SET DEFAULT '';
ALTER TABLE _collections ALTER COLUMN delete_rule SET DEFAULT '';
`,
}
