package api

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"
	"github.com/gresbase/gresbase/internal/app"
	apimw "github.com/gresbase/gresbase/internal/api/middleware"
	"github.com/gresbase/gresbase/internal/ui"
	"github.com/rs/zerolog/log"
)

// Server wraps the HTTP server with optional TLS.
type Server struct {
	app     *app.App
	router  chi.Router
	srv     *http.Server
	h       *Handlers
	mw      *apimw.Middleware
	uiHandler http.Handler
}

// NewServer creates a fully wired API server.
func NewServer(application *app.App) *Server {
	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(middleware.CleanPath)
	r.Use(middleware.Heartbeat("/health"))

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Requested-With"},
		ExposedHeaders:   []string{"Link", "X-Total-Count"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Use(apimw.SecurityHeaders)
	r.Use(apimw.RequestIDMiddleware)

	mw := apimw.NewMiddleware(application)
	h := NewHandlers(application)

	s := &Server{
		app:    application,
		router: r,
		h:      h,
		mw:     mw,
	}

	// Setup embedded UI handler (pre-built frontend or fallback)
	s.uiHandler = ui.Handler()

	s.mountRoutes()
	return s
}

func (s *Server) mountRoutes() {
	r := s.router
	mw := s.mw
	h := s.h

	// Built-in web UI dashboard (embedded frontend or fallback HTML).
	// Everything that's not an API route is served by the SPA handler.

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", h.Health)
		r.Get("/openapi.json", h.OpenAPI)
		r.Get("/metrics", h.Metrics)

		// First-time setup — only available when no admins exist
		r.Get("/setup", h.SetupStatus)
		r.Post("/setup", h.SetupCreate)

		// Auth
		r.Route("/auth", func(r chi.Router) {
			r.With(httprate.LimitByIP(10, time.Minute)).Post("/login", h.Login)
			r.With(httprate.LimitByIP(5, time.Minute)).Post("/register", h.Register)
			r.Post("/refresh", h.RefreshToken)
			r.With(mw.RequireAuth).Post("/logout", h.Logout)
			r.With(httprate.LimitByIP(5, time.Minute)).Post("/otp/request", h.OTPRequest)
			r.Post("/otp/verify", h.OTPVerify)
			r.Post("/magic-link", h.MagicLink)
			r.Post("/magic-link/verify", h.MagicLinkVerify)
		})

		// OAuth
		r.Route("/oauth", func(r chi.Router) {
			r.Get("/{provider}", h.OAuthRedirect)
			r.Get("/{provider}/callback", h.OAuthCallback)
		})

		// Admin
		r.Route("/admin", func(r chi.Router) {
			r.Use(mw.RequireAuth)
			r.Get("/me", h.AdminMe)
			r.Put("/me", h.AdminUpdateMe)
			r.Get("/users", h.AdminList)
			r.Post("/users", h.AdminCreate)
			r.Get("/users/{id}", h.AdminGet)
			r.Put("/users/{id}", h.AdminUpdate)
			r.Delete("/users/{id}", h.AdminDelete)
		})

		// Collections
		r.Route("/collections", func(r chi.Router) {
			r.Use(mw.RequireAuth)
			r.Get("/", h.CollectionsList)
			r.Post("/", h.CollectionsCreate)
			r.Get("/{id}", h.CollectionsGet)
			r.Put("/{id}", h.CollectionsUpdate)
			r.Delete("/{id}", h.CollectionsDelete)
			r.Post("/import", h.CollectionsImport)
		})

		// Records — public or rule-based
		r.Route("/records/{collection}", func(r chi.Router) {
			r.Use(mw.OptionalAuth)
			r.Get("/", h.RecordsList)
			r.Post("/", h.RecordsCreate)
			r.Get("/{recordId}", h.RecordsGet)
			r.Put("/{recordId}", h.RecordsUpdate)
			r.Patch("/{recordId}", h.RecordsUpdate)
			r.Delete("/{recordId}", h.RecordsDelete)
		})

		// Record Auth — end-user authentication for auth collections
		r.Route("/collections/{collection}", func(r chi.Router) {
			// Auth methods listing (no auth required)
			r.Get("/auth-methods", h.RecordAuthMethods)
		})

		r.Route("/collections/{collection}/auth", func(r chi.Router) {
			// Password auth
			r.Post("/auth-with-password", h.RecordAuthPassword)
			// OTP
			r.Post("/auth-with-otp", h.RecordAuthOTPVerify)
			r.Post("/auth-otp-request", h.RecordAuthOTPRequest)
			// OAuth2
			r.Get("/oauth2/{provider}", h.RecordAuthOAuth2Redirect)
			r.Get("/oauth2/{provider}/callback", h.RecordAuthOAuth2Callback)
			// Refresh
			r.Post("/auth-refresh", h.RecordAuthRefresh)
			// Impersonation (admin only)
			r.With(mw.RequireAuth).Post("/impersonate/{recordId}", h.RecordAuthImpersonate)
			// Password reset
			r.Post("/request-password-reset", h.RecordPasswordResetRequest)
			r.Post("/confirm-password-reset", h.RecordPasswordResetConfirm)
			// Email verification
			r.Post("/request-verification", h.RecordVerificationRequest)
			r.Post("/confirm-verification", h.RecordVerificationConfirm)
			// Email change
			r.Post("/request-email-change", h.RecordEmailChangeRequest)
			r.Post("/confirm-email-change", h.RecordEmailChangeConfirm)
		})

		// Batch
		r.With(mw.OptionalAuth).Post("/batch", h.Batch)

		// Files
		r.Route("/files", func(r chi.Router) {
			r.Get("/{collection}/{recordId}/{filename}", h.FileDownload)
			r.With(mw.RequireAuth).Post("/upload", h.FileUpload)
			r.With(mw.RequireAuth).Delete("/{collection}/{recordId}/{filename}", h.FileDelete)
		})

		// Realtime — WebSocket + SSE fallback
		r.Get("/realtime", h.RealtimeConnect)
		r.Get("/sse", h.RealtimeSSE)

		// ACME
		r.Route("/acme", func(r chi.Router) {
			r.Get("/directory", h.ACMEDirectory)
			r.Post("/new-account", h.ACMENewAccount)
			r.Post("/new-order", h.ACMENewOrder)
			r.Post("/challenge/{id}", h.ACMEChallenge)
			r.Post("/finalize/{id}", h.ACMEFinalize)
			r.Get("/cert/{id}", h.ACMECertificate)
			r.Post("/revoke", h.ACMERevoke)
		})

		// Certificates
		r.Route("/certificates", func(r chi.Router) {
			r.Use(mw.RequireAuth)
			r.Get("/", h.CertificatesList)
			r.Post("/issue", h.CertificatesIssue)
			r.Delete("/{id}", h.CertificatesRevoke)
		})

		// Settings
		r.Route("/settings", func(r chi.Router) {
			r.Use(mw.RequireAuth)
			r.Get("/", h.SettingsGet)
			r.Put("/", h.SettingsUpdate)
		})

		// Logs
		r.Route("/logs", func(r chi.Router) {
			r.Use(mw.RequireAuth)
			r.Get("/", h.LogsList)
		})

		// API Keys
		r.Route("/api-keys", func(r chi.Router) {
			r.Use(mw.RequireAuth)
			r.Get("/", h.APIKeysList)
			r.Post("/", h.APIKeysCreate)
			r.Delete("/{id}", h.APIKeysDelete)
		})

		// Backups
		r.Route("/backups", func(r chi.Router) {
			r.Use(mw.RequireAuth)
			r.Get("/", h.BackupsList)
			r.Post("/", h.BackupsCreate)
			r.Get("/{name}/download", h.BackupsDownload)
			r.Post("/{name}/restore", h.BackupsRestore)
			r.Delete("/{name}", h.BackupsDelete)
		})

		// Search (FTS)
		r.Route("/search", func(r chi.Router) {
			r.Use(mw.OptionalAuth)
			r.Post("/{collection}", h.SearchRecords)
		})

		// FTS Index
		r.Route("/fts", func(r chi.Router) {
			r.Use(mw.RequireAuth)
			r.Post("/{collection}", h.FTSCreateIndex)
			r.Delete("/{collection}", h.FTSRemoveIndex)
		})

		// Batch
		r.Route("/batch", func(r chi.Router) {
			r.Use(mw.OptionalAuth)
			r.Post("/{collection}", h.BatchRecords)
		})

		// JS Plugins
		r.Route("/plugins/js", func(r chi.Router) {
			r.Use(mw.RequireAuth)
			r.Get("/", h.JSPluginsList)
			r.Post("/", h.JSPluginCreate)
			r.Get("/{id}", h.JSPluginGet)
			r.Post("/{id}/execute", h.JSPluginExecute)
			r.Delete("/{id}", h.JSPluginDelete)
		})

		// Cron jobs
		r.Route("/jobs", func(r chi.Router) {
			r.Use(mw.RequireAuth)
			r.Get("/", h.JobsList)
			r.Post("/", h.JobsCreate)
			r.Post("/{id}/run", h.JobsRun)
			r.Delete("/{id}", h.JobsDelete)
		})
	})

	// ACME HTTP-01 challenge handler at well-known path
	r.Get("/.well-known/acme-challenge/{token}", h.ACMEHTTPChallenge)

	// Catch-all SPA route — serves the embedded dashboard for any path
	// not matched by API routes above. Must be registered last.
	r.Handle("/*", http.HandlerFunc(s.serveUI))
}

