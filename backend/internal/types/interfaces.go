// Package types defines shared interfaces and types used across Gresbase packages.
// This package has no dependencies on internal packages, preventing import cycles.
package types

import (
	"net/http"
)

// APIServer is the interface an HTTP server must implement to be used by the App.
// The chi-based api.Server, any custom HTTP server, or even a test mock
// can satisfy this interface.
type APIServer interface {
	Start(addr string) error
	Shutdown() error
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

// Store is a key-value store scoped to a plugin or component.
type Store interface {
	Get(key string) (any, bool)
	Set(key string, value any)
	Delete(key string)
}

// Logger is a minimal logging interface.
type Logger interface {
	Debug(msg string)
	Info(msg string)
	Warn(msg string)
	Error(msg string)
}

// PluginInfo describes a plugin.
type PluginInfo struct {
	Name        string
	Version     string
	Description string
	Author      string
	Required    bool
}

// Plugin is the interface a plugin must implement.
type Plugin interface {
	Info() PluginInfo
	Init(app any) error // Uses any to avoid circular imports with app package
}

// Collection is a collection definition (used for cross-package type sharing).
type Collection struct {
	ID     string
	Name   string
	Type   string // "base", "auth", "view"
	Schema []SchemaField
	System bool
}

// SchemaField is a field definition in a collection schema.
type SchemaField struct {
	ID       string
	Name     string
	Type     string
	System   bool
	Required bool
	Unique   bool
	Options  map[string]any
}

// Record is a generic record map used for cross-package references.
type Record map[string]any
