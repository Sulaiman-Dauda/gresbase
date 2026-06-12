package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gresbase/gresbase/internal/auth"
	"github.com/gresbase/gresbase/internal/collection"
	"github.com/gresbase/gresbase/internal/config"
	"github.com/gresbase/gresbase/internal/database"
	"github.com/gresbase/gresbase/internal/events"
	"github.com/gresbase/gresbase/internal/job"
	"github.com/gresbase/gresbase/internal/jsplugin"
	"github.com/gresbase/gresbase/internal/mailer"
	"github.com/gresbase/gresbase/internal/metrics"
	"github.com/gresbase/gresbase/internal/plugin"
	"github.com/gresbase/gresbase/internal/realtime"
	"github.com/gresbase/gresbase/internal/schemamigrate"
	"github.com/gresbase/gresbase/internal/settings"
	"github.com/gresbase/gresbase/internal/storage"
	"github.com/gresbase/gresbase/internal/tools/search"
	"github.com/gresbase/gresbase/internal/walcapture"
	"github.com/gresbase/gresbase/internal/webhook"
	"github.com/gresbase/gresbase/migrations"
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
	apiServer       APIServer // set via SetAPIServer after bootstrap
	apiRouter       Router    // set via SetAPIRouter after bootstrap
	authService     *auth.Service
	oauthService    *auth.OAuthService
	recordAuthSvc   *auth.RecordAuthService
	passkeySvc      *auth.PasskeyService
	collections     *collection.Service
	storageSvc      *storage.Service
	realtimeHub     *realtime.Hub
	mailerSvc       *mailer.Service
	backupSvc       *storage.BackupService
	mfaService      *auth.MFAService
	verifyService   *auth.VerificationService
	imageProcessor  *storage.ImageProcessor
	jobScheduler    *job.Scheduler
	pluginRegistry  *plugin.Registry
	jsRuntime       *jsplugin.Runtime
	searchProvider  *search.Provider
	settingsService *settings.Service
	webhookService  *webhook.Service
	metricsColl     *metrics.Collector
	migrations      *database.Migrations
	runner          *database.MigrationRunner
	walCapture      *walcapture.Service
	schemaMigrate   *schemamigrate.Runner

	// Event hooks — fired at every lifecycle point
	onBootstrap                *events.Hook
	onTerminate                *events.Hook
	onServe                    *events.Hook
	onModelValidate            *events.Hook
	onModelCreate              *events.Hook
	onModelUpdate              *events.Hook
	onModelDelete              *events.Hook
	onRecordCreate             *events.Hook
	onRecordUpdate             *events.Hook
	onRecordDelete             *events.Hook
	onAuthLogin                *events.Hook
	onAuthRefresh              *events.Hook
	onRealtimeConn             *events.Hook
	onCollectionCreate         *events.Hook
	onCollectionUpdate         *events.Hook
	onCollectionDelete         *events.Hook
	onCollectionsImportRequest *events.Hook
	onSettingsListRequest      *events.Hook
	onSettingsUpdateRequest    *events.Hook
	onBatchRequest             *events.Hook
	onRecordsListRequest       *events.Hook
	onRecordViewRequest        *events.Hook
	onRecordCreateRequest      *events.Hook
	onRecordUpdateRequest      *events.Hook
	onRecordDeleteRequest      *events.Hook
	onAdminUserRequest         *events.Hook
	onCollectionRequest        *events.Hook
	onAdminAuthRequest         *events.Hook
	onRecordAuthRequest        *events.Hook
	onRealtimeRequest          *events.Hook
	onFileDownloadRequest      *events.Hook
	onFileUploadRequest        *events.Hook
	onFileDeleteRequest        *events.Hook
	onBackupListRequest        *events.Hook
	onBackupCreateRequest      *events.Hook
	onBackupRestoreRequest     *events.Hook
	onBackupDeleteRequest      *events.Hook
	onBackupDownloadRequest    *events.Hook
	onLogsListRequest          *events.Hook
	onAPIKeyRequest            *events.Hook
	onSearchRequest            *events.Hook
	onFTSIndexRequest          *events.Hook
	onJobRequest               *events.Hook

	boundPlugins map[string]struct{}
	mu           sync.RWMutex
	ready        bool

	// bgCtx scopes long-lived background goroutines (cluster listener, WAL
	// capture, hooks-dir watcher). Cancelled at the start of Shutdown so they
	// stop cleanly instead of leaking past process teardown.
	bgCtx    context.Context
	bgCancel context.CancelFunc
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
		cfg:                        cfg,
		migrations:                 database.NewMigrations(),
		onBootstrap:                events.NewHook(),
		onTerminate:                events.NewHook(),
		onServe:                    events.NewHook(),
		onModelValidate:            events.NewHook(),
		onModelCreate:              events.NewHook(),
		onModelUpdate:              events.NewHook(),
		onModelDelete:              events.NewHook(),
		onRecordCreate:             events.NewHook(),
		onRecordUpdate:             events.NewHook(),
		onRecordDelete:             events.NewHook(),
		onAuthLogin:                events.NewHook(),
		onAuthRefresh:              events.NewHook(),
		onRealtimeConn:             events.NewHook(),
		onCollectionCreate:         events.NewHook(),
		onCollectionUpdate:         events.NewHook(),
		onCollectionDelete:         events.NewHook(),
		onCollectionsImportRequest: events.NewHook(),
		onSettingsListRequest:      events.NewHook(),
		onSettingsUpdateRequest:    events.NewHook(),
		onBatchRequest:             events.NewHook(),
		onRecordsListRequest:       events.NewHook(),
		onRecordViewRequest:        events.NewHook(),
		onRecordCreateRequest:      events.NewHook(),
		onRecordUpdateRequest:      events.NewHook(),
		onRecordDeleteRequest:      events.NewHook(),
		onAdminUserRequest:         events.NewHook(),
		onCollectionRequest:        events.NewHook(),
		onAdminAuthRequest:         events.NewHook(),
		onRecordAuthRequest:        events.NewHook(),
		onRealtimeRequest:          events.NewHook(),
		onFileDownloadRequest:      events.NewHook(),
		onFileUploadRequest:        events.NewHook(),
		onFileDeleteRequest:        events.NewHook(),
		onBackupListRequest:        events.NewHook(),
		onBackupCreateRequest:      events.NewHook(),
		onBackupRestoreRequest:     events.NewHook(),
		onBackupDeleteRequest:      events.NewHook(),
		onBackupDownloadRequest:    events.NewHook(),
		onLogsListRequest:          events.NewHook(),
		onAPIKeyRequest:            events.NewHook(),
		onSearchRequest:            events.NewHook(),
		onFTSIndexRequest:          events.NewHook(),
		onJobRequest:               events.NewHook(),
		boundPlugins:               map[string]struct{}{},
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

	// Scope all long-lived background goroutines to a cancellable context so
	// Shutdown() can stop them deterministically.
	app.bgCtx, app.bgCancel = context.WithCancel(context.Background())

	// 1. Connect to database
	db, err := database.New(app.cfg)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	app.db = db

	// 2. Initialize services
	app.authService = auth.NewService(app.db, app.cfg)
	app.oauthService = auth.NewOAuthService(app.db)
	app.recordAuthSvc = auth.NewRecordAuthService(app.db, app.cfg, app.authService)

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

	app.mailerSvc = mailer.NewService(app.cfg)
	app.backupSvc = storage.NewBackupService(app.db, app.cfg)
	app.mfaService = auth.NewMFAService(app.db)
	app.verifyService = auth.NewVerificationService(app.db, app.mailerSvc)
	app.imageProcessor = storage.NewImageProcessor()

	app.realtimeHub = realtime.NewHub()
	app.realtimeHub.SetIdleTimeout(app.cfg.RealtimeIdleTimeout)
	app.realtimeHub.SetMaxConnections(app.cfg.RealtimeMaxConnections)
	app.realtimeHub.SetMaxMessageSize(app.cfg.RealtimeMaxMessageSize)
	app.realtimeHub.SetMaxConnectionAge(app.cfg.RealtimeMaxConnectionAge)

	// 2b. Plugin registry
	app.pluginRegistry = plugin.NewRegistry()
	for _, p := range plugin.LoadBuiltinPlugins() {
		if err := app.RegisterPlugin(p); err != nil {
			log.Warn().Err(err).Str("plugin", p.Name).Msg("Failed to register plugin")
		}
	}

	// 2c. Job scheduler
	app.jobScheduler = job.NewScheduler(app.db)
	if err := app.jobScheduler.EnsureTable(context.Background()); err != nil {
		log.Warn().Err(err).Msg("Failed to ensure jobs table")
	}
	app.registerBackupJob()

	// 2d. File-based JS hooks (PocketBase pb_hooks-style). The goja runtime
	// only ever executes *.js files placed next to the binary — placing files
	// on the server's filesystem already implies full trust (same model as Go
	// hooks), so there is no separate enable flag. Disable with HOOKS_DIR="".
	app.jsRuntime = jsplugin.NewRuntime(30 * time.Second)
	hooksLoaded := 0
	if dir := app.cfg.HooksDir; dir != "" {
		n, err := app.jsRuntime.LoadHooksDir(dir)
		if err != nil {
			log.Warn().Err(err).Str("dir", dir).Msg("Failed to load JS hooks directory")
		} else if n > 0 {
			hooksLoaded = n
			log.Info().Int("hooks", n).Str("dir", dir).Msg("Loaded JS hook files (unsandboxed — trusted code only)")
		}
		if app.cfg.HooksWatch {
			if _, statErr := os.Stat(dir); statErr == nil {
				go app.jsRuntime.WatchHooksDir(app.bgCtx, dir, 2*time.Second)
			}
		}
	}

	// Wire JS hooks to Go event hooks when file hooks are present.
	if app.cfg.HooksDir != "" || hooksLoaded > 0 {
		app.wireJSHooks()
	}

	// 2e. Search provider
	app.searchProvider = search.NewProvider(app.db)

	// 2e. Settings service
	app.settingsService = settings.NewService(app.db)
	if err := app.settingsService.EnsureTable(context.Background()); err != nil {
		log.Warn().Err(err).Msg("Failed to ensure settings table")
	}

	// Feed superuser email template overrides from settings into the mailer.
	// The settings service caches in memory and Save refreshes the cache, so
	// reading on every send is cheap and changes apply immediately.
	app.mailerSvc.SetOverrideProvider(func() map[string]mailer.TemplateOverride {
		st, err := app.settingsService.Get(context.Background())
		if err != nil || st == nil || len(st.EmailTemplates) == 0 {
			return nil
		}
		overrides := make(map[string]mailer.TemplateOverride, len(st.EmailTemplates))
		for id, tmpl := range st.EmailTemplates {
			overrides[id] = mailer.TemplateOverride{Subject: tmpl.Subject, Body: tmpl.Body}
		}
		return overrides
	})

	// 2f. Webhooks — HMAC-signed event forwarding to external URLs.
	app.webhookService = webhook.NewService(app.db)
	if err := app.webhookService.EnsureTable(context.Background()); err != nil {
		log.Warn().Err(err).Msg("Failed to ensure webhooks table")
	}
	app.wireWebhooks()

	// 2g. Metrics collector — feeds /metrics (Prometheus) and the JSON health report.
	app.metricsColl = metrics.NewCollector()
	app.metricsColl.DBStats = func() (open, idle, max int32) {
		if app.db == nil || app.db.Pool == nil {
			return 0, 0, 0
		}
		stat := app.db.Pool.Stat()
		return stat.AcquiredConns(), stat.IdleConns(), stat.MaxConns()
	}
	app.metricsColl.DBMode = func() string {
		if app.db != nil && app.db.IsEmbedded() {
			return "embedded"
		}
		return "external"
	}
	app.metricsColl.RealtimeStats = func() (clients int, topics, sent, dropped int64) {
		if app.realtimeHub == nil {
			return 0, 0, 0, 0
		}
		stats := app.realtimeHub.Stats()
		return int(stats.TotalConnections), stats.TotalTopics, stats.MessagesSent, stats.MessagesDropped
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

	// 4b. Collection schema migrations (gb_migrations): replay declarative
	// JSON snapshots of USER collections so schemas tracked in git materialize
	// on boot. Runs after Gresbase's own migrations (so _collections exists)
	// and before serving. Fail-closed: a bad file aborts startup.
	app.schemaMigrate = schemamigrate.New(app.db, app.collections, app.cfg.MigrationsDir)
	// Large schema migrations (many collections / heavy ALTERs) need a generous,
	// configurable deadline so they don't fail spuriously on a short timeout.
	migrateCtx := context.Background()
	var migrateCancel context.CancelFunc
	if app.cfg.MigrationApplyTimeout > 0 {
		migrateCtx, migrateCancel = context.WithTimeout(migrateCtx, app.cfg.MigrationApplyTimeout)
	}
	appliedSchema, err := app.schemaMigrate.Apply(migrateCtx)
	if migrateCancel != nil {
		migrateCancel()
	}
	if err != nil {
		return fmt.Errorf("collection schema migrations: %w", err)
	}
	for _, name := range appliedSchema {
		log.Info().Str("file", name).Msg("Applied collection schema migration")
	}

	// 4c. Dev automigrate (PocketBase behavior): collection changes made
	// through the dashboard/API write migration files automatically.
	if app.cfg.DevMode && app.cfg.MigrationsDir != "" {
		app.bindSchemaAutomigrate()
	}

	// 5. Ensure storage directory exists
	if app.cfg.StorageBackend == "local" {
		if err := os.MkdirAll(app.cfg.StorageLocal, 0755); err != nil {
			return fmt.Errorf("storage dir: %w", err)
		}
	}

	// 6. Start realtime hub
	go app.realtimeHub.Run()

	// 6b. Cluster realtime: when multi-node mode is on, record events travel
	// between app nodes over Postgres LISTEN/NOTIFY — no extra infrastructure.
	if app.cfg.RealtimeMultiNode {
		app.realtimeHub.EnableCluster(func(ctx context.Context, payload string) error {
			return app.db.Notify(ctx, realtime.ClusterChannel, payload)
		})
		go app.realtimeHub.RunClusterListener(app.bgCtx, app.db)
		log.Info().Msg("Realtime cluster mode enabled (Postgres LISTEN/NOTIFY)")
	}

	// 6c. WAL change capture: record events sourced from logical replication,
	// so rows changed via SQL (console, psql, the RLS role) reach realtime
	// subscribers too. While the stream is healthy it suppresses the direct
	// API broadcast path (single source of truth); if it degrades, the API
	// path resumes automatically.
	if app.cfg.RealtimeWALEnabled {
		app.walCapture = walcapture.New(app.db, app.realtimeHub, app.collections,
			app.cfg.RealtimeWALSlot, app.cfg.RealtimeWALPublication)
		app.metricsColl.WALStats = app.walCapture.Stats

		// Collection DDL updates publication membership immediately; the
		// capture also reconciles on a 30s ticker as a fallback.
		syncPublication := func(e events.Event) error {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := app.walCapture.SyncPublication(ctx); err != nil {
					log.Warn().Err(err).Msg("WAL publication sync failed")
				}
			}()
			return e.Next()
		}
		app.onCollectionCreate.BindFunc(syncPublication)
		app.onCollectionUpdate.BindFunc(syncPublication)
		app.onCollectionDelete.BindFunc(syncPublication)

		go app.walCapture.Run(app.bgCtx)
		log.Info().Str("slot", app.cfg.RealtimeWALSlot).Str("publication", app.cfg.RealtimeWALPublication).
			Msg("Realtime WAL change capture enabled (logical replication)")
	}

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
	if app.jobScheduler != nil {
		// Re-arm jobs persisted by previous runs; without this, scheduled
		// jobs silently stop after a restart.
		if err := app.jobScheduler.LoadJobs(context.Background()); err != nil {
			log.Warn().Err(err).Msg("Failed to load persisted jobs")
		}
		app.jobScheduler.Start()
	}
	if app.webhookService != nil {
		app.webhookService.Start(context.Background())
	}

	if app.apiServer == nil {
		return fmt.Errorf("API server not initialized — call SetAPIServer() after Bootstrap()")
	}
	return app.apiServer.Start(app.cfg.Addr)
}

