package api

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/gresbase/gresbase/internal/app"
	"github.com/gresbase/gresbase/internal/collection"
	"github.com/gresbase/gresbase/internal/config"
	"github.com/gresbase/gresbase/internal/realtime"
	"github.com/gresbase/gresbase/internal/testutil"
)

// newWALEnv boots a full app with WAL change capture enabled against the
// ephemeral test PostgreSQL (which runs with wal_level=logical) and waits for
// the replication stream to come up.
func newWALEnv(t *testing.T) *integrationEnv {
	t.Helper()
	pg := testutil.StartPostgres(t)

	cfg := config.DefaultConfig()
	cfg.DatabaseURL = pg.ConnString
	cfg.JWTSecret = "integration-test-secret"
	cfg.DataDir = t.TempDir()
	cfg.StorageLocal = filepath.Join(cfg.DataDir, "storage")
	// Same automigrate isolation as newBootstrappedEnv.
	cfg.MigrationsDir = filepath.Join(cfg.DataDir, "gb_migrations")
	cfg.DevMode = true
	cfg.LogLevel = "error"
	cfg.RealtimeWALEnabled = true

	a, err := app.New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	if err := a.Bootstrap(); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	s := NewServer(a)
	a.SetAPIServer(s)
	a.SetAPIRouter(s)
	t.Cleanup(func() { a.Shutdown() })

	env := &integrationEnv{app: a, server: s}
	waitForWALHealthy(t, env)
	return env
}

func waitForWALHealthy(t *testing.T, env *integrationEnv) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if env.app.WALCapture() != nil && env.app.WALCapture().Healthy() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("WAL capture did not become healthy")
}

