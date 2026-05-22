// Package plugin provides the plugin extension system for Gresbase.
package plugin

import (
	"fmt"
	"sync"

	"github.com/gresbase/gresbase/internal/events"
	"github.com/rs/zerolog/log"
)

// Plugin represents an extension module that hooks into the platform lifecycle.
type Plugin struct {
	ID          string
	Name        string
	Version     string
	Description string
	Enabled     bool
	Priority    int
	Hooks       map[string]events.Handler
}

// Registry manages the collection of loaded plugins.
type Registry struct {
	mu       sync.RWMutex
	plugins  map[string]*Plugin
	order    []string
}

// NewRegistry creates a plugin registry.
func NewRegistry() *Registry {
	return &Registry{
		plugins: make(map[string]*Plugin),
		order:   make([]string, 0),
	}
}

// Register adds a plugin to the registry.
func (r *Registry) Register(p *Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.plugins[p.ID]; exists {
		return fmt.Errorf("plugin %q already registered", p.ID)
	}

	r.plugins[p.ID] = p
	r.order = append(r.order, p.ID)

	log.Info().Str("plugin", p.Name).Str("version", p.Version).Msg("Plugin registered")
	return nil
}

// Unregister removes a plugin.
func (r *Registry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.plugins, id)
	for i, pid := range r.order {
		if pid == id {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}

	log.Info().Str("plugin_id", id).Msg("Plugin unregistered")
}

// Get returns a plugin by ID.
func (r *Registry) Get(id string) *Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.plugins[id]
}

// List returns all registered plugins in order.
func (r *Registry) List() []*Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Plugin, 0, len(r.order))
	for _, id := range r.order {
		if p, ok := r.plugins[id]; ok {
			result = append(result, p)
		}
	}
	return result
}

// GetHooks returns all hooks from all enabled plugins for a given hook name.
func (r *Registry) GetHooks(hookName string) []events.Handler {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var handlers []events.Handler
	for _, id := range r.order {
		p := r.plugins[id]
		if !p.Enabled {
			continue
		}
		if h, ok := p.Hooks[hookName]; ok {
			h.Priority = p.Priority
			handlers = append(handlers, h)
		}
	}
	return handlers
}

// LoadBuiltinPlugins loads the default built-in plugins.
func LoadBuiltinPlugins() []*Plugin {
	return []*Plugin{
		{
			ID:          "gresbase.auth",
			Name:        "Authentication",
			Version:     "0.1.0",
			Description: "Built-in authentication system with JWT, OAuth, OTP, and API keys",
			Enabled:     true,
			Priority:    events.PriorityHighest,
		},
		{
			ID:          "gresbase.collections",
			Name:        "Collections",
			Version:     "0.1.0",
			Description: "Dynamic collection management with auto-migration",
			Enabled:     true,
			Priority:    events.PriorityHighest,
		},
		{
			ID:          "gresbase.realtime",
			Name:        "Realtime",
			Version:     "0.1.0",
			Description: "WebSocket and SSE realtime engine with channel pub/sub",
			Enabled:     true,
			Priority:    events.PriorityDefault,
		},
		{
			ID:          "gresbase.storage",
			Name:        "File Storage",
			Version:     "0.1.0",
			Description: "Local and S3-compatible file storage with image processing",
			Enabled:     true,
			Priority:    events.PriorityDefault,
		},
		{
			ID:          "gresbase.acme",
			Name:        "ACME CA",
			Version:     "0.1.0",
			Description: "Embedded ACME-compatible Certificate Authority",
			Enabled:     true,
			Priority:    events.PriorityDefault,
		},
		{
			ID:          "gresbase.jobs",
			Name:        "Job Scheduler",
			Version:     "0.1.0",
			Description: "Cron-based job scheduler with database persistence",
			Enabled:     true,
			Priority:    events.PriorityDefault,
		},
		{
			ID:          "gresbase.mailer",
			Name:        "Email",
			Version:     "0.1.0",
			Description: "SMTP email delivery with HTML templates",
			Enabled:     true,
			Priority:    events.PriorityDefault,
		},
		{
			ID:          "gresbase.logging",
			Name:        "Audit Logging",
			Version:     "0.1.0",
			Description: "Structured audit logging with PostgreSQL storage",
			Enabled:     true,
			Priority:    events.PriorityLow,
		},
		{
			ID:          "gresbase.backup",
			Name:        "Backups",
			Version:     "0.1.0",
			Description: "Database + storage backup and restore",
			Enabled:     true,
			Priority:    events.PriorityLow,
		},
	}
}
