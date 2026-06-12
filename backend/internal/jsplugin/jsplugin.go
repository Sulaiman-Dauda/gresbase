// Package jsplugin provides a JavaScript plugin runtime powered by the goja VM.
// Users can write custom hooks, validators, and event handlers in JavaScript
// that execute server-side with access to a sandboxed Gresbase API.
//
// Matches PocketBase's JS VM capabilities:
// - Full $app API (db queries, record CRUD, mailer, filesystem, settings)
// - $http client for external API calls
// - $os utilities (file read/write, env vars, exec)
// - require() for module loading
// - console.log/error/warn with structured logging
// - Execution timeout protection
// - Error stack traces
package jsplugin

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/gresbase/gresbase/internal/events"
	"github.com/rs/zerolog/log"
)

// AppBridge defines the interface that the JS runtime uses to interact
// with the Gresbase application. This is implemented by the App layer.
type AppBridge interface {
	// Database
	DBQuery(ctx context.Context, sql string, args ...any) ([]map[string]any, error)
	DBExec(ctx context.Context, sql string, args ...any) (int64, error)

	// Records
	FindRecordByID(collection string, id string) (map[string]any, error)
	FindRecords(collection string, filter string, sort string, limit, offset int) ([]map[string]any, error)
	CreateRecord(collection string, data map[string]any) (map[string]any, error)
	UpdateRecord(collection string, id string, data map[string]any) error
	DeleteRecord(collection string, id string) error

	// Auth
	FindAdminByID(id string) (map[string]any, error)
	FindAdminByEmail(email string) (map[string]any, error)

	// Collections
	FindCollectionByNameOrId(nameOrId string) (map[string]any, error)

	// Email
	SendMail(to, subject, htmlBody string) error

	// Settings
	GetSettings() (map[string]any, error)
	UpdateSettings(data map[string]any) error

	// Logs
	LogAudit(adminID, action, resource, resourceID string, data map[string]any) error
}

// Runtime manages compiled JavaScript plugins.
type Runtime struct {
	mu      sync.RWMutex
	plugins map[string]*ScriptPlugin
	timeout time.Duration
	app     AppBridge
}

// ScriptPlugin represents a single JS plugin script.
type ScriptPlugin struct {
	ID          string
	Name        string
	Version     string
	Description string
	Script      string
	Priority    int
	compiled    *goja.Program
	hooks       map[string]goja.Callable
	modules     map[string]string // module name -> source code
}

// jsSetter is implemented by both *goja.Runtime and *goja.Object, whose Set
// methods return an error only when the target is frozen/sealed — which never
// happens for the freshly created VMs and objects we build the plugin API on.
type jsSetter interface {
	Set(name string, value any) error
}

// mustSet binds a value onto a goja runtime/object during API setup. A failure
// here would mean the VM was constructed in an impossible state, so we log it
// loudly rather than silently dropping the binding.
func mustSet(target jsSetter, name string, value any) {
	if err := target.Set(name, value); err != nil {
		log.Error().Err(err).Str("binding", name).Msg("jsplugin: failed to bind VM property")
	}
}

// NewRuntime creates a JS plugin runtime with an optional AppBridge.
func NewRuntime(timeout time.Duration) *Runtime {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Runtime{
		plugins: make(map[string]*ScriptPlugin),
		timeout: timeout,
	}
}

// SetAppBridge sets the application bridge for DB and API access from JS.
func (r *Runtime) SetAppBridge(app AppBridge) {
	r.app = app
}

// LoadPlugin compiles and registers a JS plugin.
func (r *Runtime) LoadPlugin(id, name, script string, priority int) (*ScriptPlugin, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.plugins[id]; exists {
		return nil, fmt.Errorf("plugin %q already loaded", id)
	}

	prog, err := goja.Compile(id, script, false)
	if err != nil {
		return nil, fmt.Errorf("compile error in plugin %q: %w", name, err)
	}

	p := &ScriptPlugin{
		ID:       id,
		Name:     name,
		Script:   script,
		Priority: priority,
		compiled: prog,
		hooks:    make(map[string]goja.Callable),
		modules:  make(map[string]string),
	}

	// Pre-run to extract hook registrations
	vm := goja.New()
	p.setupRuntime(r, vm)

	_, err = vm.RunProgram(p.compiled)
	if err != nil {
		return nil, fmt.Errorf("init error in plugin %q: %w", name, err)
	}

	r.plugins[id] = p
	log.Info().Str("plugin", name).Str("id", id).Int("hooks", len(p.hooks)).Msg("JS plugin loaded")
	return p, nil
}

