package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gresbase/gresbase/internal/app"
	"github.com/gresbase/gresbase/internal/collection"
	"github.com/gresbase/gresbase/internal/config"
	"github.com/gresbase/gresbase/internal/testutil"
)

// newSchemaMigrateEnv boots a full app + HTTP server on a fresh ephemeral
// PostgreSQL, pointed at the given collection migrations directory, with dev
// mode (automigrate) on.
func newSchemaMigrateEnv(t *testing.T, migrationsDir string) *integrationEnv {
	t.Helper()
	a, err := bootSchemaMigrateApp(t, migrationsDir)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	s := NewServer(a)
	a.SetAPIServer(s)
	a.SetAPIRouter(s)

	env := &integrationEnv{app: a, server: s}
	env.http = httptest.NewServer(env.server)
	t.Cleanup(func() {
		env.http.Close()
	})
	return env
}

// bootSchemaMigrateApp creates and bootstraps an app, returning the bootstrap
// error instead of failing the test, so fail-closed behavior is assertable.
func bootSchemaMigrateApp(t *testing.T, migrationsDir string) (*app.App, error) {
	t.Helper()
	pg := testutil.StartPostgres(t)

	cfg := config.DefaultConfig()
	cfg.DatabaseURL = pg.ConnString
	cfg.JWTSecret = "integration-test-secret"
	cfg.DataDir = t.TempDir()
	cfg.StorageLocal = filepath.Join(cfg.DataDir, "storage")
	cfg.DevMode = true
	cfg.LogLevel = "error"
	cfg.MigrationsDir = migrationsDir

	a, err := app.New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	bootErr := a.Bootstrap()
	t.Cleanup(func() {
		a.Shutdown()
	})
	return a, bootErr
}

func listMigrationFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read migrations dir: %v", err)
	}
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			files = append(files, e.Name())
		}
	}
	return files
}

func countAppliedMigrations(t *testing.T, a *app.App) int {
	t.Helper()
	var n int
	err := a.DB().QueryRow(context.Background(),
		"SELECT COUNT(*) FROM _collection_migrations").Scan(&n)
	if err != nil {
		t.Fatalf("count applied migrations: %v", err)
	}
	return n
}

