package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gresbase/gresbase/internal/auth"
	"github.com/gresbase/gresbase/internal/collection"
)

func createAdminWithRole(t *testing.T, env *integrationEnv, email, role string) (string, string) {
	t.Helper()
	admin, err := env.app.Auth().CreateAdmin(context.Background(), email, "password123", role, "default")
	if err != nil {
		t.Fatalf("create admin (%s): %v", role, err)
	}
	token, _, err := env.app.Auth().GenerateTokens(admin.ID, admin.Email, admin.Role, admin.TenantID)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return admin.ID, token
}

func registerStubOAuthProvider(t *testing.T, env *integrationEnv, providerName string) *httptest.Server {
	t.Helper()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"stub-access","token_type":"Bearer"}`))
		case "/userinfo":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"sub":"stub-user-1","email":"oauth@example.com","name":"OAuth User","picture":"https://example.com/avatar.png"}`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	env.app.OAuth().RegisterProvider(providerName, &auth.OAuthProvider{
		Name:         providerName,
		DisplayName:  "Stub OAuth",
		ClientID:     "stub-client",
		ClientSecret: "stub-secret",
		AuthURL:      stub.URL + "/authorize",
		TokenURL:     stub.URL + "/token",
		UserInfoURL:  stub.URL + "/userinfo",
		Scopes:       []string{"openid", "email", "profile"},
		Enabled:      true,
	})
	t.Cleanup(stub.Close)
	return stub
}

func TestAPIKeyAuthAndViewerRBAC(t *testing.T) {
	env := newIntegrationEnv(t)
	adminID, _ := createAdminWithRole(t, env, "viewer@example.com", "viewer")

	key, _, err := env.app.Auth().GenerateAPIKey(context.Background(), adminID, "viewer-key", nil)
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}

	listResp := doJSONRequest(t, http.MethodGet, env.http.URL+"/api/v1/collections", nil, map[string]string{
		"Authorization": key,
	})
	if listResp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, listResp, &body)
		t.Fatalf("expected API key auth to access collections list, got %d body=%v", listResp.StatusCode, body)
	}
	listResp.Body.Close()

	createResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/collections", map[string]any{
		"name":   "forbidden_posts",
		"type":   "base",
		"schema": []map[string]any{{"name": "title", "type": "text", "required": true}},
	}, map[string]string{"Authorization": key})
	if createResp.StatusCode != http.StatusForbidden {
		var body map[string]any
		readJSONBody(t, createResp, &body)
		t.Fatalf("expected viewer API key to be forbidden from collection creation, got %d body=%v", createResp.StatusCode, body)
	}
	createResp.Body.Close()
}

func TestEditorRBACAndPocketBaseAlias(t *testing.T) {
	env := newIntegrationEnv(t)
	_, editorToken := createAdminWithRole(t, env, "editor@example.com", "editor")

	createCollectionResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/collections", map[string]any{
		"name":        "editor_posts",
		"type":        "base",
		"schema":      []map[string]any{{"name": "title", "type": "text", "required": true}},
		"create_rule": "",
		"view_rule":   "",
	}, map[string]string{"Authorization": "Bearer " + editorToken})
	if createCollectionResp.StatusCode != http.StatusCreated {
		var body map[string]any
		readJSONBody(t, createCollectionResp, &body)
		t.Fatalf("expected editor to create collection, got %d body=%v", createCollectionResp.StatusCode, body)
	}
	createCollectionResp.Body.Close()

	createAdminResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/admin/users", map[string]any{
		"email":    "new-admin@example.com",
		"password": "password123",
		"role":     "admin",
	}, map[string]string{"Authorization": "Bearer " + editorToken})
	if createAdminResp.StatusCode != http.StatusForbidden {
		var body map[string]any
		readJSONBody(t, createAdminResp, &body)
		t.Fatalf("expected editor to be forbidden from creating admins, got %d body=%v", createAdminResp.StatusCode, body)
	}
	createAdminResp.Body.Close()

	aliasCreateResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/collections/editor_posts/records/", map[string]any{
		"title": "Alias record",
	}, nil)
	if aliasCreateResp.StatusCode != http.StatusCreated {
		var body map[string]any
		readJSONBody(t, aliasCreateResp, &body)
		t.Fatalf("expected PocketBase alias record create to work, got %d body=%v", aliasCreateResp.StatusCode, body)
	}
	var created map[string]any
	readJSONBody(t, aliasCreateResp, &created)
	recordID := created["id"].(string)

	aliasViewResp := doJSONRequest(t, http.MethodGet, env.http.URL+"/api/collections/editor_posts/records/"+recordID, nil, nil)
	if aliasViewResp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, aliasViewResp, &body)
		t.Fatalf("expected PocketBase alias record view to work, got %d body=%v", aliasViewResp.StatusCode, body)
	}
	aliasViewResp.Body.Close()
}