// Shutdown gracefully shuts down all subsystems.
func (app *App) Shutdown() {
	log.Info().Msg("Shutting down Gresbase...")

	// 1. Stop accepting new background work.
	if app.jobScheduler != nil {
		app.jobScheduler.Stop()
	}
	if app.webhookService != nil {
		app.webhookService.Stop()
	}

	// 2. Cancel long-lived background goroutines (cluster listener, WAL
	// capture loop, hooks-dir watcher) so they unwind before subsystems and
	// the DB pool go away.
	if app.bgCancel != nil {
		app.bgCancel()
	}

	// 3. Stop subsystems, then close the DB last.
	if app.walCapture != nil {
		// Stops the stream, sends a final standby status update, and closes
		// the replication connection before the hub goes away.
		app.walCapture.Stop()
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
	admin, err := app.authService.CreateAdmin(context.Background(), email, password, "admin")
	if err != nil {
		return fmt.Errorf("create admin: %w", err)
	}
	log.Info().Str("id", admin.ID).Str("email", admin.Email).Msg("Admin created")
	return nil
}

// registerBackupJob wires the "system.backup" cron handler: snapshot the
// database, optionally mirror to S3, prune old snapshots. When backup_cron is
// configured, a job row is created (or its schedule updated) on boot.
func (app *App) registerBackupJob() {
	// Scheduled (cron) backup failures alert all superusers by email; manual
	// backup failures never do — the operator triggering them sees the error.
	alerter := newBackupFailureAlerter(app)

	app.jobScheduler.RegisterHandler("system.backup", func(ctx context.Context, data map[string]any) error {
		name := fmt.Sprintf("auto-%s", time.Now().UTC().Format("20060102-150405"))
		info, err := app.backupSvc.CreateBackup(ctx, name, true)
		if err != nil {
			alerter.notify(ctx, err)
			return fmt.Errorf("scheduled backup: %w", err)
		}
		log.Info().Str("backup", info.ID).Int64("size", info.Size).Msg("Scheduled backup created")

		if app.cfg.BackupUploadS3 && app.cfg.StorageBackend == "s3" {
			if err := app.backupSvc.UploadBackupToS3(ctx, info.ID, app.storageSvc); err != nil {
				log.Error().Err(err).Str("backup", info.ID).Msg("Backup S3 upload failed")
			}
		}

		// Retention: prune oldest auto- backups beyond backup_max_keep.
		// Manual backups are never pruned.
		if keep := app.cfg.BackupMaxKeep; keep > 0 {
			backups, err := app.backupSvc.ListBackups()
			if err != nil {
				// Backup already succeeded; retention is best-effort, so don't
				// fail the job — just log and skip pruning this run.
				log.Warn().Err(err).Msg("backup retention: list failed; skipping prune")
				return nil
			}
			auto := make([]*storage.BackupInfo, 0, len(backups))
			for _, b := range backups {
				if strings.HasPrefix(b.Name, "auto-") {
					auto = append(auto, b)
				}
			}
			sort.Slice(auto, func(i, j int) bool { return auto[i].CreatedAt.After(auto[j].CreatedAt) })
			for _, b := range auto[min(keep, len(auto)):] {
				if err := app.backupSvc.DeleteBackup(b.ID); err != nil {
					log.Warn().Err(err).Str("backup", b.ID).Msg("Backup prune failed")
				}
			}
		}
		return nil
	})

	cronExpr := strings.TrimSpace(app.cfg.BackupCron)
	if cronExpr == "" {
		return
	}
	if err := app.jobScheduler.ValidateCron(cronExpr); err != nil {
		log.Error().Err(err).Str("cron", cronExpr).Msg("Invalid backup_cron expression; scheduled backups disabled")
		return
	}
	ctx := context.Background()
	jobs, err := app.jobScheduler.ListJobs(ctx)
	if err == nil {
		for _, j := range jobs {
			if j.Handler == "system.backup" {
				return // already scheduled; managed via the jobs API from here on
			}
		}
	}
	if _, err := app.jobScheduler.AddJob(ctx, "Scheduled backup", cronExpr, "system.backup", nil); err != nil {
		log.Error().Err(err).Msg("Failed to schedule backup job")
	}
}

// ---------------------------------------------------------------------------
// Accessors
// ---------------------------------------------------------------------------

func (app *App) DB() *database.DB                        { return app.db }
func (app *App) Config() *config.Config                  { return app.cfg }
func (app *App) Auth() *auth.Service                     { return app.authService }
func (app *App) OAuth() *auth.OAuthService               { return app.oauthService }
func (app *App) Collections() *collection.Service        { return app.collections }
func (app *App) Storage() *storage.Service               { return app.storageSvc }
func (app *App) Realtime() *realtime.Hub                 { return app.realtimeHub }
func (app *App) Mailer() *mailer.Service                 { return app.mailerSvc }
func (app *App) Backup() *storage.BackupService          { return app.backupSvc }
func (app *App) MFA() *auth.MFAService                   { return app.mfaService }
func (app *App) Verification() *auth.VerificationService { return app.verifyService }
func (app *App) ImageProcessor() *storage.ImageProcessor { return app.imageProcessor }
func (app *App) IsReady() bool                           { app.mu.RLock(); defer app.mu.RUnlock(); return app.ready }
func (app *App) IsBootstrapped() bool                    { return app.IsReady() }
func (app *App) Jobs() *job.Scheduler                    { return app.jobScheduler }
func (app *App) Plugins() *plugin.Registry               { return app.pluginRegistry }
func (app *App) JSRuntime() *jsplugin.Runtime            { return app.jsRuntime }
func (app *App) Search() *search.Provider                { return app.searchProvider }
func (app *App) Settings() *settings.Service             { return app.settingsService }
func (app *App) Webhooks() *webhook.Service              { return app.webhookService }
func (app *App) Metrics() *metrics.Collector             { return app.metricsColl }
func (app *App) WALCapture() *walcapture.Service         { return app.walCapture }
func (app *App) Migrations() *database.MigrationRunner   { return app.runner }
func (app *App) RecordAuth() *auth.RecordAuthService {
	if app.recordAuthSvc == nil && app.authService != nil && app.db != nil && app.cfg != nil {
		app.recordAuthSvc = auth.NewRecordAuthService(app.db, app.cfg, app.authService)
	}
	return app.recordAuthSvc
}

// Passkeys lazily constructs the WebAuthn passkey service for record auth.
// The settings service is resolved lazily too, since the relying party is
// derived from the admin-editable application URL at ceremony time.
func (app *App) Passkeys() *auth.PasskeyService {
	if app.passkeySvc == nil && app.db != nil && app.cfg != nil && app.RecordAuth() != nil {
		app.passkeySvc = auth.NewPasskeyService(app.db, app.cfg, app.RecordAuth(), app.Settings)
	}
	return app.passkeySvc
}

// RegisterPlugin adds a plugin and immediately wires its hooks/routes.
func (app *App) RegisterPlugin(p *plugin.Plugin) error {
	if app.pluginRegistry == nil {
		app.pluginRegistry = plugin.NewRegistry()
	}
	if err := app.pluginRegistry.Register(p); err != nil {
		return err
	}
	app.bindPlugin(p)
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

func (app *App) OnBootstrap() *events.Hook                { return app.onBootstrap }
func (app *App) OnTerminate() *events.Hook                { return app.onTerminate }
func (app *App) OnServe() *events.Hook                    { return app.onServe }
func (app *App) OnModelValidate() *events.Hook            { return app.onModelValidate }
func (app *App) OnModelCreate() *events.Hook              { return app.onModelCreate }
func (app *App) OnModelUpdate() *events.Hook              { return app.onModelUpdate }
func (app *App) OnModelDelete() *events.Hook              { return app.onModelDelete }
func (app *App) OnRecordCreate() *events.Hook             { return app.onRecordCreate }
func (app *App) OnRecordUpdate() *events.Hook             { return app.onRecordUpdate }
func (app *App) OnRecordDelete() *events.Hook             { return app.onRecordDelete }
func (app *App) OnAuthLogin() *events.Hook                { return app.onAuthLogin }
func (app *App) OnAuthRefresh() *events.Hook              { return app.onAuthRefresh }
func (app *App) OnRealtimeConn() *events.Hook             { return app.onRealtimeConn }
func (app *App) OnCollectionCreate() *events.Hook         { return app.onCollectionCreate }
func (app *App) OnCollectionUpdate() *events.Hook         { return app.onCollectionUpdate }
func (app *App) OnCollectionDelete() *events.Hook         { return app.onCollectionDelete }
func (app *App) OnCollectionsImportRequest() *events.Hook { return app.onCollectionsImportRequest }
func (app *App) OnSettingsListRequest() *events.Hook      { return app.onSettingsListRequest }
func (app *App) OnSettingsUpdateRequest() *events.Hook    { return app.onSettingsUpdateRequest }
func (app *App) OnBatchRequest() *events.Hook             { return app.onBatchRequest }
func (app *App) OnRecordsListRequest() *events.Hook       { return app.onRecordsListRequest }
func (app *App) OnRecordViewRequest() *events.Hook        { return app.onRecordViewRequest }
func (app *App) OnRecordCreateRequest() *events.Hook      { return app.onRecordCreateRequest }
func (app *App) OnRecordUpdateRequest() *events.Hook      { return app.onRecordUpdateRequest }
func (app *App) OnRecordDeleteRequest() *events.Hook      { return app.onRecordDeleteRequest }
func (app *App) OnAdminUserRequest() *events.Hook         { return app.onAdminUserRequest }
func (app *App) OnCollectionRequest() *events.Hook        { return app.onCollectionRequest }
func (app *App) OnAdminAuthRequest() *events.Hook         { return app.onAdminAuthRequest }
func (app *App) OnRecordAuthRequest() *events.Hook        { return app.onRecordAuthRequest }
func (app *App) OnRealtimeRequest() *events.Hook          { return app.onRealtimeRequest }
func (app *App) OnFileDownloadRequest() *events.Hook      { return app.onFileDownloadRequest }
func (app *App) OnFileUploadRequest() *events.Hook        { return app.onFileUploadRequest }
func (app *App) OnFileDeleteRequest() *events.Hook        { return app.onFileDeleteRequest }
func (app *App) OnBackupListRequest() *events.Hook        { return app.onBackupListRequest }
func (app *App) OnBackupCreateRequest() *events.Hook      { return app.onBackupCreateRequest }
func (app *App) OnBackupRestoreRequest() *events.Hook     { return app.onBackupRestoreRequest }
func (app *App) OnBackupDeleteRequest() *events.Hook      { return app.onBackupDeleteRequest }
func (app *App) OnBackupDownloadRequest() *events.Hook    { return app.onBackupDownloadRequest }
func (app *App) OnLogsListRequest() *events.Hook          { return app.onLogsListRequest }
func (app *App) OnAPIKeyRequest() *events.Hook            { return app.onAPIKeyRequest }
func (app *App) OnSearchRequest() *events.Hook            { return app.onSearchRequest }
func (app *App) OnFTSIndexRequest() *events.Hook          { return app.onFTSIndexRequest }
func (app *App) OnJobRequest() *events.Hook               { return app.onJobRequest }

// ---------------------------------------------------------------------------
// Internal migrations
// ---------------------------------------------------------------------------

func (app *App) registerInternalMigrations() {
	// Load all migrations from the migrations package (file-based Go migrations)
	for _, m := range migrations.All() {
		app.migrations.Add(m)
	}
}

func (app *App) bindPlugin(p *plugin.Plugin) {
	if p == nil || p.ID == "" {
		return
	}
	if _, exists := app.boundPlugins[p.ID]; exists {
		return
	}
	app.boundPlugins[p.ID] = struct{}{}

	bind := func(hook *events.Hook, handler events.Handler) {
		handler.Priority = p.Priority
		original := handler.Func
		handler.Func = func(e events.Event) error {
			if !p.Enabled {
				return e.Next()
			}
			if original == nil {
				return e.Next()
			}
			return original(e)
		}
		hook.Bind(handler)
	}

	for hookName, handler := range p.Hooks {
		switch hookName {
		case "onBootstrap":
			bind(app.OnBootstrap(), handler)
		case "onServe":
			bind(app.OnServe(), handler)
		case "onTerminate":
			bind(app.OnTerminate(), handler)
		case "onRecordCreate":
			bind(app.OnRecordCreate(), handler)
		case "onRecordUpdate":
			bind(app.OnRecordUpdate(), handler)
		case "onRecordDelete":
			bind(app.OnRecordDelete(), handler)
		case "onCollectionCreate":
			bind(app.OnCollectionCreate(), handler)
		case "onCollectionUpdate":
			bind(app.OnCollectionUpdate(), handler)
		case "onCollectionDelete":
			bind(app.OnCollectionDelete(), handler)
		case "onCollectionsImportRequest":
			bind(app.OnCollectionsImportRequest(), handler)
		case "onSettingsListRequest":
			bind(app.OnSettingsListRequest(), handler)
		case "onSettingsUpdateRequest":
			bind(app.OnSettingsUpdateRequest(), handler)
		case "onBatchRequest":
			bind(app.OnBatchRequest(), handler)
		case "onRecordsListRequest":
			bind(app.OnRecordsListRequest(), handler)
		case "onRecordViewRequest":
			bind(app.OnRecordViewRequest(), handler)
		case "onRecordCreateRequest":
			bind(app.OnRecordCreateRequest(), handler)
		case "onRecordUpdateRequest":
			bind(app.OnRecordUpdateRequest(), handler)
		case "onRecordDeleteRequest":
			bind(app.OnRecordDeleteRequest(), handler)
		case "onAdminUserRequest":
			bind(app.OnAdminUserRequest(), handler)
		case "onCollectionRequest":
			bind(app.OnCollectionRequest(), handler)
		case "onAdminAuthRequest":
			bind(app.OnAdminAuthRequest(), handler)
		case "onRecordAuthRequest":
			bind(app.OnRecordAuthRequest(), handler)
		case "onRealtimeRequest":
			bind(app.OnRealtimeRequest(), handler)
		case "onFileDownloadRequest":
			bind(app.OnFileDownloadRequest(), handler)
		case "onFileUploadRequest":
			bind(app.OnFileUploadRequest(), handler)
		case "onFileDeleteRequest":
			bind(app.OnFileDeleteRequest(), handler)
		case "onBackupListRequest":
			bind(app.OnBackupListRequest(), handler)
		case "onBackupCreateRequest":
			bind(app.OnBackupCreateRequest(), handler)
		case "onBackupRestoreRequest":
			bind(app.OnBackupRestoreRequest(), handler)
		case "onBackupDeleteRequest":
			bind(app.OnBackupDeleteRequest(), handler)
		case "onBackupDownloadRequest":
			bind(app.OnBackupDownloadRequest(), handler)
		case "onLogsListRequest":
			bind(app.OnLogsListRequest(), handler)
		case "onAPIKeyRequest":
			bind(app.OnAPIKeyRequest(), handler)
		case "onSearchRequest":
			bind(app.OnSearchRequest(), handler)
		case "onFTSIndexRequest":
			bind(app.OnFTSIndexRequest(), handler)
		case "onJobRequest":
			bind(app.OnJobRequest(), handler)
		case "onAuthLogin":
			bind(app.OnAuthLogin(), handler)
		case "onAuthRefresh":
			bind(app.OnAuthRefresh(), handler)
		case "onRealtimeConnect":
			bind(app.OnRealtimeConn(), handler)
		}
	}

	if len(p.Middlewares) > 0 || len(p.Routes) > 0 {
		app.OnServe().Bind(events.Handler{
			ID:       "plugin:" + p.ID + ":serve-extensions",
			Priority: p.Priority,
			Func: func(e events.Event) error {
				serveEvent, ok := e.(*events.ServeEvent)
				if !ok {
					return e.Next()
				}
				if len(p.Middlewares) > 0 {
					wrappedMiddlewares := make([]func(http.Handler) http.Handler, 0, len(p.Middlewares))
					for _, mw := range p.Middlewares {
						middlewareFn := mw
						wrappedMiddlewares = append(wrappedMiddlewares, func(next http.Handler) http.Handler {
							return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
								if !p.Enabled || middlewareFn == nil {
									next.ServeHTTP(w, r)
									return
								}
								middlewareFn(next).ServeHTTP(w, r)
							})
						})
					}
					serveEvent.Router.Use(wrappedMiddlewares...)
				}
				for _, route := range p.Routes {
					route := route
					wrappedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if !p.Enabled || route.Handler == nil {
							http.NotFound(w, r)
							return
						}
						route.Handler.ServeHTTP(w, r)
					})
					if route.Method != "" {
						serveEvent.Router.Method(route.Method, route.Pattern, wrappedHandler)
					} else {
						serveEvent.Router.Handle(route.Pattern, wrappedHandler)
					}
				}
				return e.Next()
			},
		})
	}
}

