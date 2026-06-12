package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds all configuration for the Gresbase platform.
type Config struct {
	// Server
	Addr string `mapstructure:"addr"`
	// Domain is the public base URL (used for email links and the WebAuthn
	// relying party), e.g. https://app.example.com.
	Domain string `mapstructure:"domain"`
	// EnableTLS serves HTTPS directly from operator-provided cert/key files.
	// For automatic certificates, terminate TLS at a reverse proxy instead.
	EnableTLS   bool   `mapstructure:"enable_tls"`
	TLSCertFile string `mapstructure:"tls_cert_file"`
	TLSKeyFile  string `mapstructure:"tls_key_file"`
	DataDir     string `mapstructure:"data_dir"`

	// HTTP security / CORS
	CORSAllowedOrigins   []string `mapstructure:"cors_allowed_origins"`
	CORSAllowCredentials bool     `mapstructure:"cors_allow_credentials"`

	// Database
	DatabaseURL          string        `mapstructure:"database_url"`
	DatabaseMaxOpenConns int           `mapstructure:"database_max_open_conns"`
	DatabaseMaxIdleConns int           `mapstructure:"database_max_idle_conns"`
	DatabaseMaxIdleTime  time.Duration `mapstructure:"database_max_idle_time"`
	EmbeddedPort         int           `mapstructure:"embedded_port"` // port for embedded PostgreSQL

	// Auth
	JWTSecret          string        `mapstructure:"jwt_secret"`
	JWTSecretGenerated bool          `mapstructure:"-"`
	JWTAlgorithm       string        `mapstructure:"jwt_algorithm"`
	JWTKeyID           string        `mapstructure:"jwt_key_id"`
	JWTPrivateKey      string        `mapstructure:"jwt_private_key"`
	JWTPublicKey       string        `mapstructure:"jwt_public_key"`
	AccessTokenExpiry  time.Duration `mapstructure:"access_token_expiry"`
	RefreshTokenExpiry time.Duration `mapstructure:"refresh_token_expiry"`
	AdminTokenExpiry   time.Duration `mapstructure:"admin_token_expiry"`

	// OAuth
	OAuthProviders map[string]OAuthProviderConfig `mapstructure:"oauth_providers"`

	// Storage
	StorageBackend string `mapstructure:"storage_backend"` // local or s3
	StorageLocal   string `mapstructure:"storage_local_path"`
	S3Endpoint     string `mapstructure:"s3_endpoint"`
	S3Bucket       string `mapstructure:"s3_bucket"`
	S3Region       string `mapstructure:"s3_region"`
	S3AccessKey    string `mapstructure:"s3_access_key"`
	S3SecretKey    string `mapstructure:"s3_secret_key"`
	S3UseSSL       bool   `mapstructure:"s3_use_ssl"`

	// JSPluginsEnabled gates the in-process JavaScript plugin runtime. It is
	// OFF by default: the runtime is not sandboxed, so it must be opted into
	// explicitly and treated as trusted code.
	JSPluginsEnabled bool `mapstructure:"js_plugins_enabled"`

	// HooksDir is a directory of *.js hook files loaded at boot (PocketBase
	// pb_hooks-style). File hooks run regardless of JSPluginsEnabled: writing
	// to the server's filesystem already implies full trust. Empty disables.
	HooksDir string `mapstructure:"hooks_dir"`
	// HooksWatch hot-reloads the hooks directory on file changes.
	HooksWatch bool `mapstructure:"hooks_watch"`

	// MigrationsDir is the directory of collection schema migration files
	// (PocketBase pb_migrations-style). Files are applied on boot; in dev mode
	// collection changes made through the dashboard/API are recorded here
	// automatically. Empty disables the feature.
	MigrationsDir string `mapstructure:"migrations_dir"`

	// DatabaseReplicaURL, when set alongside an external DATABASE_URL, routes
	// replica-safe reads (record lists, aggregations, relation expansion) to a
	// PostgreSQL read replica. Writes and rule-feeding reads stay on the
	// primary. Expect normal replication lag on routed reads.
	DatabaseReplicaURL string `mapstructure:"database_replica_url"`

	// Realtime
	RealtimeMaxConnections int           `mapstructure:"realtime_max_connections"`
	RealtimeIdleTimeout    time.Duration `mapstructure:"realtime_idle_timeout"`
	RealtimeMaxMessageSize int64         `mapstructure:"realtime_max_message_size"`
	// RealtimeMultiNode enables cross-node realtime via Postgres LISTEN/NOTIFY.
	// Turn this on when running more than one app node behind a load balancer.
	RealtimeMultiNode bool `mapstructure:"realtime_multi_node"`
	// RealtimeMaxConnectionAge caps how long a single realtime connection
	// (SSE or WebSocket) may stay open before the server closes it cleanly;
	// clients auto-reconnect. Prevents zombie connections (PocketBase uses the
	// same 30m default). 0 disables the cap.
	RealtimeMaxConnectionAge time.Duration `mapstructure:"realtime_max_connection_age"`

	// RealtimeWALEnabled turns on WAL-based change capture: record events are
	// sourced from PostgreSQL logical replication instead of the API write
	// handlers, so rows changed via the SQL console, psql, the generated RLS
	// role, or any direct connection reach realtime subscribers too.
	//
	// Requirements: wal_level = logical and a role with REPLICATION (the
	// embedded PostgreSQL is configured automatically when this is enabled).
	// OFF by default.
	RealtimeWALEnabled bool `mapstructure:"realtime_wal_enabled"`
	// RealtimeWALSlot is the logical replication slot name.
	//
	// IMPORTANT for multi-node deployments: every node MUST use a UNIQUE slot
	// name — two nodes sharing a slot will each see only part of the stream.
	// Also note that a replication slot retains WAL on the server until it is
	// consumed: if you stop running Gresbase with WAL capture enabled (or
	// rename the slot), drop the abandoned slot manually with
	// SELECT pg_drop_replication_slot('gresbase_realtime'); or the server's
	// disk will fill with retained WAL.
	RealtimeWALSlot string `mapstructure:"realtime_wal_slot"`
	// RealtimeWALPublication is the publication holding the collection record
	// tables. It is created and kept in sync with collections automatically.
	RealtimeWALPublication string `mapstructure:"realtime_wal_publication"`

	// Rate Limiting
	RateLimitEnabled bool `mapstructure:"rate_limit_enabled"`
	RateLimitRPS     int  `mapstructure:"rate_limit_rps"`
	RateLimitBurst   int  `mapstructure:"rate_limit_burst"`

	// Observability — Prometheus text endpoint at /metrics. A static bearer
	// token gates scrapes when set; empty token means open (node_exporter
	// convention for trusted networks).
	MetricsEnabled bool   `mapstructure:"metrics_enabled"`
	MetricsToken   string `mapstructure:"metrics_token"`

	// Scheduled backups. When backup_cron is set, a system job snapshots the
	// database on that schedule; backup_max_keep prunes old snapshots and
	// backup_upload_s3 mirrors them to the configured S3 backend.
	BackupCron     string `mapstructure:"backup_cron"`
	BackupMaxKeep  int    `mapstructure:"backup_max_keep"`
	BackupUploadS3 bool   `mapstructure:"backup_upload_s3"`

	// RLSRole is the Postgres role generated row-level-security policies are
	// granted to (for direct DB connections that bypass the API).
	RLSRole string `mapstructure:"rls_role"`

	// Email
	SMTPHost     string `mapstructure:"smtp_host"`
	SMTPPort     int    `mapstructure:"smtp_port"`
	SMTPUsername string `mapstructure:"smtp_username"`
	SMTPPassword string `mapstructure:"smtp_password"`
	SMTPFrom     string `mapstructure:"smtp_from"`

	// Dev
	DevMode  bool   `mapstructure:"dev_mode"`
	LogLevel string `mapstructure:"log_level"`

	// Multi-tenant
	MultiTenant bool `mapstructure:"multi_tenant"`

	// Hide banner on startup
	HideStartBanner bool `mapstructure:"hide_start_banner"`
}

