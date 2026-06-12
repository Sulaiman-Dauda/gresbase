package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gresbase/gresbase/internal/collection"
	"github.com/gresbase/gresbase/internal/realtime"
)

// createLockedCollection creates a collection with no rules: locked by
// default, accessible to superusers only.
func (e *integrationEnv) createLockedCollection(t *testing.T, name string) *collection.Collection {
	t.Helper()
	coll := &collection.Collection{
		TenantID: "default",
		Name:     name,
		Type:     collection.TypeBase,
		Schema: []collection.SchemaField{
			{Name: "title", Type: collection.FieldText, Required: true},
		},
	}
	if err := e.app.Collections().CreateCollection(context.Background(), coll); err != nil {
		t.Fatalf("create locked collection: %v", err)
	}
	return coll
}

// TestSecurity_CollectionsLockedByDefault asserts that collections created
// without rules deny anonymous access entirely while admins retain access.
func TestSecurity_CollectionsLockedByDefault(t *testing.T) {
	env := newIntegrationEnv(t)
	env.createLockedCollection(t, "secrets")
	adminToken := env.createAdminToken(t)

	// Anonymous list is denied.
	listResp := doJSONRequest(t, http.MethodGet, env.http.URL+"/api/v1/records/secrets", nil, nil)
	if listResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected anonymous list on locked collection to be 403, got %d", listResp.StatusCode)
	}
	listResp.Body.Close()

	// Anonymous create is denied.
	createResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/records/secrets", map[string]any{"title": "nope"}, nil)
	if createResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected anonymous create on locked collection to be 403, got %d", createResp.StatusCode)
	}
	createResp.Body.Close()

	// Anonymous search is denied before any query runs.
	searchResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/search/secrets", map[string]any{"query": "anything"}, nil)
	if searchResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected anonymous search on locked collection to be 403, got %d", searchResp.StatusCode)
	}
	searchResp.Body.Close()

	// Admins retain full access.
	adminList := doJSONRequest(t, http.MethodGet, env.http.URL+"/api/v1/records/secrets", nil, map[string]string{"Authorization": "Bearer " + adminToken})
	if adminList.StatusCode != http.StatusOK {
		t.Fatalf("expected admin list on locked collection to be 200, got %d", adminList.StatusCode)
	}
	adminList.Body.Close()

	adminCreate := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/records/secrets", map[string]any{"title": "ok"}, map[string]string{"Authorization": "Bearer " + adminToken})
	if adminCreate.StatusCode != http.StatusCreated {
		t.Fatalf("expected admin create on locked collection to be 201, got %d", adminCreate.StatusCode)
	}
	adminCreate.Body.Close()
}

// TestSecurity_BatchRecordsEnforcesRules is a regression test for the batch
// endpoint bypass: POST /api/v1/batch/{collection} must honor collection
// rules instead of mutating data for anonymous callers.
func TestSecurity_BatchRecordsEnforcesRules(t *testing.T) {
	env := newIntegrationEnv(t)
	coll := env.createLockedCollection(t, "batch_locked")
	adminToken := env.createAdminToken(t)

	// Anonymous batch create on a locked collection must be rejected.
	anonBatch := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/batch/batch_locked", map[string]any{
		"creates": []map[string]any{{"title": "smuggled"}},
	}, nil)
	if anonBatch.StatusCode != http.StatusForbidden {
		var body map[string]any
		readJSONBody(t, anonBatch, &body)
		t.Fatalf("expected anonymous batch create to be 403, got %d body=%v", anonBatch.StatusCode, body)
	}
	anonBatch.Body.Close()

	// Nothing must have been written.
	_, total, err := env.app.Collections().ListRecords(context.Background(), coll, "", "", 1, 10)
	if err != nil {
		t.Fatalf("list records: %v", err)
	}
	if total != 0 {
		t.Fatalf("expected zero records after rejected batch, got %d", total)
	}

	// Admin batch create works.
	adminBatch := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/batch/batch_locked", map[string]any{
		"creates": []map[string]any{{"title": "legit"}},
	}, map[string]string{"Authorization": "Bearer " + adminToken})
	if adminBatch.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, adminBatch, &body)
		t.Fatalf("expected admin batch create to be 200, got %d body=%v", adminBatch.StatusCode, body)
	}
	adminBatch.Body.Close()
}

