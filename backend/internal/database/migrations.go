package database

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog/log"
)

// Migration represents a single database migration.
type Migration struct {
	Name string
	Up   string
	Down string
}

// Migrations is a sorted list of migrations.
type Migrations struct {
	list []*Migration
}

// NewMigrations creates a Migrations list.
func NewMigrations() *Migrations {
	m := &Migrations{}
	return m
}

// Add adds a migration to the list.
func (m *Migrations) Add(migration *Migration) {
	m.list = append(m.list, migration)
	sort.Slice(m.list, func(i, j int) bool {
		return m.list[i].Name < m.list[j].Name
	})
}

// Items returns all migrations.
func (m *Migrations) Items() []*Migration {
	return m.list
}

// MigrationRunner manages migration execution.
type MigrationRunner struct {
	db         *DB
	migrations *Migrations
	tableName  string
}

// NewMigrationRunner creates a new MigrationRunner.
func NewMigrationRunner(db *DB, migrations *Migrations) *MigrationRunner {
	return &MigrationRunner{
		db:         db,
		migrations: migrations,
		tableName:  "_gresbase_migrations",
	}
}

// ensureMigrationTable creates the migrations tracking table.
func (r *MigrationRunner) ensureMigrationTable(ctx context.Context) error {
	sql := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			name       VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			checksum   VARCHAR(64)
		)
	`, r.tableName)

	return r.db.Exec(ctx, sql)
}

// Up runs all pending migrations.
func (r *MigrationRunner) Up(ctx context.Context) ([]string, error) {
	if err := r.ensureMigrationTable(ctx); err != nil {
		return nil, fmt.Errorf("failed to ensure migrations table: %w", err)
	}

	var applied []string

	err := r.db.RunInTransaction(ctx, func(tx pgx.Tx) error {
		for _, m := range r.migrations.Items() {
			alreadyApplied, err := r.isApplied(ctx, tx, m.Name)
			if err != nil {
				return err
			}
			if alreadyApplied {
				continue
			}

			log.Info().Str("migration", m.Name).Msg("Applying migration")

			if m.Up != "" {
				if _, err := tx.Exec(ctx, m.Up); err != nil {
					return fmt.Errorf("migration %s failed: %w", m.Name, err)
				}
			}

			if err := r.markApplied(ctx, tx, m.Name); err != nil {
				return err
			}

			applied = append(applied, m.Name)
		}
		return nil
	})

	if err != nil {
		return applied, err
	}

	return applied, nil
}

// Down reverts the last N migrations.
func (r *MigrationRunner) Down(ctx context.Context, steps int) ([]string, error) {
	if err := r.ensureMigrationTable(ctx); err != nil {
		return nil, fmt.Errorf("failed to ensure migrations table: %w", err)
	}

	var reverted []string

	err := r.db.RunInTransaction(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, fmt.Sprintf(
			"SELECT name FROM %s ORDER BY applied_at DESC LIMIT $1", r.tableName), steps)
		if err != nil {
			return err
		}
		defer rows.Close()

		var names []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			names = append(names, name)
		}

		for _, name := range names {
			for _, m := range r.migrations.Items() {
				if m.Name != name {
					continue
				}

				log.Info().Str("migration", m.Name).Msg("Reverting migration")

				if m.Down != "" {
					if _, err := tx.Exec(ctx, m.Down); err != nil {
						return fmt.Errorf("revert migration %s failed: %w", m.Name, err)
					}
				}

				_, err := tx.Exec(ctx, fmt.Sprintf(
					"DELETE FROM %s WHERE name = $1", r.tableName), name)
				if err != nil {
					return err
				}

				reverted = append(reverted, name)
			}
		}
		return nil
	})

	return reverted, err
}

func (r *MigrationRunner) isApplied(ctx context.Context, tx pgx.Tx, name string) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, fmt.Sprintf(
		"SELECT EXISTS(SELECT 1 FROM %s WHERE name = $1)", r.tableName), name).Scan(&exists)
	return exists, err
}

func (r *MigrationRunner) markApplied(ctx context.Context, tx pgx.Tx, name string) error {
	_, err := tx.Exec(ctx, fmt.Sprintf(
		"INSERT INTO %s (name) VALUES ($1)", r.tableName), name)
	return err
}

// Ensure unused imports compile
var _ = pgconn.CommandTag{}
var _ = fmt.Sprintf

// AsSlice splits SQL by semicolons into individual statements.
func AsSlice(sql string) []string {
	var statements []string
	for _, s := range strings.Split(sql, ";") {
		s = strings.TrimSpace(s)
		if s != "" {
			statements = append(statements, s)
		}
	}
	return statements
}

// Now returns the current UTC time.
func Now() time.Time {
	return time.Now().UTC()
}
