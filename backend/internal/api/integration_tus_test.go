package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gresbase/gresbase/internal/collection"
	"github.com/gresbase/gresbase/internal/events"
)

// ---------------------------------------------------------------------------
// TUS resumable upload integration tests: real HTTP against an ephemeral PG.
// ---------------------------------------------------------------------------

// tinyPNG is a valid 1x1 PNG so content sniffing yields image/png.
var tinyPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG signature
	0x00, 0x00, 0x00, 0x0D, 'I', 'H', 'D', 'R',
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89,
	0x00, 0x00, 0x00, 0x0A, 'I', 'D', 'A', 'T',
	0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01,
	0x0D, 0x0A, 0x2D, 0xB4,
	0x00, 0x00, 0x00, 0x00, 'I', 'E', 'N', 'D',
	0xAE, 0x42, 0x60, 0x82,
}

func tusMetadataHeader(pairs map[string]string) string {
	parts := make([]string, 0, len(pairs))
	for k, v := range pairs {
		parts = append(parts, k+" "+base64.StdEncoding.EncodeToString([]byte(v)))
	}
	return strings.Join(parts, ",")
}

// createTUSFileCollection creates a base collection with an owner text field
// and a "doc" file field, gated by the given update rule.
func createTUSFileCollection(t *testing.T, env *integrationEnv, name string, updateRule *string, fileOpts map[string]any) *collection.Collection {
	t.Helper()
	public := ""
	coll := &collection.Collection{
		Name: name,
		Type: collection.TypeBase,
		Schema: []collection.SchemaField{
			{Name: "owner", Type: collection.FieldText},
			{Name: "doc", Type: collection.FieldFile, Options: fileOpts},
		},
		ListRule:   &public,
		ViewRule:   &public,
		CreateRule: &public,
		UpdateRule: updateRule,
		DeleteRule: &public,
	}
	if err := env.app.Collections().CreateCollection(context.Background(), coll); err != nil {
		t.Fatalf("create collection %s: %v", name, err)
	}
	return coll
}

func createTUSUser(t *testing.T, env *integrationEnv, coll *collection.Collection, email string) string {
	t.Helper()
	record, err := env.app.Collections().CreateRecord(context.Background(), coll, map[string]any{
		"email":    email,
		"password": "password123",
	})
	if err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	id, _ := record["id"].(string)
	return id
}

// tusCreate issues the TUS creation POST and returns the response.
func tusCreate(t *testing.T, env *integrationEnv, token string, length int, meta map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, env.http.URL+"/api/v1/files/tus/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Tus-Resumable", "1.0.0")
	req.Header.Set("Upload-Length", strconv.Itoa(length))
	req.Header.Set("Upload-Metadata", tusMetadataHeader(meta))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("tus create: %v", err)
	}
	return resp
}

// tusPatch sends one chunk at the given offset and returns the response.
func tusPatch(t *testing.T, location string, offset int, chunk []byte, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPatch, location, bytes.NewReader(chunk))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Tus-Resumable", "1.0.0")
	req.Header.Set("Upload-Offset", strconv.Itoa(offset))
	req.Header.Set("Content-Type", "application/offset+octet-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("tus patch: %v", err)
	}
	return resp
}

func tusChunkDir(env *integrationEnv) string {
	return filepath.Join(env.app.Config().StorageLocal, ".tus_uploads")
}

func tusChunkCount(t *testing.T, env *integrationEnv) int {
	t.Helper()
	entries, err := os.ReadDir(tusChunkDir(env))
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read tus dir: %v", err)
	}
	return len(entries)
}

