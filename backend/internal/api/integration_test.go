package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gresbase/gresbase/internal/app"
	"github.com/gresbase/gresbase/internal/collection"
	"github.com/gresbase/gresbase/internal/config"
	"github.com/gresbase/gresbase/internal/events"
	appplugin "github.com/gresbase/gresbase/internal/plugin"
	"github.com/gresbase/gresbase/internal/realtime"
	appsettings "github.com/gresbase/gresbase/internal/settings"
	"github.com/gresbase/gresbase/internal/testutil"
)

type integrationEnv struct {
	app    *app.App
	server *Server
	http   *httptest.Server
}

func newBootstrappedEnv(t *testing.T) *integrationEnv {
	t.Helper()
	pg := testutil.StartPostgres(t)

	cfg := config.DefaultConfig()
	cfg.DatabaseURL = pg.ConnString
	cfg.JWTSecret = "integration-test-secret"
	cfg.DataDir = t.TempDir()
	cfg.StorageLocal = filepath.Join(cfg.DataDir, "storage")
	// Isolate dev-mode automigrate output per test server: the default
	// ./gb_migrations would accumulate snapshot files in the package dir and
	// replay them into every later test's fresh database.
	cfg.MigrationsDir = filepath.Join(cfg.DataDir, "gb_migrations")
	cfg.DevMode = true
	cfg.LogLevel = "error"

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

	t.Cleanup(func() {
		a.Shutdown()
	})

	return &integrationEnv{app: a, server: s}
}

func newIntegrationEnv(t *testing.T) *integrationEnv {
	env := newBootstrappedEnv(t)
	env.http = httptest.NewServer(env.server)
	t.Cleanup(func() {
		env.http.Close()
	})
	return env
}

func (e *integrationEnv) createAdminToken(t *testing.T) string {
	t.Helper()
	admin, err := e.app.Auth().CreateAdmin(context.Background(), "admin@example.com", "password123", "super_admin", "default")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	token, _, err := e.app.Auth().GenerateTokens(admin.ID, admin.Email, admin.Role, admin.TenantID)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return token
}

func (e *integrationEnv) createCollection(t *testing.T, name string) *collection.Collection {
	t.Helper()
	public := ""
	coll := &collection.Collection{
		TenantID: "default",
		Name:     name,
		Type:     collection.TypeBase,
		Schema: []collection.SchemaField{
			{Name: "title", Type: collection.FieldText, Required: true},
		},
		ListRule:   &public,
		ViewRule:   &public,
		CreateRule: &public,
		UpdateRule: &public,
		DeleteRule: &public,
	}
	if err := e.app.Collections().CreateCollection(context.Background(), coll); err != nil {
		t.Fatalf("create collection: %v", err)
	}
	return coll
}

func (e *integrationEnv) createAuthCollection(t *testing.T, name string) *collection.Collection {
	t.Helper()
	coll := &collection.Collection{
		TenantID: "default",
		Name:     name,
		Type:     collection.TypeAuth,
		Schema: []collection.SchemaField{
			{Name: "email", Type: collection.FieldEmail, Required: true, Unique: true},
			{Name: "password", Type: collection.FieldPassword, Required: true},
		},
	}
	if err := e.app.Collections().CreateCollection(context.Background(), coll); err != nil {
		t.Fatalf("create auth collection: %v", err)
	}
	return coll
}

func doJSONRequest(t *testing.T, method, url string, body any, headers map[string]string) *http.Response {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func doMultipartRequest(t *testing.T, method, url string, fields map[string]string, fileField, fileName string, fileContent []byte, headers map[string]string) *http.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			t.Fatalf("write field: %v", err)
		}
	}
	part, err := writer.CreateFormFile(fileField, fileName)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(fileContent); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req, err := http.NewRequest(method, url, &body)
	if err != nil {
		t.Fatalf("new multipart request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do multipart request: %v", err)
	}
	return resp
}

