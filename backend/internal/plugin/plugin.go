// Package plugin provides the plugin extension system for Gresbase.
package plugin

import (
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/gresbase/gresbase/internal/events"
	"github.com/rs/zerolog/log"
)

// Route describes a route registration contributed by a plugin.
type Route struct {
	Method  string
	Pattern string
	Handler http.Handler
}

// Plugin represents an extension module that hooks into the platform lifecycle.
type Plugin struct {
	ID          string
	Name        string
	Version     string
	Description string
	Enabled     bool
	Priority    int
	Hooks       map[string]events.Handler
	Middlewares []func(http.Handler) http.Handler
	Routes      []Route
}

// New creates a plugin with sensible defaults.
func New(id, name, version string) *Plugin {
	return &Plugin{
		ID:      id,
		Name:    name,
		Version: version,
		Enabled: true,
		Hooks:   map[string]events.Handler{},
		Routes:  []Route{},
	}
}

// Hook registers a named hook handler on the plugin.
func (p *Plugin) Hook(hookName string, handler events.Handler) *Plugin {
	if p.Hooks == nil {
		p.Hooks = map[string]events.Handler{}
	}
	p.Hooks[hookName] = handler
	return p
}

// HookFunc registers a simple function hook on the plugin.
func (p *Plugin) HookFunc(hookName, handlerID string, priority int, fn func(events.Event) error) *Plugin {
	return p.Hook(hookName, events.Handler{ID: handlerID, Priority: priority, Func: fn})
}

// Use appends global HTTP middleware(s) to the plugin contribution.
func (p *Plugin) Use(middlewares ...func(http.Handler) http.Handler) *Plugin {
	p.Middlewares = append(p.Middlewares, middlewares...)
	return p
}

// Handle registers a route handler contribution.
func (p *Plugin) Handle(pattern string, handler http.Handler) *Plugin {
	p.Routes = append(p.Routes, Route{Pattern: pattern, Handler: handler})
	return p
}

// HandleFunc registers a route handler function contribution.
func (p *Plugin) HandleFunc(pattern string, handlerFn http.HandlerFunc) *Plugin {
	return p.Handle(pattern, handlerFn)
}

// Method registers a method-specific route contribution.
func (p *Plugin) Method(method, pattern string, handler http.Handler) *Plugin {
	p.Routes = append(p.Routes, Route{Method: method, Pattern: pattern, Handler: handler})
	return p
}

// MethodFunc registers a method-specific route function contribution.
func (p *Plugin) MethodFunc(method, pattern string, handlerFn http.HandlerFunc) *Plugin {
	return p.Method(method, pattern, handlerFn)
}

// Get registers a GET route contribution.
func (p *Plugin) Get(pattern string, handlerFn http.HandlerFunc) *Plugin {
	return p.MethodFunc(http.MethodGet, pattern, handlerFn)
}

// Post registers a POST route contribution.
func (p *Plugin) Post(pattern string, handlerFn http.HandlerFunc) *Plugin {
	return p.MethodFunc(http.MethodPost, pattern, handlerFn)
}

// Put registers a PUT route contribution.
func (p *Plugin) Put(pattern string, handlerFn http.HandlerFunc) *Plugin {
	return p.MethodFunc(http.MethodPut, pattern, handlerFn)
}

// Patch registers a PATCH route contribution.
func (p *Plugin) Patch(pattern string, handlerFn http.HandlerFunc) *Plugin {
	return p.MethodFunc(http.MethodPatch, pattern, handlerFn)
}

// Delete registers a DELETE route contribution.
func (p *Plugin) Delete(pattern string, handlerFn http.HandlerFunc) *Plugin {
	return p.MethodFunc(http.MethodDelete, pattern, handlerFn)
}

// Group defines a prefixed route group with route-scoped middleware.
func (p *Plugin) Group(prefix string, fn func(*RouteGroup)) *Plugin {
	if fn == nil {
		return p
	}
	fn(&RouteGroup{plugin: p, prefix: joinRoutePattern("", prefix)})
	return p
}

// Mount is an alias for Group for mount-style route registration.
func (p *Plugin) Mount(prefix string, fn func(*RouteGroup)) *Plugin {
	return p.Group(prefix, fn)
}

// RouteGroup is a prefixed route builder with route-scoped middleware.
type RouteGroup struct {
	plugin      *Plugin
	prefix      string
	middlewares []func(http.Handler) http.Handler
}

// Use appends route-scoped middlewares to the group.
func (g *RouteGroup) Use(middlewares ...func(http.Handler) http.Handler) *RouteGroup {
	g.middlewares = append(g.middlewares, middlewares...)
	return g
}

// Handle registers a route handler contribution within the group.
func (g *RouteGroup) Handle(pattern string, handler http.Handler) *RouteGroup {
	if g == nil || g.plugin == nil {
		return g
	}
	g.plugin.Routes = append(g.plugin.Routes, Route{
		Pattern: joinRoutePattern(g.prefix, pattern),
		Handler: wrapHandler(handler, g.middlewares),
	})
	return g
}

// HandleFunc registers a route handler function contribution within the group.
func (g *RouteGroup) HandleFunc(pattern string, handlerFn http.HandlerFunc) *RouteGroup {
	return g.Handle(pattern, handlerFn)
}

// Method registers a method-specific route contribution within the group.
func (g *RouteGroup) Method(method, pattern string, handler http.Handler) *RouteGroup {
	if g == nil || g.plugin == nil {
		return g
	}
	g.plugin.Routes = append(g.plugin.Routes, Route{
		Method:  method,
		Pattern: joinRoutePattern(g.prefix, pattern),
		Handler: wrapHandler(handler, g.middlewares),
	})
	return g
}

