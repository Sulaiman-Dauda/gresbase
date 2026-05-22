package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gresbase/gresbase/internal/acme"
	"github.com/gresbase/gresbase/internal/auth"
	"github.com/gresbase/gresbase/internal/collection"
	"github.com/gresbase/gresbase/internal/config"
	"github.com/gresbase/gresbase/internal/database"
	"github.com/gresbase/gresbase/internal/events"
	"github.com/gresbase/gresbase/migrations"
	"github.com/gresbase/gresbase/internal/job"
	"github.com/gresbase/gresbase/internal/jsplugin"
	"github.com/gresbase/gresbase/internal/mailer"
	"github.com/gresbase/gresbase/internal/plugin"
	"github.com/gresbase/gresbase/internal/realtime"
	"github.com/gresbase/gresbase/internal/settings"
	"github.com/gresbase/gresbase/internal/storage"
	"github.com/gresbase/gresbase/internal/tenant"
	"github.com/gresbase/gresbase/internal/tools/search"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// APIServer is the interface the API server must satisfy.
type APIServer interface {
	Start(addr string) error
	Shutdown() error
}

// Router is a minimal interface for the HTTP request router.
type Router interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

// App is the central application instance.
// All subsystems are accessible through this struct.
type App struct {
	cfg *config.Config
	db  *database.DB

	// Subsystems — all initialized during Bootstrap
	apiServer      APIServer // set via SetAPIServer after bootstrap
	apiRouter      Router     // set via SetAPIRouter after bootstrap
	authService    *auth.Service
	oauthService   *auth.OAuthService
	collections    *collection.Service
	storageSvc     *storage.Service
	acmeService    *acme.Service
	realtimeHub    *realtime.Hub
	mailerSvc      *mailer.Service
	backupSvc      *storage.BackupService
	mfaService     *auth.MFAService
	verifyService  *auth.VerificationService
	imageProcessor *storage.ImageProcessor
	jobScheduler   *job.Scheduler
	pluginRegistry *plugin.Registry
	jsRuntime      *jsplugin.Runtime
	searchProvider *search.Provider
	tenantService  *tenant.Service
	settingsService *settings.Service
	migrations     *database.Migrations
	runner         *database.MigrationRunner

	// Event hooks — fired at every lifecycle point
	onBootstrap      *events.Hook
	onTerminate      *events.Hook
	onServe          *events.Hook
	onModelValidate  *events.Hook
	onModelCreate    *events.Hook
	onModelUpdate    *events.Hook
	onModelDelete    *events.Hook
	onRecordCreate   *events.Hook
	onRecordUpdate   *events.Hook
	onRecordDelete   *events.Hook
	onAuthLogin      *events.Hook
	onAuthRefresh    *events.Hook
	onRealtimeConn   *events.Hook
	onCollectionCreate *events.Hook
	onCollectionUpdate *events.Hook
	onCollectionDelete *events.Hook

	mu    sync.RWMutex
	ready bool
}

// New creates a new App instance. Does NOT connect to the database yet.
func New(cfg *config.Config) (*App, error) {
	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	if cfg.DevMode {
		log.Logger = log.Output(zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: "15:04:05",
		})
	}

	app := &App{
		cfg:               cfg,
		migrations:        database.NewMigrations(),
		onBootstrap:       events.NewHook(),
		onTerminate:       events.NewHook(),
		onServe:           events.NewHook(),
		onModelValidate:   events.NewHook(),
		onModelCreate:     events.NewHook(),
		onModelUpdate:     events.NewHook(),
		onModelDelete:     events.NewHook(),
		onRecordCreate:    events.NewHook(),
		onRecordUpdate:    events.NewHook(),
		onRecordDelete:    events.NewHook(),
		onAuthLogin:       events.NewHook(),
		onAuthRefresh:     events.NewHook(),
		onRealtimeConn:    events.NewHook(),
		onCollectionCreate: events.NewHook(),
		onCollectionUpdate: events.NewHook(),
		onCollectionDelete: events.NewHook(),
	}

	return app, nil
}