// TestIntegration_SchemaMigrations_AutomigrateAndReplay is the end-to-end
// round trip: collection changes made through the HTTP API in dev mode emit
// migration files (pre-marked applied), and a FRESH database pointed at the
// same directory replays them on boot into an identical schema — including
// the tri-state access rules (nil=locked, ""=public, "expr"=filter), which
// must survive the file round trip distinctly.
func TestIntegration_SchemaMigrations_AutomigrateAndReplay(t *testing.T) {
	migrationsDir := filepath.Join(t.TempDir(), "gb_migrations")

	// --- Environment 1: make changes through the API (dev mode) ---
	env1 := newSchemaMigrateEnv(t, migrationsDir)
	adminToken := env1.createAdminToken(t)
	authHeader := map[string]string{"Authorization": "Bearer " + adminToken}

	// 1. Create: list_rule omitted (locked/nil), view_rule "" (public),
	// create_rule an expression.
	createBody := map[string]any{
		"name": "articles",
		"type": "base",
		"schema": []map[string]any{
			{"name": "title", "type": "text", "required": true},
			{"name": "votes", "type": "number"},
		},
		"view_rule":   "",
		"create_rule": "title != ''",
		"options":     map[string]any{"note": "kept"},
	}
	resp := doJSONRequest(t, http.MethodPost, env1.http.URL+"/api/v1/collections", createBody, authHeader)
	if resp.StatusCode != http.StatusCreated {
		var body map[string]any
		readJSONBody(t, resp, &body)
		t.Fatalf("create collection status = %d body=%v", resp.StatusCode, body)
	}
	var created collection.Collection
	readJSONBody(t, resp, &created)

	files := listMigrationFiles(t, migrationsDir)
	if len(files) != 1 {
		t.Fatalf("expected 1 migration file after create, got %v", files)
	}
	// The emitted file must encode the tri-state distinctly.
	raw, err := os.ReadFile(filepath.Join(migrationsDir, files[0]))
	if err != nil {
		t.Fatalf("read emitted file: %v", err)
	}
	text := string(raw)
	for _, want := range []string{`"list_rule": null`, `"view_rule": ""`, `"create_rule": "title != ''"`} {
		if !strings.Contains(text, want) {
			t.Errorf("emitted migration file missing %s:\n%s", want, text)
		}
	}
	// Automigrate files must be pre-marked as applied.
	if n := countAppliedMigrations(t, env1.app); n != 1 {
		t.Fatalf("expected 1 applied migration recorded, got %d", n)
	}

	// 2. Update: add a field and set update_rule.
	updateBody := map[string]any{
		"name": "articles",
		"type": "base",
		"schema": []map[string]any{
			{"name": "title", "type": "text", "required": true},
			{"name": "votes", "type": "number"},
			{"name": "summary", "type": "text"},
		},
		"view_rule":   "",
		"create_rule": "title != ''",
		"update_rule": "@request.auth.id != ''",
		"options":     map[string]any{"note": "kept"},
	}
	resp = doJSONRequest(t, http.MethodPut, env1.http.URL+"/api/v1/collections/"+created.ID, updateBody, authHeader)
	if resp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, resp, &body)
		t.Fatalf("update collection status = %d body=%v", resp.StatusCode, body)
	}
	resp.Body.Close()

	// 3. Create a scratch collection, then delete it.
	resp = doJSONRequest(t, http.MethodPost, env1.http.URL+"/api/v1/collections", map[string]any{
		"name": "scratch",
		"type": "base",
		"schema": []map[string]any{
			{"name": "data", "type": "json"},
		},
	}, authHeader)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create scratch status = %d", resp.StatusCode)
	}
	var scratch collection.Collection
	readJSONBody(t, resp, &scratch)

	resp = doJSONRequest(t, http.MethodDelete, env1.http.URL+"/api/v1/collections/"+scratch.ID, nil, authHeader)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete scratch status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	files = listMigrationFiles(t, migrationsDir)
	if len(files) != 4 {
		t.Fatalf("expected 4 migration files (create, update, create, delete), got %v", files)
	}

	// --- Environment 2: FRESH database, same migrations dir ---
	env2 := newSchemaMigrateEnv(t, migrationsDir)

	replayed, err := env2.app.Collections().GetCollectionByName(context.Background(), "articles")
	if err != nil {
		t.Fatalf("articles not materialized on fresh DB: %v", err)
	}

	// Tri-state rules must replay identically.
	if replayed.ListRule != nil {
		t.Errorf("ListRule: expected nil (locked), got %q", *replayed.ListRule)
	}
	if replayed.ViewRule == nil || *replayed.ViewRule != "" {
		t.Errorf("ViewRule: expected \"\" (public), got %v", replayed.ViewRule)
	}
	if replayed.CreateRule == nil || *replayed.CreateRule != "title != ''" {
		t.Errorf("CreateRule: expected expression, got %v", replayed.CreateRule)
	}
	if replayed.UpdateRule == nil || *replayed.UpdateRule != "@request.auth.id != ''" {
		t.Errorf("UpdateRule: expected expression, got %v", replayed.UpdateRule)
	}
	if replayed.DeleteRule != nil {
		t.Errorf("DeleteRule: expected nil (locked), got %q", *replayed.DeleteRule)
	}

	// Final schema = post-update schema (3 fields), deep-compared on the
	// attributes the service round-trips.
	original, err := env1.app.Collections().GetCollectionByName(context.Background(), "articles")
	if err != nil {
		t.Fatalf("get original articles: %v", err)
	}
	if len(replayed.Schema) != 3 || len(original.Schema) != 3 {
		t.Fatalf("expected 3 fields, original=%d replayed=%d", len(original.Schema), len(replayed.Schema))
	}
	origJSON, _ := json.Marshal(struct {
		Schema  []collection.SchemaField `json:"schema"`
		Options map[string]any           `json:"options"`
		Type    collection.CollectionType
	}{original.Schema, original.Options, original.Type})
	replJSON, _ := json.Marshal(struct {
		Schema  []collection.SchemaField `json:"schema"`
		Options map[string]any           `json:"options"`
		Type    collection.CollectionType
	}{replayed.Schema, replayed.Options, replayed.Type})
	if string(origJSON) != string(replJSON) {
		t.Errorf("schema/options mismatch after replay:\noriginal: %s\nreplayed: %s", origJSON, replJSON)
	}

	// The physical table must exist and accept the new column.
	if _, err := env2.app.Collections().CreateRecord(context.Background(), replayed, map[string]any{
		"title":   "hello",
		"summary": "replayed",
	}); err != nil {
		t.Errorf("insert into replayed table: %v", err)
	}

	// Deleted collection must not exist.
	if _, err := env2.app.Collections().GetCollectionByName(context.Background(), "scratch"); err == nil {
		t.Error("scratch collection should have been deleted by replay")
	}

	// All 4 files recorded as applied on the fresh DB.
	if n := countAppliedMigrations(t, env2.app); n != 4 {
		t.Errorf("expected 4 applied migrations on fresh DB, got %d", n)
	}

	// Replay is idempotent: a second Apply is a no-op.
	again, err := env2.app.SchemaMigrations().Apply(context.Background())
	if err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("expected no files on re-apply, got %v", again)
	}
}

