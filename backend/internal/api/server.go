package api

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"
	apimw "github.com/gresbase/gresbase/internal/api/middleware"
	"github.com/gresbase/gresbase/internal/app"
	"github.com/gresbase/gresbase/internal/buildinfo"
	"github.com/gresbase/gresbase/internal/events"
	"github.com/gresbase/gresbase/internal/openapi"
	"github.com/gresbase/gresbase/internal/ui"
	"github.com/rs/zerolog/log"
)

// Server wraps the HTTP server with optional TLS.
const extensionNotFoundHeader = "X-Gresbase-Extension-NotFound"

type Server struct {
	app                  *app.App
	router               chi.Router
	extensions           *chi.Mux
	extensionMiddlewares []func(http.Handler) http.Handler
	srv                  *http.Server
	h                    *Handlers
	mw                   *apimw.Middleware
	uiHandler            http.Handler
	tusJanitorStop       chan struct{}
}

// NewServer creates a fully wired API server.
func NewServer(application *app.App) *Server {
	r := chi.NewRouter()

	// Resolve the client IP in a trusted-proxy-aware way (NOT chi's naive
	// RealIP, which blindly trusts X-Forwarded-For and would let any client
	// spoof the IP used for rate limiting and audit logs).
	var trustedProxies []string
	if application != nil && application.Config() != nil {
		trustedProxies = application.Config().TrustedProxies
	}

	// Global middleware
	r.Use(middleware.RequestID)
	r.Use(apimw.RealIP(trustedProxies))
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(apimw.TimeoutExcept(60*time.Second, apimw.IsLongLivedRequest))
	r.Use(middleware.CleanPath)
	r.Use(middleware.Heartbeat("/health"))

	allowedOrigins := []string{"*"}
	allowCredentials := true
	if application != nil && application.Config() != nil {
		if len(application.Config().CORSAllowedOrigins) > 0 {
			allowedOrigins = application.Config().CORSAllowedOrigins
		}
		allowCredentials = application.Config().CORSAllowCredentials
	}
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: allowedOrigins,
		// HEAD is required by the TUS offset-retrieval request.
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD"},
		AllowedHeaders: []string{
			"Accept", "Authorization", "Content-Type", "X-Requested-With",
			// TUS resumable upload protocol headers
			"Tus-Resumable", "Upload-Length", "Upload-Offset", "Upload-Metadata",
			"Upload-Defer-Length", "X-HTTP-Method-Override", "X-Request-ID",
		},
		ExposedHeaders: []string{
			"Link", "X-Total-Count",
			// TUS resumable upload protocol headers
			"Location", "Tus-Resumable", "Tus-Version", "Tus-Max-Size", "Tus-Extension",
			"Upload-Offset", "Upload-Length", "Upload-Metadata", "Upload-Defer-Length",
			"Upload-Expires", "X-Gresbase-Filename",
		},
		AllowCredentials: allowCredentials,
		MaxAge:           300,
	}))

	r.Use(apimw.SecurityHeaders)
	r.Use(apimw.RequestIDMiddleware)

	// Request body size cap. TUS and direct file-upload routes stream large
	// bodies and are exempt; everything else is capped to MaxRequestBodyBytes.
	maxBody := int64(10 << 20)
	if application != nil && application.Config() != nil && application.Config().MaxRequestBodyBytes > 0 {
		maxBody = application.Config().MaxRequestBodyBytes
	}
	r.Use(apimw.MaxBodyBytes(maxBody,
		"/api/v1/files/tus/",
		"/api/v1/files/upload",
	))

	// CSRF protection (double-submit). Only enforced for cookie-authenticated
	// state-changing requests; Authorization-header / API-key callers and
	// pre-session endpoints (login/refresh/register) are exempt by design.
	r.Use(apimw.CSRF)

	mw := apimw.NewMiddleware(application)
	h := NewHandlers(application)

	// Enforce collection access rules on realtime record event delivery.
	if application != nil && application.Realtime() != nil {
		application.Realtime().SetRuleChecker(newRealtimeRuleChecker(application))
		// Client broadcasts may not target collection record topics, so they
		// can never spoof record events to rule-checked subscribers.
		application.Realtime().SetChannelGuard(func(topic string) bool {
			name := topic
			if i := strings.Index(topic, "/"); i >= 0 {
				name = topic[:i]
			}
			if strings.TrimSpace(name) == "" || strings.HasPrefix(name, "_") {
				return false
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, err := application.Collections().GetCollectionByName(ctx, name)
			return err != nil
		})
	}

	s := &Server{
		app:                  application,
		router:               r,
		extensions:           chi.NewRouter(),
		extensionMiddlewares: []func(http.Handler) http.Handler{},
		h:                    h,
		mw:                   mw,
	}
	s.extensions.NotFound(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(extensionNotFoundHeader, "1")
		http.NotFound(w, r)
	})

	// Setup embedded UI handler (pre-built frontend or fallback)
	s.uiHandler = ui.Handler()

	s.mountRoutes()
	return s
}