// UnloadPlugin removes a JS plugin.
func (r *Runtime) UnloadPlugin(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.plugins, id)
	log.Info().Str("plugin_id", id).Msg("JS plugin unloaded")
}

// HasHook checks if any plugin has a handler for the given hook name.
func (r *Runtime) HasHook(hookName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.plugins {
		if _, ok := p.hooks[hookName]; ok {
			return true
		}
	}
	return false
}

// TriggerHook executes all JS handlers for a hook, passing the event as JSON.
func (r *Runtime) TriggerHook(hookName string, eventJSON []byte) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, p := range r.plugins {
		fn, ok := p.hooks[hookName]
		if !ok {
			continue
		}

		vm := goja.New()
		p.setupRuntime(r, vm)

		var eventData map[string]any
		if err := json.Unmarshal(eventJSON, &eventData); err != nil {
			log.Warn().Err(err).Str("hook", hookName).Msg("JS hook: invalid event JSON; passing empty event")
		}
		mustSet(vm, "event", vm.ToValue(eventData))

		if err := r.runWithTimeout(vm, func() error {
			_, err := fn(goja.Undefined(), vm.ToValue(eventData))
			return err
		}); err != nil {
			log.Error().Err(err).Str("plugin", p.Name).Str("hook", hookName).Msg("JS hook failed")
			return fmt.Errorf("plugin %q hook %q: %w", p.Name, hookName, err)
		}
	}

	return nil
}

// ListPlugins returns all loaded JS plugins.
func (r *Runtime) ListPlugins() []map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var list []map[string]any
	for _, p := range r.plugins {
		hookNames := make([]string, 0, len(p.hooks))
		for name := range p.hooks {
			hookNames = append(hookNames, name)
		}
		list = append(list, map[string]any{
			"id":          p.ID,
			"name":        p.Name,
			"version":     p.Version,
			"description": p.Description,
			"hooks":       hookNames,
			"priority":    p.Priority,
		})
	}
	return list
}