func readJSONBody(t *testing.T, resp *http.Response, target any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

type mockRealtimeClient struct {
	id          string
	connectedAt time.Time
	lastSeen    time.Time
	subs        map[string]*realtime.Subscription
	store       map[string]any
	auth        *realtime.AuthInfo
	messages    chan *realtime.RealtimeMessage
	closed      bool
	mu          sync.RWMutex
}

func newMockRealtimeClient(id string) *mockRealtimeClient {
	now := time.Now()
	return &mockRealtimeClient{
		id:          id,
		connectedAt: now,
		lastSeen:    now,
		subs:        map[string]*realtime.Subscription{},
		store:       map[string]any{},
		messages:    make(chan *realtime.RealtimeMessage, 32),
	}
}

func (c *mockRealtimeClient) ID() string             { return c.id }
func (c *mockRealtimeClient) Transport() string      { return "test" }
func (c *mockRealtimeClient) ConnectedAt() time.Time { return c.connectedAt }
func (c *mockRealtimeClient) LastSeen() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastSeen
}
func (c *mockRealtimeClient) Touch() { c.mu.Lock(); c.lastSeen = time.Now(); c.mu.Unlock() }
func (c *mockRealtimeClient) AuthRecord() *realtime.AuthInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.auth
}
func (c *mockRealtimeClient) SetAuthRecord(auth *realtime.AuthInfo) {
	c.mu.Lock()
	c.auth = auth
	c.mu.Unlock()
}
func (c *mockRealtimeClient) IsClosed() bool { c.mu.RLock(); defer c.mu.RUnlock(); return c.closed }
func (c *mockRealtimeClient) Subscriptions() map[string]*realtime.Subscription {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := map[string]*realtime.Subscription{}
	for k, v := range c.subs {
		out[k] = v
	}
	return out
}
func (c *mockRealtimeClient) Subscribe(topic string, sub *realtime.Subscription) {
	c.mu.Lock()
	c.subs[topic] = sub
	c.lastSeen = time.Now()
	c.mu.Unlock()
}
func (c *mockRealtimeClient) Unsubscribe(topic string) {
	c.mu.Lock()
	delete(c.subs, topic)
	c.mu.Unlock()
}
func (c *mockRealtimeClient) UnsubscribeAll() {
	c.mu.Lock()
	c.subs = map[string]*realtime.Subscription{}
	c.mu.Unlock()
}
func (c *mockRealtimeClient) Set(key string, value any) {
	c.mu.Lock()
	if value == nil {
		delete(c.store, key)
	} else {
		c.store[key] = value
	}
	c.mu.Unlock()
}
func (c *mockRealtimeClient) Get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.store[key]
	return v, ok
}
func (c *mockRealtimeClient) Unset(key string)                         { c.mu.Lock(); delete(c.store, key); c.mu.Unlock() }
func (c *mockRealtimeClient) Send(msg *realtime.RealtimeMessage) error { c.messages <- msg; return nil }
func (c *mockRealtimeClient) SendRaw(data []byte) error {
	var msg realtime.RealtimeMessage
	_ = json.Unmarshal(data, &msg)
	c.messages <- &msg
	return nil
}
func (c *mockRealtimeClient) Close() { c.mu.Lock(); c.closed = true; c.mu.Unlock() }

func waitForRealtimeMessage(t *testing.T, client *mockRealtimeClient, timeout time.Duration, want string) (*realtime.RealtimeMessage, bool) {
	t.Helper()
	select {
	case msg := <-client.messages:
		if msg != nil && msg.Event == want {
			return msg, true
		}
		deadline := time.After(timeout)
		for {
			select {
			case next := <-client.messages:
				if next != nil && next.Event == want {
					return next, true
				}
			case <-deadline:
				return nil, false
			}
		}
	case <-time.After(timeout):
		return nil, false
	}
}

func drainRealtimeMessages(client *mockRealtimeClient) {
	for {
		select {
		case <-client.messages:
		default:
			return
		}
	}
}

func TestIntegration_AdminLoginAndRefresh(t *testing.T) {
	env := newIntegrationEnv(t)
	_, err := env.app.Auth().CreateAdmin(context.Background(), "admin@example.com", "password123", "super_admin", "default")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}

	loginResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/auth/login", map[string]any{
		"email":    "admin@example.com",
		"password": "password123",
	}, nil)
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", loginResp.StatusCode)
	}
	var loginBody map[string]any
	readJSONBody(t, loginResp, &loginBody)
	refreshToken, _ := loginBody["refreshToken"].(string)
	if refreshToken == "" {
		t.Fatal("expected refresh token")
	}

	refreshResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/auth/refresh", map[string]any{
		"refreshToken": refreshToken,
	}, nil)
	if refreshResp.StatusCode != http.StatusOK {
		t.Fatalf("refresh status = %d", refreshResp.StatusCode)
	}
	var refreshBody map[string]any
	readJSONBody(t, refreshResp, &refreshBody)
	if refreshBody["token"] == "" || refreshBody["refreshToken"] == "" {
		t.Fatal("expected refreshed tokens")
	}
}