func (s *Server) mountRoutes() {
	r := s.router
	mw := s.mw
	h := s.h

	viewer := mw.RequireMinRole("viewer")
	editor := mw.RequireMinRole("editor")
	admin := mw.RequireMinRole("admin")
	superAdmin := mw.RequireMinRole("super_admin")
	adminsRead := mw.RequirePermission("admins.read")
	adminsWrite := mw.RequirePermission("admins.write")
	collectionsRead := mw.RequirePermission("collections.read")
	collectionsWrite := mw.RequirePermission("collections.write")
	filesWrite := mw.RequirePermission("files.write")
	settingsRead := mw.RequirePermission("settings.read")
	settingsWrite := mw.RequirePermission("settings.write")
	logsRead := mw.RequirePermission("logs.read")
	apiKeysRead := mw.RequirePermission("api_keys.read")
	apiKeysWrite := mw.RequirePermission("api_keys.write")
	backupsRead := mw.RequirePermission("backups.read")
	backupsWrite := mw.RequirePermission("backups.write")
	ftsWrite := mw.RequirePermission("fts.write")
	jobsRead := mw.RequirePermission("jobs.read")
	jobsWrite := mw.RequirePermission("jobs.write")
	recordsImpersonate := mw.RequirePermission("records.impersonate")
	webhooksRead := mw.RequirePermission("webhooks.read")
	webhooksWrite := mw.RequirePermission("webhooks.write")
	sqlExecute := mw.RequirePermission("sql.execute")

	// Built-in web UI dashboard (embedded frontend or fallback HTML).
	// Everything that's not an API route is served by the SPA handler.
	r.Get("/.well-known/jwks.json", h.JWKS)

	// Prometheus text-format metrics. In-process observability — no
	// log-shipping sidecars. Optionally gated by a static bearer token.
	r.Get("/metrics", s.prometheusMetrics)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", h.Health)
		r.Get("/ready", h.Ready)
		r.Get("/features", h.Features)
		r.Get("/.well-known/jwks.json", h.JWKS)
		r.Get("/openapi.json", h.OpenAPI)
		r.With(mw.RequireAuth, viewer).Get("/metrics", h.Metrics)
		r.Get("/collections/meta/oauth2-providers", h.OAuth2ProvidersMeta)
		r.Get("/oauth2-redirect", h.OAuth2RedirectBridge)
		r.Post("/oauth2-redirect", h.OAuth2RedirectBridge)

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
			r.Post("/request-password-reset", h.PasswordResetRequest)
			r.Post("/confirm-password-reset", h.PasswordResetConfirm)
			r.Post("/request-verification", h.VerificationRequest)
			r.Post("/confirm-verification", h.VerificationConfirm)
			r.With(mw.RequireAuth).Post("/request-email-change", h.EmailChangeRequest)
			r.Post("/confirm-email-change", h.EmailChangeConfirm)
		})

		// OAuth
		r.Route("/oauth", func(r chi.Router) {
			r.Get("/{provider}", h.OAuthRedirect)
			r.Get("/{provider}/callback", h.OAuthCallback)
		})

		// Admin
		r.Route("/admin", func(r chi.Router) {
			r.Use(mw.RequireAuth)
			r.With(adminsRead).Get("/me", h.AdminMe)
			r.With(adminsWrite).Put("/me", h.AdminUpdateMe)
			r.With(admin, adminsRead).Get("/users", h.AdminList)
			r.With(superAdmin, adminsWrite).Post("/users", h.AdminCreate)
			r.With(admin, adminsRead).Get("/users/{id}", h.AdminGet)
			r.With(superAdmin, adminsWrite).Put("/users/{id}", h.AdminUpdate)
			r.With(superAdmin, adminsWrite).Delete("/users/{id}", h.AdminDelete)
		})

		// Typed SDK generation from live schema
		r.With(mw.RequireAuth, viewer, collectionsRead).Get("/types.ts", h.TypesTS)

		// Collections
		r.Route("/collections", func(r chi.Router) {
			r.Use(mw.RequireAuth)
			r.With(viewer, collectionsRead).Get("/", h.CollectionsList)
			r.With(viewer, collectionsRead).Get("/export", h.CollectionsExport)
			r.With(editor, collectionsWrite).Post("/", h.CollectionsCreate)
			r.With(editor, collectionsWrite).Post("/import", h.CollectionsImport)
			// Rule presets + simulator (attacks the untestable-policy footgun)
			r.With(viewer, collectionsRead).Get("/meta/rule-presets", h.RulePresets)
			r.With(viewer, collectionsRead).Post("/meta/rule-simulate", h.RuleSimulate)
			r.With(viewer, collectionsRead).Get("/{id}", h.CollectionsGet)
			r.With(editor, collectionsWrite).Put("/{id}", h.CollectionsUpdate)
			r.With(editor, collectionsWrite).Delete("/{id}", h.CollectionsDelete)
			r.With(editor, collectionsWrite).Post("/{id}/refresh-view", h.CollectionsRefreshView)
		})

		// Records — public or rule-based
		r.Route("/records/{collection}", func(r chi.Router) {
			r.Use(mw.OptionalAuth)
			r.Get("/", h.RecordsList)
			r.Post("/", h.RecordsCreate)
			r.Post("/search-vector", h.VectorSearch)
			r.Get("/aggregate", h.RecordsAggregate)
			r.Get("/{recordId}", h.RecordsGet)
			r.Put("/{recordId}", h.RecordsUpdate)
			r.Patch("/{recordId}", h.RecordsUpdate)
			r.Delete("/{recordId}", h.RecordsDelete)
		})

		// Record Auth — end-user authentication for auth collections
		authRate := httprate.LimitByIP(10, time.Minute)
		r.Get("/collections/{collection}/auth-methods", h.RecordAuthMethods)
		r.Route("/collections/{collection}/auth", func(r chi.Router) {
			// Password auth
			r.With(authRate).Post("/auth-with-password", h.RecordAuthPassword)
			// Anonymous sign-in (collection must opt in via allowAnonymous)
			r.With(authRate).Post("/auth-with-anonymous", h.RecordAuthAnonymous)
			// OTP
			r.With(authRate).Post("/auth-with-otp", h.RecordAuthOTPVerify)
			r.With(authRate).Post("/auth-otp-request", h.RecordAuthOTPRequest)
			r.With(authRate).Post("/auth-with-oauth2", h.RecordAuthOAuth2)
			// OAuth2
			r.Get("/oauth2/{provider}", h.RecordAuthOAuth2Redirect)
			r.Get("/oauth2/{provider}/callback", h.RecordAuthOAuth2Callback)
			// Passkeys (WebAuthn) — collection must opt in via allowPasskeys
			s.mountPasskeyRoutes(r, authRate)
			// Refresh
			r.Post("/auth-refresh", h.RecordAuthRefresh)
			// Impersonation (admin only)
			r.With(mw.RequireAuth, admin, recordsImpersonate).Post("/impersonate/{recordId}", h.RecordAuthImpersonate)
			// Password reset
			r.Post("/request-password-reset", h.RecordPasswordResetRequest)
			r.Post("/confirm-password-reset", h.RecordPasswordResetConfirm)
			// Email verification
			r.Post("/request-verification", h.RecordVerificationRequest)
			r.Post("/confirm-verification", h.RecordVerificationConfirm)
			// Email change
			r.With(mw.OptionalAuth).Post("/request-email-change", h.RecordEmailChangeRequest)
			r.Post("/confirm-email-change", h.RecordEmailChangeConfirm)
		})

		// Batch
		r.With(mw.OptionalAuth).Post("/batch", h.HandleBatch)
		r.With(mw.OptionalAuth).Post("/batch/{collection}", h.BatchRecords)

		// Resumable uploads (TUS protocol) — see tus.go
		s.mountTUS(r)

		// Files
		r.Route("/files", func(r chi.Router) {
			// Short-lived token for protected-file access from img/video tags.
			r.With(mw.OptionalAuth).Post("/token", h.FileToken)
			r.With(mw.OptionalAuth).Get("/{collection}/{recordId}/{filename}", h.FileDownload)
			r.With(mw.RequireAuth, editor, filesWrite).Post("/upload", h.FileUpload)
			r.With(mw.RequireAuth, editor, filesWrite).Post("/promote", h.FilesPromote)
			r.With(mw.RequireAuth, editor, filesWrite).Delete("/{collection}/{recordId}/{filename}", h.FileDelete)
		})

		// Realtime — WebSocket + SSE fallback
		r.With(mw.OptionalAuth).Get("/realtime", h.RealtimeConnect)
		r.With(mw.OptionalAuth).Post("/realtime", h.RealtimeConnect)
		r.With(mw.OptionalAuth).Post("/realtime/broadcast", h.RealtimeBroadcast)
		r.With(mw.OptionalAuth).Get("/sse", h.RealtimeSSE)

		// Settings
		r.Route("/settings", func(r chi.Router) {
			r.Use(mw.RequireAuth)
			r.With(admin, settingsRead).Get("/", h.SettingsGet)
			r.With(admin, settingsWrite).Put("/", h.SettingsUpdate)
			r.With(admin, settingsRead).Get("/email-templates", h.SettingsEmailTemplates)
		})

		// Logs
		r.Route("/logs", func(r chi.Router) {
			r.Use(mw.RequireAuth)
			r.With(viewer, logsRead).Get("/", h.LogsList)
		})

		// API Keys
		r.Route("/api-keys", func(r chi.Router) {
			r.Use(mw.RequireAuth)
			r.With(admin, apiKeysRead).Get("/", h.APIKeysList)
			r.With(admin, apiKeysWrite).Post("/", h.APIKeysCreate)
			r.With(admin, apiKeysWrite).Delete("/{id}", h.APIKeysDelete)
		})

		// Backups
		r.Route("/backups", func(r chi.Router) {
			r.Use(mw.RequireAuth)
			r.With(admin, backupsRead).Get("/", h.BackupsList)
			r.With(admin, backupsWrite).Post("/", h.BackupsCreate)
			r.With(admin, backupsRead).Get("/{name}/download", h.BackupsDownload)
			r.With(admin, backupsWrite).Post("/{name}/restore", h.BackupsRestore)
			r.With(admin, backupsWrite).Delete("/{name}", h.BackupsDelete)
		})

		// Search (FTS)
		r.Route("/search", func(r chi.Router) {
			r.Use(mw.OptionalAuth)
			r.Post("/{collection}", h.SearchRecords)
		})

		// FTS Index
		r.Route("/fts", func(r chi.Router) {
			r.Use(mw.RequireAuth)
			r.With(editor, ftsWrite).Post("/{collection}", h.FTSCreateIndex)
			r.With(editor, ftsWrite).Delete("/{collection}", h.FTSRemoveIndex)
		})

		// Cron jobs
		r.Route("/jobs", func(r chi.Router) {
			r.Use(mw.RequireAuth)
			r.With(admin, jobsRead).Get("/", h.JobsList)
			r.With(admin, jobsWrite).Post("/", h.JobsCreate)
			r.With(admin, jobsWrite).Post("/{id}/run", h.JobsRun)
			r.With(admin, jobsWrite).Patch("/{id}", h.JobsUpdate)
			r.With(admin, jobsRead).Get("/{id}/runs", h.JobsRuns)
			r.With(admin, jobsWrite).Delete("/{id}", h.JobsDelete)
		})

		// Webhooks — HMAC-signed event forwarding to external URLs
		r.Route("/webhooks", func(r chi.Router) {
			r.Use(mw.RequireAuth)
			r.With(admin, webhooksRead).Get("/", h.WebhooksList)
			r.With(admin, webhooksWrite).Post("/", h.WebhooksCreate)
			r.With(admin, webhooksRead).Get("/{id}", h.WebhooksGet)
			r.With(admin, webhooksWrite).Put("/{id}", h.WebhooksUpdate)
			r.With(admin, webhooksWrite).Delete("/{id}", h.WebhooksDelete)
			r.With(admin, webhooksRead).Get("/{id}/deliveries", h.WebhooksDeliveries)
			r.With(admin, webhooksWrite).Post("/{id}/test", h.WebhooksTest)
		})

		// SQL console + schema introspection (superuser only)
		r.Route("/sql", func(r chi.Router) {
			r.Use(mw.RequireAuth)
			r.With(superAdmin, sqlExecute).Post("/", h.SQLExecute)
			r.With(superAdmin, sqlExecute).Get("/schema", h.SQLSchema)
		})

		// Postgres Row-Level Security generated from collection rules
		r.Route("/rls", func(r chi.Router) {
			r.Use(mw.RequireAuth)
			r.With(superAdmin, collectionsRead).Get("/script", h.RLSScript)
			r.With(superAdmin, collectionsWrite).Post("/apply", h.RLSApply)
			r.With(superAdmin, collectionsWrite).Post("/remove", h.RLSRemove)
		})
	})

	r.Route("/api", func(r chi.Router) {
		r.Get("/health", h.Health)
		r.Get("/collections/meta/oauth2-providers", h.OAuth2ProvidersMeta)
		r.Get("/oauth2-redirect", h.OAuth2RedirectBridge)
		r.Post("/oauth2-redirect", h.OAuth2RedirectBridge)

		r.Route("/collections/{collection}/records", func(r chi.Router) {
			r.Use(mw.OptionalAuth)
			r.Get("/", h.RecordsList)
			r.Post("/", h.RecordsCreate)
			r.Get("/{recordId}", h.RecordsGet)
			r.Put("/{recordId}", h.RecordsUpdate)
			r.Patch("/{recordId}", h.RecordsUpdate)
			r.Delete("/{recordId}", h.RecordsDelete)
		})

		r.Get("/collections/{collection}/auth-methods", h.RecordAuthMethods)
		pbAuthRate := httprate.LimitByIP(10, time.Minute)
		r.Route("/collections/{collection}", func(r chi.Router) {
			r.With(pbAuthRate).Post("/auth-with-password", h.RecordAuthPassword)
			r.With(pbAuthRate).Post("/auth-with-otp", h.RecordAuthOTPVerify)
			r.With(pbAuthRate).Post("/auth-otp-request", h.RecordAuthOTPRequest)
			r.With(pbAuthRate).Post("/auth-with-oauth2", h.RecordAuthOAuth2)
			r.Post("/auth-refresh", h.RecordAuthRefresh)
			r.Post("/request-password-reset", h.RecordPasswordResetRequest)
			r.Post("/confirm-password-reset", h.RecordPasswordResetConfirm)
			r.Post("/request-verification", h.RecordVerificationRequest)
			r.Post("/confirm-verification", h.RecordVerificationConfirm)
			r.With(mw.OptionalAuth).Post("/request-email-change", h.RecordEmailChangeRequest)
			r.Post("/confirm-email-change", h.RecordEmailChangeConfirm)
		})
	})
	// Fallback UI handler for non-API GET/HEAD requests.
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			http.NotFound(w, r)
			return
		}
		s.serveUI(w, r)
	})
}

