package database

import (
	"os"
	"path/filepath"
	"testing"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

// writePGVersion creates a fake initialized data dir with the given
// PG_VERSION contents.
func writePGVersion(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "PG_VERSION"), []byte(contents), 0644); err != nil {
		t.Fatalf("write PG_VERSION: %v", err)
	}
	return dir
}

func TestSelectEmbeddedVersionNewDataDir(t *testing.T) {
	// Empty dir: no PG_VERSION yet — new clusters get the current default.
	version, pinned, err := selectEmbeddedVersion(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != embeddedpostgres.V17 {
		t.Errorf("expected new data dir to get PostgreSQL 17 (%s), got %s", embeddedpostgres.V17, version)
	}
	if pinned {
		t.Error("new data dir must not report a pinned version")
	}
}

func TestSelectEmbeddedVersionMissingDataDir(t *testing.T) {
	// Not-yet-created dir behaves like a new cluster.
	version, pinned, err := selectEmbeddedVersion(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != embeddedpostgres.V17 {
		t.Errorf("expected missing data dir to get PostgreSQL 17, got %s", version)
	}
	if pinned {
		t.Error("missing data dir must not report a pinned version")
	}
}

func TestSelectEmbeddedVersionExistingOlderMajorIsPinned(t *testing.T) {
	// A data dir initialized with 16 must KEEP running 16 — never start a
	// new major against an old cluster.
	dir := writePGVersion(t, "16\n")
	version, pinned, err := selectEmbeddedVersion(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != embeddedpostgres.V16 {
		t.Errorf("expected data dir to pin PostgreSQL 16 (%s), got %s", embeddedpostgres.V16, version)
	}
	if !pinned {
		t.Error("older data dir must report the version as pinned")
	}
}

func TestSelectEmbeddedVersionCurrentMajorNotPinned(t *testing.T) {
	dir := writePGVersion(t, "17\n")
	version, pinned, err := selectEmbeddedVersion(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != embeddedpostgres.V17 {
		t.Errorf("expected PostgreSQL 17, got %s", version)
	}
	if pinned {
		t.Error("data dir already on the default major must not report pinned")
	}
}

func TestSelectEmbeddedVersionLegacyDottedMajor(t *testing.T) {
	// Pre-10 clusters record dotted majors like "9.6". The supported map
	// intentionally starts at 10, so this must fail closed (clear error)
	// rather than guess.
	dir := writePGVersion(t, "9.6\n")
	if _, _, err := selectEmbeddedVersion(dir); err == nil {
		t.Fatal("expected an error for a major this build cannot run")
	}
}

func TestSelectEmbeddedVersionGarbageFailsClosed(t *testing.T) {
	dir := writePGVersion(t, "not-a-version\n")
	if _, _, err := selectEmbeddedVersion(dir); err == nil {
		t.Fatal("expected an error for unparseable PG_VERSION, got nil")
	}
}

func TestSelectEmbeddedVersionUnsupportedMajorFailsClosed(t *testing.T) {
	dir := writePGVersion(t, "8\n")
	if _, _, err := selectEmbeddedVersion(dir); err == nil {
		t.Fatal("expected an error for an unsupported major, got nil")
	}
}