// TestSecurity_RealtimeEnforcesRulesOnDelivery asserts that record events are
// only delivered to subscribers allowed by the collection rules.
func TestSecurity_RealtimeEnforcesRulesOnDelivery(t *testing.T) {
	env := newIntegrationEnv(t)
	env.createLockedCollection(t, "private_notes")
	adminToken := env.createAdminToken(t)

	// Anonymous subscriber on a locked collection receives nothing.
	anonClient := newMockRealtimeClient("anon-client")
	env.app.Realtime().Register(anonClient)
	defer env.app.Realtime().Unregister(anonClient)

	// Admin subscriber receives events.
	adminClient := newMockRealtimeClient("admin-client")
	adminClient.SetAuthRecord(&realtime.AuthInfo{AdminID: "admin-1", Role: "super_admin", Verified: true})
	env.app.Realtime().Register(adminClient)
	defer env.app.Realtime().Unregister(adminClient)

	time.Sleep(50 * time.Millisecond)
	if err := env.app.Realtime().SubscribeClient(anonClient, "private_notes/*", &realtime.Subscription{Topic: "private_notes/*"}); err != nil {
		t.Fatalf("subscribe anon: %v", err)
	}
	if err := env.app.Realtime().SubscribeClient(adminClient, "private_notes/*", &realtime.Subscription{Topic: "private_notes/*"}); err != nil {
		t.Fatalf("subscribe admin: %v", err)
	}
	drainRealtimeMessages(anonClient)
	drainRealtimeMessages(adminClient)

	createResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/records/private_notes", map[string]any{"title": "classified"}, map[string]string{"Authorization": "Bearer " + adminToken})
	if createResp.StatusCode != http.StatusCreated {
		var body map[string]any
		readJSONBody(t, createResp, &body)
		t.Fatalf("admin create status = %d body=%v", createResp.StatusCode, body)
	}
	createResp.Body.Close()

	if msg, ok := waitForRealtimeMessage(t, adminClient, 2*time.Second, "record:create"); !ok {
		t.Fatalf("expected admin subscriber to receive record:create, got %v", msg)
	}
	if msg, ok := waitForRealtimeMessage(t, anonClient, 400*time.Millisecond, "record:create"); ok {
		t.Fatalf("anonymous subscriber must not receive events from a locked collection, got %v", msg)
	}
}

// TestSecurity_RealtimeSubscriberFilterApplies asserts that a subscriber's
// own filter expression narrows delivery on a public collection.
func TestSecurity_RealtimeSubscriberFilterApplies(t *testing.T) {
	env := newIntegrationEnv(t)
	env.createCollection(t, "feed") // public rules
	adminToken := env.createAdminToken(t)

	filtered := newMockRealtimeClient("filtered-client")
	env.app.Realtime().Register(filtered)
	defer env.app.Realtime().Unregister(filtered)
	time.Sleep(50 * time.Millisecond)
	if err := env.app.Realtime().SubscribeClient(filtered, "feed/*", &realtime.Subscription{
		Topic:   "feed/*",
		Options: &realtime.SubscribeOptions{Filter: `title = "match"`},
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	drainRealtimeMessages(filtered)

	miss := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/records/feed", map[string]any{"title": "no-match"}, map[string]string{"Authorization": "Bearer " + adminToken})
	miss.Body.Close()
	if msg, ok := waitForRealtimeMessage(t, filtered, 400*time.Millisecond, "record:create"); ok {
		t.Fatalf("filter should have suppressed event, got %v", msg)
	}

	hit := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/records/feed", map[string]any{"title": "match"}, map[string]string{"Authorization": "Bearer " + adminToken})
	hit.Body.Close()
	if _, ok := waitForRealtimeMessage(t, filtered, 2*time.Second, "record:create"); !ok {
		t.Fatal("expected filtered subscriber to receive matching record:create")
	}
}

// TestSecurity_FileDownloadHonorsViewRule asserts the file endpoint applies
// the owning collection's view rule.
func TestSecurity_FileDownloadHonorsViewRule(t *testing.T) {
	env := newIntegrationEnv(t)
	adminToken := env.createAdminToken(t)

	coll := &collection.Collection{
		TenantID: "default",
		Name:     "vault_docs",
		Type:     collection.TypeBase,
		Schema: []collection.SchemaField{
			{Name: "title", Type: collection.FieldText, Required: true},
			{Name: "attachment", Type: collection.FieldFile},
		},
		// No ViewRule: locked.
	}
	if err := env.app.Collections().CreateCollection(context.Background(), coll); err != nil {
		t.Fatalf("create collection: %v", err)
	}
	record, err := env.app.Collections().CreateRecord(context.Background(), coll, map[string]any{"title": "doc"})
	if err != nil {
		t.Fatalf("create record: %v", err)
	}
	recordID, _ := record["id"].(string)

	uploadResp := doMultipartRequest(t, http.MethodPost, env.http.URL+"/api/v1/files/upload?collection=vault_docs&record="+recordID, nil, "file", "secret.txt", []byte("top secret"), map[string]string{
		"Authorization": "Bearer " + adminToken,
	})
	if uploadResp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d", uploadResp.StatusCode)
	}
	var uploaded map[string]any
	readJSONBody(t, uploadResp, &uploaded)
	path, _ := uploaded["path"].(string)
	segments := strings.Split(path, "/")
	fileName := segments[len(segments)-1]

	// Anonymous download is denied without leaking existence.
	anonResp := doJSONRequest(t, http.MethodGet, env.http.URL+"/api/v1/files/vault_docs/"+recordID+"/"+fileName, nil, nil)
	if anonResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected anonymous download of locked file to be 404, got %d", anonResp.StatusCode)
	}
	anonResp.Body.Close()

	// Admin download works.
	adminResp := doJSONRequest(t, http.MethodGet, env.http.URL+"/api/v1/files/vault_docs/"+recordID+"/"+fileName, nil, map[string]string{"Authorization": "Bearer " + adminToken})
	if adminResp.StatusCode != http.StatusOK {
		t.Fatalf("expected admin download to be 200, got %d", adminResp.StatusCode)
	}
	adminResp.Body.Close()
}