// wireWebhooks forwards record and collection lifecycle events to configured
// webhook endpoints. Fire is non-blocking; delivery happens on worker
// goroutines so request latency is unaffected.
func (app *App) wireWebhooks() {
	bindRecord := func(hook *events.Hook, eventName string) {
		hook.BindFunc(func(e events.Event) error {
			if re, ok := e.(*events.RecordEvent); ok {
				app.webhookService.Fire(context.Background(), eventName, re.CollectionName, re.Record)
			}
			return e.Next()
		})
	}
	bindRecord(app.OnRecordCreate(), "record.create")
	bindRecord(app.OnRecordUpdate(), "record.update")
	bindRecord(app.OnRecordDelete(), "record.delete")

	bindCollection := func(hook *events.Hook, eventName string) {
		hook.BindFunc(func(e events.Event) error {
			if ce, ok := e.(*events.CollectionEvent); ok {
				app.webhookService.Fire(context.Background(), eventName, ce.CollectionName, map[string]any{
					"id":   ce.CollectionID,
					"name": ce.CollectionName,
				})
			}
			return e.Next()
		})
	}
	bindCollection(app.OnCollectionCreate(), "collection.create")
	bindCollection(app.OnCollectionUpdate(), "collection.update")
	bindCollection(app.OnCollectionDelete(), "collection.delete")
}