func TestIntegration_CollectionsImportDeleteMissing(t *testing.T) {
	env := newIntegrationEnv(t)
	env.createCollection(t, "old_posts")
	token := env.createAdminToken(t)

	resp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/collections/import", map[string]any{
		"collections": []map[string]any{
			{
				"name":   "posts",
				"type":   "base",
				"schema": []map[string]any{{"name": "title", "type": "text", "required": true}},
			},
		},
		"deleteMissing": true,
	}, map[string]string{"Authorization": "Bearer " + token})
	if resp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, resp, &body)
		t.Fatalf("import status = %d body=%v", resp.StatusCode, body)
	}
	resp.Body.Close()

	if _, err := env.app.Collections().GetCollectionByName(context.Background(), "old_posts"); err == nil {
		t.Fatal("expected old_posts to be deleted by import")
	}
	if _, err := env.app.Collections().GetCollectionByName(context.Background(), "posts"); err != nil {
		t.Fatalf("expected posts collection to exist: %v", err)
	}
}

func TestIntegration_BatchRollbackAndRealtime(t *testing.T) {
	env := newIntegrationEnv(t)
	env.createCollection(t, "posts")

	client := newMockRealtimeClient("integration-client")
	env.app.Realtime().Register(client)
	defer env.app.Realtime().Unregister(client)
	time.Sleep(50 * time.Millisecond)
	env.app.Realtime().SubscribeClient(client, "posts/*", &realtime.Subscription{Topic: "posts/*"})
	drainRealtimeMessages(client)

	batchResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/batch?transactional=true", BatchPayload{
		Requests: []BatchRequest{
			{Method: http.MethodPost, URL: "/api/v1/records/posts/", Headers: map[string]string{"Content-Type": "application/json"}, Body: json.RawMessage(`{"title":"ok"}`)},
			{Method: http.MethodPost, URL: "/api/v1/records/posts/", Headers: map[string]string{"Content-Type": "application/json"}, Body: json.RawMessage(`{}`)},
		},
	}, nil)
	if batchResp.StatusCode != http.StatusOK {
		t.Fatalf("batch status = %d", batchResp.StatusCode)
	}
	batchResp.Body.Close()

	if _, ok := waitForRealtimeMessage(t, client, 400*time.Millisecond, "record:create"); ok {
		t.Fatal("did not expect record:create event after transactional rollback")
	}

	coll, err := env.app.Collections().GetCollectionByName(context.Background(), "posts")
	if err != nil {
		t.Fatalf("get collection: %v", err)
	}
	records, total, err := env.app.Collections().ListRecords(context.Background(), coll, "", "", 1, 20)
	if err != nil {
		t.Fatalf("list records: %v", err)
	}
	if total != 0 || len(records) != 0 {
		t.Fatalf("expected rollback to leave zero records, total=%d len=%d", total, len(records))
	}

	createResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/records/posts/", map[string]any{"title": "committed"}, nil)
	if createResp.StatusCode != http.StatusCreated {
		var body map[string]any
		readJSONBody(t, createResp, &body)
		t.Fatalf("create status = %d body=%v", createResp.StatusCode, body)
	}
	createResp.Body.Close()

	msg, ok := waitForRealtimeMessage(t, client, 2*time.Second, "record:create")
	if !ok {
		t.Fatal("expected record:create event after committed create")
	}
	if msg.Event != "record:create" {
		t.Fatalf("unexpected realtime message: %#v", msg)
	}
}

