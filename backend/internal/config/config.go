package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
)

// Config holds all configuration for the Gresbase platform.
type Config struct {
	// Server
	Addr       string `mapstructure:"addr"`
	Domain     string `mapstructure:"domain"`
	EnableTLS  bool   `mapstructure:"enable_tls"`
	CertEmail  string `mapstructure:"cert_email"`
	DataDir    string `mapstructure:"data_dir"`

	// Database
	DatabaseURL          string        `mapstructure:"database_url"`
	DatabaseMaxOpenConns int           `mapstructure:"database_max_open_conns"`
	DatabaseMaxIdleConns int           `mapstructure:"database_max_idle_conns"`
	DatabaseMaxIdleTime  time.Duration `mapstructure:"database_max_idle_time"`
	EmbeddedPort         int           `mapstructure:"embedded_port"` // port for embedded PostgreSQL

	// Auth
	JWTSecret           string        `mapstructure:"jwt_secret"`
	AccessTokenExpiry   time.Duration `mapstructure:"access_token_expiry"`
	RefreshTokenExpiry  time.Duration `mapstructure:"refresh_token_expiry"`
	AdminTokenExpiry    time.Duration `mapstructure:"admin_token_expiry"`

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

	// ACME
	ACMECacheDir string `mapstructure:"acme_cache_dir"`
	ACMEEnabled  bool   `mapstructure:"acme_enabled"`

	// Realtime
	RealtimeMaxConnections int           `mapstructure:"realtime_max_connections"`
	RealtimeIdleTimeout    time.Duration `mapstructure:"realtime_idle_timeout"`
	RealtimeMaxMessageSize int64         `mapstructure:"realtime_max_message_size"`

	// Rate Limiting
	RateLimitEnabled bool `mapstructure:"rate_limit_enabled"`
	RateLimitRPS     int  `mapstructure:"rate_limit_rps"`
	RateLimitBurst   int  `mapstructure:"rate_limit_burst"`

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
		Addr:                   ":8080",
		DatabaseMaxOpenConns:   25,
		DatabaseMaxIdleConns:   5,
		DatabaseMaxIdleTime:    5 * time.Minute,
		EmbeddedPort:           5433,
		AccessTokenExpiry:      15 * time.Minute,
		RefreshTokenExpiry:     7 * 24 * time.Hour,
		AdminTokenExpiry:       24 * time.Hour,
		StorageBackend:         "local",
		StorageLocal:           "./storage",
		ACMECacheDir:           "./.certmagic",
		RealtimeMaxConnections: 10000,
		RealtimeIdleTimeout:    5 * time.Minute,
		RealtimeMaxMessageSize: 65536,
		RateLimitEnabled:       true,
		RateLimitRPS:           100,
		RateLimitBurst:         200,
		LogLevel:               "info",
		OAuthProviders:          make(map[string]OAuthProviderConfig),
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
	viper.BindEnv("jwt_secret", "JWT_SECRET")
	viper.BindEnv("addr", "ADDR")
	viper.BindEnv("data_dir", "DATA_DIR")
	viper.BindEnv("storage_local_path", "STORAGE_PATH")

	viper.Unmarshal(cfg)

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
				// Generate a random secret for production
				cfg.JWTSecret = generateRandomSecret()
			}
		}
	}

	// Resolve relative paths against data dir
	if !filepath.IsAbs(cfg.StorageLocal) && cfg.StorageLocal != "" {
		cfg.StorageLocal = filepath.Join(cfg.DataDir, cfg.StorageLocal)
	}

	// Load OAuth from env vars
	loadOAuthEnv(cfg, "google", "GOOGLE")
	loadOAuthEnv(cfg, "github", "GITHUB")
	loadOAuthEnv(cfg, "microsoft", "MICROSOFT")
	loadOAuthEnv(cfg, "gitlab", "GITLAB")
	loadOAuthEnv(cfg, "discord", "DISCORD")
	loadOAuthEnv(cfg, "facebook", "FACEBOOK")
	loadOAuthEnv(cfg, "apple", "APPLE")

	return cfg
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

func generateRandomSecret() string {
	b := make([]byte, 32)
	if f, err := os.Open("/dev/urandom"); err == nil {
		f.Read(b)
		f.Close()
		return fmt.Sprintf("%x", b)
	}
	// Fallback — not cryptographically secure
	return fmt.Sprintf("gresbase-%d", time.Now().UnixNano())
}

// String returns a human-readable config summary.
func (c *Config) String() string {
	dbMode := "external"
	if c.DatabaseURL == "" {
		dbMode = "embedded"
	}
	return fmt.Sprintf("addr=%s db=%s storage=%s acme=%v dev=%v",
		c.Addr, dbMode, c.StorageBackend, c.ACMEEnabled, c.DevMode)
}


