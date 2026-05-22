package migrations

import (
	"testing"
)

func TestAllMigrations(t *testing.T) {
	migrations := All()

	if len(migrations) == 0 {
		t.Fatal("expected at least 1 migration")
	}

	// Check that all migrations have required fields
	for i, m := range migrations {
		if m.Name == "" {
			t.Errorf("migration %d has empty name", i)
		}
		if m.Up == "" {
			t.Errorf("migration %s has empty Up SQL", m.Name)
		}
		if m.Down == "" {
			t.Errorf("migration %s has empty Down SQL", m.Name)
		}
	}

	// Verify migration count (should be 4)
	if len(migrations) != 4 {
		t.Errorf("expected 4 migrations, got %d", len(migrations))
	}
}

func TestMigrationOrder(t *testing.T) {
	migrations := All()

	// Migrations should be ordered by name (timestamp)
	for i := 1; i < len(migrations); i++ {
		if migrations[i].Name < migrations[i-1].Name {
			t.Errorf("migrations not in order: %s before %s",
				migrations[i-1].Name, migrations[i].Name)
		}
	}
}

func TestMigrationUpSQLValid(t *testing.T) {
	migrations := All()

	for _, m := range migrations {
		// Each Up should contain CREATE TABLE statements
		if m.Up == "" {
			t.Errorf("migration %s has empty Up", m.Name)
		}
	}
}

func TestMigrationDownSQLValid(t *testing.T) {
	migrations := All()

	for _, m := range migrations {
		// Each Down should contain DROP TABLE statements
		if m.Down == "" {
			t.Errorf("migration %s has empty Down", m.Name)
		}
	}
}

func TestMigration001Content(t *testing.T) {
	if Migration001.Name != "1749000000_initial_schema" {
		t.Errorf("unexpected name: %s", Migration001.Name)
	}

	// Should create core tables
	if Migration001.Up == "" {
		t.Error("Migration001 Up is empty")
	}
	if Migration001.Down == "" {
		t.Error("Migration001 Down is empty")
	}

	// Verify key tables are in the Up migration
	expectedTables := []string{"_tenants", "_admins", "_collections", "_api_keys",
		"_audit_logs", "_certificates", "_sessions", "_external_auths", "_otp",
		"_magic_links", "_password_resets", "_verifications", "_email_changes"}

	for _, table := range expectedTables {
		if !containsSQL(Migration001.Up, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Errorf("Migration001 missing table: %s", table)
		}
	}
}

func TestMigration004Content(t *testing.T) {
	if Migration004.Name != "1749300000_mfa_support" {
		t.Errorf("unexpected name: %s", Migration004.Name)
	}

	// Should create MFA tables
	expectedTables := []string{"_mfa_secrets", "_record_mfa", "_migrations_tracking"}
	for _, table := range expectedTables {
		if !containsSQL(Migration004.Up, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Errorf("Migration004 missing table: %s", table)
		}
	}
}

// containsSQL is a helper that checks if a SQL string contains a substring.
func containsSQL(sql, substr string) bool {
	return len(sql) > 0 && len(substr) > 0 &&
		// Simple substring check
		func(s, sub string) bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}(sql, substr)
}
