package database

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/gresbase/gresbase/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// safeInt32 clamps an int to the int32 range so the conversion used for pgx
// pool sizing cannot overflow regardless of misconfiguration.
func safeInt32(v int) int32 {
	switch {
	case v > math.MaxInt32:
		return math.MaxInt32
	case v < math.MinInt32:
		return math.MinInt32
	default:
		return int32(v)
	}
}

// DB wraps the PostgreSQL connection pool.
type DB struct {
	Pool     *pgxpool.Pool
	cfg      *config.Config
	embedded *EmbeddedPostgres // non-nil when using embedded PostgreSQL

	// readPool, when non-nil, serves replica-safe read paths (record lists,
	// aggregations, relation expansion). Writes, transactional reads, and
	// reads that feed rule checks before writes always use the primary.
	readPool *pgxpool.Pool

	vectorOnce      sync.Once
	vectorAvailable bool

	connString string // retained for opening dedicated LISTEN connections
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
		if cfg.RealtimeWALEnabled {
			// WAL change capture needs logical decoding; the embedded server
			// is ours, so configure it transparently.
			embCfg.ServerParameters = map[string]string{
				"wal_level":             "logical",
				"max_replication_slots": "8",
				"max_wal_senders":       "8",
			}
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
			_ = embedded.Stop() // best-effort shutdown on init failure
		}
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}

	poolCfg.MaxConns = safeInt32(cfg.DatabaseMaxOpenConns)
	poolCfg.MinConns = safeInt32(cfg.DatabaseMaxIdleConns)
	poolCfg.MaxConnIdleTime = cfg.DatabaseMaxIdleTime

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		if embedded != nil {
			_ = embedded.Stop() // best-effort shutdown on init failure
		}
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Verify connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		if embedded != nil {
			_ = embedded.Stop() // best-effort shutdown on init failure
		}
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	db := &DB{Pool: pool, cfg: cfg, embedded: embedded, connString: connStr}

	if embedded != nil {
		log.Info().Int("port", embedded.port).Msg("Connected to embedded PostgreSQL ✓")
	} else {
		log.Info().Msg("Connected to PostgreSQL ✓")
	}

	// Optional read replica. Replica lag means recently written rows may be
	// briefly missing from routed reads, so failure to connect degrades to
	// the primary instead of blocking boot.
	if cfg.DatabaseReplicaURL != "" {
		if embedded != nil {
			log.Warn().Msg("DATABASE_REPLICA_URL ignored in embedded PostgreSQL mode")
		} else if readPool, err := newReplicaPool(cfg); err != nil {
			log.Warn().Err(err).Msg("Read replica unavailable — all reads stay on the primary")
		} else {
			db.readPool = readPool
			log.Info().Msg("Connected to PostgreSQL read replica ✓ (lists, aggregations, and expansion route here)")
		}
	}

	return db, nil
}

func newReplicaPool(cfg *config.Config) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseReplicaURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse replica URL: %w", err)
	}
	poolCfg.MaxConns = safeInt32(cfg.DatabaseMaxOpenConns)
	poolCfg.MinConns = safeInt32(cfg.DatabaseMaxIdleConns)
	poolCfg.MaxConnIdleTime = cfg.DatabaseMaxIdleTime

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create replica pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping replica: %w", err)
	}
	return pool, nil
}

// Close closes the database connection pool and stops embedded PostgreSQL if running.
func (db *DB) Close() {
	if db.Pool != nil {
		db.Pool.Close()
	}
	if db.readPool != nil {
		db.readPool.Close()
	}
	if db.embedded != nil {
		_ = db.embedded.Stop() // best-effort shutdown
	}
}

// Exec executes a query without returning rows.
func (db *DB) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := db.ExecResult(ctx, sql, args...)
	return err
}

// ExecResult executes a query and returns its command tag.
func (db *DB) ExecResult(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if tx, ok := TxFromContext(ctx); ok {
		return tx.Exec(ctx, sql, args...)
	}
	return db.Pool.Exec(ctx, sql, args...)
}

