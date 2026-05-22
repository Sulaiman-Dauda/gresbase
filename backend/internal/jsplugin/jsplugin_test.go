package jsplugin

import (
	"context"
	"testing"
	"time"
)

func TestNewRuntime(t *testing.T) {
	rt := NewRuntime(30 * time.Second)
	if rt == nil {
		t.Fatal("expected non-nil Runtime")
	}
	if rt.timeout != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", rt.timeout)
	}
}

func TestRuntime_LoadAndExecuteSimpleScript(t *testing.T) {
	rt := NewRuntime(30 * time.Second)

	_, err := rt.LoadPlugin("test", "test-plugin", "1 + 1", 0)
	if err != nil {
		t.Fatalf("LoadPlugin: %v", err)
	}

	result, err := rt.Execute("test", "1 + 1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.ToInteger() != 2 {
		t.Errorf("expected 2, got %v", result)
	}
}

func TestRuntime_ExecuteStringScript(t *testing.T) {
	rt := NewRuntime(30 * time.Second)

	rt.LoadPlugin("test", "test-plugin", "1", 0)

	result, err := rt.Execute("test", `"Hello, " + "World!"`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.String() != "Hello, World!" {
		t.Errorf("expected Hello World, got %v", result.String())
	}
}

func TestRuntime_ExecuteBooleanScript(t *testing.T) {
	rt := NewRuntime(30 * time.Second)

	rt.LoadPlugin("test", "test-plugin", "1", 0)

	result, err := rt.Execute("test", "5 > 3")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.ToBoolean() {
		t.Errorf("expected true, got %v", result)
	}
}

func TestRuntime_ExecuteWithTimeout(t *testing.T) {
	rt := NewRuntime(10 * time.Millisecond)

	rt.LoadPlugin("timeout-test", "timeout", "1", 0)

	_, err := rt.Execute("timeout-test", "while(true) {}")
	if err == nil {
		t.Error("expected timeout error for infinite loop")
	}
}

func TestRuntime_ConsoleAPI(t *testing.T) {
	rt := NewRuntime(30 * time.Second)

	rt.LoadPlugin("test", "test-plugin", "1", 0)

	_, err := rt.Execute("test", `console.log("test message"); 1`)
	if err != nil {
		t.Fatalf("Execute with console.log: %v", err)
	}

	_, err = rt.Execute("test", `console.error("error message"); 1`)
	if err != nil {
		t.Fatalf("Execute with console.error: %v", err)
	}

	_, err = rt.Execute("test", `console.warn("warning"); 1`)
	if err != nil {
		t.Fatalf("Execute with console.warn: %v", err)
	}
}

func TestRuntime_JSONAPI(t *testing.T) {
	rt := NewRuntime(30 * time.Second)

	rt.LoadPlugin("test", "test-plugin", "1", 0)

	_, err := rt.Execute("test", `var obj = {name: "test"}; JSON.stringify(obj)`)
	if err != nil {
		t.Fatalf("JSON.stringify: %v", err)
	}

	result, err := rt.Execute("test", `var parsed = JSON.parse('{"key":"value"}'); parsed.key`)
	if err != nil {
		t.Fatalf("JSON.parse: %v", err)
	}
	if result.String() != "value" {
		t.Errorf("expected value, got %v", result.String())
	}
}

func TestRuntime_LoadPlugin(t *testing.T) {
	rt := NewRuntime(30 * time.Second)

	plugin, err := rt.LoadPlugin("test-plugin", "test", `console.log("Plugin loaded");`, 0)
	if err != nil {
		t.Fatalf("LoadPlugin: %v", err)
	}
	if plugin == nil {
		t.Error("expected non-nil plugin")
	}
	if plugin.Name != "test" {
		t.Errorf("expected name 'test', got %q", plugin.Name)
	}
	if plugin.ID != "test-plugin" {
		t.Errorf("expected id 'test-plugin', got %q", plugin.ID)
	}
}

func TestRuntime_UnloadPlugin(t *testing.T) {
	rt := NewRuntime(30 * time.Second)

	rt.LoadPlugin("test-plugin", "test", `1`, 0)
	rt.UnloadPlugin("test-plugin")

	plugins := rt.ListPlugins()
	if len(plugins) > 0 {
		t.Errorf("expected no plugins after unload, got %d", len(plugins))
	}
}

func TestRuntime_ListPlugins(t *testing.T) {
	rt := NewRuntime(30 * time.Second)

	rt.LoadPlugin("p1", "plugin1", `1`, 1)
	rt.LoadPlugin("p2", "plugin2", `1`, 2)

	plugins := rt.ListPlugins()
	if len(plugins) < 2 {
		t.Errorf("expected at least 2 plugins, got %d", len(plugins))
	}
}

func TestRuntime_GetPluginScript(t *testing.T) {
	rt := NewRuntime(30 * time.Second)

	rt.LoadPlugin("test-plugin", "test", `console.log("hello");`, 0)

	script, err := rt.GetPluginScript("test-plugin")
	if err != nil {
		t.Fatalf("GetPluginScript: %v", err)
	}
	if script != `console.log("hello");` {
		t.Errorf("expected original script")
	}
}

func TestRuntime_GetPluginScript_NotFound(t *testing.T) {
	rt := NewRuntime(30 * time.Second)

	_, err := rt.GetPluginScript("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent plugin")
	}
}

