package database_test

import (
	"testing"

	"github.com/gresbase/gresbase/internal/database"
)

func TestAsSlice(t *testing.T) {
	sql := "CREATE TABLE test (id TEXT); CREATE INDEX idx_test ON test(id);"

	statements := database.AsSlice(sql)

	if len(statements) != 2 {
		t.Errorf("Expected 2 statements, got %d", len(statements))
	}

	if statements[0] != "CREATE TABLE test (id TEXT)" {
		t.Errorf("Unexpected first statement: %q", statements[0])
	}
}

func TestAsSliceEmpty(t *testing.T) {
	statements := database.AsSlice("")
	if len(statements) != 0 {
		t.Errorf("Expected 0 statements, got %d", len(statements))
	}
}

func TestAsSliceTrimming(t *testing.T) {
	sql := "  SELECT 1;  SELECT 2;  "

	statements := database.AsSlice(sql)

	if len(statements) != 2 {
		t.Errorf("Expected 2 statements, got %d", len(statements))
	}

	if statements[0] != "SELECT 1" {
		t.Errorf("Expected 'SELECT 1', got %q", statements[0])
	}
}

func TestMigrationNameOrdering(t *testing.T) {
	migrations := database.NewMigrations()

	migrations.Add(&database.Migration{Name: "002_second"})
	migrations.Add(&database.Migration{Name: "001_first"})
	migrations.Add(&database.Migration{Name: "003_third"})

	items := migrations.Items()

	if len(items) != 3 {
		t.Fatalf("Expected 3 migrations, got %d", len(items))
	}

	if items[0].Name != "001_first" {
		t.Errorf("Expected '001_first' first, got %q", items[0].Name)
	}
	if items[1].Name != "002_second" {
		t.Errorf("Expected '002_second' second, got %q", items[1].Name)
	}
	if items[2].Name != "003_third" {
		t.Errorf("Expected '003_third' third, got %q", items[2].Name)
	}
}

func TestNow(t *testing.T) {
	now := database.Now()
	if now.IsZero() {
		t.Error("Now() should not return zero time")
	}
	// Should be UTC
	if now.Location().String() != "UTC" {
		t.Errorf("Expected UTC, got %s", now.Location().String())
	}
}