func TestIntegration_RecordAuthPasswordAndRefresh(t *testing.T) {
	env := newIntegrationEnv(t)
	coll := env.createAuthCollection(t, "users")
	record, err := env.app.Collections().CreateRecord(context.Background(), coll, map[string]any{
		"email":    "user@example.com",
		"password": "password123",
	})
	if err != nil {
		t.Fatalf("create auth record: %v", err)
	}
	if storedPassword, _ := record["password"].(string); storedPassword == "password123" {
		t.Fatal("expected auth collection password to be hashed before storage")
	}

	actions := []string{}
	providers := []string{}
	refreshProviders := []string{}
	env.app.OnRecordAuthRequest().BindFunc(func(e events.Event) error {
		event, ok := e.(*events.RecordAuthRequestEvent)
		if ok {
			actions = append(actions, event.Action)
		}
		return e.Next()
	})
	env.app.OnAuthLogin().BindFunc(func(e events.Event) error {
		event, ok := e.(*events.AuthEvent)
		if ok && strings.HasPrefix(event.Provider, "record:") {
			providers = append(providers, event.Provider)
		}
		return e.Next()
	})
	env.app.OnAuthRefresh().BindFunc(func(e events.Event) error {
		event, ok := e.(*events.AuthEvent)
		if ok && strings.HasPrefix(event.Provider, "record:") {
			refreshProviders = append(refreshProviders, event.Provider)
		}
		return e.Next()
	})

	loginResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/collections/users/auth/auth-with-password", map[string]any{
		"identity": "user@example.com",
		"password": "password123",
	}, nil)
	if loginResp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, loginResp, &body)
		t.Fatalf("record auth login status = %d body=%v", loginResp.StatusCode, body)
	}
	var loginBody map[string]any
	readJSONBody(t, loginResp, &loginBody)
	refreshToken, _ := loginBody["refreshToken"].(string)
	if refreshToken == "" {
		t.Fatalf("expected record refresh token, got body=%v", loginBody)
	}

	refreshResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/collections/users/auth/auth-refresh", map[string]any{
		"refreshToken": refreshToken,
	}, nil)
	if refreshResp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, refreshResp, &body)
		t.Fatalf("record auth refresh status = %d body=%v", refreshResp.StatusCode, body)
	}
	var refreshBody map[string]any
	readJSONBody(t, refreshResp, &refreshBody)
	if refreshBody["token"] == "" || refreshBody["refreshToken"] == "" {
		t.Fatalf("expected refreshed record tokens, got %v", refreshBody)
	}

	if len(actions) < 2 || actions[0] != "password" || actions[1] != "refresh" {
		t.Fatalf("expected record auth request actions [password refresh], got %v", actions)
	}
	if len(providers) == 0 || providers[0] != "record:password" {
		t.Fatalf("expected record login provider to be triggered, got %v", providers)
	}
	if len(refreshProviders) == 0 || refreshProviders[0] != "record:refresh" {
		t.Fatalf("expected record refresh provider to be triggered, got %v", refreshProviders)
	}
}

func TestIntegration_AdminPasswordResetVerificationAndEmailChange(t *testing.T) {
	env := newIntegrationEnv(t)
	admin, err := env.app.Auth().CreateAdmin(context.Background(), "admin@example.com", "password123", "super_admin", "default")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}

	resetToken, err := env.app.Auth().CreatePasswordResetToken(context.Background(), admin.Email)
	if err != nil {
		t.Fatalf("create reset token: %v", err)
	}
	resetResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/auth/confirm-password-reset", map[string]any{
		"token":    resetToken,
		"password": "newpassword123",
	}, nil)
	if resetResp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, resetResp, &body)
		t.Fatalf("admin reset confirm status = %d body=%v", resetResp.StatusCode, body)
	}
	resetResp.Body.Close()

	loginResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/auth/login", map[string]any{
		"email":    admin.Email,
		"password": "newpassword123",
	}, nil)
	if loginResp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, loginResp, &body)
		t.Fatalf("admin login after reset status = %d body=%v", loginResp.StatusCode, body)
	}
	loginResp.Body.Close()

	verificationToken, err := env.app.Auth().CreateVerificationToken(context.Background(), admin.Email)
	if err != nil {
		t.Fatalf("create verification token: %v", err)
	}
	verificationResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/auth/confirm-verification", map[string]any{
		"token": verificationToken,
	}, nil)
	if verificationResp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, verificationResp, &body)
		t.Fatalf("admin verification confirm status = %d body=%v", verificationResp.StatusCode, body)
	}
	verificationResp.Body.Close()
	if !env.app.Verification().IsVerified(context.Background(), admin.ID) {
		t.Fatal("expected admin to be marked verified")
	}

	emailChangeToken, err := env.app.Auth().CreateEmailChangeToken(context.Background(), admin.ID, "admin+new@example.com")
	if err != nil {
		t.Fatalf("create email change token: %v", err)
	}
	emailChangeResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/auth/confirm-email-change", map[string]any{
		"token": emailChangeToken,
	}, nil)
	if emailChangeResp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, emailChangeResp, &body)
		t.Fatalf("admin email change confirm status = %d body=%v", emailChangeResp.StatusCode, body)
	}
	emailChangeResp.Body.Close()

	updatedAdmin, err := env.app.Auth().FindAdminByID(context.Background(), admin.ID)
	if err != nil {
		t.Fatalf("find updated admin: %v", err)
	}
	if updatedAdmin.Email != "admin+new@example.com" {
		t.Fatalf("expected updated admin email, got %q", updatedAdmin.Email)
	}
}

