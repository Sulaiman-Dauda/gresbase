package app

import (
	"net/http"
	"testing"

	"github.com/gresbase/gresbase/internal/config"
)

func TestNew(t *testing.T) {
	cfg := &config.Config{
		Addr:     ":8080",
		LogLevel: "info",
	}

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if app == nil {
		t.Fatal("expected non-nil App")
	}
	if app.cfg != cfg {
		t.Error("expected config to be stored")
	}
	if app.IsReady() {
		t.Error("expected not ready before bootstrap")
	}
	if app.IsBootstrapped() {
		t.Error("expected not bootstrapped before bootstrap")
	}

	// Check hooks are initialized
	if app.OnBootstrap() == nil {
		t.Error("expected OnBootstrap hook")
	}
	if app.OnTerminate() == nil {
		t.Error("expected OnTerminate hook")
	}
	if app.OnRecordCreate() == nil {
		t.Error("expected OnRecordCreate hook")
	}
	if app.OnRecordUpdate() == nil {
		t.Error("expected OnRecordUpdate hook")
	}
	if app.OnRecordDelete() == nil {
		t.Error("expected OnRecordDelete hook")
	}
	if app.OnAuthLogin() == nil {
		t.Error("expected OnAuthLogin hook")
	}
	if app.OnAuthRefresh() == nil {
		t.Error("expected OnAuthRefresh hook")
	}
	if app.OnRealtimeConn() == nil {
		t.Error("expected OnRealtimeConn hook")
	}
	if app.OnCollectionCreate() == nil {
		t.Error("expected OnCollectionCreate hook")
	}
	if app.OnCollectionUpdate() == nil {
		t.Error("expected OnCollectionUpdate hook")
	}
	if app.OnCollectionDelete() == nil {
		t.Error("expected OnCollectionDelete hook")
	}
}

func TestNew_DebugLog(t *testing.T) {
	cfg := &config.Config{
		Addr:     ":8080",
		LogLevel: "debug",
		DevMode:  true,
	}

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if app == nil {
		t.Fatal("expected non-nil App")
	}
}

func TestApp_AccessorsBeforeBootstrap(t *testing.T) {
	cfg := &config.Config{Addr: ":8080", LogLevel: "info"}
	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if app.DB() != nil {
		t.Error("expected nil DB before bootstrap")
	}
	if app.Auth() != nil {
		t.Error("expected nil Auth before bootstrap")
	}
	if app.Collections() != nil {
		t.Error("expected nil Collections before bootstrap")
	}
	if app.Storage() != nil {
		t.Error("expected nil Storage before bootstrap")
	}
	if app.ACME() != nil {
		t.Error("expected nil ACME before bootstrap")
	}
	if app.Realtime() != nil {
		t.Error("expected nil Realtime before bootstrap")
	}
	if app.Mailer() != nil {
		t.Error("expected nil Mailer before bootstrap")
	}
	if app.Jobs() != nil {
		t.Error("expected nil Jobs before bootstrap")
	}
	if app.Plugins() != nil {
		t.Error("expected nil Plugins before bootstrap")
	}
	if app.Tenants() != nil {
		t.Error("expected nil Tenants before bootstrap")
	}
	if app.Search() != nil {
		t.Error("expected nil Search before bootstrap")
	}
	if app.Settings() != nil {
		t.Error("expected nil Settings before bootstrap")
	}
	if app.Migrations() != nil {
		t.Error("expected nil Migrations before bootstrap")
	}
	if app.MFA() != nil {
		t.Error("expected nil MFA before bootstrap")
	}
	if app.Verification() != nil {
		t.Error("expected nil Verification before bootstrap")
	}
	if app.Backup() != nil {
		t.Error("expected nil Backup before bootstrap")
	}
	if app.ImageProcessor() != nil {
		t.Error("expected nil ImageProcessor before bootstrap")
	}
}

func TestApp_ServeWithoutBootstrap(t *testing.T) {
	cfg := &config.Config{Addr: ":8080", LogLevel: "info"}
	app, _ := New(cfg)

	err := app.Serve()
	if err == nil {
		t.Fatal("expected error serving without bootstrap")
	}
}

func TestApp_RunMigrationsWithoutBootstrap(t *testing.T) {
	cfg := &config.Config{Addr: ":8080", LogLevel: "info"}
	app, _ := New(cfg)

	err := app.RunMigrations()
	if err == nil {
		t.Fatal("expected error running migrations without bootstrap")
	}
}

func TestApp_CreateAdminWithoutBootstrap(t *testing.T) {
	cfg := &config.Config{Addr: ":8080", LogLevel: "info"}
	app, _ := New(cfg)

	err := app.CreateAdmin("admin@test.com", "password")
	if err == nil {
		t.Fatal("expected error creating admin without bootstrap")
	}
}

func TestApp_IssueCertificateWithoutBootstrap(t *testing.T) {
	cfg := &config.Config{Addr: ":8080", LogLevel: "info"}
	app, _ := New(cfg)

	err := app.IssueCertificate("example.com")
	if err == nil {
		t.Fatal("expected error issuing certificate without bootstrap")
	}
}

func TestApp_Config(t *testing.T) {
	cfg := &config.Config{Addr: ":9090", LogLevel: "warn"}
	app, _ := New(cfg)

	if app.Config() != cfg {
		t.Error("expected Config() to return the same config")
	}
}

func TestApp_SetAPIServer(t *testing.T) {
	cfg := &config.Config{Addr: ":8080", LogLevel: "info"}
	app, _ := New(cfg)

	mockServer := &mockAPIServer{}
	app.SetAPIServer(mockServer)

	// Can't test Serve() without bootstrap, but we can verify it's set
	// The SetAPIServer should store it
}

func TestApp_ShutdownWithoutBootstrap(t *testing.T) {
	cfg := &config.Config{Addr: ":8080", LogLevel: "info"}
	app, _ := New(cfg)

	// Should not panic even when not bootstrapped
	app.Shutdown()
}

func TestApp_SetAPIRouter(t *testing.T) {
	cfg := &config.Config{Addr: ":8080", LogLevel: "info"}
	app, _ := New(cfg)

	mockRouter := &mockRouter{}
	app.SetAPIRouter(mockRouter)

	if app.APIRouter() != mockRouter {
		t.Error("expected APIRouter() to return the set router")
	}
}

// ---------------------------------------------------------------------------
// Mock implementations for testing
// ---------------------------------------------------------------------------

type mockAPIServer struct{}

func (m *mockAPIServer) Start(addr string) error { return nil }
func (m *mockAPIServer) Shutdown() error         { return nil }

type mockRouter struct{}

func (m *mockRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {}
