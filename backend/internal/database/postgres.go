package database

import (
	"context"
	"fmt"
	"time"

	"github.com/gresbase/gresbase/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// DB wraps the PostgreSQL connection pool.
type DB struct {
	Pool     *pgxpool.Pool
	cfg      *config.Config
	embedded *EmbeddedPostgres // non-nil when using embedded PostgreSQL
}

// New creates a new database connection, optionally starting an embedded PostgreSQL.
func New(cfg *config.Config) (*DB, error) {
	var connStr string
	var embedded *EmbeddedPostgres

	if cfg.DatabaseURL != "" {
		connStr = cfg.DatabaseURL
	} else {
		// Start embedded PostgreSQL — zero dependency mode
		log.Info().Msg("No DATABASE_URL set — starting embedded PostgreSQL (single-binary mode)")

		dataDir := cfg.DataDir
		if dataDir == "" {
			dataDir = "./gresbase_data"
		}

		embCfg := DefaultEmbeddedConfig(dataDir)
		if cfg.EmbeddedPort > 0 {
			embCfg.Port = cfg.EmbeddedPort
		}

		embedded = NewEmbeddedPostgres(embCfg)
		if err := embedded.Start(); err != nil {
			return nil, fmt.Errorf("embedded PostgreSQL failed: %w", err)
		}

		connStr = embedded.ConnectionString()
	}

	poolCfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		if embedded != nil {
			embedded.Stop()
		}
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}

	poolCfg.MaxConns = int32(cfg.DatabaseMaxOpenConns)
	poolCfg.MinConns = int32(cfg.DatabaseMaxIdleConns)
	poolCfg.MaxConnIdleTime = cfg.DatabaseMaxIdleTime

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		if embedded != nil {
			embedded.Stop()
		}
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Verify connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		if embedded != nil {
			embedded.Stop()
		}
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	db := &DB{Pool: pool, cfg: cfg, embedded: embedded}

	if embedded != nil {
		log.Info().Int("port", embedded.port).Msg("Connected to embedded PostgreSQL ✓")
	} else {
		log.Info().Msg("Connected to PostgreSQL ✓")
	}

	return db, nil
}

// Close closes the database connection pool and stops embedded PostgreSQL if running.
func (db *DB) Close() {
	if db.Pool != nil {
		db.Pool.Close()
	}
	if db.embedded != nil {
		db.embedded.Stop()
	}
}

// Exec executes a query without returning rows.
func (db *DB) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := db.Pool.Exec(ctx, sql, args...)
	return err
}

// Query executes a query returning rows.
func (db *DB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return db.Pool.Query(ctx, sql, args...)
}

// QueryRow executes a query returning a single row.
func (db *DB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return db.Pool.QueryRow(ctx, sql, args...)
}

// Begin starts a new transaction.
func (db *DB) Begin(ctx context.Context) (Tx, error) {
	return db.Pool.Begin(ctx)
}

// Tx is an alias for pgx Tx.
type Tx = pgx.Tx

// RunInTransaction executes the given function within a database transaction.
func (db *DB) RunInTransaction(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Ping verifies the database connection.
func (db *DB) Ping(ctx context.Context) error {
	return db.Pool.Ping(ctx)
}

// IsEmbedded returns whether the DB is using an embedded PostgreSQL instance.
func (db *DB) IsEmbedded() bool {
	return db.embedded != nil
}

// EmbeddedInfo returns info about the embedded instance, or nil if using external.
func (db *DB) EmbeddedInfo() *EmbeddedPostgres {
	return db.embedded
}