func TestIntegration_RecordAuthResetVerificationAndEmailChange(t *testing.T) {
	env := newIntegrationEnv(t)
	coll := env.createAuthCollection(t, "users")
	record, err := env.app.Collections().CreateRecord(context.Background(), coll, map[string]any{
		"email":    "user@example.com",
		"password": "password123",
	})
	if err != nil {
		t.Fatalf("create auth record: %v", err)
	}
	recordID, _ := record["id"].(string)
	if recordID == "" {
		t.Fatal("expected record id")
	}

	resetToken, err := env.app.RecordAuth().RequestRecordPasswordReset(context.Background(), "users", "user@example.com")
	if err != nil {
		t.Fatalf("create record reset token: %v", err)
	}
	resetResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/collections/users/auth/confirm-password-reset", map[string]any{
		"token":    resetToken,
		"password": "newpassword123",
	}, nil)
	if resetResp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, resetResp, &body)
		t.Fatalf("record reset confirm status = %d body=%v", resetResp.StatusCode, body)
	}
	resetResp.Body.Close()

	loginResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/collections/users/auth/auth-with-password", map[string]any{
		"identity": "user@example.com",
		"password": "newpassword123",
	}, nil)
	if loginResp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, loginResp, &body)
		t.Fatalf("record login after reset status = %d body=%v", loginResp.StatusCode, body)
	}
	loginResp.Body.Close()

	verificationToken, err := env.app.RecordAuth().RequestRecordVerification(context.Background(), "users", recordID)
	if err != nil {
		t.Fatalf("create record verification token: %v", err)
	}
	verificationResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/collections/users/auth/confirm-verification", map[string]any{
		"token": verificationToken,
	}, nil)
	if verificationResp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, verificationResp, &body)
		t.Fatalf("record verification confirm status = %d body=%v", verificationResp.StatusCode, body)
	}
	verificationResp.Body.Close()

	updatedRecord, err := env.app.Collections().GetRecord(context.Background(), coll, recordID)
	if err != nil {
		t.Fatalf("get updated record: %v", err)
	}
	if verified, _ := updatedRecord["verified"].(bool); !verified {
		t.Fatal("expected record to be marked verified")
	}

	emailChangeToken, err := env.app.RecordAuth().RequestRecordEmailChange(context.Background(), "users", recordID, "user+new@example.com")
	if err != nil {
		t.Fatalf("create record email change token: %v", err)
	}
	emailChangeResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/collections/users/auth/confirm-email-change", map[string]any{
		"token": emailChangeToken,
	}, nil)
	if emailChangeResp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, emailChangeResp, &body)
		t.Fatalf("record email change confirm status = %d body=%v", emailChangeResp.StatusCode, body)
	}
	emailChangeResp.Body.Close()

	loginNewEmailResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/collections/users/auth/auth-with-password", map[string]any{
		"identity": "user+new@example.com",
		"password": "newpassword123",
	}, nil)
	if loginNewEmailResp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, loginNewEmailResp, &body)
		t.Fatalf("record login with new email status = %d body=%v", loginNewEmailResp.StatusCode, body)
	}
	loginNewEmailResp.Body.Close()
}