// Start begins listening. Supports TLS when configured.
func (s *Server) Start(addr string) error {
	cfg := s.app.Config()

	handler := s.router

	// TLS with CertMagic auto-cert
	if cfg.EnableTLS && cfg.Domain != "" {
		tlsCfg, err := s.app.ACME().GetTLSConfig(context.Background())
		if err != nil {
			log.Warn().Err(err).Msg("Internal CA not available, falling back to HTTP")
		} else {
			s.srv = &http.Server{
				Addr:              addr,
				Handler:           handler,
				TLSConfig:         tlsCfg,
				ReadTimeout:       30 * time.Second,
				WriteTimeout:      60 * time.Second,
				ReadHeaderTimeout: 10 * time.Second,
				IdleTimeout:       120 * time.Second,
			}
			log.Info().Str("addr", addr).Str("domain", cfg.Domain).Msg("Server starting with TLS")
			return s.srv.ListenAndServeTLS("", "")
		}
	}

	// Plain HTTP
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	s.srv = &http.Server{
		Handler:           handler,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	log.Info().Str("addr", addr).Msg("Server starting (HTTP)")
	return s.srv.Serve(listener)
}

// Shutdown gracefully closes the server.
func (s *Server) Shutdown() error {
	if s.srv != nil {
		return s.srv.Close()
	}
	return nil
}

// Router returns the chi router for internal sub-requests.
func (s *Server) Router() *chi.Mux {
	return s.router.(*chi.Mux)
}

// ServeHTTP allows the Server to be used as an http.Handler directly.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}
