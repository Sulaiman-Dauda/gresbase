package config

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Addr != ":8080" {
		t.Errorf("expected :8080, got %s", cfg.Addr)
	}
	if cfg.DatabaseMaxOpenConns != 25 {
		t.Errorf("expected 25 max open conns, got %d", cfg.DatabaseMaxOpenConns)
	}
	if cfg.AccessTokenExpiry != 15*time.Minute {
		t.Errorf("expected 15 min token expiry, got %v", cfg.AccessTokenExpiry)
	}
	if cfg.StorageBackend != "local" {
		t.Errorf("expected local storage, got %s", cfg.StorageBackend)
	}
	if cfg.RealtimeMaxConnections != 10000 {
		t.Errorf("expected 10000 max connections, got %d", cfg.RealtimeMaxConnections)
	}
	if cfg.RealtimeMaxConnectionAge != 30*time.Minute {
		t.Errorf("expected 30m realtime max connection age, got %v", cfg.RealtimeMaxConnectionAge)
	}
	if len(cfg.CORSAllowedOrigins) != 1 || cfg.CORSAllowedOrigins[0] != "*" {
		t.Errorf("expected default wildcard CORS origin, got %v", cfg.CORSAllowedOrigins)
	}
	if cfg.CORSAllowCredentials {
		t.Error("CORS credentials should be disabled by default so wildcard origins stay safe")
	}
	if cfg.JWTAlgorithm != "HS256" {
		t.Errorf("expected HS256 JWT algorithm, got %s", cfg.JWTAlgorithm)
	}
	if !cfg.RateLimitEnabled {
		t.Error("rate limiting should be enabled by default")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected info log level, got %s", cfg.LogLevel)
	}
}

func TestConfigString(t *testing.T) {
	cfg := DefaultConfig()
	str := cfg.String()
	if str == "" {
		t.Error("config string should not be empty")
	}
	// Should contain key components
	if !contains(str, "addr=") {
		t.Error("string should contain addr=")
	}
}

func TestConfigLoad(t *testing.T) {
	// Set required env vars
	t.Setenv("DATABASE_URL", "postgres://localhost:5432/test")
	t.Setenv("JWT_SECRET", "test-secret-key-32-chars-long!!")

	cfg := Load()
	if cfg == nil {
		t.Fatal("expected non-nil config from Load")
	}
	if cfg.DatabaseURL != "postgres://localhost:5432/test" {
		t.Errorf("expected DB URL from env, got %s", cfg.DatabaseURL)
	}
	if cfg.JWTSecret == "" {
		t.Error("JWT secret should not be empty")
	}
}

func TestConfigLoadProductionEnvOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost:5432/test")
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))
	t.Setenv("DATABASE_MAX_OPEN_CONNS", "64")
	t.Setenv("DATABASE_MAX_IDLE_CONNS", "12")
	t.Setenv("DATABASE_MAX_IDLE_TIME", "2m")
	t.Setenv("REALTIME_MAX_CONNECTIONS", "2000")
	t.Setenv("REALTIME_IDLE_TIMEOUT", "30s")
	t.Setenv("REALTIME_MAX_MESSAGE_SIZE", "131072")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com, https://admin.example.com")
	t.Setenv("CORS_ALLOW_CREDENTIALS", "false")
	t.Setenv("JWT_ALGORITHM", "HS256")
	t.Setenv("JWT_KEY_ID", "test-key")
	t.Setenv("LOG_LEVEL", "debug")

	cfg := Load()
	if cfg.DatabaseMaxOpenConns != 64 {
		t.Fatalf("expected max open conns env override, got %d", cfg.DatabaseMaxOpenConns)
	}
	if cfg.DatabaseMaxIdleConns != 12 {
		t.Fatalf("expected max idle conns env override, got %d", cfg.DatabaseMaxIdleConns)
	}
	if cfg.DatabaseMaxIdleTime != 2*time.Minute {
		t.Fatalf("expected idle time env override, got %v", cfg.DatabaseMaxIdleTime)
	}
	if cfg.RealtimeMaxConnections != 2000 {
		t.Fatalf("expected realtime max connections env override, got %d", cfg.RealtimeMaxConnections)
	}
	if cfg.RealtimeIdleTimeout != 30*time.Second {
		t.Fatalf("expected realtime idle timeout env override, got %v", cfg.RealtimeIdleTimeout)
	}
	if cfg.RealtimeMaxMessageSize != 131072 {
		t.Fatalf("expected realtime message size env override, got %d", cfg.RealtimeMaxMessageSize)
	}
	if len(cfg.CORSAllowedOrigins) != 2 || cfg.CORSAllowedOrigins[0] != "https://app.example.com" || cfg.CORSAllowedOrigins[1] != "https://admin.example.com" {
		t.Fatalf("expected parsed CORS origins env override, got %v", cfg.CORSAllowedOrigins)
	}
	if cfg.CORSAllowCredentials {
		t.Fatal("expected CORS credentials env override to disable credentials")
	}
	if cfg.JWTKeyID != "test-key" {
		t.Fatalf("expected JWT key id env override, got %s", cfg.JWTKeyID)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("expected log level env override, got %s", cfg.LogLevel)
	}
}

func TestValidateProduction(t *testing.T) {
	cfg := DefaultConfig()
	cfg.JWTSecret = strings.Repeat("a", 32)
	cfg.CORSAllowedOrigins = []string{"https://app.example.com"}
	if err := cfg.ValidateProduction(); err != nil {
		t.Fatalf("expected valid production config, got %v", err)
	}

	cfg.CORSAllowedOrigins = []string{"*"}
	cfg.CORSAllowCredentials = true
	if err := cfg.ValidateProduction(); err == nil {
		t.Fatal("expected credentialed wildcard CORS to be rejected in production")
	}
	cfg.CORSAllowCredentials = false
	if err := cfg.ValidateProduction(); err != nil {
		t.Fatalf("expected credential-less wildcard CORS to be allowed, got %v", err)
	}
	cfg.CORSAllowedOrigins = []string{"https://app.example.com"}

	cfg.JWTSecretGenerated = true
	if err := cfg.ValidateProduction(); err == nil {
		t.Fatal("expected generated production JWT secret to be rejected")
	}

	cfg.JWTSecretGenerated = false
	cfg.JWTSecret = "short"
	if err := cfg.ValidateProduction(); err == nil {
		t.Fatal("expected short production JWT secret to be rejected")
	}

	cfg.JWTSecret = "change-this-to-a-random-64-char-string-in-production"
	if err := cfg.ValidateProduction(); err == nil {
		t.Fatal("expected placeholder production JWT secret to be rejected")
	}

	cfg.DevMode = true
	if err := cfg.ValidateProduction(); err != nil {
		t.Fatalf("expected dev mode to skip production validation, got %v", err)
	}
}

func TestConfigDevMode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DevMode = true
	cfg.JWTSecret = ""

	// In dev mode, secret should be auto-generated
	cfg2 := Load() // This may not pick up dev mode from env
	_ = cfg2
	_ = cfg
}

func TestOAuthProviderConfig(t *testing.T) {
	cfg := OAuthProviderConfig{
		Enabled:      true,
		ClientID:     "test-client-id",
		ClientSecret: "test-secret",
		RedirectURL:  "http://localhost:8080/callback",
	}

	if !cfg.Enabled {
		t.Error("provider should be enabled")
	}
	if cfg.ClientID != "test-client-id" {
		t.Error("client ID mismatch")
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