func TestIntegration_SettingsRequestHooksCanMutatePersistedSettings(t *testing.T) {
	env := newIntegrationEnv(t)
	token := env.createAdminToken(t)

	settingsListCalled := false
	settingsUpdateCalled := false
	env.app.OnSettingsListRequest().BindFunc(func(e events.Event) error {
		settingsListCalled = true
		return e.Next()
	})
	env.app.OnSettingsUpdateRequest().BindFunc(func(e events.Event) error {
		settingsUpdateCalled = true
		event, ok := e.(*events.SettingsUpdateRequestEvent)
		if ok {
			if s, ok := event.Settings.(*appsettings.Settings); ok {
				s.AppName = "Hooked App"
			}
		}
		return e.Next()
	})

	updateResp := doJSONRequest(t, http.MethodPut, env.http.URL+"/api/v1/settings/", map[string]any{
		"app_name": "Client Value",
	}, map[string]string{"Authorization": "Bearer " + token})
	if updateResp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, updateResp, &body)
		t.Fatalf("settings update status = %d body=%v", updateResp.StatusCode, body)
	}
	updateResp.Body.Close()

	getResp := doJSONRequest(t, http.MethodGet, env.http.URL+"/api/v1/settings/", nil, map[string]string{"Authorization": "Bearer " + token})
	if getResp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, getResp, &body)
		t.Fatalf("settings get status = %d body=%v", getResp.StatusCode, body)
	}
	var current appsettings.Settings
	readJSONBody(t, getResp, &current)
	if current.AppName != "Hooked App" {
		t.Fatalf("expected settings hook to mutate persisted app name, got %q", current.AppName)
	}
	if !settingsUpdateCalled || !settingsListCalled {
		t.Fatalf("expected settings request hooks to fire, update=%v list=%v", settingsUpdateCalled, settingsListCalled)
	}
}

func TestIntegration_FileRequestHooks(t *testing.T) {
	env := newIntegrationEnv(t)
	token := env.createAdminToken(t)

	uploads := 0
	downloads := 0
	deletes := 0
	env.app.OnFileUploadRequest().BindFunc(func(e events.Event) error {
		uploads++
		return e.Next()
	})
	env.app.OnFileDownloadRequest().BindFunc(func(e events.Event) error {
		downloads++
		return e.Next()
	})
	env.app.OnFileDeleteRequest().BindFunc(func(e events.Event) error {
		deletes++
		return e.Next()
	})

	uploadResp := doMultipartRequest(t, http.MethodPost, env.http.URL+"/api/v1/files/upload?collection=docs&record=rec1", nil, "file", "hello.txt", []byte("hello world"), map[string]string{"Authorization": "Bearer " + token})
	if uploadResp.StatusCode != http.StatusCreated {
		var body map[string]any
		readJSONBody(t, uploadResp, &body)
		t.Fatalf("upload status = %d body=%v", uploadResp.StatusCode, body)
	}
	var uploadBody map[string]any
	readJSONBody(t, uploadResp, &uploadBody)
	path, _ := uploadBody["path"].(string)
	if path == "" {
		t.Fatalf("expected upload path, got %v", uploadBody)
	}
	segments := strings.Split(path, "/")
	filename := segments[len(segments)-1]

	downloadResp := doJSONRequest(t, http.MethodGet, env.http.URL+"/api/v1/files/docs/rec1/"+filename, nil, map[string]string{"Authorization": "Bearer " + token})
	if downloadResp.StatusCode != http.StatusOK {
		t.Fatalf("download status = %d", downloadResp.StatusCode)
	}
	downloadResp.Body.Close()

	deleteResp := doJSONRequest(t, http.MethodDelete, env.http.URL+"/api/v1/files/docs/rec1/"+filename, nil, map[string]string{"Authorization": "Bearer " + token})
	if deleteResp.StatusCode != http.StatusOK {
		var body map[string]any
		readJSONBody(t, deleteResp, &body)
		t.Fatalf("delete status = %d body=%v", deleteResp.StatusCode, body)
	}
	deleteResp.Body.Close()

	if uploads != 1 || downloads != 1 || deletes != 1 {
		t.Fatalf("expected file hooks upload/download/delete = 1/1/1, got %d/%d/%d", uploads, downloads, deletes)
	}
}

