package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/rs/zerolog/log"
)

// defaultEmbeddedMajor is the PostgreSQL major version used for NEW (empty)
// embedded data directories. Existing data directories always keep the major
// recorded in their PG_VERSION file — PostgreSQL cannot start a new major
// against an old cluster, and silently trying would risk the data.
const defaultEmbeddedMajor = 17

// embeddedVersions maps a PostgreSQL major version to the full version the
// embedded-postgres library downloads and runs.
var embeddedVersions = map[int]embeddedpostgres.PostgresVersion{
	18: embeddedpostgres.V18,
	17: embeddedpostgres.V17,
	16: embeddedpostgres.V16,
	15: embeddedpostgres.V15,
	14: embeddedpostgres.V14,
	13: embeddedpostgres.V13,
	12: embeddedpostgres.V12,
	11: embeddedpostgres.V11,
	10: embeddedpostgres.V10,
}

// selectEmbeddedVersion picks the PostgreSQL version to run for dataPath.
//
//   - New/empty data dir (no PG_VERSION file): the current default major.
//   - Existing data dir: the major recorded in PG_VERSION, even if it differs
//     from the default — the data directory pins the version (pinned=true).
//   - Unreadable/unparseable PG_VERSION or a major this build cannot run:
//     fail closed with an error instead of booting a mismatched major
//     against the cluster.
func selectEmbeddedVersion(dataPath string) (version embeddedpostgres.PostgresVersion, pinned bool, err error) {
	raw, readErr := os.ReadFile(filepath.Join(dataPath, "PG_VERSION"))
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return embeddedVersions[defaultEmbeddedMajor], false, nil
		}
		return "", false, fmt.Errorf("read PG_VERSION in %s: %w", dataPath, readErr)
	}

	// PG_VERSION holds the major version, e.g. "16\n" (or "9.6" historically).
	text := strings.TrimSpace(string(raw))
	major, parseErr := strconv.Atoi(strings.SplitN(text, ".", 2)[0])
	if parseErr != nil || major <= 0 {
		return "", false, fmt.Errorf("data directory %s has an unrecognized PG_VERSION %q; refusing to guess a PostgreSQL version", dataPath, text)
	}

	v, ok := embeddedVersions[major]
	if !ok {
		return "", false, fmt.Errorf("data directory %s was initialized with PostgreSQL %d, which this build cannot run; use an external DATABASE_URL or migrate the data directory", dataPath, major)
	}
	return v, major != defaultEmbeddedMajor, nil
}

// EmbeddedPostgres manages an embedded PostgreSQL instance using the
// well-tested embedded-postgres library. Unlike the previous approach,
// this properly downloads pre-built PostgreSQL binaries per-platform,
// manages lifecycle, and does NOT require sudo or system package managers.
// safePort converts a configured TCP port to uint32, clamping to the valid
// 1..65535 range so the conversion cannot overflow or yield an invalid port.
func safePort(p int) uint32 {
	if p < 1 || p > 65535 {
		return 5433 // default embedded port
	}
	return uint32(p)
}

type EmbeddedPostgres struct {
	mu        sync.Mutex
	ep        *embeddedpostgres.EmbeddedPostgres
	dataDir   string
	port      int
	user      string
	password  string
	dbName    string
	srvParams map[string]string
	running   bool
}

// EmbeddedConfig configures the embedded PostgreSQL instance.
type EmbeddedConfig struct {
	DataDir  string // where to store PG data
	Port     int    // port (default: 5433)
	User     string // PG user
	Password string // PG password
	DBName   string // database name
	// ServerParameters are extra postgresql run-time parameters passed to the
	// server at start (-c key=value). Used e.g. to set wal_level=logical for
	// WAL-based realtime change capture.
	ServerParameters map[string]string
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
		dataDir:   cfg.DataDir,
		port:      cfg.Port,
		user:      cfg.User,
		password:  cfg.Password,
		dbName:    cfg.DBName,
		srvParams: cfg.ServerParameters,
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

	// Ensure directories exist (including the bin/ subdirectory for PG binary extraction)
	for _, dir := range []string{runtimePath, dataPath, filepath.Join(runtimePath, "bin")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Pick the PostgreSQL version: new data dirs get the current default
	// major; existing data dirs pin the major they were initialized with.
	version, pinned, err := selectEmbeddedVersion(dataPath)
	if err != nil {
		return fmt.Errorf("embedded PostgreSQL version selection: %w", err)
	}
	if pinned {
		log.Info().
			Str("version", string(version)).
			Int("default_major", defaultEmbeddedMajor).
			Str("data_dir", dataPath).
			Msg("Existing data directory pins the embedded PostgreSQL major; keeping it instead of the new default (no automatic major upgrades)")
	}

	log.Info().
		Int("port", ep.port).
		Str("version", string(version)).
		Str("data_dir", dataPath).
		Msg("Starting embedded PostgreSQL (development mode)")

	// Configure embedded-postgres with proper settings.
	// The library extracts the txz archive to BinariesPath, and the archive
	// root already contains bin/ lib/ share/ directories.
	// So we point BinariesPath at the same level as RuntimePath, and the
	// library will create bin/ inside it with the postgres binary.
	epCfg := embeddedpostgres.DefaultConfig().
		Version(version).
		Port(safePort(ep.port)).
		Username(ep.user).
		Password(ep.password).
		Database(ep.dbName).
		RuntimePath(runtimePath).
		DataPath(dataPath).
		BinariesPath(runtimePath).
		StartTimeout(30 * time.Second).
		Logger(log.Logger)
	if len(ep.srvParams) > 0 {
		epCfg = epCfg.StartParameters(ep.srvParams)
	}
	embeddedPg := embeddedpostgres.NewDatabase(epCfg)

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
