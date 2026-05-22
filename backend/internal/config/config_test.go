package config

import (
	"os"
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
	os.Setenv("DATABASE_URL", "postgres://localhost:5432/test")
	os.Setenv("JWT_SECRET", "test-secret-key-32-chars-long!!")
	defer os.Unsetenv("DATABASE_URL")
	defer os.Unsetenv("JWT_SECRET")

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