// prometheusMetrics serves the Prometheus exposition endpoint. Disabled via
// metrics_enabled=false; when metrics_token is set, scrapers must send it as
// a bearer token.
func (s *Server) prometheusMetrics(w http.ResponseWriter, r *http.Request) {
	cfg := s.app.Config()
	if s.app == nil || cfg == nil || !cfg.MetricsEnabled || s.app.Metrics() == nil {
		http.NotFound(w, r)
		return
	}
	if cfg.MetricsToken != "" {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(cfg.MetricsToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "Invalid metrics token")
			return
		}
	}
	s.app.Metrics().PrometheusHandler(buildinfo.Version)(w, r)
}

// Start begins listening. Supports TLS when configured.
func (s *Server) Start(addr string) error {
	cfg := s.app.Config()

	serveEvent := &events.ServeEvent{App: s.app, Addr: addr, Router: s, Server: s}
	if err := s.app.OnServe().Trigger(serveEvent, func(e events.Event) error { return serveEvent.Next() }); err != nil {
		return err
	}
	handler := s.extensionAwareHandler()

	// TLS from operator-provided certificate + key files. For automatic
	// certificate management, terminate TLS at a reverse proxy (Caddy,
	// nginx, Traefik) — that is the recommended production deployment.
	if cfg.EnableTLS {
		if cfg.TLSCertFile == "" || cfg.TLSKeyFile == "" {
			return fmt.Errorf("enable_tls is set but tls_cert_file/tls_key_file are not configured")
		}
		s.srv = &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      0,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       120 * time.Second,
		}
		log.Info().Str("addr", addr).Msg("Server starting with TLS")
		return s.srv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
	}

	// Plain HTTP
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	s.srv = &http.Server{
		Handler:           handler,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	log.Info().Str("addr", addr).Msg("Server starting (HTTP)")
	return s.srv.Serve(listener)
}