func TestRecordUpdateRemovesOrphanedFiles(t *testing.T) {
	env := newIntegrationEnv(t)
	adminToken := env.createAdminToken(t)

	publicView := ""
	coll := &collection.Collection{
		TenantID: "default",
		Name:     "docs",
		Type:     collection.TypeBase,
		Schema: []collection.SchemaField{
			{Name: "title", Type: collection.FieldText, Required: true},
			{Name: "attachment", Type: collection.FieldFile},
		},
		ViewRule: &publicView,
	}
	if err := env.app.Collections().CreateCollection(context.Background(), coll); err != nil {
		t.Fatalf("create docs collection: %v", err)
	}

	record, err := env.app.Collections().CreateRecord(context.Background(), coll, map[string]any{"title": "Document"})
	if err != nil {
		t.Fatalf("create record: %v", err)
	}
	recordID, _ := record["id"].(string)

	uploadResp := doMultipartRequest(t, http.MethodPost, env.http.URL+"/api/v1/files/upload?collection=docs&record="+recordID, nil, "file", "hello.txt", []byte("hello world"), map[string]string{
		"Authorization": "Bearer " + adminToken,
	})
	if uploadResp.StatusCode != http.StatusCreated {
		var body map[string]any
		readJSONBody(t, uploadResp, &body)
		t.Fatalf("upload file status=%d body=%v", uploadResp.StatusCode, body)
	}
	var uploaded map[string]any
	readJSONBody(t, uploadResp, &uploaded)
	filePath, _ := uploaded["path"].(string)
	parts := strings.Split(filePath, "/")
	fileName := parts[len(parts)-1]

	attachResp := doJSONRequest(t, http.MethodPatch, env.http.URL+"/api/v1/records/docs/"+recordID, map[string]any{
		"attachment": fileName,
	}, map[string]string{"Authorization": "Bearer " + adminToken})
	if attachResp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, attachResp, &body)
		t.Fatalf("attach file status=%d body=%v", attachResp.StatusCode, body)
	}
	attachResp.Body.Close()

	fileResp := doJSONRequest(t, http.MethodGet, env.http.URL+"/api/v1/files/docs/"+recordID+"/"+fileName, nil, nil)
	if fileResp.StatusCode != http.StatusOK {
		fileResp.Body.Close()
		t.Fatalf("expected uploaded file to be downloadable, got %d", fileResp.StatusCode)
	}
	fileResp.Body.Close()

	clearResp := doJSONRequest(t, http.MethodPatch, env.http.URL+"/api/v1/records/docs/"+recordID, map[string]any{
		"attachment": "",
	}, map[string]string{"Authorization": "Bearer " + adminToken})
	if clearResp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, clearResp, &body)
		t.Fatalf("clear attachment status=%d body=%v", clearResp.StatusCode, body)
	}
	clearResp.Body.Close()

	deletedFileResp := doJSONRequest(t, http.MethodGet, env.http.URL+"/api/v1/files/docs/"+recordID+"/"+fileName, nil, nil)
	if deletedFileResp.StatusCode != http.StatusNotFound {
		deletedFileResp.Body.Close()
		t.Fatalf("expected orphaned file to be removed after record update, got %d", deletedFileResp.StatusCode)
	}
	deletedFileResp.Body.Close()
}

