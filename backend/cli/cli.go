// Package cli provides the command-line interface for Gresbase.
package cli

import (
	"os"
	"strings"

	"github.com/gresbase/gresbase"
	"github.com/gresbase/gresbase/internal/sdkgen"
	"github.com/spf13/cobra"
)

// Execute runs the root command using the library entrypoint.
func Execute() error {
	app := gresbase.New()

	// Register all commands (serve, superuser, version + extras)
	app.Root().AddCommand(serveCommand(app))
	app.Root().AddCommand(superuserCommand(app))
	app.Root().AddCommand(migrateCommand(app))
	app.Root().AddCommand(migrationsCommand(app))
	app.Root().AddCommand(backupCommand(app))
	app.Root().AddCommand(infoCommand(app))
	app.Root().AddCommand(typesCommand(app))
	app.Root().AddCommand(hooksCommand(app))
	app.Root().AddCommand(updateCommand())
	app.Root().AddCommand(versionCommand())

	return app.Execute()
}

func serveCommand(gb *gresbase.Gresbase) *cobra.Command {
	var addr string
	var devMode bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the Gresbase server",
		RunE: func(cmd *cobra.Command, args []string) error {
			if addr != "" {
				gb.SetAddr(addr)
			}
			if devMode {
				gb.SetDevMode(true)
			}
			if err := gb.Bootstrap(); err != nil {
				return err
			}
			return gb.App().Serve()
		},
	}
	cmd.Flags().StringVarP(&addr, "addr", "a", "", "Server address (e.g. :9876)")
	cmd.Flags().BoolVar(&devMode, "dev", false, "Enable dev mode")
	return cmd
}

func superuserCommand(gb *gresbase.Gresbase) *cobra.Command {
	var dataDir string
	cmd := &cobra.Command{
		Use:   "superuser",
		Short: "Manage superuser accounts",
	}
	cmd.PersistentFlags().StringVar(&dataDir, "dir", "", "data directory")

	cmd.AddCommand(&cobra.Command{
		Use:   "create [email] [password]",
		Short: "Create a superuser",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if dataDir != "" {
				gb.SetDataDir(dataDir)
			}
			if err := gb.Bootstrap(); err != nil {
				return err
			}
			return gb.App().CreateAdmin(args[0], args[1])
		},
	})
	return cmd
}

func migrateCommand(gb *gresbase.Gresbase) *cobra.Command {
	var (
		dryRun  bool
		steps   int
		reverse bool
	)

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run database migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := gb.Bootstrap(); err != nil {
				return err
			}

			if dryRun {
				cmd.Println("Dry run — showing pending migrations")
				return nil
			}

			if reverse {
				if steps < 1 {
					steps = 1
				}
				reverted, err := gb.App().Migrations().Down(cmd.Context(), steps)
				if err != nil {
					return err
				}
				for _, name := range reverted {
					cmd.Printf("Reverted: %s\n", name)
				}
				return nil
			}

			return gb.App().RunMigrations()
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show pending migrations")
	cmd.Flags().IntVar(&steps, "steps", 0, "Number of steps to roll back")
	cmd.Flags().BoolVar(&reverse, "reverse", false, "Roll back migrations")
	return cmd
}

func backupCommand(gb *gresbase.Gresbase) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Backup and restore",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "create [name]",
		Short: "Create a backup",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := gb.Bootstrap(); err != nil {
				return err
			}
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			info, err := gb.App().Backup().CreateBackup(cmd.Context(), name, true)
			if err != nil {
				return err
			}
			cmd.Printf("Backup created: %s (%d bytes)\n", info.Name, info.Size)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "restore [path]",
		Short: "Restore from a backup",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := gb.Bootstrap(); err != nil {
				return err
			}
			return gb.App().Backup().RestoreBackup(cmd.Context(), args[0])
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List backups",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := gb.Bootstrap(); err != nil {
				return err
			}
			backups, err := gb.App().Backup().ListBackups()
			if err != nil {
				return err
			}
			for _, b := range backups {
				cmd.Printf("%s  %d  %s\n", b.Name, b.Size, b.CreatedAt.Format("2006-01-02 15:04:05"))
			}
			return nil
		},
	})

	return cmd
}

func infoCommand(gb *gresbase.Gresbase) *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show system information",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := gb.Bootstrap(); err != nil {
				return err
			}

			cfg := gb.Config()
			cmd.Println("Gresbase System Information")
			cmd.Println("===========================")
			cmd.Printf("Version:      %s\n", gresbase.Version)
			cmd.Printf("Address:      %s\n", cfg.Addr)
			cmd.Printf("Data dir:     %s\n", cfg.DataDir)
			cmd.Printf("Dev mode:     %v\n", cfg.DevMode)
			cmd.Printf("Log level:    %s\n", cfg.LogLevel)
			cmd.Printf("Storage:      %s\n", cfg.StorageBackend)

			if gb.App().DB() != nil {
				if gb.App().DB().IsEmbedded() {
					cmd.Println("Database:     embedded PostgreSQL")
				} else {
					cmd.Println("Database:     external PostgreSQL")
				}
				cmd.Printf("DB status:    %v\n", gb.App().DB().Ping(cmd.Context()) == nil)
			}

			cmd.Printf("Realtime:     %d clients\n", gb.App().Realtime().ClientCount())
			return nil
		},
	}
}

func typesCommand(gb *gresbase.Gresbase) *cobra.Command {
	var baseURL string
	cmd := &cobra.Command{
		Use:   "types",
		Short: "Generate a typed TypeScript SDK from the live collection schema",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := gb.Bootstrap(); err != nil {
				return err
			}

			url := baseURL
			if url == "" {
				addr := gb.Config().Addr
				if strings.HasPrefix(addr, ":") {
					addr = "localhost" + addr
				}
				url = "http://" + addr + "/api/v1"
			}

			colls, err := gb.App().Collections().ListCollections(cmd.Context())
			if err != nil {
				return err
			}

			cmd.Print(sdkgen.Generate(colls, url))
			return nil
		},
	}
	cmd.Flags().StringVar(&baseURL, "base-url", "", "Base API URL embedded in the generated client (default derived from server addr)")
	return cmd
}

func versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Printf("Gresbase %s\n", gresbase.Version)
			cmd.Printf("  Build: %s\n", gresbase.BuildTime)
			cmd.Printf("  Commit: %s\n", gresbase.GitCommit)
		},
	}
}

// Ensure unused import compiles
var _ = os.Stderr