// Shutdown gracefully closes the server.
func (s *Server) Shutdown() error {
	if s.tusJanitorStop != nil {
		close(s.tusJanitorStop)
		s.tusJanitorStop = nil
	}
	if s.srv != nil {
		return s.srv.Close()
	}
	return nil
}

// Router returns the chi router for internal sub-requests.
func (s *Server) Router() *chi.Mux {
	return s.router.(*chi.Mux)
}

// OpenAPISpec generates the current OpenAPI spec from the live core and
// extension routers.
func (s *Server) OpenAPISpec(baseURL string) *openapi.Spec {
	routes := openapi.ExtractRoutes(s.router)
	if s.extensions != nil {
		routes = append(routes, openapi.ExtractRoutes(s.extensions)...)
	}
	return openapi.Generate(buildinfo.Version, baseURL, routes)
}

func (s *Server) extensionAwareHandler() http.Handler {
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.extensions != nil {
			rr := httptest.NewRecorder()
			s.extensions.ServeHTTP(rr, r)
			notFoundFromExtensionRouter := rr.Code == http.StatusNotFound && rr.Header().Get(extensionNotFoundHeader) == "1"
			if !notFoundFromExtensionRouter {
				for k, values := range rr.Header() {
					if k == extensionNotFoundHeader {
						continue
					}
					for _, v := range values {
						w.Header().Add(k, v)
					}
				}
				w.WriteHeader(rr.Code)
				_, _ = w.Write(rr.Body.Bytes())
				return
			}
		}
		s.router.ServeHTTP(w, r)
	})

	handler := http.Handler(base)
	for i := len(s.extensionMiddlewares) - 1; i >= 0; i-- {
		handler = s.extensionMiddlewares[i](handler)
	}
	return handler
}