// Query executes a query returning rows.
func (db *DB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if tx, ok := TxFromContext(ctx); ok {
		return tx.Query(ctx, sql, args...)
	}
	return db.Pool.Query(ctx, sql, args...)
}

// QueryRow executes a query returning a single row.
func (db *DB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if tx, ok := TxFromContext(ctx); ok {
		return tx.QueryRow(ctx, sql, args...)
	}
	return db.Pool.QueryRow(ctx, sql, args...)
}

// ReadQuery executes a read on the replica when one is configured. Inside a
// transaction it stays on the transaction; without a replica it falls back to
// the primary. Use only for replica-safe reads — paths that tolerate replica
// lag and never feed a write decision.
func (db *DB) ReadQuery(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if tx, ok := TxFromContext(ctx); ok {
		return tx.Query(ctx, sql, args...)
	}
	if db.readPool != nil {
		return db.readPool.Query(ctx, sql, args...)
	}
	return db.Pool.Query(ctx, sql, args...)
}

// ReadQueryRow is the single-row counterpart of ReadQuery.
func (db *DB) ReadQueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if tx, ok := TxFromContext(ctx); ok {
		return tx.QueryRow(ctx, sql, args...)
	}
	if db.readPool != nil {
		return db.readPool.QueryRow(ctx, sql, args...)
	}
	return db.Pool.QueryRow(ctx, sql, args...)
}

// HasReadReplica reports whether a read replica pool is active.
func (db *DB) HasReadReplica() bool { return db.readPool != nil }

// Begin starts a new transaction.
func (db *DB) Begin(ctx context.Context) (Tx, error) {
	return db.Pool.Begin(ctx)
}

// Tx is an alias for pgx Tx.
type Tx = pgx.Tx

// RunInTransaction executes the given function within a database transaction.
func (db *DB) RunInTransaction(ctx context.Context, fn func(tx pgx.Tx) error) error {
	if tx, ok := TxFromContext(ctx); ok {
		return fn(tx)
	}
	return db.RunInTransactionContext(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		return fn(tx)
	})
}

// RunInTransactionContext executes the given function within a database transaction
// and propagates the transaction through the returned context.
func (db *DB) RunInTransactionContext(ctx context.Context, fn func(txCtx context.Context, tx pgx.Tx) error) error {
	if tx, ok := TxFromContext(ctx); ok {
		return fn(ctx, tx)
	}

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after commit

	txCtx := ContextWithTx(ctx, tx)
	state, _ := txStateFromContext(txCtx)

	if err := fn(txCtx, tx); err != nil {
		if state != nil {
			for _, callback := range state.drainAfterRollback() {
				callback()
			}
		}
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		if state != nil {
			for _, callback := range state.drainAfterRollback() {
				callback()
			}
		}
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	if state != nil {
		for _, callback := range state.drainAfterCommit() {
			callback()
		}
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

// Connect opens a fresh dedicated connection to the same database. Used for
// long-lived LISTEN connections that must not occupy a pool slot.
func (db *DB) Connect(ctx context.Context) (*pgx.Conn, error) {
	return pgx.Connect(ctx, db.connString)
}

// ConnString returns the connection string in use. Used by subsystems that
// need dedicated non-pool connections (e.g. logical replication).
func (db *DB) ConnString() string {
	return db.connString
}

// Notify sends a NOTIFY on the given channel with a text payload, using a
// parameterized pg_notify call so the payload needs no manual escaping.
func (db *DB) Notify(ctx context.Context, channel, payload string) error {
	_, err := db.Pool.Exec(ctx, "SELECT pg_notify($1, $2)", channel, payload)
	return err
}

// HasVectorSupport reports whether the pgvector extension is available on this
// PostgreSQL server (installable). The result is cached after the first check.
func (db *DB) HasVectorSupport(ctx context.Context) bool {
	db.vectorOnce.Do(func() {
		var available bool
		err := db.Pool.QueryRow(ctx,
			"SELECT EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'vector')").Scan(&available)
		if err != nil {
			log.Debug().Err(err).Msg("pgvector availability check failed")
			return
		}
		db.vectorAvailable = available
	})
	return db.vectorAvailable
}