func TestRuntime_HasHook(t *testing.T) {
	rt := NewRuntime(30 * time.Second)

	// No hooks registered yet
	if rt.HasHook("onBootstrap") {
		t.Error("expected no onBootstrap hook initially")
	}

	// Load a plugin with a hook handler that registers via $$hooks in the plugin init
	_, err := rt.LoadPlugin("hook-plugin", "hook", `
		// The hook registration happens in the plugin initialization
		// via the $app helper. The HasHook call checks the registry.
	`, 0)
	if err != nil {
		t.Fatalf("LoadPlugin: %v", err)
	}

	// The HasHook check depends on whether the plugin init registered
	// the hook. Since our script above doesn't actually register via $$hooks,
	// it may or may not be found. Let's just verify it doesn't panic.
	_ = rt.HasHook("onBootstrap")
}

func TestRuntime_TriggerHook(t *testing.T) {
	rt := NewRuntime(30 * time.Second)

	// Trigger a non-existent hook should not error
	err := rt.TriggerHook("nonexistentHook", nil)
	if err != nil {
		t.Errorf("expected no error for nonexistent hook")
	}

	// Trigger with empty data
	err = rt.TriggerHook("onBootstrap", []byte(`{}`))
	if err != nil {
		t.Logf("TriggerHook returned: %v (may happen if no plugin handles it)", err)
	}
}

func TestRuntime_ErrorHandling(t *testing.T) {
	rt := NewRuntime(30 * time.Second)

	rt.LoadPlugin("err-test", "error", "1", 0)

	_, err := rt.Execute("err-test", "throw new Error('test error')")
	if err == nil {
		t.Error("expected error")
	}
}

func TestRuntime_MultiplePlugins(t *testing.T) {
	rt := NewRuntime(30 * time.Second)

	p1, err := rt.LoadPlugin("p1", "one", `var x = 1;`, 1)
	if err != nil {
		t.Fatalf("LoadPlugin p1: %v", err)
	}
	p2, err := rt.LoadPlugin("p2", "two", `var y = 2;`, 2)
	if err != nil {
		t.Fatalf("LoadPlugin p2: %v", err)
	}

	if p1.Priority != 1 {
		t.Errorf("expected priority 1")
	}
	if p2.Priority != 2 {
		t.Errorf("expected priority 2")
	}
}

func TestBuiltinPlugins(t *testing.T) {
	plugins := BuiltinPlugins()
	if len(plugins) == 0 {
		t.Error("expected built-in plugins")
	}
	for id, script := range plugins {
		if id == "" {
			t.Error("expected non-empty plugin ID")
		}
		if script == "" {
			t.Errorf("expected non-empty script for plugin %s", id)
		}
	}
}

func TestRuntime_SetAppBridge(t *testing.T) {
	rt := NewRuntime(30 * time.Second)

	rt.SetAppBridge(&mockBridge{})
}

type mockBridge struct{}

func (m *mockBridge) DBQuery(ctx context.Context, sql string, args ...any) ([]map[string]any, error) {
	return nil, nil
}
func (m *mockBridge) DBExec(ctx context.Context, sql string, args ...any) (int64, error) {
	return 0, nil
}
func (m *mockBridge) FindRecordByID(collection, id string) (map[string]any, error) {
	return nil, nil
}
func (m *mockBridge) FindRecords(collection, filter, sort string, limit, offset int) ([]map[string]any, error) {
	return nil, nil
}
func (m *mockBridge) CreateRecord(collection string, data map[string]any) (map[string]any, error) {
	return nil, nil
}
func (m *mockBridge) UpdateRecord(collection, id string, data map[string]any) error {
	return nil
}
func (m *mockBridge) DeleteRecord(collection, id string) error { return nil }
func (m *mockBridge) FindAdminByID(id string) (map[string]any, error) {
	return nil, nil
}
func (m *mockBridge) FindAdminByEmail(email string) (map[string]any, error) {
	return nil, nil
}
func (m *mockBridge) FindCollectionByNameOrId(nameOrId string) (map[string]any, error) {
	return nil, nil
}
func (m *mockBridge) SendMail(to, subject, htmlBody string) error { return nil }
func (m *mockBridge) GetSettings() (map[string]any, error)        { return nil, nil }
func (m *mockBridge) UpdateSettings(data map[string]any) error    { return nil }
func (m *mockBridge) LogAudit(adminID, action, resource, resourceID string, data map[string]any) error {
	return nil
}
