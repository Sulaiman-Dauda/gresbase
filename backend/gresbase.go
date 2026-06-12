// Package gresbase is the main library entrypoint.
//
// Gresbase can be used as:
//  1. A standalone server:   ./gresbase serve
//  2. A Go library/framework: import "github.com/gresbase/gresbase"
//
// Example usage as a Go library (see examples/embed for a runnable version):
//
//	package main
//
//	import (
//	    "log"
//	    "net/http"
//
//	    "github.com/gresbase/gresbase"
//	    "github.com/gresbase/gresbase/internal/events"
//	)
//
//	func main() {
//	    gb := gresbase.New()
//
//	    // Real Go hooks — registered before Start(), bound to the live event bus.
//	    gb.OnRecordCreate().BindFunc(func(e events.Event) error {
//	        if re, ok := e.(*events.RecordEvent); ok {
//	            log.Printf("record created in %s: %v", re.CollectionName, re.RecordID)
//	        }
//	        return e.Next()
//	    })
//
//	    // Custom routes alongside the built-in API.
//	    gb.OnServe().BindFunc(func(e events.Event) error {
//	        if se, ok := e.(*events.ServeEvent); ok {
//	            se.Router.Get("/hello", func(w http.ResponseWriter, r *http.Request) {
//	                w.Write([]byte("Hello from a custom Go route!"))
//	            })
//	        }
//	        return e.Next()
//	    })
//
//	    if err := gb.Start(); err != nil {
//	        log.Fatal(err)
//	    }
//	}
package gresbase

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/gresbase/gresbase/internal/api"
	"github.com/gresbase/gresbase/internal/app"
	"github.com/gresbase/gresbase/internal/config"
	"github.com/gresbase/gresbase/internal/events"
	"github.com/gresbase/gresbase/internal/ui"
	"github.com/spf13/cobra"
)

// Version is the current Gresbase version, set at build time.
var Version = "0.3.0"

// BuildTime is set at build time via ldflags.
var BuildTime = "dev"

// GitCommit is set at build time via ldflags.
var GitCommit = "unknown"

// Gresbase is the main application struct.
// It wraps the internal app.App and provides a clean public API.
type Gresbase struct {
	app  *app.App
	cfg  *config.Config
	root *cobra.Command

	// CLI flags (set before Start)
	dataDir string
	devMode bool
	addr    string
}

// New creates a new Gresbase instance with default settings.
// Use NewWithConfig for custom configuration.
//
// Note: The app is not bootstrapped yet. Everything initializes on Start().
func New() *Gresbase {
	return NewWithConfig(nil)
}

// NewWithConfig creates a Gresbase instance with a custom config.
// Pass nil to use defaults.
func NewWithConfig(cfg *config.Config) *Gresbase {
	if cfg == nil {
		cfg = config.Load()
	}

	if cfg.DataDir == "" {
		if dir, err := os.Getwd(); err == nil {
			cfg.DataDir = filepath.Join(dir, "gresbase_data")
		} else {
			cfg.DataDir = "./gresbase_data"
		}
	}

	gb := &Gresbase{
		cfg:     cfg,
		dataDir: cfg.DataDir,
	}

	// Create the underlying app eagerly (lightweight — no DB yet) so hooks
	// registered BEFORE Start()/Bootstrap() bind to the real event bus, as
	// embedding users expect. Heavy initialization is deferred to Bootstrap().
	if a, err := app.New(cfg); err == nil {
		gb.app = a
	}

	// Build CLI
	gb.root = &cobra.Command{
		Use:                "gresbase",
		Short:              "Gresbase — Modern Backend Platform",
		Long:               `Gresbase is an open-source backend with embedded PostgreSQL, realtime engine, file storage, and admin dashboard.`,
		Version:            Version,
		FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true},
		CompletionOptions:  cobra.CompletionOptions{DisableDefaultCmd: true},
	}

	gb.root.PersistentFlags().StringVar(&gb.dataDir, "dir", cfg.DataDir, "data directory")
	gb.root.PersistentFlags().BoolVar(&gb.devMode, "dev", cfg.DevMode, "enable dev mode")
	gb.root.PersistentFlags().StringVar(&gb.addr, "addr", cfg.Addr, "server address")

	// Hide default help command
	gb.root.SetHelpCommand(&cobra.Command{Hidden: true})

	return gb
}