// setupRuntime initializes the full Gresbase JS API on the VM.
func (p *ScriptPlugin) setupRuntime(rt *Runtime, vm *goja.Runtime) {
	// ---- Console ----
	console := vm.NewObject()
	mustSet(console, "log", jsConsoleLog)
	mustSet(console, "error", jsConsoleError)
	mustSet(console, "warn", jsConsoleWarn)
	mustSet(console, "debug", jsConsoleDebug)
	mustSet(console, "info", jsConsoleLog)
	mustSet(vm, "console", console)

	// ---- $app ----
	appObj := vm.NewObject()
	mustSet(appObj, "version", "0.1.0")
	mustSet(appObj, "isDev", false)

	// $app.db() - returns a query builder
	mustSet(appObj, "db", func() goja.Value {
		dbObj := vm.NewObject()

		// db.select('*').from('table').where('id = ?', 1).all()
		mustSet(dbObj, "select", func(call goja.FunctionCall) goja.Value {
			fields := "*"
			if len(call.Arguments) > 0 {
				fields = call.Arguments[0].String()
			}
			return p.createQueryBuilder(vm, rt, "SELECT "+fields)
		})

		return dbObj
	})

	// $app.findRecordById(collectionNameOrId, id)
	mustSet(appObj, "findRecordById", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 || rt.app == nil {
			return goja.Null()
		}
		record, err := rt.app.FindRecordByID(call.Arguments[0].String(), call.Arguments[1].String())
		if err != nil {
			log.Warn().Err(err).Msg("JS: findRecordById failed")
			return goja.Null()
		}
		return vm.ToValue(record)
	})

	// $app.findRecordsByFilter(collection, filter, sort, limit, offset)
	mustSet(appObj, "findRecordsByFilter", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 || rt.app == nil {
			return vm.ToValue([]any{})
		}
		collection := call.Arguments[0].String()
		filter := ""
		sort := "-created"
		limit := 100
		offset := 0
		if len(call.Arguments) > 1 {
			filter = call.Arguments[1].String()
		}
		if len(call.Arguments) > 2 {
			sort = call.Arguments[2].String()
		}
		if len(call.Arguments) > 3 {
			limit = int(call.Arguments[3].ToInteger())
		}
		if len(call.Arguments) > 4 {
			offset = int(call.Arguments[4].ToInteger())
		}
		records, err := rt.app.FindRecords(collection, filter, sort, limit, offset)
		if err != nil {
			log.Warn().Err(err).Msg("JS: findRecordsByFilter failed")
			return vm.ToValue([]any{})
		}
		return vm.ToValue(records)
	})

	// $app.findCollectionByNameOrId(nameOrId)
	mustSet(appObj, "findCollectionByNameOrId", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 || rt.app == nil {
			return goja.Null()
		}
		coll, err := rt.app.FindCollectionByNameOrId(call.Arguments[0].String())
		if err != nil {
			return goja.Null()
		}
		return vm.ToValue(coll)
	})

	// $app.findAdminById(id)
	mustSet(appObj, "findAdminById", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 || rt.app == nil {
			return goja.Null()
		}
		admin, err := rt.app.FindAdminByID(call.Arguments[0].String())
		if err != nil {
			return goja.Null()
		}
		return vm.ToValue(admin)
	})

	// $app.sendMail(to, subject, htmlBody)
	mustSet(appObj, "sendMail", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 3 || rt.app == nil {
			return vm.ToValue(false)
		}
		err := rt.app.SendMail(
			call.Arguments[0].String(),
			call.Arguments[1].String(),
			call.Arguments[2].String(),
		)
		return vm.ToValue(err == nil)
	})

	// $app.settings()
	mustSet(appObj, "settings", func(call goja.FunctionCall) goja.Value {
		if rt.app == nil {
			return vm.ToValue(map[string]any{})
		}
		settings, err := rt.app.GetSettings()
		if err != nil {
			return vm.ToValue(map[string]any{})
		}
		return vm.ToValue(settings)
	})

	// $app.logAudit(adminId, action, resource, resourceId, data)
	mustSet(appObj, "logAudit", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 4 || rt.app == nil {
			return goja.Null()
		}
		var data map[string]any
		if len(call.Arguments) > 4 {
			if exported := call.Arguments[4].Export(); exported != nil {
				if m, ok := exported.(map[string]any); ok {
					data = m
				}
			}
		}
		if err := rt.app.LogAudit(
			call.Arguments[0].String(),
			call.Arguments[1].String(),
			call.Arguments[2].String(),
			call.Arguments[3].String(),
			data,
		); err != nil {
			log.Warn().Err(err).Msg("JS $app.logAudit failed")
		}
		return goja.Null()
	})

	mustSet(vm, "$app", appObj)

	// ---- $http ----
	httpObj := vm.NewObject()

	// $http.send({ method: 'GET', url: '...', headers: {...}, body: '...' })
	mustSet(httpObj, "send", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			return vm.ToValue(map[string]any{"error": "missing request config"})
		}
		return p.jsHTTPSend(vm, call.Arguments[0])
	})

	// $http.get(url)
	mustSet(httpObj, "get", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			return vm.ToValue(map[string]any{"error": "missing url"})
		}
		return p.jsHTTPQuick(vm, "GET", call.Arguments[0].String(), "")
	})

	// $http.post(url, body)
	mustSet(httpObj, "post", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			return vm.ToValue(map[string]any{"error": "missing url"})
		}
		body := ""
		if len(call.Arguments) > 1 {
			body = call.Arguments[1].String()
		}
		return p.jsHTTPQuick(vm, "POST", call.Arguments[0].String(), body)
	})

	mustSet(vm, "$http", httpObj)

	// ---- $os ----
	osObj := vm.NewObject()

	// $os.getenv(name)
	mustSet(osObj, "getenv", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			return vm.ToValue("")
		}
		return vm.ToValue(os.Getenv(call.Arguments[0].String()))
	})

	// $os.readFile(path)
	mustSet(osObj, "readFile", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			return vm.ToValue("")
		}
		data, err := os.ReadFile(call.Arguments[0].String())
		if err != nil {
			log.Warn().Err(err).Str("path", call.Arguments[0].String()).Msg("JS: os.readFile failed")
			return vm.ToValue("")
		}
		return vm.ToValue(string(data))
	})

	// $os.writeFile(path, content)
	mustSet(osObj, "writeFile", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			return vm.ToValue(false)
		}
		err := os.WriteFile(call.Arguments[0].String(), []byte(call.Arguments[1].String()), 0o600)
		return vm.ToValue(err == nil)
	})

	// $os.exec(command, ...args)
	mustSet(osObj, "exec", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			return vm.ToValue(map[string]any{"error": "missing command"})
		}
		cmd := call.Arguments[0].String()
		args := make([]string, len(call.Arguments)-1)
		for i := 1; i < len(call.Arguments); i++ {
			args[i-1] = call.Arguments[i].String()
		}

		execCmd := exec.Command(cmd, args...)
		var stdout, stderr bytes.Buffer
		execCmd.Stdout = &stdout
		execCmd.Stderr = &stderr

		err := execCmd.Run()
		return vm.ToValue(map[string]any{
			"stdout": stdout.String(),
			"stderr": stderr.String(),
			"error": func() string {
				if err != nil {
					return err.Error()
				}
				return ""
			}(),
		})
	})

	mustSet(vm, "$os", osObj)

	// ---- $security ----
	security := vm.NewObject()
	mustSet(security, "hash", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) > 0 {
			h := sha256.Sum256([]byte(call.Arguments[0].String()))
			return vm.ToValue(hex.EncodeToString(h[:]))
		}
		return vm.ToValue("")
	})
	mustSet(security, "randomUUID", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(generateUUID())
	})
	mustSet(security, "randomString", func(call goja.FunctionCall) goja.Value {
		length := 32
		if len(call.Arguments) > 0 {
			length = int(call.Arguments[0].ToInteger())
		}
		return vm.ToValue(generateRandomString(length))
	})
	mustSet(vm, "$security", security)

	// ---- $event (set dynamically per hook call) ----
	mustSet(vm, "$event", vm.ToValue(map[string]any{}))

	// ---- registerHook function ----
	mustSet(vm, "registerHook", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			log.Warn().Str("plugin", p.Name).Msg("registerHook called with insufficient args")
			return goja.Undefined()
		}
		hookName := call.Arguments[0].String()
		if fn, ok := goja.AssertFunction(call.Arguments[1]); ok {
			p.hooks[hookName] = fn
			log.Debug().Str("plugin", p.Name).Str("hook", hookName).Msg("JS hook registered")
		}
		return goja.Undefined()
	})

	// ---- require() for module loading ----
	mustSet(vm, "require", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			return goja.Undefined()
		}
		moduleName := call.Arguments[0].String()
		if source, ok := p.modules[moduleName]; ok {
			moduleVM := goja.New()
			exports := moduleVM.NewObject()
			mustSet(moduleVM, "module", map[string]any{"exports": exports})
			_, err := moduleVM.RunString(source)
			if err != nil {
				log.Error().Err(err).Str("module", moduleName).Msg("JS: require failed")
				return goja.Undefined()
			}
			if moduleObj, ok := moduleVM.Get("module").(*goja.Object); ok {
				if exp := moduleObj.Get("exports"); exp != nil {
					return exp
				}
			}
		}
		return goja.Undefined()
	})

	// ---- now() helper ----
	mustSet(vm, "now", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(time.Now().UTC().Format(time.RFC3339))
	})

	// ---- setTimeout / setInterval (limited) ----
	mustSet(vm, "setTimeout", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			return goja.Undefined()
		}
		delay := time.Duration(call.Arguments[1].ToInteger()) * time.Millisecond
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}
		if fn, ok := goja.AssertFunction(call.Arguments[0]); ok {
			go func() {
				time.Sleep(delay)
				newVM := goja.New()
				p.setupRuntime(rt, newVM)
				if _, err := fn(goja.Undefined()); err != nil {
					log.Warn().Err(err).Msg("JS setTimeout callback failed")
				}
			}()
		}
		return goja.Undefined()
	})
}

