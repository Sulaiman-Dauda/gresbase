package plugin

import (
	"testing"

	"github.com/gresbase/gresbase/internal/events"
)

func TestNewRegistry(t *testing.T) {
	reg := NewRegistry()
	if reg == nil {
		t.Fatal("expected non-nil registry")
	}
	if len(reg.List()) != 0 {
		t.Errorf("expected 0 plugins, got %d", len(reg.List()))
	}
}

func TestRegisterPlugin(t *testing.T) {
	reg := NewRegistry()

	p := &Plugin{
		ID:      "test.plugin",
		Name:    "Test Plugin",
		Version: "1.0.0",
		Enabled: true,
	}

	err := reg.Register(p)
	if err != nil {
		t.Fatal(err)
	}

	if len(reg.List()) != 1 {
		t.Errorf("expected 1 plugin, got %d", len(reg.List()))
	}

	// Duplicate registration should fail
	err = reg.Register(p)
	if err == nil {
		t.Error("expected error for duplicate registration")
	}
}

func TestUnregisterPlugin(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&Plugin{ID: "p1", Name: "One", Enabled: true})
	reg.Register(&Plugin{ID: "p2", Name: "Two", Enabled: true})

	if len(reg.List()) != 2 {
		t.Errorf("expected 2 plugins, got %d", len(reg.List()))
	}

	reg.Unregister("p1")
	if len(reg.List()) != 1 {
		t.Errorf("expected 1 plugin after unregister, got %d", len(reg.List()))
	}
	if reg.Get("p1") != nil {
		t.Error("p1 should be nil after unregister")
	}
}

func TestGetHooks(t *testing.T) {
	reg := NewRegistry()

	p1 := &Plugin{ID: "p1", Name: "One", Enabled: true, Priority: 10}
	p1.Hooks = map[string]events.Handler{
		"onCreate": {ID: "h1", Func: func(e events.Event) error { return nil }},
	}

	p2 := &Plugin{ID: "p2", Name: "Two", Enabled: true, Priority: 5}
	p2.Hooks = map[string]events.Handler{
		"onCreate": {ID: "h2", Func: func(e events.Event) error { return nil }},
		"onDelete": {ID: "h3", Func: func(e events.Event) error { return nil }},
	}

	reg.Register(p1)
	reg.Register(p2)

	hooks := reg.GetHooks("onCreate")
	if len(hooks) != 2 {
		t.Errorf("expected 2 onCreate hooks, got %d", len(hooks))
	}

	hooks = reg.GetHooks("onDelete")
	if len(hooks) != 1 {
		t.Errorf("expected 1 onDelete hook, got %d", len(hooks))
	}

	hooks = reg.GetHooks("nonexistent")
	if len(hooks) != 0 {
		t.Errorf("expected 0 hooks for nonexistent, got %d", len(hooks))
	}
}

func TestDisabledPluginHooks(t *testing.T) {
	reg := NewRegistry()

	p := &Plugin{ID: "p1", Name: "Disabled", Enabled: false}
	p.Hooks = map[string]events.Handler{
		"onEvent": {ID: "h1"},
	}

	reg.Register(p)

	hooks := reg.GetHooks("onEvent")
	if len(hooks) != 0 {
		t.Errorf("disabled plugin should not expose hooks, got %d", len(hooks))
	}
}

func TestLoadBuiltinPlugins(t *testing.T) {
	plugins := LoadBuiltinPlugins()
	if len(plugins) == 0 {
		t.Error("expected at least some builtin plugins")
	}

	// All should be enabled
	for _, p := range plugins {
		if !p.Enabled {
			t.Errorf("builtin plugin %q should be enabled", p.ID)
		}
		if p.ID == "" {
			t.Error("plugin should have an ID")
		}
		if p.Name == "" {
			t.Error("plugin should have a Name")
		}
	}
}