// Bootstrap initializes all subsystems: database, services, migrations, realtime hub, API server.
func (app *App) Bootstrap() error {
	app.mu.Lock()
	defer app.mu.Unlock()

	if app.ready {
		return nil
	}

	log.Info().Msg("Bootstrapping Gresbase...")

	// 1. Connect to database
	db, err := database.New(app.cfg)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	app.db = db

	// 2. Initialize services
	app.authService = auth.NewService(app.db, app.cfg)
	app.oauthService = auth.NewOAuthService(app.db)

	// Inject OAuth client credentials from config
	oauthCfg := app.cfg.OAuthProviders
	if oauthCfg != nil {
		for name, p := range oauthCfg {
			if p.Enabled && p.ClientID != "" && p.ClientSecret != "" {
				if provider, ok := app.oauthService.GetProviderByName(name); ok {
					provider.ClientID = p.ClientID
					provider.ClientSecret = p.ClientSecret
					provider.Enabled = true
					if p.RedirectURL != "" {
						provider.RedirectURL = p.RedirectURL
					}
				}
			}
		}
	}

	// Initialize OAuth tables
	app.oauthService.EnsureTable(context.Background())
	app.collections = collection.NewService(app.db)

	storageSvc, err := storage.NewService(app.cfg)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}
	app.storageSvc = storageSvc

	acmeSvc, err := acme.NewService(app.db, app.cfg)
	if err != nil {
		return fmt.Errorf("acme: %w", err)
	}
	app.acmeService = acmeSvc
	app.mailerSvc = mailer.NewService(app.cfg)
	app.backupSvc = storage.NewBackupService(app.db, app.cfg)
	app.mfaService = auth.NewMFAService(app.db)
	app.verifyService = auth.NewVerificationService(app.db, app.mailerSvc)
	app.imageProcessor = storage.NewImageProcessor()

	app.realtimeHub = realtime.NewHub()

	// 2b. Plugin registry
	app.pluginRegistry = plugin.NewRegistry()
	for _, p := range plugin.LoadBuiltinPlugins() {
		if err := app.pluginRegistry.Register(p); err != nil {
			log.Warn().Err(err).Str("plugin", p.Name).Msg("Failed to register plugin")
		}
	}

	// 2c. Job scheduler
	app.jobScheduler = job.NewScheduler(app.db)
	if err := app.jobScheduler.EnsureTable(context.Background()); err != nil {
		log.Warn().Err(err).Msg("Failed to ensure jobs table")
	}

	// 2d. JS plugin runtime
	app.jsRuntime = jsplugin.NewRuntime(30 * time.Second)
	if app.cfg.DevMode {
		for id, script := range jsplugin.BuiltinPlugins() {
			if _, err := app.jsRuntime.LoadPlugin(id, id, script, 0); err != nil {
				log.Warn().Err(err).Str("plugin", id).Msg("Failed to load builtin JS plugin")
			}
		}
	}

	// Wire JS plugin hooks to Go event hooks
	app.wireJSHooks()

	// 2e. Search provider
	app.searchProvider = search.NewProvider(app.db)

	// 2d. Tenant service
	app.tenantService = tenant.NewService(app.db)

	// 2e. Settings service
	app.settingsService = settings.NewService(app.db)
	if err := app.settingsService.EnsureTable(context.Background()); err != nil {
		log.Warn().Err(err).Msg("Failed to ensure settings table")
	}

	// 3. Migration runner
	app.runner = database.NewMigrationRunner(app.db, app.migrations)
	app.registerInternalMigrations()

	// 4. Run system migrations (auto-create tables)
	applied, err := app.runner.Up(context.Background())
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	if len(applied) > 0 {
		for _, name := range applied {
			log.Info().Str("migration", name).Msg("Applied")
		}
	}

	// 5. Ensure storage directory exists
	if app.cfg.StorageBackend == "local" {
		if err := os.MkdirAll(app.cfg.StorageLocal, 0755); err != nil {
			return fmt.Errorf("storage dir: %w", err)
		}
	}

	// 6. Start realtime hub
	go app.realtimeHub.Run()

	// 7. Build the API server and inject all services
	// The API server is created externally (to avoid import cycles) and set via SetAPIServer.
	// Callers: after Bootstrap(), call api.NewServer(app) and app.SetAPIServer(server).

	// 8. Fire bootstrap event for plugins
	bootstrapEvent := &events.BootstrapEvent{App: app}
	if err := app.onBootstrap.Trigger(bootstrapEvent, func(e events.Event) error { return bootstrapEvent.Next() }); err != nil {
		return fmt.Errorf("bootstrap hook: %w", err)
	}

	app.ready = true
	log.Info().Msg("Gresbase bootstrapped successfully")
	return nil
}