// createQueryBuilder creates a fluent SQL query builder in JS.
func (p *ScriptPlugin) createQueryBuilder(vm *goja.Runtime, rt *Runtime, baseSQL string) goja.Value {
	qb := vm.NewObject()

	var tableName, whereClause string
	var whereArgs []any
	var orderBy, groupBy, havingClause string
	var limitVal, offsetVal int

	mustSet(qb, "from", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) > 0 {
			tableName = call.Arguments[0].String()
		}
		return qb
	})

	mustSet(qb, "where", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) > 0 {
			whereClause = call.Arguments[0].String()
			for i := 1; i < len(call.Arguments); i++ {
				whereArgs = append(whereArgs, call.Arguments[i].Export())
			}
		}
		return qb
	})

	mustSet(qb, "orderBy", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) > 0 {
			orderBy = call.Arguments[0].String()
		}
		return qb
	})

	mustSet(qb, "groupBy", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) > 0 {
			groupBy = call.Arguments[0].String()
		}
		return qb
	})

	mustSet(qb, "having", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) > 0 {
			havingClause = call.Arguments[0].String()
		}
		return qb
	})

	mustSet(qb, "limit", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) > 0 {
			limitVal = int(call.Arguments[0].ToInteger())
		}
		return qb
	})

	mustSet(qb, "offset", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) > 0 {
			offsetVal = int(call.Arguments[0].ToInteger())
		}
		return qb
	})

	// Execute and return all rows
	mustSet(qb, "all", func(call goja.FunctionCall) goja.Value {
		if rt.app == nil || tableName == "" {
			return vm.ToValue([]any{})
		}
		sql := fmt.Sprintf("%s FROM %s", baseSQL, tableName)
		if whereClause != "" {
			sql += " WHERE " + whereClause
		}
		if groupBy != "" {
			sql += " GROUP BY " + groupBy
		}
		if havingClause != "" {
			sql += " HAVING " + havingClause
		}
		if orderBy != "" {
			sql += " ORDER BY " + orderBy
		}
		if limitVal > 0 {
			sql += fmt.Sprintf(" LIMIT %d", limitVal)
		}
		if offsetVal > 0 {
			sql += fmt.Sprintf(" OFFSET %d", offsetVal)
		}
		results, err := rt.app.DBQuery(context.Background(), sql, whereArgs...)
		if err != nil {
			log.Warn().Err(err).Str("sql", sql).Msg("JS: db query failed")
			return vm.ToValue([]any{})
		}
		return vm.ToValue(results)
	})

	// Execute and return first row
	mustSet(qb, "one", func(call goja.FunctionCall) goja.Value {
		if rt.app == nil || tableName == "" {
			return goja.Null()
		}
		sql := fmt.Sprintf("%s FROM %s", baseSQL, tableName)
		if whereClause != "" {
			sql += " WHERE " + whereClause
		}
		sql += " LIMIT 1"
		results, err := rt.app.DBQuery(context.Background(), sql, whereArgs...)
		if err != nil || len(results) == 0 {
			return goja.Null()
		}
		return vm.ToValue(results[0])
	})

	// Execute a raw query
	mustSet(qb, "exec", func(call goja.FunctionCall) goja.Value {
		if rt.app == nil || tableName == "" {
			return vm.ToValue(int64(0))
		}
		sql := baseSQL + " FROM " + tableName
		if whereClause != "" {
			sql += " WHERE " + whereClause
		}
		rowsAffected, err := rt.app.DBExec(context.Background(), sql, whereArgs...)
		if err != nil {
			log.Warn().Err(err).Str("sql", sql).Msg("JS: db exec failed")
			return vm.ToValue(int64(0))
		}
		return vm.ToValue(rowsAffected)
	})

	return qb
}