// waitForPublicationTable syncs publication membership (in production the
// collection API handlers fire OnCollectionCreate/Update/Delete hooks that do
// this; these tests create collections via the service directly) and polls
// until the table is published.
func waitForPublicationTable(t *testing.T, env *integrationEnv, table string) {
	t.Helper()
	if err := env.app.WALCapture().SyncPublication(context.Background()); err != nil {
		t.Fatalf("sync publication: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var exists bool
		err := env.app.DB().Pool.QueryRow(context.Background(),
			"SELECT EXISTS (SELECT 1 FROM pg_publication_tables WHERE pubname = 'gresbase_realtime' AND tablename = $1)",
			table).Scan(&exists)
		if err == nil && exists {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("table %s never joined the publication", table)
}

func decodeRecordEvent(t *testing.T, msg *realtime.RealtimeMessage) (action string, record map[string]any) {
	t.Helper()
	var payload struct {
		Action string         `json:"action"`
		Record map[string]any `json:"record"`
	}
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		t.Fatalf("decode event payload: %v", err)
	}
	return payload.Action, payload.Record
}

// TestWAL_SQLChangesReachSubscribers is the core capability test: rows
// changed with raw SQL (no API involvement) must reach realtime subscribers
// with the same JSON shapes the API path broadcasts.
func TestWAL_SQLChangesReachSubscribers(t *testing.T) {
	env := newWALEnv(t)
	ctx := context.Background()

	public := ""
	coll := &collection.Collection{
		TenantID: "default",
		Name:     "wal_posts",
		Type:     collection.TypeBase,
		Schema: []collection.SchemaField{
			{Name: "title", Type: collection.FieldText},
			{Name: "count", Type: collection.FieldNumber},
			{Name: "active", Type: collection.FieldBool},
			{Name: "meta", Type: collection.FieldJSON},
		},
		ListRule: &public,
		ViewRule: &public,
	}
	if err := env.app.Collections().CreateCollection(ctx, coll); err != nil {
		t.Fatalf("create collection: %v", err)
	}
	waitForPublicationTable(t, env, "wal_posts")

	sub := newMockRealtimeClient("wal-sub")
	env.app.Realtime().Register(sub)
	time.Sleep(20 * time.Millisecond)
	if err := env.app.Realtime().SubscribeClient(sub, "wal_posts/*", &realtime.Subscription{Topic: "wal_posts/*"}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// INSERT via raw SQL — bypasses every API handler.
	if _, err := env.app.DB().Pool.Exec(ctx,
		`INSERT INTO wal_posts (id, title, count, active, meta) VALUES ('walrec1', 'from sql', 4.5, true, '{"a": 1}')`); err != nil {
		t.Fatalf("raw insert: %v", err)
	}

	msg, ok := waitForRealtimeMessage(t, sub, 10*time.Second, "record:create")
	if !ok {
		t.Fatal("expected record:create from WAL capture")
	}
	action, record := decodeRecordEvent(t, msg)
	if action != "create" {
		t.Fatalf("action = %q", action)
	}
	if record["id"] != "walrec1" || record["title"] != "from sql" {
		t.Fatalf("unexpected record: %v", record)
	}
	if v, ok := record["count"].(float64); !ok || v != 4.5 {
		t.Fatalf("count must be a JSON number 4.5, got %T %v", record["count"], record["count"])
	}
	if v, ok := record["active"].(bool); !ok || !v {
		t.Fatalf("active must be a JSON bool, got %T %v", record["active"], record["active"])
	}
	if meta, ok := record["meta"].(map[string]any); !ok || meta["a"] != float64(1) {
		t.Fatalf("meta must be an unmarshaled object, got %T %v", record["meta"], record["meta"])
	}
	if _, ok := record["created_at"].(string); !ok {
		t.Fatalf("created_at must serialize to a string timestamp, got %T", record["created_at"])
	}

	// UPDATE via raw SQL.
	if _, err := env.app.DB().Pool.Exec(ctx,
		`UPDATE wal_posts SET title = 'updated via sql' WHERE id = 'walrec1'`); err != nil {
		t.Fatalf("raw update: %v", err)
	}
	msg, ok = waitForRealtimeMessage(t, sub, 10*time.Second, "record:update")
	if !ok {
		t.Fatal("expected record:update from WAL capture")
	}
	if _, record = decodeRecordEvent(t, msg); record["title"] != "updated via sql" {
		t.Fatalf("update record wrong: %v", record)
	}

	// DELETE via raw SQL → id-only payload (default replica identity).
	if _, err := env.app.DB().Pool.Exec(ctx, `DELETE FROM wal_posts WHERE id = 'walrec1'`); err != nil {
		t.Fatalf("raw delete: %v", err)
	}
	msg, ok = waitForRealtimeMessage(t, sub, 10*time.Second, "record:delete")
	if !ok {
		t.Fatal("expected record:delete from WAL capture")
	}
	_, record = decodeRecordEvent(t, msg)
	if record["id"] != "walrec1" {
		t.Fatalf("delete record must carry the id, got %v", record)
	}
	if len(record) != 1 {
		t.Fatalf("delete record must be id-only (matching the API shape), got %v", record)
	}
}

// TestWAL_RuleEnforcementStillApplies asserts WAL-sourced events go through
// the same per-subscriber rule check: a locked collection (nil rules)
// delivers to admins only.
func TestWAL_RuleEnforcementStillApplies(t *testing.T) {
	env := newWALEnv(t)
	ctx := context.Background()

	coll := &collection.Collection{
		TenantID: "default",
		Name:     "wal_secrets",
		Type:     collection.TypeBase,
		Schema: []collection.SchemaField{
			{Name: "title", Type: collection.FieldText},
		},
		// All rules nil → locked: superusers only.
	}
	if err := env.app.Collections().CreateCollection(ctx, coll); err != nil {
		t.Fatalf("create collection: %v", err)
	}
	waitForPublicationTable(t, env, "wal_secrets")

	anon := newMockRealtimeClient("wal-anon")
	admin := newMockRealtimeClient("wal-admin")
	admin.SetAuthRecord(&realtime.AuthInfo{AdminID: "admin-1", Role: "super_admin", Verified: true})
	env.app.Realtime().Register(anon)
	env.app.Realtime().Register(admin)
	time.Sleep(20 * time.Millisecond)
	for _, c := range []*mockRealtimeClient{anon, admin} {
		if err := env.app.Realtime().SubscribeClient(c, "wal_secrets/*", &realtime.Subscription{Topic: "wal_secrets/*"}); err != nil {
			t.Fatalf("subscribe: %v", err)
		}
	}

	if _, err := env.app.DB().Pool.Exec(ctx,
		`INSERT INTO wal_secrets (id, title) VALUES ('s1', 'classified')`); err != nil {
		t.Fatalf("raw insert: %v", err)
	}

	// The admin receiving the event proves it flowed through WAL capture…
	if _, ok := waitForRealtimeMessage(t, admin, 10*time.Second, "record:create"); !ok {
		t.Fatal("admin should receive the WAL-sourced event")
	}
	// …so the anonymous client's silence is rule enforcement, not a dead stream.
	if msg, ok := waitForRealtimeMessage(t, anon, 1500*time.Millisecond, "record:create"); ok {
		t.Fatalf("locked collection must not deliver to anonymous subscribers, got %v", msg)
	}
}

// TestWAL_HealthAwareBroadcastSwitch asserts the degraded-mode behavior:
// while the WAL stream is healthy direct API broadcasts are suppressed; when
// the stream dies the API-emitted path resumes immediately.
func TestWAL_HealthAwareBroadcastSwitch(t *testing.T) {
	env := newWALEnv(t)
	ctx := context.Background()

	hub := env.app.Realtime()
	if !hub.DirectRecordBroadcastsSuppressed() {
		t.Fatal("healthy WAL stream must suppress direct record broadcasts")
	}

	admin := newMockRealtimeClient("switch-admin")
	admin.SetAuthRecord(&realtime.AuthInfo{AdminID: "admin-1", Role: "super_admin", Verified: true})
	hub.Register(admin)
	time.Sleep(20 * time.Millisecond)
	if err := hub.SubscribeClient(admin, "anything/*", &realtime.Subscription{Topic: "anything/*"}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Healthy: a direct broadcast must be dropped (WAL is the source of truth).
	hub.BroadcastRecord("create", "anything", "x1", map[string]any{"id": "x1"})
	if msg, ok := waitForRealtimeMessage(t, admin, 800*time.Millisecond, "record:create"); ok {
		t.Fatalf("direct broadcast should be suppressed while WAL is healthy, got %v", msg)
	}

	// Kill the walsender backend; the stream must report unhealthy and the
	// API-emitted path must resume until the capture reconnects.
	if _, err := env.app.DB().Pool.Exec(ctx,
		`SELECT pg_terminate_backend(active_pid) FROM pg_replication_slots WHERE slot_name = 'gresbase_realtime' AND active_pid IS NOT NULL`); err != nil {
		t.Fatalf("terminate walsender: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for hub.DirectRecordBroadcastsSuppressed() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.DirectRecordBroadcastsSuppressed() {
		t.Fatal("direct broadcasts must resume when the WAL stream dies")
	}

	hub.BroadcastRecord("create", "anything", "x2", map[string]any{"id": "x2"})
	if _, ok := waitForRealtimeMessage(t, admin, 2*time.Second, "record:create"); !ok {
		t.Fatal("API-emitted broadcast must deliver while WAL is degraded")
	}

	// And the capture reconnects on its own (backoff starts at 1s).
	waitForWALHealthy(t, env)
}