// Start bootstraps and runs the application with default commands.
// Equivalent to calling cli.Execute() from the library.
func (gb *Gresbase) Start() error {
	// Register default commands only if root has no subcommands yet
	if !gb.root.HasSubCommands() {
		gb.root.AddCommand(gb.serveCommand())
		gb.root.AddCommand(gb.superuserCommand())
		gb.root.AddCommand(&cobra.Command{
			Use:   "version",
			Short: "Print version",
			Run: func(cmd *cobra.Command, args []string) {
				fmt.Printf("Gresbase %s\nBuild: %s\nCommit: %s\n", Version, BuildTime, GitCommit)
			},
		})
	}
	return gb.Execute()
}

// Execute runs the CLI. Bootstrap is deferred to commands that need it.
func (gb *Gresbase) Execute() error {
	// Parse flags but don't bootstrap yet — commands that need the DB
	// call Bootstrap themselves via ensureBootstrapped().

	done := make(chan bool, 1)

	go func() {
		sigch := make(chan os.Signal, 1)
		signal.Notify(sigch, os.Interrupt, syscall.SIGTERM)
		<-sigch
		done <- true
	}()

	go func() {
		gb.root.Execute()
		done <- true
	}()

	<-done

	// Graceful shutdown if bootstrapped
	if gb.app != nil {
		gb.app.Shutdown()
	}
	return nil
}

// Bootstrap initializes the application (database, migrations, services).
// Call this before accessing any subsystems. Hooks registered before Bootstrap
// are preserved because the underlying app is created eagerly in New().
func (gb *Gresbase) Bootstrap() error {
	if gb.IsBootstrapped() {
		return nil
	}

	gb.cfg.DataDir = gb.dataDir
	gb.cfg.DevMode = gb.devMode
	if gb.addr != "" {
		gb.cfg.Addr = gb.addr
	}
	if err := gb.cfg.ValidateProduction(); err != nil {
		return fmt.Errorf("invalid production configuration: %w", err)
	}

	a := gb.app
	if a == nil {
		var err error
		a, err = app.New(gb.cfg)
		if err != nil {
			return fmt.Errorf("failed to create app: %w", err)
		}
		gb.app = a
	}

	if err := a.Bootstrap(); err != nil {
		return fmt.Errorf("bootstrap failed: %w", err)
	}

	// Wire the API server (created externally to avoid import cycles)
	server := api.NewServer(a)
	a.SetAPIServer(server)
	a.SetAPIRouter(server)

	return nil
}

// IsBootstrapped returns whether the app has been initialized.
func (gb *Gresbase) IsBootstrapped() bool {
	return gb.app != nil && gb.app.IsBootstrapped()
}

// Root returns the cobra root command for adding custom commands.
func (gb *Gresbase) Root() *cobra.Command {
	return gb.root
}

// ---------------------------------------------------------------------------
// CLI Commands
// ---------------------------------------------------------------------------

func (gb *Gresbase) serveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the Gresbase server",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := gb.Bootstrap(); err != nil {
				return err
			}

			// Print startup banner
			if !gb.cfg.HideStartBanner {
				gb.printBanner()
			}

			return gb.app.Serve()
		},
	}
	return cmd
}

func (gb *Gresbase) superuserCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "superuser",
		Short: "Manage superuser accounts",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "create [email] [password]",
		Short: "Create a superuser",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := gb.Bootstrap(); err != nil {
				return err
			}
			return gb.app.CreateAdmin(args[0], args[1])
		},
	})

	return cmd
}

func (gb *Gresbase) versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Gresbase %s\n", Version)
			fmt.Printf("  Build time: %s\n", BuildTime)
			fmt.Printf("  Git commit: %s\n", GitCommit)
			fmt.Printf("  Has frontend: %v\n", ui.HasEmbeddedFrontend)
		},
	}
}

func (gb *Gresbase) printBanner() {
	cyan := color.New(color.FgCyan).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()

	fmt.Println()
	fmt.Printf("  %s\n", cyan("⚡ Gresbase "+Version))
	fmt.Println("  ─────────────────────────────────────────────")

	dbInfo := green("connected")
	if gb.app.DB().IsEmbedded() {
		dbInfo = green("embedded") + yellow(" (zero-deps mode)")
	}

	fmt.Printf("  %s: %s\n", cyan("Database"), dbInfo)
	fmt.Printf("  %s: %s\n", cyan("Server"), green("http://"+gb.cfg.Addr))
	fmt.Printf("  %s: %s\n", cyan("Dashboard"), green("http://"+gb.cfg.Addr+"/_/"))
	fmt.Printf("  %s: %s\n", cyan("REST API"), green("http://"+gb.cfg.Addr+"/api/v1"))
	fmt.Printf("  %s: %s\n", cyan("Realtime"), green("ws://"+gb.cfg.Addr+"/api/v1/realtime"))
	fmt.Println("  ─────────────────────────────────────────────")
	fmt.Println()

	if gb.cfg.DevMode {
		fmt.Printf("  %s Dev mode enabled — verbose logging\n", yellow("⚠"))
	}

	if gb.cfg.DatabaseURL == "" {
		fmt.Printf("  %s Running with embedded PostgreSQL\n", green("✓"))
		fmt.Printf("  %s No external dependencies required!\n", green("✓"))
	}

	fmt.Println()
}