func TestTUS_HappyPathTwoChunks(t *testing.T) {
	env := newIntegrationEnv(t)
	users := env.createAuthCollection(t, "tus_users")
	aliceID := createTUSUser(t, env, users, "alice@example.com")
	aliceToken := env.recordToken(t, "tus_users", "alice@example.com")

	rule := "owner = @request.auth.id"
	coll := createTUSFileCollection(t, env, "tus_docs", &rule, map[string]any{"max_select": float64(1)})
	record, err := env.app.Collections().CreateRecord(context.Background(), coll, map[string]any{"owner": aliceID})
	if err != nil {
		t.Fatalf("create record: %v", err)
	}
	recordID := record["id"].(string)

	// Observe the model-level update hook the completion step must fire.
	var hookFired atomic.Bool
	env.app.OnRecordUpdate().BindFunc(func(e events.Event) error {
		if re, ok := e.(*events.RecordEvent); ok && re.CollectionName == "tus_docs" {
			hookFired.Store(true)
		}
		return e.Next()
	})

	content := tinyPNG
	resp := tusCreate(t, env, aliceToken, len(content), map[string]string{
		"collection": "tus_docs",
		"recordId":   recordID,
		"field":      "doc",
		"filename":   "avatar.png",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	resp.Body.Close()
	if location == "" {
		t.Fatal("create: no Location header")
	}

	// Chunk 1
	half := len(content) / 2
	r1 := tusPatch(t, location, 0, content[:half], aliceToken)
	if r1.StatusCode != http.StatusNoContent {
		t.Fatalf("patch 1: expected 204, got %d", r1.StatusCode)
	}
	if got := r1.Header.Get("Upload-Offset"); got != strconv.Itoa(half) {
		t.Fatalf("patch 1: expected offset %d, got %s", half, got)
	}
	r1.Body.Close()

	// Chunk 2 — completes the upload and attaches the file.
	r2 := tusPatch(t, location, half, content[half:], aliceToken)
	if r2.StatusCode != http.StatusNoContent {
		body := make([]byte, 512)
		n, _ := r2.Body.Read(body)
		t.Fatalf("patch 2: expected 204, got %d: %s", r2.StatusCode, body[:n])
	}
	if got := r2.Header.Get("Upload-Offset"); got != strconv.Itoa(len(content)) {
		t.Fatalf("patch 2: expected offset %d, got %s", len(content), got)
	}
	storedName := r2.Header.Get("X-Gresbase-Filename")
	r2.Body.Close()
	if storedName == "" {
		t.Fatal("patch 2: no X-Gresbase-Filename header")
	}

	// Record field updated with the stored filename.
	updated, err := env.app.Collections().GetRecord(context.Background(), coll, recordID)
	if err != nil {
		t.Fatalf("get record: %v", err)
	}
	if got := fmt.Sprint(updated["doc"]); got != storedName {
		t.Fatalf("record doc field: expected %q, got %q", storedName, got)
	}

	// File exists in storage with the right content.
	data, info, err := env.app.Storage().Download(context.Background(), fmt.Sprintf("tus_docs/%s/%s", recordID, storedName))
	if err != nil {
		t.Fatalf("download stored file: %v", err)
	}
	if !bytes.Equal(data, content) {
		t.Fatalf("stored file content mismatch: %d vs %d bytes", len(data), len(content))
	}
	_ = info

	if !hookFired.Load() {
		t.Fatal("OnRecordUpdate hook did not fire")
	}

	// Chunk files cleaned up after a successful attach.
	if n := tusChunkCount(t, env); n != 0 {
		t.Fatalf("expected empty chunk dir, found %d entries", n)
	}
}

func TestTUS_RuleDenial(t *testing.T) {
	env := newIntegrationEnv(t)
	users := env.createAuthCollection(t, "tus_users2")
	aliceID := createTUSUser(t, env, users, "alice2@example.com")
	createTUSUser(t, env, users, "bob2@example.com")
	aliceToken := env.recordToken(t, "tus_users2", "alice2@example.com")
	bobToken := env.recordToken(t, "tus_users2", "bob2@example.com")
	adminToken := env.createAdminToken(t)

	rule := "owner = @request.auth.id"
	coll := createTUSFileCollection(t, env, "tus_owned", &rule, nil)
	record, err := env.app.Collections().CreateRecord(context.Background(), coll, map[string]any{"owner": aliceID})
	if err != nil {
		t.Fatalf("create record: %v", err)
	}
	recordID := record["id"].(string)

	meta := map[string]string{"collection": "tus_owned", "recordId": recordID, "field": "doc", "filename": "f.png"}

	// Stranger's token → 403 at creation.
	resp := tusCreate(t, env, bobToken, 10, meta)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("stranger: expected 403, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Owner is allowed.
	resp = tusCreate(t, env, aliceToken, 10, meta)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("owner: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Locked collection (nil update rule): non-superuser denied, superuser allowed.
	locked := createTUSFileCollection(t, env, "tus_locked", nil, nil)
	lockedRec, err := env.app.Collections().CreateRecord(context.Background(), locked, map[string]any{"owner": aliceID})
	if err != nil {
		t.Fatalf("create locked record: %v", err)
	}
	lockedMeta := map[string]string{"collection": "tus_locked", "recordId": lockedRec["id"].(string), "field": "doc", "filename": "f.png"}

	resp = tusCreate(t, env, aliceToken, 10, lockedMeta)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("locked non-superuser: expected 403, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = tusCreate(t, env, "", 10, lockedMeta)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("locked anonymous: expected 403, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = tusCreate(t, env, adminToken, 10, lockedMeta)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("locked superuser: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestTUS_MimeRejectedAtCompletion(t *testing.T) {
	env := newIntegrationEnv(t)
	users := env.createAuthCollection(t, "tus_users3")
	aliceID := createTUSUser(t, env, users, "alice3@example.com")
	aliceToken := env.recordToken(t, "tus_users3", "alice3@example.com")

	rule := "owner = @request.auth.id"
	coll := createTUSFileCollection(t, env, "tus_pdfs", &rule, map[string]any{
		"mime_types": []any{"application/pdf"},
	})
	record, err := env.app.Collections().CreateRecord(context.Background(), coll, map[string]any{"owner": aliceID})
	if err != nil {
		t.Fatalf("create record: %v", err)
	}
	recordID := record["id"].(string)

	// PNG bytes disguised as a .pdf: the actual bytes are sniffed, so the
	// upload must be rejected at completion.
	content := tinyPNG
	resp := tusCreate(t, env, aliceToken, len(content), map[string]string{
		"collection": "tus_pdfs", "recordId": recordID, "field": "doc", "filename": "report.pdf",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	resp.Body.Close()

	final := tusPatch(t, location, 0, content, aliceToken)
	if final.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("completion: expected 415, got %d", final.StatusCode)
	}
	final.Body.Close()

	// Record untouched.
	after, err := env.app.Collections().GetRecord(context.Background(), coll, recordID)
	if err != nil {
		t.Fatalf("get record: %v", err)
	}
	if got := fmt.Sprint(after["doc"]); got != "" && got != "<nil>" {
		t.Fatalf("record doc field should be empty, got %q", got)
	}

	// No file landed in record storage.
	files, _ := env.app.Storage().ListFiles(context.Background(), "tus_pdfs/"+recordID+"/")
	if len(files) != 0 {
		t.Fatalf("expected no stored files, found %d", len(files))
	}

	// Chunks cleaned even on rejection.
	if n := tusChunkCount(t, env); n != 0 {
		t.Fatalf("expected empty chunk dir after rejection, found %d entries", n)
	}
}

func TestTUS_TerminationIdentity(t *testing.T) {
	env := newIntegrationEnv(t)
	users := env.createAuthCollection(t, "tus_users4")
	aliceID := createTUSUser(t, env, users, "alice4@example.com")
	createTUSUser(t, env, users, "bob4@example.com")
	aliceToken := env.recordToken(t, "tus_users4", "alice4@example.com")
	bobToken := env.recordToken(t, "tus_users4", "bob4@example.com")

	rule := "owner = @request.auth.id"
	coll := createTUSFileCollection(t, env, "tus_term", &rule, nil)
	record, err := env.app.Collections().CreateRecord(context.Background(), coll, map[string]any{"owner": aliceID})
	if err != nil {
		t.Fatalf("create record: %v", err)
	}

	resp := tusCreate(t, env, aliceToken, 100, map[string]string{
		"collection": "tus_term", "recordId": record["id"].(string), "field": "doc", "filename": "f.bin",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	resp.Body.Close()

	del := func(token string) int {
		req, err := http.NewRequest(http.MethodDelete, location, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Tus-Resumable", "1.0.0")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("delete: %v", err)
		}
		defer r.Body.Close()
		return r.StatusCode
	}

	// Different identity and anonymous → denied.
	if code := del(bobToken); code != http.StatusForbidden {
		t.Fatalf("bob delete: expected 403, got %d", code)
	}
	if code := del(""); code != http.StatusForbidden {
		t.Fatalf("anonymous delete: expected 403, got %d", code)
	}
	// Creator → allowed.
	if code := del(aliceToken); code != http.StatusNoContent {
		t.Fatalf("alice delete: expected 204, got %d", code)
	}
}

func TestTUS_MaxSizeFromFieldOptions(t *testing.T) {
	env := newIntegrationEnv(t)
	users := env.createAuthCollection(t, "tus_users5")
	aliceID := createTUSUser(t, env, users, "alice5@example.com")
	aliceToken := env.recordToken(t, "tus_users5", "alice5@example.com")

	rule := "owner = @request.auth.id"
	coll := createTUSFileCollection(t, env, "tus_small", &rule, map[string]any{"max_size": float64(100)})
	record, err := env.app.Collections().CreateRecord(context.Background(), coll, map[string]any{"owner": aliceID})
	if err != nil {
		t.Fatalf("create record: %v", err)
	}

	resp := tusCreate(t, env, aliceToken, 200, map[string]string{
		"collection": "tus_small", "recordId": record["id"].(string), "field": "doc", "filename": "big.bin",
	})
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize create: expected 413, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Within the limit it is accepted.
	resp = tusCreate(t, env, aliceToken, 50, map[string]string{
		"collection": "tus_small", "recordId": record["id"].(string), "field": "doc", "filename": "ok.bin",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("in-limit create: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestTUS_MissingMetadataRejected(t *testing.T) {
	env := newIntegrationEnv(t)
	resp := tusCreate(t, env, "", 10, map[string]string{"collection": "whatever"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}
