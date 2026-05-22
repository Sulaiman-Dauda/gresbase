package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/rs/zerolog/log"
)

// EmbeddedPostgres manages an embedded PostgreSQL instance using the
// well-tested embedded-postgres library. Unlike the previous approach,
// this properly downloads pre-built PostgreSQL binaries per-platform,
// manages lifecycle, and does NOT require sudo or system package managers.
type EmbeddedPostgres struct {
	mu       sync.Mutex
	ep       *embeddedpostgres.EmbeddedPostgres
	dataDir  string
	port     int
	user     string
	password string
	dbName   string
	running  bool
}

// EmbeddedConfig configures the embedded PostgreSQL instance.
type EmbeddedConfig struct {
	DataDir  string // where to store PG data
	Port     int    // port (default: 5433)
	User     string // PG user
	Password string // PG password
	DBName   string // database name
}

// DefaultEmbeddedConfig returns sensible development defaults.
func DefaultEmbeddedConfig(dataDir string) EmbeddedConfig {
	return EmbeddedConfig{
		DataDir:  dataDir,
		Port:     5433,
		User:     "gresbase",
		Password: "gresbase",
		DBName:   "gresbase",
	}
}

// NewEmbeddedPostgres creates a new embedded PostgreSQL manager using
// the proper embedded-postgres library that downloads and manages
// platform-appropriate PostgreSQL binaries.
func NewEmbeddedPostgres(cfg EmbeddedConfig) *EmbeddedPostgres {
	return &EmbeddedPostgres{
		dataDir:  cfg.DataDir,
		port:     cfg.Port,
		user:     cfg.User,
		password: cfg.Password,
		dbName:   cfg.DBName,
	}
}

// Start initializes and starts the embedded PostgreSQL instance.
// Uses the embedded-postgres library which handles binary download,
// initdb, startup, and lifecycle management without sudo or apt-get.
func (ep *EmbeddedPostgres) Start() error {
	ep.mu.Lock()
	defer ep.mu.Unlock()

	if ep.running {
		return nil
	}

	runtimePath := filepath.Join(ep.dataDir, "pg_runtime")
	dataPath := filepath.Join(ep.dataDir, "pg_data")

	// Ensure directories exist
	for _, dir := range []string{runtimePath, dataPath} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	log.Info().
		Int("port", ep.port).
		Str("data_dir", dataPath).
		Msg("Starting embedded PostgreSQL (development mode)")

	// Configure embedded-postgres with proper settings
	embeddedPg := embeddedpostgres.NewDatabase(
		embeddedpostgres.DefaultConfig().
			Port(uint32(ep.port)).
			Username(ep.user).
			Password(ep.password).
			Database(ep.dbName).
			RuntimePath(runtimePath).
			DataPath(dataPath).
			BinariesPath(filepath.Join(runtimePath, "bin")).
			StartTimeout(30 * time.Second).
			Logger(log.Logger),
	)

	ep.ep = embeddedPg

	if err := embeddedPg.Start(); err != nil {
		return fmt.Errorf("embedded PostgreSQL failed to start: %w.\n\n"+
			"TIPS:\n"+
			"  • Set DATABASE_URL to use an external PostgreSQL instead\n"+
			"  • Ensure port %d is free (or set EMBEDDED_PORT env)\n"+
			"  • First launch downloads PostgreSQL (~100MB) - this may take a moment",
			err, ep.port)
	}

	ep.running = true
	log.Info().
		Int("port", ep.port).
		Str("db", ep.dbName).
		Str("user", ep.user).
		Msg("Embedded PostgreSQL ready (dev mode)")
	return nil
}

// ConnectionString returns the PostgreSQL connection URL.
func (ep *EmbeddedPostgres) ConnectionString() string {
	return fmt.Sprintf("postgres://%s:%s@localhost:%d/%s?sslmode=disable",
		ep.user, ep.password, ep.port, ep.dbName)
}

// Stop gracefully shuts down the embedded PostgreSQL instance.
func (ep *EmbeddedPostgres) Stop() error {
	ep.mu.Lock()
	defer ep.mu.Unlock()

	if !ep.running || ep.ep == nil {
		return nil
	}

	log.Info().Msg("Stopping embedded PostgreSQL")
	if err := ep.ep.Stop(); err != nil {
		log.Warn().Err(err).Msg("Error stopping embedded PostgreSQL")
	}
	ep.running = false
	return nil
}

// IsRunning returns whether PostgreSQL is currently running.
func (ep *EmbeddedPostgres) IsRunning() bool {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	return ep.running
}

// EnsureContext provides a context for database operations during startup.
func (ep *EmbeddedPostgres) EnsureContext() context.Context {
	return context.Background()
}