// ---------------------------------------------------------------------------
// Event Hooks (identical API to Pocketbase)
// ---------------------------------------------------------------------------

// BootstrapEvent is fired after the app is bootstrapped.
type BootstrapEvent = events.BootstrapEvent

// TerminateEvent is fired on app shutdown.
type TerminateEvent = events.TerminateEvent

// ServeEvent is fired when the server starts.
type ServeEvent struct {
	App    *app.App
	Router *http.ServeMux
}

// OnBootstrap registers a hook that fires after bootstrapping.
func (gb *Gresbase) OnBootstrap() *events.Hook {
	if gb.app == nil {
		// Return a no-op hook if not bootstrapped yet
		return events.NewHook()
	}
	return gb.app.OnBootstrap()
}

// OnTerminate registers a hook that fires on shutdown.
func (gb *Gresbase) OnTerminate() *events.Hook {
	if gb.app == nil {
		return events.NewHook()
	}
	return gb.app.OnTerminate()
}

// OnServe registers a hook that fires when serving starts.
func (gb *Gresbase) OnServe() *events.Hook {
	if gb.app == nil {
		return events.NewHook()
	}
	return gb.app.OnServe()
}

// OnRecordCreate fires when a record is created.
func (gb *Gresbase) OnRecordCreate() *events.Hook {
	if gb.app == nil {
		return events.NewHook()
	}
	return gb.app.OnRecordCreate()
}

// OnRecordUpdate fires when a record is updated.
func (gb *Gresbase) OnRecordUpdate() *events.Hook {
	if gb.app == nil {
		return events.NewHook()
	}
	return gb.app.OnRecordUpdate()
}

// OnRecordDelete fires when a record is deleted.
func (gb *Gresbase) OnRecordDelete() *events.Hook {
	if gb.app == nil {
		return events.NewHook()
	}
	return gb.app.OnRecordDelete()
}

// OnAuthLogin fires on successful login.
func (gb *Gresbase) OnAuthLogin() *events.Hook {
	if gb.app == nil {
		return events.NewHook()
	}
	return gb.app.OnAuthLogin()
}

// OnCollectionCreate fires when a collection is created.
func (gb *Gresbase) OnCollectionCreate() *events.Hook {
	if gb.app == nil {
		return events.NewHook()
	}
	return gb.app.OnCollectionCreate()
}

// OnCollectionUpdate fires when a collection is updated.
func (gb *Gresbase) OnCollectionUpdate() *events.Hook {
	if gb.app == nil {
		return events.NewHook()
	}
	return gb.app.OnCollectionUpdate()
}

// OnCollectionDelete fires when a collection is deleted.
func (gb *Gresbase) OnCollectionDelete() *events.Hook {
	if gb.app == nil {
		return events.NewHook()
	}
	return gb.app.OnCollectionDelete()
}

// ---------------------------------------------------------------------------
// App accessors
// ---------------------------------------------------------------------------

// App returns the underlying app.App instance for advanced usage.
func (gb *Gresbase) App() *app.App { return gb.app }

// Config returns the current configuration.
func (gb *Gresbase) Config() *config.Config { return gb.cfg }

// DataDir returns the data directory.
func (gb *Gresbase) DataDir() string { return gb.dataDir }

// SetDataDir sets the data directory.
func (gb *Gresbase) SetDataDir(dir string) { gb.dataDir = dir; gb.cfg.DataDir = dir }

// SetDevMode enables/disables dev mode.
func (gb *Gresbase) SetDevMode(dev bool) { gb.devMode = dev; gb.cfg.DevMode = dev }

// SetAddr sets the listen address.
func (gb *Gresbase) SetAddr(addr string) { gb.addr = addr; gb.cfg.Addr = addr }

// HideStartBanner suppresses the startup banner.
func (gb *Gresbase) HideStartBanner() { gb.cfg.HideStartBanner = true }

// ---------------------------------------------------------------------------
// SDK-style convenience methods
// ---------------------------------------------------------------------------

// Ping checks if the database is reachable.
func (gb *Gresbase) Ping(ctx context.Context) error {
	if gb.app == nil || gb.app.DB() == nil {
		return fmt.Errorf("not bootstrapped")
	}
	return gb.app.DB().Ping(ctx)
}

// Ensure default imports
var _ = time.Now
var _ = http.DefaultServeMux