// MethodFunc registers a method-specific route function contribution within the group.
func (g *RouteGroup) MethodFunc(method, pattern string, handlerFn http.HandlerFunc) *RouteGroup {
	return g.Method(method, pattern, handlerFn)
}

// Get registers a GET route within the group.
func (g *RouteGroup) Get(pattern string, handlerFn http.HandlerFunc) *RouteGroup {
	return g.MethodFunc(http.MethodGet, pattern, handlerFn)
}

// Post registers a POST route within the group.
func (g *RouteGroup) Post(pattern string, handlerFn http.HandlerFunc) *RouteGroup {
	return g.MethodFunc(http.MethodPost, pattern, handlerFn)
}

// Put registers a PUT route within the group.
func (g *RouteGroup) Put(pattern string, handlerFn http.HandlerFunc) *RouteGroup {
	return g.MethodFunc(http.MethodPut, pattern, handlerFn)
}

// Patch registers a PATCH route within the group.
func (g *RouteGroup) Patch(pattern string, handlerFn http.HandlerFunc) *RouteGroup {
	return g.MethodFunc(http.MethodPatch, pattern, handlerFn)
}

// Delete registers a DELETE route within the group.
func (g *RouteGroup) Delete(pattern string, handlerFn http.HandlerFunc) *RouteGroup {
	return g.MethodFunc(http.MethodDelete, pattern, handlerFn)
}

// Group defines a nested prefixed route group.
func (g *RouteGroup) Group(prefix string, fn func(*RouteGroup)) *RouteGroup {
	if g == nil || fn == nil {
		return g
	}
	fn(&RouteGroup{
		plugin:      g.plugin,
		prefix:      joinRoutePattern(g.prefix, prefix),
		middlewares: append([]func(http.Handler) http.Handler(nil), g.middlewares...),
	})
	return g
}

// Mount is an alias for Group for nested route groups.
func (g *RouteGroup) Mount(prefix string, fn func(*RouteGroup)) *RouteGroup {
	return g.Group(prefix, fn)
}

func wrapHandler(handler http.Handler, middlewares []func(http.Handler) http.Handler) http.Handler {
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

func joinRoutePattern(prefix, pattern string) string {
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

// Registry manages the collection of loaded plugins.
type Registry struct {
	mu      sync.RWMutex
	plugins map[string]*Plugin
	order   []string
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
	if p.Hooks == nil {
		p.Hooks = map[string]events.Handler{}
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

	if p, ok := r.plugins[id]; ok {
		p.Enabled = false
	}
	delete(r.plugins, id)
	for i, pid := range r.order {
		if pid == id {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}

	log.Info().Str("plugin_id", id).Msg("Plugin unregistered")
}

// Enable marks a plugin enabled.
func (r *Registry) Enable(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.plugins[id]
	if !ok {
		return fmt.Errorf("plugin %q not found", id)
	}
	p.Enabled = true
	return nil
}

// Disable marks a plugin disabled.
func (r *Registry) Disable(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.plugins[id]
	if !ok {
		return fmt.Errorf("plugin %q not found", id)
	}
	p.Enabled = false
	return nil
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
			Hooks:       map[string]events.Handler{},
		},
		{
			ID:          "gresbase.collections",
			Name:        "Collections",
			Version:     "0.1.0",
			Description: "Dynamic collection management with auto-migration",
			Enabled:     true,
			Priority:    events.PriorityHighest,
			Hooks:       map[string]events.Handler{},
		},
		{
			ID:          "gresbase.realtime",
			Name:        "Realtime",
			Version:     "0.1.0",
			Description: "WebSocket and SSE realtime engine with channel pub/sub",
			Enabled:     true,
			Priority:    events.PriorityDefault,
			Hooks:       map[string]events.Handler{},
		},
		{
			ID:          "gresbase.storage",
			Name:        "File Storage",
			Version:     "0.1.0",
			Description: "Local and S3-compatible file storage with image processing",
			Enabled:     true,
			Priority:    events.PriorityDefault,
			Hooks:       map[string]events.Handler{},
		},
		{
			ID:          "gresbase.acme",
			Name:        "ACME CA",
			Version:     "0.1.0",
			Description: "Embedded ACME-compatible Certificate Authority",
			Enabled:     true,
			Priority:    events.PriorityDefault,
			Hooks:       map[string]events.Handler{},
		},
		{
			ID:          "gresbase.jobs",
			Name:        "Job Scheduler",
			Version:     "0.1.0",
			Description: "Cron-based job scheduler with database persistence",
			Enabled:     true,
			Priority:    events.PriorityDefault,
			Hooks:       map[string]events.Handler{},
		},
		{
			ID:          "gresbase.mailer",
			Name:        "Email",
			Version:     "0.1.0",
			Description: "SMTP email delivery with HTML templates",
			Enabled:     true,
			Priority:    events.PriorityDefault,
			Hooks:       map[string]events.Handler{},
		},
		{
			ID:          "gresbase.logging",
			Name:        "Audit Logging",
			Version:     "0.1.0",
			Description: "Structured audit logging with PostgreSQL storage",
			Enabled:     true,
			Priority:    events.PriorityLow,
			Hooks:       map[string]events.Handler{},
		},
		{
			ID:          "gresbase.backup",
			Name:        "Backups",
			Version:     "0.1.0",
			Description: "Database + storage backup and restore",
			Enabled:     true,
			Priority:    events.PriorityLow,
			Hooks:       map[string]events.Handler{},
		},
	}
}