func TestScopedAPIKeyPermissions(t *testing.T) {
	env := newIntegrationEnv(t)
	adminID, _ := createAdminWithRole(t, env, "scoped-admin@example.com", "admin")

	coll := &collection.Collection{
		TenantID: "default",
		Name:     "scoped_posts",
		Type:     collection.TypeBase,
		Schema:   []collection.SchemaField{{Name: "title", Type: collection.FieldText, Required: true}},
	}
	if err := env.app.Collections().CreateCollection(context.Background(), coll); err != nil {
		t.Fatalf("create scoped_posts collection: %v", err)
	}
	if _, err := env.app.Collections().CreateRecord(context.Background(), coll, map[string]any{"title": "hello"}); err != nil {
		t.Fatalf("create scoped_posts record: %v", err)
	}

	key, _, err := env.app.Auth().GenerateAPIKey(context.Background(), adminID, "scoped-key", []string{"collections.read", "records.read"})
	if err != nil {
		t.Fatalf("generate scoped api key: %v", err)
	}

	collectionsResp := doJSONRequest(t, http.MethodGet, env.http.URL+"/api/v1/collections", nil, map[string]string{"Authorization": key})
	if collectionsResp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, collectionsResp, &body)
		t.Fatalf("expected scoped key to read collections, got %d body=%v", collectionsResp.StatusCode, body)
	}
	collectionsResp.Body.Close()

	recordsResp := doJSONRequest(t, http.MethodGet, env.http.URL+"/api/v1/records/scoped_posts", nil, map[string]string{"Authorization": key})
	if recordsResp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, recordsResp, &body)
		t.Fatalf("expected scoped key to read records, got %d body=%v", recordsResp.StatusCode, body)
	}
	recordsResp.Body.Close()

	forbiddenCollectionCreate := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/collections", map[string]any{
		"name":   "should_fail",
		"type":   "base",
		"schema": []map[string]any{{"name": "title", "type": "text", "required": true}},
	}, map[string]string{"Authorization": key})
	if forbiddenCollectionCreate.StatusCode != http.StatusForbidden {
		var body map[string]any
		readJSONBody(t, forbiddenCollectionCreate, &body)
		t.Fatalf("expected scoped key to be blocked from collection writes, got %d body=%v", forbiddenCollectionCreate.StatusCode, body)
	}
	forbiddenCollectionCreate.Body.Close()

	forbiddenRecordCreate := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/records/scoped_posts", map[string]any{
		"title": "nope",
	}, map[string]string{"Authorization": key})
	if forbiddenRecordCreate.StatusCode != http.StatusForbidden {
		var body map[string]any
		readJSONBody(t, forbiddenRecordCreate, &body)
		t.Fatalf("expected scoped key to be blocked from record writes, got %d body=%v", forbiddenRecordCreate.StatusCode, body)
	}
	forbiddenRecordCreate.Body.Close()

	forbiddenAPIKeysList := doJSONRequest(t, http.MethodGet, env.http.URL+"/api/v1/api-keys", nil, map[string]string{"Authorization": key})
	if forbiddenAPIKeysList.StatusCode != http.StatusForbidden {
		var body map[string]any
		readJSONBody(t, forbiddenAPIKeysList, &body)
		t.Fatalf("expected scoped key to be blocked from api key listing, got %d body=%v", forbiddenAPIKeysList.StatusCode, body)
	}
	forbiddenAPIKeysList.Body.Close()
}