// TestIntegration_SchemaMigrations_FailingFileAbortsBoot proves fail-closed
// startup: a malformed migration file aborts Bootstrap with the filename in
// the error.
func TestIntegration_SchemaMigrations_FailingFileAbortsBoot(t *testing.T) {
	migrationsDir := t.TempDir()
	const badFile = "1700000000_bad.json"
	if err := os.WriteFile(filepath.Join(migrationsDir, badFile), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write bad file: %v", err)
	}

	_, err := bootSchemaMigrateApp(t, migrationsDir)
	if err == nil {
		t.Fatal("expected bootstrap to fail on malformed migration file")
	}
	if !strings.Contains(err.Error(), badFile) {
		t.Fatalf("expected error to name %q, got: %v", badFile, err)
	}

	// Unknown field type must also fail-close, naming the file.
	migrationsDir2 := t.TempDir()
	const typoFile = "1700000001_typo.json"
	content := `{"formatVersion": 1, "name": "typo", "collections": [{"name": "posts", "type": "base", "schema": [{"name": "x", "type": "hologram"}]}]}`
	if err := os.WriteFile(filepath.Join(migrationsDir2, typoFile), []byte(content), 0o644); err != nil {
		t.Fatalf("write typo file: %v", err)
	}
	_, err = bootSchemaMigrateApp(t, migrationsDir2)
	if err == nil {
		t.Fatal("expected bootstrap to fail on unknown field type")
	}
	if !strings.Contains(err.Error(), typoFile) || !strings.Contains(err.Error(), "hologram") {
		t.Fatalf("expected error naming %q and the bad type, got: %v", typoFile, err)
	}
}

// TestIntegration_SchemaMigrations_Snapshot covers `gresbase migrations
// snapshot`: a single file captures all non-system collections, marked
// applied, and bootstraps a fresh instance.
func TestIntegration_SchemaMigrations_Snapshot(t *testing.T) {
	// Source environment WITHOUT automigrate noise: empty migrations dir,
	// collections created via the service directly.
	srcDir := filepath.Join(t.TempDir(), "src_migrations")
	env1 := newSchemaMigrateEnv(t, srcDir)

	locked := (*string)(nil)
	public := ""
	expr := "published = true"
	for _, coll := range []*collection.Collection{
		{
			Name: "posts",
			Type: collection.TypeBase,
			Schema: []collection.SchemaField{
				{Name: "title", Type: collection.FieldText, Required: true},
				{Name: "published", Type: collection.FieldBool},
			},
			ListRule:   &expr,
			ViewRule:   &public,
			CreateRule: locked,
		},
		{
			Name: "tags",
			Type: collection.TypeBase,
			Schema: []collection.SchemaField{
				{Name: "label", Type: collection.FieldText, Required: true},
			},
		},
	} {
		if err := env1.app.Collections().CreateCollection(context.Background(), coll); err != nil {
			t.Fatalf("create %s: %v", coll.Name, err)
		}
	}

	snapDir := filepath.Join(t.TempDir(), "snap_migrations")
	file, err := env1.app.SchemaMigrations().Snapshot(context.Background(), "init")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !strings.HasSuffix(file, "_init.json") {
		t.Fatalf("unexpected snapshot filename %q", file)
	}
	// Snapshot is marked applied at write time.
	if n := countAppliedMigrations(t, env1.app); n != 1 {
		t.Fatalf("expected snapshot to be marked applied, count=%d", n)
	}
	// Copy the snapshot to a fresh dir to prove a single file bootstraps.
	data, err := os.ReadFile(filepath.Join(srcDir, file))
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(snapDir, file), data, 0o644); err != nil {
		t.Fatalf("copy snapshot: %v", err)
	}

	env2 := newSchemaMigrateEnv(t, snapDir)
	posts, err := env2.app.Collections().GetCollectionByName(context.Background(), "posts")
	if err != nil {
		t.Fatalf("posts not materialized from snapshot: %v", err)
	}
	if posts.ListRule == nil || *posts.ListRule != expr {
		t.Errorf("ListRule: expected %q, got %v", expr, posts.ListRule)
	}
	if posts.ViewRule == nil || *posts.ViewRule != "" {
		t.Errorf("ViewRule: expected public, got %v", posts.ViewRule)
	}
	if posts.CreateRule != nil {
		t.Errorf("CreateRule: expected locked (nil), got %q", *posts.CreateRule)
	}
	if _, err := env2.app.Collections().GetCollectionByName(context.Background(), "tags"); err != nil {
		t.Errorf("tags not materialized from snapshot: %v", err)
	}
}