// Serve starts the HTTP server and blocks.
func (app *App) Serve() error {
	if !app.ready {
		return fmt.Errorf("application not bootstrapped — call Bootstrap() first")
	}

	// Start background tasks
	go app.startACMEAutoRenewal()
	if app.jobScheduler != nil {
		app.jobScheduler.Start()
	}

	if app.apiServer == nil {
		return fmt.Errorf("API server not initialized — call SetAPIServer() after Bootstrap()")
	}
	return app.apiServer.Start(app.cfg.Addr)
}

// Shutdown gracefully shuts down all subsystems.
func (app *App) Shutdown() {
	log.Info().Msg("Shutting down Gresbase...")

	if app.jobScheduler != nil {
		app.jobScheduler.Stop()
	}
	if app.realtimeHub != nil {
		app.realtimeHub.Shutdown()
	}
	if app.apiServer != nil {
		app.apiServer.Shutdown()
	}
	if app.db != nil {
		app.db.Close()
	}

	// Fire terminate event
	termEvent := &events.TerminateEvent{App: app}
	app.onTerminate.Trigger(termEvent, func(e events.Event) error { return termEvent.Next() })

	log.Info().Msg("Gresbase shutdown complete")
}

// RunMigrations applies pending migrations and exits.
func (app *App) RunMigrations() error {
	if !app.ready {
		return fmt.Errorf("not bootstrapped")
	}
	ctx := context.Background()
	applied, err := app.runner.Up(ctx)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		log.Info().Msg("No pending migrations")
	} else {
		for _, name := range applied {
			log.Info().Str("migration", name).Msg("Applied")
		}
	}
	return nil
}

// CreateAdmin creates a superuser account from the CLI.
func (app *App) CreateAdmin(email, password string) error {
	if !app.ready {
		return fmt.Errorf("not bootstrapped")
	}
	admin, err := app.authService.CreateAdmin(context.Background(), email, password, "admin", "default")
	if err != nil {
		return fmt.Errorf("create admin: %w", err)
	}
	log.Info().Str("id", admin.ID).Str("email", admin.Email).Msg("Admin created")
	return nil
}

// IssueCertificate issues a TLS certificate via the internal ACME CA.
func (app *App) IssueCertificate(domain string) error {
	if !app.ready {
		return fmt.Errorf("not bootstrapped")
	}
	cert, err := app.acmeService.IssueForDomain(context.Background(), domain)
	if err != nil {
		return fmt.Errorf("certificate: %w", err)
	}
	log.Info().Str("domain", cert.Domain).Str("id", cert.ID).Msg("Certificate issued")
	return nil
}

func (app *App) startACMEAutoRenewal() {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		if err := app.acmeService.AutoRenew(context.Background()); err != nil {
			log.Error().Err(err).Msg("ACME auto-renewal error")
		}
	}
}

// ---------------------------------------------------------------------------
// Accessors
// ---------------------------------------------------------------------------

func (app *App) DB() *database.DB                     { return app.db }
func (app *App) Config() *config.Config                { return app.cfg }
func (app *App) Auth() *auth.Service                   { return app.authService }
func (app *App) OAuth() *auth.OAuthService             { return app.oauthService }
func (app *App) Collections() *collection.Service       { return app.collections }
func (app *App) Storage() *storage.Service              { return app.storageSvc }
func (app *App) ACME() *acme.Service                    { return app.acmeService }
func (app *App) Realtime() *realtime.Hub                { return app.realtimeHub }
func (app *App) Mailer() *mailer.Service                { return app.mailerSvc }
func (app *App) Backup() *storage.BackupService         { return app.backupSvc }
func (app *App) MFA() *auth.MFAService                   { return app.mfaService }
func (app *App) Verification() *auth.VerificationService { return app.verifyService }
func (app *App) ImageProcessor() *storage.ImageProcessor { return app.imageProcessor }
func (app *App) IsReady() bool                          { app.mu.RLock(); defer app.mu.RUnlock(); return app.ready }
func (app *App) IsBootstrapped() bool                   { return app.IsReady() }
func (app *App) Jobs() *job.Scheduler                   { return app.jobScheduler }
func (app *App) Plugins() *plugin.Registry              { return app.pluginRegistry }
func (app *App) Tenants() *tenant.Service               { return app.tenantService }
func (app *App) JSRuntime() *jsplugin.Runtime            { return app.jsRuntime }
func (app *App) Search() *search.Provider                { return app.searchProvider }
func (app *App) Settings() *settings.Service              { return app.settingsService }
func (app *App) Migrations() *database.MigrationRunner   { return app.runner }
func (app *App) RecordAuth() *auth.RecordAuthService      {
	if app.authService != nil && app.db != nil && app.cfg != nil {
		return auth.NewRecordAuthService(app.db, app.cfg, app.authService)
	}
	return nil
}