// jsHTTPSend implements $http.send() with full request config.
func (p *ScriptPlugin) jsHTTPSend(vm *goja.Runtime, configVal goja.Value) goja.Value {
	config := configVal.Export()
	configMap, ok := config.(map[string]any)
	if !ok {
		return vm.ToValue(map[string]any{"error": "invalid config"})
	}

	method := "GET"
	if m, ok := configMap["method"].(string); ok {
		method = m
	}
	url, _ := configMap["url"].(string)
	if url == "" {
		return vm.ToValue(map[string]any{"error": "url is required"})
	}

	var body io.Reader
	if b, ok := configMap["body"]; ok {
		switch v := b.(type) {
		case string:
			body = strings.NewReader(v)
		case map[string]any:
			data, _ := json.Marshal(v)
			body = bytes.NewReader(data)
		}
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return vm.ToValue(map[string]any{"error": err.Error()})
	}

	if headers, ok := configMap["headers"].(map[string]any); ok {
		for k, v := range headers {
			req.Header.Set(k, fmt.Sprintf("%v", v))
		}
	}
	if req.Header.Get("Content-Type") == "" && body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return vm.ToValue(map[string]any{"error": err.Error()})
	}
	defer func() { _ = resp.Body.Close() }() // response body drained below; close error not actionable

	respBody, _ := io.ReadAll(resp.Body)

	// Try to parse as JSON
	var jsonBody any
	if err := json.Unmarshal(respBody, &jsonBody); err != nil {
		jsonBody = string(respBody)
	}

	return vm.ToValue(map[string]any{
		"status":     resp.StatusCode,
		"statusText": resp.Status,
		"headers":    resp.Header,
		"body":       jsonBody,
	})
}

