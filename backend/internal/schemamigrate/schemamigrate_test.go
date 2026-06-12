package schemamigrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gresbase/gresbase/internal/collection"
)

// TestTriStateRulesSurviveFileRoundTrip proves the locked-by-default rule
// tri-state survives a write→read cycle distinctly: nil (locked) stays nil,
// "" (public) stays "", and an expression stays itself. This is the property
// the whole feature depends on — a migration replay must not silently unlock
// (nil → "") or lock ("" → nil) a collection.
func TestTriStateRulesSurviveFileRoundTrip(t *testing.T) {
	dir := t.TempDir()

	public := ""
	expr := "@request.auth.id != ''"
	coll := &collection.Collection{
		ID:   "c1",
		Name: "articles",
		Type: collection.TypeBase,
		Schema: []collection.SchemaField{
			{Name: "title", Type: collection.FieldText, Required: true},
		},
		ListRule:   nil,     // locked
		ViewRule:   &public, // public
		CreateRule: &expr,   // expression
	}

	mf := &MigrationFile{
		FormatVersion: FormatVersion,
		Name:          "create articles",
		CreatedAt:     time.Now().UTC(),
		Collections:   []*collection.Collection{coll},
	}

	filename, err := WriteFile(dir, "create_articles", mf)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, filename))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	// The raw JSON must encode the three states differently.
	text := string(raw)
	if !strings.Contains(text, `"list_rule": null`) {
		t.Errorf("expected list_rule to serialize as null, file:\n%s", text)
	}
	if !strings.Contains(text, `"view_rule": ""`) {
		t.Errorf("expected view_rule to serialize as empty string, file:\n%s", text)
	}
	if !strings.Contains(text, `"create_rule": "@request.auth.id != ''"`) {
		t.Errorf("expected create_rule to serialize as expression, file:\n%s", text)
	}

	parsed, err := ParseFile(raw)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	got := parsed.Collections[0]
	if got.ListRule != nil {
		t.Errorf("ListRule: expected nil (locked), got %q", *got.ListRule)
	}
	if got.ViewRule == nil || *got.ViewRule != "" {
		t.Errorf("ViewRule: expected \"\" (public), got %v", got.ViewRule)
	}
	if got.CreateRule == nil || *got.CreateRule != expr {
		t.Errorf("CreateRule: expected %q, got %v", expr, got.CreateRule)
	}
	if got.UpdateRule != nil {
		t.Errorf("UpdateRule: expected nil for omitted rule, got %q", *got.UpdateRule)
	}
}

func TestParseFileRejectsMalformedAndUnknown(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{"malformed json", `{not json`, "invalid JSON"},
		{
			"unsupported version",
			`{"formatVersion": 99, "collections": [{"name": "x"}]}`,
			"unsupported formatVersion",
		},
		{
			"unknown field type",
			`{"formatVersion": 1, "collections": [{"name": "posts", "type": "base", "schema": [{"name": "blob", "type": "hologram"}]}]}`,
			`unknown field type "hologram"`,
		},
		{
			"missing collection name",
			`{"formatVersion": 1, "collections": [{"type": "base"}]}`,
			"missing collection name",
		},
		{
			"empty migration",
			`{"formatVersion": 1}`,
			"no collections and no deletions",
		},
		{
			"empty deleted name",
			`{"formatVersion": 1, "deleted": [""]}`,
			"empty collection name",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseFile([]byte(tc.raw))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestParseFileAcceptsDeleteOnly(t *testing.T) {
	mf, err := ParseFile([]byte(`{"formatVersion": 1, "deleted": ["scratch"]}`))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(mf.Deleted) != 1 || mf.Deleted[0] != "scratch" {
		t.Fatalf("unexpected deleted list: %v", mf.Deleted)
	}
}

// TestBuildSnapshotExcludesSystemCollections covers the snapshot command's
// file-building logic: system collections never land in migration files.
func TestBuildSnapshotExcludesSystemCollections(t *testing.T) {
	colls := []*collection.Collection{
		{Name: "users_internal", System: true},
		{Name: "articles"},
		nil,
		{Name: "comments"},
	}
	mf := BuildSnapshot(colls, "init")
	if mf.FormatVersion != FormatVersion {
		t.Fatalf("formatVersion = %d", mf.FormatVersion)
	}
	if mf.Name != "init" {
		t.Fatalf("name = %q", mf.Name)
	}
	if len(mf.Collections) != 2 {
		t.Fatalf("expected 2 collections, got %d", len(mf.Collections))
	}
	for _, c := range mf.Collections {
		if c.System {
			t.Fatalf("system collection %q leaked into snapshot", c.Name)
		}
	}
}

// TestWriteFileCollisionBumpsTimestamp ensures two files written in the same
// second still get distinct, lexicographically ordered names.
func TestWriteFileCollisionBumpsTimestamp(t *testing.T) {
	dir := t.TempDir()
	mf := &MigrationFile{FormatVersion: FormatVersion, Deleted: []string{"x"}}

	first, err := WriteFile(dir, "delete_x", mf)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	second, err := WriteFile(dir, "delete_x", mf)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if first == second {
		t.Fatalf("expected distinct filenames, both %q", first)
	}
	if !(first < second) {
		t.Fatalf("expected lexicographic ordering: %q then %q", first, second)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"create Articles":  "create_articles",
		"  weird -- name ": "weird_name",
		"":                 "migration",
		"___":              "migration",
		"update_posts":     "update_posts",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
