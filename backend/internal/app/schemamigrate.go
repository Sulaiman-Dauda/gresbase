package app

import (
	"context"
	"time"

	"github.com/gresbase/gresbase/internal/events"
	"github.com/gresbase/gresbase/internal/schemamigrate"
	"github.com/rs/zerolog/log"
)

// SchemaMigrations returns the collection schema migration runner
// (gb_migrations). Nil until Bootstrap.
func (app *App) SchemaMigrations() *schemamigrate.Runner { return app.schemaMigrate }

// bindSchemaAutomigrate wires PocketBase-style automigrate: in dev mode,
// every collection create/update/delete made through the dashboard/API writes
// a migration file to gb_migrations/, pre-marked as applied (the live
// database already has the state — the file exists for git history and for
// replaying onto other environments).
//
// The handlers append onto OnCollectionCreate/Update/Delete alongside other
// subscribers (WAL publication sync, webhooks, JS hooks) and always call
// e.Next(); a failed file write is logged, never fatal — the collection
// change itself has already committed.
func (app *App) bindSchemaAutomigrate() {
	record := func(action string) func(events.Event) error {
		return func(e events.Event) error {
			if ce, ok := e.(*events.CollectionEvent); ok {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				var (
					file string
					err  error
				)
				if action == "delete" {
					file, err = app.schemaMigrate.RecordDelete(ctx, ce.CollectionName)
				} else {
					coll, gerr := app.collections.GetCollection(ctx, ce.CollectionID)
					if gerr != nil {
						err = gerr
					} else {
						file, err = app.schemaMigrate.RecordChange(ctx, action, coll)
					}
				}
				cancel()
				if err != nil {
					log.Warn().Err(err).Str("collection", ce.CollectionName).Str("action", action).
						Msg("Automigrate: failed to write collection schema migration file")
				} else if file != "" {
					log.Info().Str("file", file).Msg("Automigrate: wrote collection schema migration")
				}
			}
			return e.Next()
		}
	}
	app.onCollectionCreate.BindFunc(record("create"))
	app.onCollectionUpdate.BindFunc(record("update"))
	app.onCollectionDelete.BindFunc(record("delete"))
}