// OAuthProviderNames is the supported provider catalog. Each name maps to
// <NAME>_CLIENT_ID / <NAME>_CLIENT_SECRET / <NAME>_REDIRECT_URL env vars.
var OAuthProviderNames = []string{
	"google", "github", "microsoft", "gitlab", "discord",
	"apple", "bitbucket", "box", "dropbox", "facebook", "figma",
	"gitea", "gitee", "instagram", "kakao", "line", "linear", "linkedin",
	"notion", "patreon", "reddit", "slack", "spotify", "strava",
	"twitch", "twitter", "vk", "wakatime", "yandex", "zoom",
}

// OAuthProviderConfig holds OAuth provider credentials.
type OAuthProviderConfig struct {
	Enabled      bool   `mapstructure:"enabled"`
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
	RedirectURL  string `mapstructure:"redirect_url"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Addr:               ":8080",
		CORSAllowedOrigins: []string{"*"},
		// Credentials default to off so the wildcard origin stays safe and
		// the binary boots in production without CORS configuration. The
		// dashboard is same-origin and SDKs use bearer tokens, so
		// cross-origin cookies are not needed out of the box.
		CORSAllowCredentials:     false,
		DatabaseMaxOpenConns:     25,
		DatabaseMaxIdleConns:     5,
		DatabaseMaxIdleTime:      5 * time.Minute,
		EmbeddedPort:             5433,
		JWTAlgorithm:             "HS256",
		JWTKeyID:                 "gresbase-default",
		AccessTokenExpiry:        15 * time.Minute,
		RefreshTokenExpiry:       7 * 24 * time.Hour,
		AdminTokenExpiry:         24 * time.Hour,
		StorageBackend:           "local",
		StorageLocal:             "./storage",
		RealtimeMaxConnections:   10000,
		RealtimeIdleTimeout:      5 * time.Minute,
		RealtimeMaxMessageSize:   65536,
		RealtimeMaxConnectionAge: 30 * time.Minute,
		RealtimeWALSlot:          "gresbase_realtime",
		RealtimeWALPublication:   "gresbase_realtime",
		HooksDir:                 "./gb_hooks",
		HooksWatch:               true,
		MigrationsDir:            "./gb_migrations",
		RateLimitEnabled:         true,
		RateLimitRPS:             100,
		RateLimitBurst:           200,
		MetricsEnabled:           true,
		BackupMaxKeep:            7,
		RLSRole:                  "gresbase_client",
		LogLevel:                 "info",
		OAuthProviders:           make(map[string]OAuthProviderConfig),
	}
}

// Load loads configuration from flags, env vars, and config files.
func Load() *Config {
	cfg := DefaultConfig()

	// Try to load from config file
	viper.SetConfigName("gresbase")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")
	viper.AddConfigPath("/etc/gresbase")
	viper.AddConfigPath("$HOME/.gresbase")

	if err := viper.ReadInConfig(); err == nil {
		viper.Unmarshal(cfg)
	}

	// Override with environment variables
	viper.AutomaticEnv()
	viper.BindEnv("database_url", "DATABASE_URL")
	viper.BindEnv("database_replica_url", "DATABASE_REPLICA_URL")
	viper.BindEnv("database_max_open_conns", "DATABASE_MAX_OPEN_CONNS")
	viper.BindEnv("database_max_idle_conns", "DATABASE_MAX_IDLE_CONNS")
	viper.BindEnv("database_max_idle_time", "DATABASE_MAX_IDLE_TIME")
	viper.BindEnv("jwt_secret", "JWT_SECRET")
	viper.BindEnv("addr", "ADDR")
	viper.BindEnv("domain", "DOMAIN")
	viper.BindEnv("enable_tls", "ENABLE_TLS")
	viper.BindEnv("tls_cert_file", "TLS_CERT_FILE")
	viper.BindEnv("tls_key_file", "TLS_KEY_FILE")
	viper.BindEnv("data_dir", "DATA_DIR")
	viper.BindEnv("cors_allow_credentials", "CORS_ALLOW_CREDENTIALS")
	viper.BindEnv("cors_allowed_origins", "CORS_ALLOWED_ORIGINS")
	viper.BindEnv("jwt_algorithm", "JWT_ALGORITHM")
	viper.BindEnv("jwt_key_id", "JWT_KEY_ID")
	viper.BindEnv("jwt_private_key", "JWT_PRIVATE_KEY")
	viper.BindEnv("jwt_public_key", "JWT_PUBLIC_KEY")
	viper.BindEnv("storage_backend", "STORAGE_BACKEND")
	viper.BindEnv("storage_local_path", "STORAGE_PATH")
	viper.BindEnv("s3_endpoint", "S3_ENDPOINT")
	viper.BindEnv("s3_bucket", "S3_BUCKET")
	viper.BindEnv("s3_region", "S3_REGION")
	viper.BindEnv("s3_access_key", "S3_ACCESS_KEY")
	viper.BindEnv("s3_secret_key", "S3_SECRET_KEY")
	viper.BindEnv("s3_use_ssl", "S3_USE_SSL")
	viper.BindEnv("realtime_max_connections", "REALTIME_MAX_CONNECTIONS")
	viper.BindEnv("hooks_dir", "HOOKS_DIR")
	viper.BindEnv("hooks_watch", "HOOKS_WATCH")
	viper.BindEnv("migrations_dir", "MIGRATIONS_DIR")
	viper.BindEnv("realtime_idle_timeout", "REALTIME_IDLE_TIMEOUT")
	viper.BindEnv("realtime_max_message_size", "REALTIME_MAX_MESSAGE_SIZE")
	viper.BindEnv("realtime_max_connection_age", "REALTIME_MAX_CONNECTION_AGE")
	viper.BindEnv("realtime_wal_enabled", "REALTIME_WAL_ENABLED")
	viper.BindEnv("realtime_wal_slot", "REALTIME_WAL_SLOT")
	viper.BindEnv("realtime_wal_publication", "REALTIME_WAL_PUBLICATION")
	viper.BindEnv("rate_limit_enabled", "RATE_LIMIT_ENABLED")
	viper.BindEnv("rate_limit_rps", "RATE_LIMIT_RPS")
	viper.BindEnv("rate_limit_burst", "RATE_LIMIT_BURST")
	viper.BindEnv("metrics_enabled", "METRICS_ENABLED")
	viper.BindEnv("metrics_token", "METRICS_TOKEN")
	viper.BindEnv("backup_cron", "BACKUP_CRON")
	viper.BindEnv("backup_max_keep", "BACKUP_MAX_KEEP")
	viper.BindEnv("backup_upload_s3", "BACKUP_UPLOAD_S3")
	viper.BindEnv("rls_role", "RLS_ROLE")
	viper.BindEnv("smtp_host", "SMTP_HOST")
	viper.BindEnv("smtp_port", "SMTP_PORT")
	viper.BindEnv("smtp_username", "SMTP_USERNAME")
	viper.BindEnv("smtp_password", "SMTP_PASSWORD")
	viper.BindEnv("smtp_from", "SMTP_FROM")
	viper.BindEnv("dev_mode", "DEV_MODE")
	viper.BindEnv("log_level", "LOG_LEVEL")
	viper.BindEnv("multi_tenant", "MULTI_TENANT")
	viper.BindEnv("hide_start_banner", "HIDE_START_BANNER")

	viper.Unmarshal(cfg)
	if origins := os.Getenv("CORS_ALLOWED_ORIGINS"); origins != "" {
		cfg.CORSAllowedOrigins = splitCSV(origins)
	}
	cfg.JWTAlgorithm = strings.ToUpper(strings.TrimSpace(cfg.JWTAlgorithm))
	if cfg.JWTAlgorithm == "" {
		cfg.JWTAlgorithm = "HS256"
	}
	if cfg.JWTKeyID == "" {
		cfg.JWTKeyID = "gresbase-default"
	}
	if strings.TrimSpace(cfg.RealtimeWALSlot) == "" {
		cfg.RealtimeWALSlot = "gresbase_realtime"
	}
	if strings.TrimSpace(cfg.RealtimeWALPublication) == "" {
		cfg.RealtimeWALPublication = "gresbase_realtime"
	}

	// Ensure data directory exists
	if cfg.DataDir == "" {
		cfg.DataDir = "./gresbase_data"
	}

	// Ensure JWT secret exists
	if cfg.JWTSecret == "" {
		cfg.JWTSecret = os.Getenv("JWT_SECRET")
		if cfg.JWTSecret == "" {
			if cfg.DevMode {
				cfg.JWTSecret = "gresbase-dev-secret-change-in-production"
			} else {
				cfg.JWTSecret = generateRandomSecret()
				cfg.JWTSecretGenerated = true
			}
		}
	}

	// Resolve relative paths against data dir
	if !filepath.IsAbs(cfg.StorageLocal) && cfg.StorageLocal != "" {
		cfg.StorageLocal = filepath.Join(cfg.DataDir, cfg.StorageLocal)
	}

	// Load OAuth from env vars: <PROVIDER>_CLIENT_ID / _CLIENT_SECRET /
	// _REDIRECT_URL for every provider in the catalog.
	for _, provider := range OAuthProviderNames {
		loadOAuthEnv(cfg, provider, strings.ToUpper(provider))
	}

	return cfg
}

// ValidateProduction checks the configuration that must be explicit and stable
// before serving non-development traffic.
func (c *Config) ValidateProduction() error {
	if c == nil {
		return fmt.Errorf("config is required")
	}
	if c.DevMode {
		return nil
	}
	if len(c.CORSAllowedOrigins) == 0 {
		return fmt.Errorf("cors_allowed_origins must include at least one origin in production")
	}
	if c.CORSAllowCredentials && containsWildcardOrigin(c.CORSAllowedOrigins) {
		return fmt.Errorf("cors_allowed_origins cannot include * when cors_allow_credentials is true in production")
	}
	algorithm := strings.ToUpper(strings.TrimSpace(c.JWTAlgorithm))
	if algorithm == "" {
		algorithm = "HS256"
	}
	if algorithm != "HS256" && algorithm != "ES256" {
		return fmt.Errorf("jwt_algorithm must be HS256 or ES256")
	}
	if algorithm == "ES256" && strings.TrimSpace(c.JWTPrivateKey) == "" {
		return fmt.Errorf("JWT_PRIVATE_KEY is required when jwt_algorithm is ES256")
	}

	if algorithm == "HS256" {
		secret := strings.TrimSpace(c.JWTSecret)
		if secret == "" {
			return fmt.Errorf("JWT_SECRET is required in production")
		}
		if c.JWTSecretGenerated {
			return fmt.Errorf("JWT_SECRET must be set explicitly in production; generated secrets invalidate sessions after restart")
		}
		if len(secret) < 32 {
			return fmt.Errorf("JWT_SECRET must be at least 32 characters in production")
		}
		if isWeakJWTSecret(secret) {
			return fmt.Errorf("JWT_SECRET uses an unsafe placeholder value")
		}
	}
	if c.DatabaseMaxOpenConns < 1 {
		return fmt.Errorf("database_max_open_conns must be at least 1")
	}
	if c.DatabaseMaxIdleConns < 0 {
		return fmt.Errorf("database_max_idle_conns must not be negative")
	}
	if c.DatabaseMaxIdleConns > c.DatabaseMaxOpenConns {
		return fmt.Errorf("database_max_idle_conns must not exceed database_max_open_conns")
	}
	if c.RealtimeMaxConnections < 1 {
		return fmt.Errorf("realtime_max_connections must be at least 1")
	}
	if c.RealtimeMaxMessageSize < 1024 {
		return fmt.Errorf("realtime_max_message_size must be at least 1024 bytes")
	}
	if c.RealtimeMaxConnectionAge < 0 {
		return fmt.Errorf("realtime_max_connection_age must not be negative (0 disables the cap)")
	}
	return nil
}

func loadOAuthEnv(cfg *Config, provider, envPrefix string) {
	clientID := os.Getenv(envPrefix + "_CLIENT_ID")
	clientSecret := os.Getenv(envPrefix + "_CLIENT_SECRET")
	redirectURL := os.Getenv(envPrefix + "_REDIRECT_URL")

	if clientID != "" && clientSecret != "" {
		if cfg.OAuthProviders == nil {
			cfg.OAuthProviders = make(map[string]OAuthProviderConfig)
		}
		cfg.OAuthProviders[provider] = OAuthProviderConfig{
			Enabled:      true,
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
		}
	}
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func containsWildcardOrigin(origins []string) bool {
	for _, origin := range origins {
		if strings.TrimSpace(origin) == "*" {
			return true
		}
	}
	return false
}

func generateRandomSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err == nil {
		return hex.EncodeToString(b)
	}
	panic("crypto random source unavailable")
}

func isWeakJWTSecret(secret string) bool {
	lower := strings.ToLower(secret)
	weakFragments := []string{
		"change-this",
		"change_me",
		"dev-secret",
		"development-secret",
		"gresbase-dev-secret",
		"your-64-char",
		"your-secret",
	}
	for _, fragment := range weakFragments {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}

// String returns a human-readable config summary.
func (c *Config) String() string {
	dbMode := "external"
	if c.DatabaseURL == "" {
		dbMode = "embedded"
	}
	return fmt.Sprintf("addr=%s db=%s storage=%s tls=%v dev=%v",
		c.Addr, dbMode, c.StorageBackend, c.EnableTLS, c.DevMode)
}