// wirePluginHooks connects app hooks to registered Go plugin handlers.
func (app *App) wirePluginHooks() {
	if app.pluginRegistry == nil {
		return
	}
	for _, p := range app.pluginRegistry.List() {
		app.bindPlugin(p)
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

	// Request hooks
	requestHooks := []struct {
		hook *events.Hook
		name string
	}{
		{app.OnCollectionsImportRequest(), "onCollectionsImportRequest"},
		{app.OnSettingsListRequest(), "onSettingsListRequest"},
		{app.OnSettingsUpdateRequest(), "onSettingsUpdateRequest"},
		{app.OnBatchRequest(), "onBatchRequest"},
		{app.OnRecordsListRequest(), "onRecordsListRequest"},
		{app.OnRecordViewRequest(), "onRecordViewRequest"},
		{app.OnRecordCreateRequest(), "onRecordCreateRequest"},
		{app.OnRecordUpdateRequest(), "onRecordUpdateRequest"},
		{app.OnRecordDeleteRequest(), "onRecordDeleteRequest"},
		{app.OnAdminUserRequest(), "onAdminUserRequest"},
		{app.OnCollectionRequest(), "onCollectionRequest"},
		{app.OnAdminAuthRequest(), "onAdminAuthRequest"},
		{app.OnRecordAuthRequest(), "onRecordAuthRequest"},
		{app.OnRealtimeRequest(), "onRealtimeRequest"},
		{app.OnFileDownloadRequest(), "onFileDownloadRequest"},
		{app.OnFileUploadRequest(), "onFileUploadRequest"},
		{app.OnFileDeleteRequest(), "onFileDeleteRequest"},
		{app.OnBackupListRequest(), "onBackupListRequest"},
		{app.OnBackupCreateRequest(), "onBackupCreateRequest"},
		{app.OnBackupRestoreRequest(), "onBackupRestoreRequest"},
		{app.OnBackupDeleteRequest(), "onBackupDeleteRequest"},
		{app.OnBackupDownloadRequest(), "onBackupDownloadRequest"},
		{app.OnLogsListRequest(), "onLogsListRequest"},
		{app.OnAPIKeyRequest(), "onAPIKeyRequest"},
		{app.OnSearchRequest(), "onSearchRequest"},
		{app.OnFTSIndexRequest(), "onFTSIndexRequest"},
		{app.OnJobRequest(), "onJobRequest"},
	}
	for _, item := range requestHooks {
		hookName := item.name
		item.hook.BindFunc(func(e events.Event) error {
			fireJS(hookName, e)
			return e.Next()
		})
	}

	log.Debug().Msg("JS plugin hooks wired to app events")
}