type extensionRegistrar struct {
	server      *Server
	prefix      string
	middlewares []func(http.Handler) http.Handler
}

func (r *extensionRegistrar) Use(middlewares ...func(http.Handler) http.Handler) {
	r.middlewares = append(r.middlewares, middlewares...)
}

func (r *extensionRegistrar) Handle(pattern string, handler http.Handler) {
	r.register("", pattern, handler)
}

func (r *extensionRegistrar) HandleFunc(pattern string, handlerFn http.HandlerFunc) {
	r.Handle(pattern, handlerFn)
}

func (r *extensionRegistrar) Method(method, pattern string, handler http.Handler) {
	r.register(method, pattern, handler)
}

func (r *extensionRegistrar) MethodFunc(method, pattern string, handlerFn http.HandlerFunc) {
	r.Method(method, pattern, handlerFn)
}

func (r *extensionRegistrar) Get(pattern string, handlerFn http.HandlerFunc) {
	r.MethodFunc(http.MethodGet, pattern, handlerFn)
}

func (r *extensionRegistrar) Post(pattern string, handlerFn http.HandlerFunc) {
	r.MethodFunc(http.MethodPost, pattern, handlerFn)
}

func (r *extensionRegistrar) Put(pattern string, handlerFn http.HandlerFunc) {
	r.MethodFunc(http.MethodPut, pattern, handlerFn)
}