func TestCollectionDeleteRemovesStoredFiles(t *testing.T) {
	env := newIntegrationEnv(t)
	adminToken := env.createAdminToken(t)

	publicView := ""
	coll := &collection.Collection{
		TenantID: "default",
		Name:     "delete_docs",
		Type:     collection.TypeBase,
		Schema: []collection.SchemaField{
			{Name: "title", Type: collection.FieldText, Required: true},
			{Name: "attachment", Type: collection.FieldFile},
		},
		ViewRule: &publicView,
	}
	if err := env.app.Collections().CreateCollection(context.Background(), coll); err != nil {
		t.Fatalf("create delete_docs collection: %v", err)
	}

	record, err := env.app.Collections().CreateRecord(context.Background(), coll, map[string]any{"title": "Delete me"})
	if err != nil {
		t.Fatalf("create delete_docs record: %v", err)
	}
	recordID, _ := record["id"].(string)

	uploadResp := doMultipartRequest(t, http.MethodPost, env.http.URL+"/api/v1/files/upload?collection=delete_docs&record="+recordID, nil, "file", "cleanup.txt", []byte("cleanup me"), map[string]string{
		"Authorization": "Bearer " + adminToken,
	})
	if uploadResp.StatusCode != http.StatusCreated {
		var body map[string]any
		readJSONBody(t, uploadResp, &body)
		t.Fatalf("upload cleanup file status=%d body=%v", uploadResp.StatusCode, body)
	}
	var uploaded map[string]any
	readJSONBody(t, uploadResp, &uploaded)
	filePath, _ := uploaded["path"].(string)
	parts := strings.Split(filePath, "/")
	fileName := parts[len(parts)-1]

	fileResp := doJSONRequest(t, http.MethodGet, env.http.URL+"/api/v1/files/delete_docs/"+recordID+"/"+fileName, nil, nil)
	if fileResp.StatusCode != http.StatusOK {
		fileResp.Body.Close()
		t.Fatalf("expected cleanup file to exist before collection delete, got %d", fileResp.StatusCode)
	}
	fileResp.Body.Close()

	deleteResp := doJSONRequest(t, http.MethodDelete, env.http.URL+"/api/v1/collections/"+coll.ID, nil, map[string]string{"Authorization": "Bearer " + adminToken})
	if deleteResp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, deleteResp, &body)
		t.Fatalf("delete collection status=%d body=%v", deleteResp.StatusCode, body)
	}
	deleteResp.Body.Close()

	deletedFileResp := doJSONRequest(t, http.MethodGet, env.http.URL+"/api/v1/files/delete_docs/"+recordID+"/"+fileName, nil, nil)
	if deletedFileResp.StatusCode != http.StatusNotFound {
		deletedFileResp.Body.Close()
		t.Fatalf("expected collection delete to remove stored files, got %d", deletedFileResp.StatusCode)
	}
	deletedFileResp.Body.Close()
}

func TestAPIKeyDeleteEnforcesOwnership(t *testing.T) {
	env := newIntegrationEnv(t)
	ownerID, _ := createAdminWithRole(t, env, "owner@example.com", "admin")
	_, otherAdminToken := createAdminWithRole(t, env, "other-admin@example.com", "admin")

	_, apiKey, err := env.app.Auth().GenerateAPIKey(context.Background(), ownerID, "owned-key", nil)
	if err != nil {
		t.Fatalf("generate owned api key: %v", err)
	}

	forbiddenDelete := doJSONRequest(t, http.MethodDelete, env.http.URL+"/api/v1/api-keys/"+apiKey.ID, nil, map[string]string{
		"Authorization": "Bearer " + otherAdminToken,
	})
	if forbiddenDelete.StatusCode != http.StatusForbidden {
		var body map[string]any
		readJSONBody(t, forbiddenDelete, &body)
		t.Fatalf("expected non-owner admin to be blocked from deleting another admin's key, got %d body=%v", forbiddenDelete.StatusCode, body)
	}
	forbiddenDelete.Body.Close()
}