func TestIntegration_RegisterPluginAddsRouteAndMiddleware(t *testing.T) {
	env := newBootstrappedEnv(t)
	plugin := appplugin.New("test.plugin", "Test Plugin", "0.0.1").
		Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Plugin", "on")
				next.ServeHTTP(w, r)
			})
		}).
		HandleFunc("/plugin-ping", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	if err := env.app.RegisterPlugin(plugin); err != nil {
		t.Fatalf("register plugin: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- env.server.Start(addr)
	}()
	defer env.server.Shutdown()

	var resp *http.Response
	for i := 0; i < 40; i++ {
		resp, err = http.Get("http://" + addr + "/plugin-ping")
		if err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("get plugin route: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("plugin route status = %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Plugin") != "on" {
		t.Fatalf("expected plugin middleware header on plugin route, got %q", resp.Header.Get("X-Plugin"))
	}

	healthResp, err := http.Get("http://" + addr + "/api/v1/health")
	if err != nil {
		t.Fatalf("get health route: %v", err)
	}
	defer healthResp.Body.Close()
	if healthResp.Header.Get("X-Plugin") != "on" {
		t.Fatalf("expected plugin middleware header on core route, got %q", healthResp.Header.Get("X-Plugin"))
	}

	_ = env.server.Shutdown()
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("server start returned unexpected error: %v", err)
	}
}

func TestIntegration_DisablingPluginAfterStartDisablesRouteAndMiddleware(t *testing.T) {
	env := newBootstrappedEnv(t)
	plugin := appplugin.New("toggle.plugin", "Toggle Plugin", "0.0.1").
		Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Toggle-Plugin", "on")
				next.ServeHTTP(w, r)
			})
		}).
		HandleFunc("/toggle-ping", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
	if err := env.app.RegisterPlugin(plugin); err != nil {
		t.Fatalf("register plugin: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- env.server.Start(addr)
	}()
	defer env.server.Shutdown()

	var resp *http.Response
	for i := 0; i < 40; i++ {
		resp, err = http.Get("http://" + addr + "/toggle-ping")
		if err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("get toggle route: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent || resp.Header.Get("X-Toggle-Plugin") != "on" {
		t.Fatalf("expected enabled plugin route+middleware, status=%d header=%q", resp.StatusCode, resp.Header.Get("X-Toggle-Plugin"))
	}
	resp.Body.Close()

	if err := env.app.Plugins().Disable(plugin.ID); err != nil {
		t.Fatalf("disable plugin: %v", err)
	}

	resp, err = http.Get("http://" + addr + "/toggle-ping")
	if err != nil {
		t.Fatalf("get disabled toggle route: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected disabled plugin route to 404, got %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Toggle-Plugin") != "" {
		t.Fatalf("expected disabled plugin middleware header to be absent, got %q", resp.Header.Get("X-Toggle-Plugin"))
	}

	healthResp, err := http.Get("http://" + addr + "/api/v1/health")
	if err != nil {
		t.Fatalf("get health after disable: %v", err)
	}
	defer healthResp.Body.Close()
	if healthResp.Header.Get("X-Toggle-Plugin") != "" {
		t.Fatalf("expected disabled plugin middleware to stop affecting core routes, got %q", healthResp.Header.Get("X-Toggle-Plugin"))
	}

	_ = env.server.Shutdown()
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("server start returned unexpected error: %v", err)
	}
}

func TestIntegration_AuditLogsPersistAfterCommitOperations(t *testing.T) {
	env := newIntegrationEnv(t)
	token := env.createAdminToken(t)

	createCollectionResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/collections/", map[string]any{
		"name":   "audited_posts",
		"type":   "base",
		"schema": []map[string]any{{"name": "title", "type": "text", "required": true}},
	}, map[string]string{"Authorization": "Bearer " + token})
	if createCollectionResp.StatusCode != http.StatusCreated {
		var body map[string]any
		readJSONBody(t, createCollectionResp, &body)
		t.Fatalf("collection create status = %d body=%v", createCollectionResp.StatusCode, body)
	}
	createCollectionResp.Body.Close()

	createRecordResp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/records/audited_posts/", map[string]any{
		"title": "hello",
	}, map[string]string{"Authorization": "Bearer " + token})
	if createRecordResp.StatusCode != http.StatusCreated {
		var body map[string]any
		readJSONBody(t, createRecordResp, &body)
		t.Fatalf("record create status = %d body=%v", createRecordResp.StatusCode, body)
	}
	createRecordResp.Body.Close()

	var collectionAuditCount int
	if err := env.app.DB().QueryRow(context.Background(), "SELECT COUNT(*) FROM _audit_logs WHERE action = $1", "collection.create").Scan(&collectionAuditCount); err != nil {
		t.Fatalf("count collection audit logs: %v", err)
	}
	if collectionAuditCount == 0 {
		t.Fatal("expected collection.create audit log to persist after commit")
	}

	var recordAuditCount int
	if err := env.app.DB().QueryRow(context.Background(), "SELECT COUNT(*) FROM _audit_logs WHERE action = $1", "record.create").Scan(&recordAuditCount); err != nil {
		t.Fatalf("count record audit logs: %v", err)
	}
	if recordAuditCount == 0 {
		t.Fatal("expected record.create audit log to persist after commit")
	}
}