// SetAPIServer sets the API server (to be called after Bootstrap).
func (app *App) SetAPIServer(server APIServer) {
	app.apiServer = server
}

// APIRouter returns the router for internal sub-requests.
func (app *App) APIRouter() Router {
	return app.apiRouter
}

// SetAPIRouter sets the router for internal sub-requests.
func (app *App) SetAPIRouter(router Router) {
	app.apiRouter = router
}

// ServeHTTP allows the app to handle internal sub-requests (for batch API).
func (app *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if app.apiRouter != nil {
		app.apiRouter.ServeHTTP(w, r)
	}
}

func (app *App) OnBootstrap() *events.Hook        { return app.onBootstrap }
func (app *App) OnTerminate() *events.Hook        { return app.onTerminate }
func (app *App) OnServe() *events.Hook            { return app.onServe }
func (app *App) OnModelValidate() *events.Hook    { return app.onModelValidate }
func (app *App) OnModelCreate() *events.Hook      { return app.onModelCreate }
func (app *App) OnModelUpdate() *events.Hook      { return app.onModelUpdate }
func (app *App) OnModelDelete() *events.Hook      { return app.onModelDelete }
func (app *App) OnRecordCreate() *events.Hook     { return app.onRecordCreate }
func (app *App) OnRecordUpdate() *events.Hook     { return app.onRecordUpdate }
func (app *App) OnRecordDelete() *events.Hook     { return app.onRecordDelete }
func (app *App) OnAuthLogin() *events.Hook        { return app.onAuthLogin }
func (app *App) OnAuthRefresh() *events.Hook      { return app.onAuthRefresh }
func (app *App) OnRealtimeConn() *events.Hook     { return app.onRealtimeConn }
func (app *App) OnCollectionCreate() *events.Hook { return app.onCollectionCreate }
func (app *App) OnCollectionUpdate() *events.Hook { return app.onCollectionUpdate }
func (app *App) OnCollectionDelete() *events.Hook { return app.onCollectionDelete }

// ---------------------------------------------------------------------------
// Internal migrations
// ---------------------------------------------------------------------------

func (app *App) registerInternalMigrations() {
	// Load all migrations from the migrations package (file-based Go migrations)
	for _, m := range migrations.All() {
		app.migrations.Add(m)
	}
}

// wireJSHooks connects Go event hooks to the JS plugin runtime.
// When a Go hook fires, registered JS hook handlers are invoked.
func (app *App) wireJSHooks() {
	if app.jsRuntime == nil {
		return
	}

	// Helper to fire JS hook with JSON event data
	fireJS := func(hookName string, data any) {
		jsonData, err := json.Marshal(data)
		if err != nil {
			return
		}
		if err := app.jsRuntime.TriggerHook(hookName, jsonData); err != nil {
			log.Debug().Err(err).Str("hook", hookName).Msg("JS hook error")
		}
	}

	// Record lifecycle hooks
	app.OnRecordCreate().BindFunc(func(e events.Event) error {
		fireJS("onRecordCreate", e)
		return e.Next()
	})
	app.OnRecordUpdate().BindFunc(func(e events.Event) error {
		fireJS("onRecordUpdate", e)
		return e.Next()
	})
	app.OnRecordDelete().BindFunc(func(e events.Event) error {
		fireJS("onRecordDelete", e)
		return e.Next()
	})

	// Collection lifecycle hooks
	app.OnCollectionCreate().BindFunc(func(e events.Event) error {
		fireJS("onCollectionCreate", e)
		return e.Next()
	})
	app.OnCollectionUpdate().BindFunc(func(e events.Event) error {
		fireJS("onCollectionUpdate", e)
		return e.Next()
	})
	app.OnCollectionDelete().BindFunc(func(e events.Event) error {
		fireJS("onCollectionDelete", e)
		return e.Next()
	})

	// Auth hooks
	app.OnAuthLogin().BindFunc(func(e events.Event) error {
		fireJS("onAuthLogin", e)
		return e.Next()
	})
	app.OnAuthRefresh().BindFunc(func(e events.Event) error {
		fireJS("onAuthRefresh", e)
		return e.Next()
	})

	// Realtime connection hooks
	app.OnRealtimeConn().BindFunc(func(e events.Event) error {
		fireJS("onRealtimeConnect", e)
		return e.Next()
	})

	log.Debug().Msg("JS plugin hooks wired to app events")
}
