// Package migrations contains all database migrations.
// Migrations are timestamped and run in order automatically on bootstrap.
// Each migration has an Up and Down function.
package migrations

import (
	"github.com/gresbase/gresbase/internal/database"
)

// All returns all migrations in order.
func All() []*database.Migration {
	return []*database.Migration{
		Migration001,
		Migration002,
		Migration003,
		Migration004,
	}
}