func TestIntegration_RealtimeRequestHookCanBlockConnect(t *testing.T) {
	env := newIntegrationEnv(t)
	called := false
	env.app.OnRealtimeRequest().BindFunc(func(e events.Event) error {
		called = true
		return errors.New("blocked realtime")
	})

	req, err := http.NewRequest(http.MethodGet, env.http.URL+"/api/v1/realtime", nil)
	if err != nil {
		t.Fatalf("new realtime request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do realtime request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected blocked realtime status 500, got %d", resp.StatusCode)
	}
	if !called {
		t.Fatal("expected realtime request hook to fire")
	}
}

func TestIntegration_CollectionRequestHookCanMutateCreate(t *testing.T) {
	env := newIntegrationEnv(t)
	token := env.createAdminToken(t)

	env.app.OnCollectionRequest().BindFunc(func(e events.Event) error {
		event, ok := e.(*events.CollectionRequestEvent)
		if ok && event.Action == "create" {
			event.Data["name"] = "hooked_posts"
		}
		return e.Next()
	})

	resp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/collections/", map[string]any{
		"name":   "posts",
		"type":   "base",
		"schema": []map[string]any{{"name": "title", "type": "text", "required": true}},
	}, map[string]string{"Authorization": "Bearer " + token})
	if resp.StatusCode != http.StatusCreated {
		var body map[string]any
		readJSONBody(t, resp, &body)
		t.Fatalf("collection create status = %d body=%v", resp.StatusCode, body)
	}
	var body map[string]any
	readJSONBody(t, resp, &body)
	if body["name"] != "hooked_posts" {
		t.Fatalf("expected collection hook to rename collection, got %v", body)
	}
	if _, err := env.app.Collections().GetCollectionByName(context.Background(), "hooked_posts"); err != nil {
		t.Fatalf("expected hooked_posts collection to exist: %v", err)
	}
}

func TestIntegration_AdminUserRequestHookCanMutateCreate(t *testing.T) {
	env := newIntegrationEnv(t)
	token := env.createAdminToken(t)

	env.app.OnAdminUserRequest().BindFunc(func(e events.Event) error {
		event, ok := e.(*events.AdminUserRequestEvent)
		if ok && event.Action == "create" {
			event.Data["role"] = "super_admin"
		}
		return e.Next()
	})

	resp := doJSONRequest(t, http.MethodPost, env.http.URL+"/api/v1/admin/users", map[string]any{
		"email":    "new-admin@example.com",
		"password": "password123",
		"role":     "admin",
	}, map[string]string{"Authorization": "Bearer " + token})
	if resp.StatusCode != http.StatusCreated {
		var body map[string]any
		readJSONBody(t, resp, &body)
		t.Fatalf("admin create status = %d body=%v", resp.StatusCode, body)
	}
	var body map[string]any
	readJSONBody(t, resp, &body)
	if body["role"] != "super_admin" {
		t.Fatalf("expected admin hook to elevate role, got %v", body)
	}
}

func TestIntegration_OnServeCanRegisterRoute(t *testing.T) {
	env := newBootstrappedEnv(t)
	env.app.OnServe().BindFunc(func(e events.Event) error {
		serveEvent, ok := e.(*events.ServeEvent)
		if !ok {
			return e.Next()
		}
		serveEvent.Router.HandleFunc("/plugin-ping", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
		return e.Next()
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- env.server.Start(addr)
	}()
	defer env.server.Shutdown()

	var resp *http.Response
	for i := 0; i < 40; i++ {
		resp, err = http.Get("http://" + addr + "/plugin-ping")
		if err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("get plugin route: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("plugin route status = %d", resp.StatusCode)
	}

	_ = env.server.Shutdown()
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("server start returned unexpected error: %v", err)
	}
}