func TestPocketBaseOAuth2Routes(t *testing.T) {
	env := newIntegrationEnv(t)
	registerStubOAuthProvider(t, env, "stuboauth")
	members := &collection.Collection{
		TenantID: "default",
		Name:     "members",
		Type:     collection.TypeAuth,
		Schema: []collection.SchemaField{
			{Name: "email", Type: collection.FieldEmail, Required: true, Unique: true},
			{Name: "password", Type: collection.FieldPassword, Required: false},
		},
	}
	if err := env.app.Collections().CreateCollection(context.Background(), members); err != nil {
		t.Fatalf("create members auth collection: %v", err)
	}

	methodsResp := doJSONRequest(t, http.MethodGet, env.http.URL+"/api/collections/members/auth-methods", nil, nil)
	if methodsResp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, methodsResp, &body)
		t.Fatalf("auth methods status=%d body=%v", methodsResp.StatusCode, body)
	}
	var methods map[string]any
	readJSONBody(t, methodsResp, &methods)
	oauthSection, _ := methods["oauth2"].(map[string]any)
	providers, _ := oauthSection["providers"].([]any)
	if len(providers) == 0 {
		t.Fatalf("expected oauth providers in auth methods response, got %v", methods)
	}

	var providerInfo map[string]any
	for _, item := range providers {
		candidate, _ := item.(map[string]any)
		if candidate["name"] == "stuboauth" {
			providerInfo = candidate
			break
		}
	}
	if providerInfo == nil {
		t.Fatalf("expected stuboauth provider in auth methods response, got %v", providers)
	}
	state, _ := providerInfo["state"].(string)
	codeVerifier, _ := providerInfo["codeVerifier"].(string)
	authURL, _ := providerInfo["authURL"].(string)
	redirectURL, _ := providerInfo["redirectURL"].(string)
	if state == "" || codeVerifier == "" || authURL == "" || redirectURL == "" {
		t.Fatalf("expected auth method provider to include live oauth fields, got %+v", providerInfo)
	}
	if !strings.Contains(authURL, "state=") {
		t.Fatalf("expected authURL to contain oauth state, got %s", authURL)
	}

	oauthResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/collections/members/auth-with-oauth2", map[string]any{
		"provider":     "stuboauth",
		"code":         "oauth-code",
		"state":        state,
		"codeVerifier": codeVerifier,
		"redirectURL":  redirectURL,
	}, nil)
	if oauthResp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, oauthResp, &body)
		t.Fatalf("auth-with-oauth2 status=%d body=%v", oauthResp.StatusCode, body)
	}
	var authResult map[string]any
	readJSONBody(t, oauthResp, &authResult)
	if strings.TrimSpace(authResult["token"].(string)) == "" {
		t.Fatalf("expected oauth auth result token, got %v", authResult)
	}
	if strings.TrimSpace(authResult["refreshToken"].(string)) == "" {
		t.Fatalf("expected oauth auth result refresh token, got %v", authResult)
	}
	record, _ := authResult["record"].(map[string]any)
	if strings.TrimSpace(record["id"].(string)) == "" {
		t.Fatalf("expected oauth auth result record id, got %v", authResult)
	}
}

func TestOAuth2MetaProvidersAndRedirectBridge(t *testing.T) {
	env := newIntegrationEnv(t)
	registerStubOAuthProvider(t, env, "stuboauth")

	metaResp := doJSONRequest(t, http.MethodGet, env.http.URL+"/api/collections/meta/oauth2-providers", nil, nil)
	if metaResp.StatusCode != http.StatusOK {
		var body any
		readJSONBody(t, metaResp, &body)
		t.Fatalf("oauth2 providers meta status=%d body=%v", metaResp.StatusCode, body)
	}
	var providers []map[string]any
	readJSONBody(t, metaResp, &providers)
	found := false
	for _, provider := range providers {
		if provider["name"] == "stuboauth" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected stuboauth in providers meta, got %v", providers)
	}

	bridgeResp := doJSONRequest(t, http.MethodGet, env.http.URL+"/api/oauth2-redirect?code=abc123&state=state456", nil, nil)
	if bridgeResp.StatusCode != http.StatusOK {
		bridgeResp.Body.Close()
		t.Fatalf("oauth2 redirect bridge status=%d", bridgeResp.StatusCode)
	}
	bridgeBody, _ := io.ReadAll(bridgeResp.Body)
	_ = bridgeResp.Body.Close()
	bodyText := string(bridgeBody)
	if !strings.Contains(bodyText, "gresbase:oauth2-redirect") || !strings.Contains(bodyText, `"code":"abc123"`) || !strings.Contains(bodyText, `"state":"state456"`) {
		t.Fatalf("unexpected oauth2 redirect bridge body: %s", bodyText)
	}
}
