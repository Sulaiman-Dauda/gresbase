package cli

import (
	"text/tabwriter"

	"github.com/gresbase/gresbase"
	"github.com/spf13/cobra"
)

// migrationsCommand manages collection schema migration files
// (gb_migrations/) — the PocketBase-style git-trackable snapshots of user
// collection schemas. Distinct from `gresbase migrate`, which runs Gresbase's
// own internal system migrations.
func migrationsCommand(gb *gresbase.Gresbase) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrations",
		Short: "Collection schema migration files (gb_migrations)",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "snapshot [name]",
		Short: "Write a migration file with the full current snapshot of all non-system collections (marked applied)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := gb.Bootstrap(); err != nil {
				return err
			}
			name := "snapshot"
			if len(args) > 0 {
				name = args[0]
			}
			file, err := gb.App().SchemaMigrations().Snapshot(cmd.Context(), name)
			if err != nil {
				return err
			}
			cmd.Printf("Wrote %s (marked applied)\n", file)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List migration files with applied/pending status",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := gb.Bootstrap(); err != nil {
				return err
			}
			statuses, err := gb.App().SchemaMigrations().List(cmd.Context())
			if err != nil {
				return err
			}
			if len(statuses) == 0 {
				cmd.Printf("No migration files in %s\n", gb.App().SchemaMigrations().Dir())
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 2, 4, 2, ' ', 0)
			defer w.Flush()
			_, _ = w.Write([]byte("STATUS\tAPPLIED AT\tFILE\n")) // best-effort write to stdout
			for _, s := range statuses {
				status, appliedAt := "pending", "-"
				if s.Applied {
					status = "applied"
					appliedAt = s.AppliedAt.Format("2006-01-02 15:04:05")
				}
				_, _ = w.Write([]byte(status + "\t" + appliedAt + "\t" + s.Filename + "\n")) // best-effort write to stdout
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "apply",
		Short: "Apply pending migration files without starting the server (for CI/deploy)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Bootstrap itself applies pending collection schema migrations
			// fail-closed; a bad file makes this command exit non-zero with
			// the filename in the error.
			if err := gb.Bootstrap(); err != nil {
				return err
			}
			applied := gb.App().SchemaMigrations().AppliedThisRun()
			if len(applied) == 0 {
				cmd.Println("No pending collection schema migrations")
				return nil
			}
			for _, name := range applied {
				cmd.Printf("Applied: %s\n", name)
			}
			return nil
		},
	})

	return cmd
}