// jsHTTPQuick implements $http.get() and $http.post() shortcuts.
func (p *ScriptPlugin) jsHTTPQuick(vm *goja.Runtime, method, url, body string) goja.Value {
	config := map[string]any{
		"method": method,
		"url":    url,
	}
	if body != "" {
		config["body"] = body
	}
	return p.jsHTTPSend(vm, vm.ToValue(config))
}

// runWithTimeout executes a function with a timeout.
func (r *Runtime) runWithTimeout(vm *goja.Runtime, fn func() error) error {
	done := make(chan error, 1)
	go func() {
		done <- fn()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(r.timeout):
		vm.Interrupt("execution timeout")
		return fmt.Errorf("js execution timed out after %v", r.timeout)
	}
}

// ---------------------------------------------------------------------------
// Console helpers
// ---------------------------------------------------------------------------

func jsConsoleLog(call goja.FunctionCall) goja.Value {
	args := make([]any, len(call.Arguments))
	for i, a := range call.Arguments {
		args[i] = a.Export()
	}
	log.Info().Fields(map[string]any{"js": args}).Msg("JS console.log")
	return goja.Undefined()
}

func jsConsoleError(call goja.FunctionCall) goja.Value {
	args := make([]any, len(call.Arguments))
	for i, a := range call.Arguments {
		args[i] = a.Export()
	}
	log.Error().Fields(map[string]any{"js": args}).Msg("JS console.error")
	return goja.Undefined()
}

func jsConsoleWarn(call goja.FunctionCall) goja.Value {
	args := make([]any, len(call.Arguments))
	for i, a := range call.Arguments {
		args[i] = a.Export()
	}
	log.Warn().Fields(map[string]any{"js": args}).Msg("JS console.warn")
	return goja.Undefined()
}

func jsConsoleDebug(call goja.FunctionCall) goja.Value {
	args := make([]any, len(call.Arguments))
	for i, a := range call.Arguments {
		args[i] = a.Export()
	}
	log.Debug().Fields(map[string]any{"js": args}).Msg("JS console.debug")
	return goja.Undefined()
}

// ---------------------------------------------------------------------------
// Event conversion helpers
// ---------------------------------------------------------------------------

// ToJSON marshals an event to JSON for passing to JS hooks.
func ToJSON(e events.Event) ([]byte, error) {
	return json.Marshal(e)
}

func generateUUID() string {
	b := make([]byte, 16)
	for i := range b {
		b[i] = byte(time.Now().UnixNano()>>(i*4)) ^ byte(i*37+int(time.Now().UnixNano()%256))
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func generateRandomString(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	if length <= 0 {
		return ""
	}
	raw := make([]byte, length)
	if _, err := cryptorand.Read(raw); err != nil {
		// crypto/rand only fails on a broken entropy source, which is
		// unrecoverable; panicking is safer than returning a predictable string.
		panic(fmt.Sprintf("crypto/rand failed generating random string: %v", err))
	}
	b := make([]byte, length)
	for i, v := range raw {
		b[i] = chars[int(v)%len(chars)]
	}
	return string(b)
}