func (r *extensionRegistrar) Patch(pattern string, handlerFn http.HandlerFunc) {
	r.MethodFunc(http.MethodPatch, pattern, handlerFn)
}

func (r *extensionRegistrar) Delete(pattern string, handlerFn http.HandlerFunc) {
	r.MethodFunc(http.MethodDelete, pattern, handlerFn)
}

func (r *extensionRegistrar) Group(prefix string, fn func(events.RouteRegistrar)) {
	if fn == nil {
		return
	}
	fn(&extensionRegistrar{
		server:      r.server,
		prefix:      joinExtensionPattern(r.prefix, prefix),
		middlewares: append([]func(http.Handler) http.Handler(nil), r.middlewares...),
	})
}

func (r *extensionRegistrar) Mount(prefix string, fn func(events.RouteRegistrar)) {
	r.Group(prefix, fn)
}

func (r *extensionRegistrar) register(method, pattern string, handler http.Handler) {
	fullPattern := joinExtensionPattern(r.prefix, pattern)
	wrapped := wrapExtensionHandler(handler, r.middlewares)
	if method == "" {
		r.server.extensions.Handle(fullPattern, wrapped)
		return
	}
	r.server.extensions.Method(method, fullPattern, wrapped)
}

func wrapExtensionHandler(handler http.Handler, middlewares []func(http.Handler) http.Handler) http.Handler {
	if handler == nil {
		return http.NotFoundHandler()
	}
	wrapped := handler
	for i := len(middlewares) - 1; i >= 0; i-- {
		if middlewares[i] == nil {
			continue
		}
		wrapped = middlewares[i](wrapped)
	}
	return wrapped
}

func joinExtensionPattern(prefix, pattern string) string {
	prefix = strings.TrimSpace(prefix)
	pattern = strings.TrimSpace(pattern)
	if prefix == "" {
		if pattern == "" {
			return "/"
		}
		if strings.HasPrefix(pattern, "/") {
			return pattern
		}
		return "/" + pattern
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	if pattern == "" || pattern == "/" {
		return strings.TrimRight(prefix, "/")
	}
	return strings.TrimRight(prefix, "/") + "/" + strings.TrimLeft(pattern, "/")
}

// Use registers extension middlewares.
func (s *Server) Use(middlewares ...func(http.Handler) http.Handler) {
	s.extensionMiddlewares = append(s.extensionMiddlewares, middlewares...)
}

// Handle registers an extension handler.
func (s *Server) Handle(pattern string, handler http.Handler) {
	s.extensions.Handle(pattern, handler)
}

// HandleFunc registers an extension handler function.
func (s *Server) HandleFunc(pattern string, handlerFn http.HandlerFunc) {
	s.extensions.HandleFunc(pattern, handlerFn)
}

// Method registers an extension method-specific handler.
func (s *Server) Method(method, pattern string, handler http.Handler) {
	s.extensions.Method(method, pattern, handler)
}

// MethodFunc registers an extension method-specific handler function.
func (s *Server) MethodFunc(method, pattern string, handlerFn http.HandlerFunc) {
	s.extensions.MethodFunc(method, pattern, handlerFn)
}

// Get registers an extension GET handler.
func (s *Server) Get(pattern string, handlerFn http.HandlerFunc) {
	s.MethodFunc(http.MethodGet, pattern, handlerFn)
}

// Post registers an extension POST handler.
func (s *Server) Post(pattern string, handlerFn http.HandlerFunc) {
	s.MethodFunc(http.MethodPost, pattern, handlerFn)
}

// Put registers an extension PUT handler.
func (s *Server) Put(pattern string, handlerFn http.HandlerFunc) {
	s.MethodFunc(http.MethodPut, pattern, handlerFn)
}

// Patch registers an extension PATCH handler.
func (s *Server) Patch(pattern string, handlerFn http.HandlerFunc) {
	s.MethodFunc(http.MethodPatch, pattern, handlerFn)
}

// Delete registers an extension DELETE handler.
func (s *Server) Delete(pattern string, handlerFn http.HandlerFunc) {
	s.MethodFunc(http.MethodDelete, pattern, handlerFn)
}

// Group registers a prefixed extension route group with route-scoped middleware.
func (s *Server) Group(prefix string, fn func(events.RouteRegistrar)) {
	if fn == nil {
		return
	}
	fn(&extensionRegistrar{server: s, prefix: joinExtensionPattern("", prefix)})
}

// Mount is an alias for Group for mount-style route registration.
func (s *Server) Mount(prefix string, fn func(events.RouteRegistrar)) {
	s.Group(prefix, fn)
}

// ServeHTTP allows the Server to be used as an http.Handler directly.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.extensionAwareHandler().ServeHTTP(w, r)
}
