// Package ui serves the embedded admin dashboard.
//
// At build time, if the frontend has been pre-built into backend/ui/dist/,
// those static files are embedded into the binary via go:embed.
// If no pre-built frontend is found (dev mode / from source), the built-in
// HTML/JS fallback dashboard is served instead.
//
// The single-binary build process:
//
//	cd frontend && npm install && npm run build    # → frontend/out/
//	cp -r frontend/out backend/ui/dist/
//	CGO_ENABLED=0 go build -o gresbase ./cmd/gresbase
//
// This produces ONE static binary containing the full admin dashboard.
package ui

import (
	"embed"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

//go:embed dist
var embeddedFrontend embed.FS

// HasEmbeddedFrontend reports whether the binary was built with the
// pre-compiled frontend (i.e. dist/ is non-empty).
var HasEmbeddedFrontend bool

func init() {
	entries, err := fs.ReadDir(embeddedFrontend, "dist")
	if err == nil {
		for _, e := range entries {
			if e.Name() != "PLACEHOLDER" {
				HasEmbeddedFrontend = true
				break
			}
		}
		if HasEmbeddedFrontend {
			log.Debug().Int("files", len(entries)).Msg("Embedded frontend loaded")
		}
	}
}

// FileSystem returns an http.FileSystem serving the embedded frontend.
func FileSystem() http.FileSystem {
	if !HasEmbeddedFrontend {
		return nil
	}
	sub, err := fs.Sub(embeddedFrontend, "dist")
	if err != nil {
		return nil
	}
	return http.FS(sub)
}

// Handler returns an http.Handler that serves the frontend SPA.
func Handler() http.Handler {
	if HasEmbeddedFrontend {
		return spaHandler(FileSystem())
	}
	return http.HandlerFunc(ServeFallback)
}

// ServeFallback serves the built-in inline HTML/JS UI.
func ServeFallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write([]byte(FallbackUI))
}

func spaHandler(fsys http.FileSystem) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Clean(r.URL.Path)
		if path == "/" {
			path = "/index.html"
		}
		f, err := fsys.Open(strings.TrimPrefix(path, "/"))
		if err != nil {
			index, err := fsys.Open("index.html")
			if err != nil {
				ServeFallback(w, r)
				return
			}
			defer index.Close()
			stat, _ := index.Stat()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			http.ServeContent(w, r, "index.html", stat.ModTime(), index)
			return
		}
		defer f.Close()
		stat, err := f.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		ext := filepath.Ext(path)
		switch ext {
		case ".html":
			w.Header().Set("Cache-Control", "no-cache")
		case ".js", ".css", ".woff", ".woff2":
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		case ".png", ".jpg", ".svg", ".ico":
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}
		http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
	})
}

// FallbackUI is the built-in dark-mode HTML/JS dashboard.
const FallbackUI = fallbackUI

const fallbackUI = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Gresbase</title>
<style>
  :root {
    --bg: #0a0a0b; --surface: #141416; --border: #26262b;
    --text: #f4f4f5; --muted: #71717a; --primary: #7c3aed;
    --primary-hover: #6d28d9; --danger: #ef4444; --success: #22c55e;
    --warning: #eab308; --radius: 10px;
  }
  * { margin:0; padding:0; box-sizing:border-box }
  body {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', sans-serif;
    background: var(--bg); color: var(--text); min-height: 100vh;
  }
  /* Login pages */
  .login-container { display: flex; align-items: center; justify-content: center; min-height: 100vh; padding: 24px }
  .auth-card { background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius); padding: 32px; width: 100%; max-width: 420px }
  .logo { display: flex; align-items: center; justify-content: center; width: 48px; height: 48px; background: var(--primary); border-radius: 12px; margin: 0 auto 16px; font-size: 22px; font-weight: 700 }
  h1 { font-size: 20px; text-align: center; margin-bottom: 4px }
  .sub { color: var(--muted); text-align: center; font-size: 13px; margin-bottom: 24px }
  .form-group { margin-bottom: 14px }
  label { display: block; font-size: 13px; font-weight: 500; margin-bottom: 6px; color: var(--muted) }
  input, select, textarea {
    width: 100%; padding: 10px 12px; background: var(--bg);
    border: 1px solid var(--border); border-radius: 8px;
    color: var(--text); font-size: 14px; outline: none; transition: border .15s;
  }
  input:focus, select:focus, textarea:focus { border-color: var(--primary) }
  button {
    width: 100%; padding: 10px; background: var(--primary);
    border: none; border-radius: 8px; color: white; font-size: 14px;
    font-weight: 500; cursor: pointer; transition: background .15s; margin-top: 8px;
  }
  button:hover { background: var(--primary-hover) }
  button:disabled { opacity: 0.5; cursor: not-allowed }
  .btn-sm { width: auto; padding: 6px 14px; font-size: 12px; margin: 0 }
  .btn-danger { background: transparent; border: 1px solid rgba(239,68,68,0.3); color: var(--danger); width: auto; padding: 4px 10px; font-size: 11px }
  .btn-danger:hover { background: rgba(239,68,68,0.1) }
  .btn-ghost { background: var(--border); color: var(--text); width: auto; padding: 6px 14px; font-size: 12px; margin: 0 }
  .btn-ghost:hover { background: #333 }
  .error { background: rgba(239,68,68,0.1); border: 1px solid rgba(239,68,68,0.3); color: var(--danger); padding: 10px 12px; border-radius: 8px; font-size: 13px; margin-bottom: 16px; display: none }
  .success { background: rgba(34,197,94,0.1); border: 1px solid rgba(34,197,94,0.3); color: var(--success); padding: 10px 12px; border-radius: 8px; font-size: 13px; margin-bottom: 16px; display: none }
  .switch { text-align: center; margin-top: 20px; font-size: 13px; color: var(--muted) }
  .switch a { color: var(--primary); cursor: pointer; text-decoration: none }
  .switch a:hover { text-decoration: underline }
  .hidden { display: none !important }
  /* Dashboard layout */
  .app { display: flex; min-height: 100vh }
  .sidebar { width: 220px; background: var(--surface); border-right: 1px solid var(--border); padding: 16px 12px; display: flex; flex-direction: column }
  .sidebar .logo-area { display: flex; align-items: center; gap: 10px; margin-bottom: 24px; padding: 0 8px }
  .sidebar .logo-area .l { width: 32px; height: 32px; background: var(--primary); border-radius: 8px; display: flex; align-items: center; justify-content: center; font-size: 16px; font-weight: 700 }
  .sidebar .logo-area span { font-size: 15px; font-weight: 600 }
  .sidebar nav { display: flex; flex-direction: column; gap: 2px; flex: 1 }
  .sidebar nav a {
    display: flex; align-items: center; gap: 8px; padding: 8px 12px;
    border-radius: 8px; font-size: 13px; color: var(--muted);
    text-decoration: none; transition: all .15s; cursor: pointer;
  }
  .sidebar nav a:hover, .sidebar nav a.active { background: var(--bg); color: var(--text) }
  .sidebar .user-area { border-top: 1px solid var(--border); padding-top: 12px; margin-top: 12px }
  .sidebar .user-area .email { font-size: 12px; color: var(--muted); margin-bottom: 6px; padding: 0 8px }
  .main { flex: 1; padding: 24px 32px; overflow-y: auto }
  .page-header { margin-bottom: 24px }
  .page-header h1 { font-size: 22px; text-align: left; font-weight: 700 }
  .page-header p { font-size: 13px; color: var(--muted); margin-top: 4px }
  /* Stats, Cards, Tables */
  .stat-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 10px; margin-bottom: 24px }
  .stat { background: var(--surface); border: 1px solid var(--border); border-radius: 8px; padding: 16px }
  .stat .num { font-size: 24px; font-weight: 700; color: var(--primary) }
  .stat .lbl { font-size: 12px; color: var(--muted); margin-top: 2px }
  .section-title { font-size: 13px; font-weight: 600; color: var(--muted); margin-bottom: 12px; text-transform: uppercase; letter-spacing: 0.5px }
  .card { background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius); padding: 20px; margin-bottom: 16px }
  .flex { display: flex; align-items: center; gap: 8px }
  .flex-between { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px }
  .flex-wrap { flex-wrap: wrap }
  .gap-2 { gap: 8px }
  .mt-2 { margin-top: 8px } .mt-4 { margin-top: 16px } .mb-4 { margin-bottom: 16px }
  .text-sm { font-size: 12px; color: var(--muted) }
  .table-wrap { background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden }
  table { width: 100%; border-collapse: collapse; font-size: 13px }
  th, td { padding: 10px 14px; text-align: left }
  th { background: var(--bg); color: var(--muted); font-weight: 500; font-size: 11px; text-transform: uppercase; letter-spacing: 0.5px }
  td { border-top: 1px solid var(--border) }
  tr:hover td { background: rgba(255,255,255,0.02) }
  .badge { font-size: 11px; padding: 2px 8px; border-radius: 100px; background: rgba(124,58,237,0.15); color: var(--primary); border: 1px solid rgba(124,58,237,0.3); display: inline-block }
  .badge.success { background: rgba(34,197,94,0.15); color: var(--success); border-color: rgba(34,197,94,0.3) }
  .badge.danger { background: rgba(239,68,68,0.15); color: var(--danger); border-color: rgba(239,68,68,0.3) }
  .badge.warning { background: rgba(234,179,8,0.15); color: var(--warning); border-color: rgba(234,179,8,0.3) }
  /* Tabs */
  .tab-bar { display: flex; gap: 2px; background: var(--bg); border-radius: 8px; padding: 3px; margin-bottom: 16px; width: fit-content }
  .tab-bar button { width: auto; padding: 6px 16px; font-size: 12px; background: transparent; color: var(--muted); border-radius: 6px; margin: 0 }
  .tab-bar button.active { background: var(--surface); color: var(--text) }
  /* Toast */
  .toast { position: fixed; bottom: 24px; right: 24px; z-index: 9999; display: flex; flex-direction: column; gap: 8px }
  .toast-item { background: var(--surface); border: 1px solid var(--border); border-radius: 8px; padding: 12px 16px; font-size: 13px; animation: slideUp 0.3s ease-out; max-width: 360px }
  .toast-item.error { border-color: rgba(239,68,68,0.5) }
  .toast-item.success { border-color: rgba(34,197,94,0.5) }
  @keyframes slideUp { from { opacity: 0; transform: translateY(10px) } to { opacity: 1; transform: translateY(0) } }
  /* Field editor */
  .field-row { display: flex; gap: 8px; align-items: center; padding: 8px; border: 1px solid var(--border); border-radius: 8px; margin-bottom: 6px; background: var(--bg) }
  .field-row input, .field-row select { width: auto; flex: 1; min-width: 80px }
  .field-row .field-name { flex: 2 }
  .field-row .field-type { flex: 1.5 }
  .field-row .field-required { width: auto; flex: 0.5; display: flex; align-items: center; gap: 4px; font-size: 12px }
  .field-row .field-required input[type=checkbox] { width: auto }
  .inline-code { background: var(--bg); padding: 2px 8px; border-radius: 4px; font-family: 'JetBrains Mono', monospace; font-size: 12px; color: var(--primary) }
  .editable-cell { cursor: pointer; transition: background .1s }
  .editable-cell:hover { background: rgba(124,58,237,0.05); outline: 1px dashed var(--primary) }
  .endpoint-card {
    background: var(--surface); border: 1px solid var(--border);
    border-radius: 8px; padding: 10px 14px; cursor: pointer;
    transition: all .15s; font-size: 13px;
  }
  .endpoint-card:hover { border-color: var(--primary); background: rgba(124,58,237,0.05) }
  .file-upload-area {
    border: 2px dashed var(--border); border-radius: 8px;
    padding: 20px; text-align: center; cursor: pointer; transition: border .15s;
  }
  .file-upload-area:hover { border-color: var(--primary) }
  .file-upload-area.dragover { border-color: var(--primary); background: rgba(124,58,237,0.05) }
  /* Responsive */
  @media (max-width: 768px) { .sidebar { width: 60px; padding: 12px 4px } .sidebar nav a span { display: none } .sidebar .logo-area span { display: none } .main { padding: 16px } }
</style>
</head>
<body>
<!-- LOGIN -->
<div class="login-container" id="loginPage">
  <div class="auth-card">
    <div class="logo">⚡</div>
    <h1>Gresbase</h1>
    <p class="sub">Sign in to your admin dashboard</p>
    <div class="error" id="loginError"></div>
    <form id="loginForm">
      <div class="form-group"><label>Email</label><input type="email" id="loginEmail" placeholder="admin@example.com" required autocomplete="email"></div>
      <div class="form-group"><label>Password</label><input type="password" id="loginPassword" placeholder="••••••••" required autocomplete="current-password"></div>
      <button type="submit">Sign in</button>
    </form>
    <p class="switch">Don't have an account? <a onclick="showRegister()">Create one</a></p>
  </div>
</div>
<!-- REGISTER -->
<div class="login-container hidden" id="registerPage">
  <div class="auth-card">
    <div class="logo">⚡</div>
    <h1>Create Account</h1>
    <p class="sub">Set up your Gresbase admin account</p>
    <div class="error" id="registerError"></div>
    <div class="success" id="registerSuccess"></div>
    <form id="registerForm">
      <div class="form-group"><label>Email</label><input type="email" id="registerEmail" placeholder="you@example.com" required autocomplete="email"></div>
      <div class="form-group"><label>Password</label><input type="password" id="registerPassword" placeholder="Min 8 characters" minlength="8" required autocomplete="new-password"></div>
      <div class="form-group"><label>Confirm Password</label><input type="password" id="registerPasswordConfirm" placeholder="Repeat password" minlength="8" required></div>
      <button type="submit">Create Account</button>
    </form>
    <p class="switch">Already have an account? <a onclick="showLogin()">Sign in</a></p>
  </div>
</div>
<!-- DASHBOARD -->
<div class="app hidden" id="dashboard">
  <aside class="sidebar">
    <div class="logo-area"><div class="l">⚡</div><span>Gresbase</span></div>
    <nav>
      <a class="active" onclick="navigate('overview')" data-nav="overview">📊 Overview</a>
      <a onclick="navigate('collections')" data-nav="collections">🗄️ Collections</a>
      <a onclick="navigate('admins')" data-nav="admins">👥 Admins</a>
      <a onclick="navigate('api-keys')" data-nav="api-keys">🔑 API Keys</a>
      <a onclick="navigate('certificates')" data-nav="certificates">🔒 Certificates</a>
      <a onclick="navigate('settings')" data-nav="settings">⚙️ Settings</a>
      <a onclick="navigate('api')" data-nav="api">📖 API Reference</a>
    </nav>
    <div class="user-area">
      <div class="email" id="sidebarEmail"></div>
      <button class="btn-sm btn-ghost" onclick="logout()" style="width:100%">Logout</button>
    </div>
  </aside>
  <main class="main">
    <!-- OVERVIEW -->
    <div id="page-overview">
      <div class="page-header"><h1>Overview</h1><p>System health and recent activity</p></div>
      <div class="stat-grid">
        <div class="stat"><div class="num" id="statCollections">-</div><div class="lbl">Collections</div></div>
        <div class="stat"><div class="num" id="statAdmins">-</div><div class="lbl">Admin Users</div></div>
        <div class="stat"><div class="num" id="statClients">-</div><div class="lbl">Realtime Clients</div></div>
        <div class="stat"><div class="num" id="statVersion">-</div><div class="lbl">Version</div></div>
      </div>
      <div class="section-title">System Health</div>
      <div class="table-wrap mb-4"><table id="healthTable">
        <tr><td style="width:140px">Version</td><td><span class="badge" id="hVersion">-</span></td></tr>
        <tr><td>Database</td><td><span class="badge" id="hDB">-</span></td></tr>
        <tr><td>Storage</td><td><span class="badge" id="hStorage">-</span></td></tr>
        <tr><td>Realtime</td><td><span class="badge" id="hRealtime">-</span></td></tr>
      </table></div>
      <div class="section-title">Recent Activity</div>
      <div class="table-wrap"><table><thead><tr><th>Action</th><th>Resource</th><th>Details</th><th>Time</th></tr></thead><tbody id="logsBody"></tbody></table></div>
    </div>
    <!-- COLLECTIONS -->
    <div id="page-collections" class="hidden">
      <div class="page-header"><h1>Collections</h1><p id="collCount">Manage database tables with dynamic schemas</p></div>
      <div class="flex-between mb-4"><div class="tab-bar"><button class="active" onclick="collTab('list')">List</button><button onclick="collTab('create')">+ Create</button></div><div class="flex gap-2"><button class="btn-sm btn-ghost" onclick="exportCollections()">⬇ Export</button><button class="btn-sm btn-ghost" onclick="importCollections()">⬆ Import</button></div></div>
      <div id="coll-list-view"><div class="table-wrap"><table><thead><tr><th>Name</th><th>Type</th><th>Fields</th><th>Rules</th><th>Created</th><th></th></tr></thead><tbody id="collectionsBody"></tbody></table></div></div>
      <div id="coll-create-view" class="hidden">
        <div class="card">
          <div class="flex gap-2 mb-4"><input id="newCollName" placeholder="collection_name (e.g. posts)" style="flex:2"><select id="newCollType" style="flex:1"><option value="base">Base</option><option value="auth">Auth</option><option value="view">View</option></select></div>
          <div class="section-title">Schema Fields <span class="text-sm">(define the columns of your table)</span></div>
          <div id="schemaFields"></div>
          <button class="btn-sm btn-ghost mt-2" onclick="addSchemaField()">+ Add Field</button>
          <div class="section-title mt-4">Auth Methods <span class="text-sm">(for "auth" type collections)</span></div>
          <div class="flex gap-2 mb-4" style="flex-wrap:wrap">
            <label class="field-required"><input type="checkbox" id="authPassword" checked> Password</label>
            <label class="field-required"><input type="checkbox" id="authOTP"> OTP</label>
            <label class="field-required"><input type="checkbox" id="authOAuth2"> OAuth2</label>
            <label class="field-required"><input type="checkbox" id="authMFA"> MFA</label>
            <label class="field-required"><input type="checkbox" id="authVerified"> Require Verified Email</label>
          </div>
          <div class="section-title mt-4">Access Rules</div>
          <div class="flex gap-2 mb-4">
            <div><label>List Rule</label><input id="newCollListRule" placeholder="published = true"></div>
            <div><label>View Rule</label><input id="newCollViewRule" placeholder=""></div>
            <div><label>Create Rule</label><input id="newCollCreateRule" placeholder="@request.auth.role = 'admin'"></div>
            <div><label>Update Rule</label><input id="newCollUpdateRule" placeholder=""></div>
            <div><label>Delete Rule</label><input id="newCollDeleteRule" placeholder=""></div>
          </div>
          <div class="flex gap-2"><button onclick="createCollection()">Create Collection</button><button class="btn-ghost" onclick="collTab('list')">Cancel</button></div>
        </div>
      </div>
      <!-- Record viewer -->
      <div id="coll-records-view" class="hidden">
        <div class="flex-between"><h3 id="recordsTitle">Records</h3><div class="flex gap-2"><button class="btn-sm btn-ghost" onclick="collTab('list')">← Back</button><button class="btn-sm" onclick="showCreateRecord()">+ New Record</button></div></div>
        <div class="card hidden mb-4" id="createRecordForm">
          <div class="section-title">Create Record</div>
          <div id="createRecordFields"></div>
          <div class="flex gap-2 mt-2"><button onclick="createRecord()">Save Record</button><button class="btn-ghost" onclick="document.getElementById('createRecordForm').classList.add('hidden')">Cancel</button></div>
        </div>
        <div id="recordsFilter" class="flex gap-2 mb-4"><input id="recordSearchFilter" placeholder="Filter records..." style="flex:1" onkeyup="loadRecords()"></div>
        <div class="table-wrap"><div style="overflow-x:auto"><table><thead id="recordsHead"></thead><tbody id="recordsBody"></tbody></table></div></div>
      </div>
    </div>
    <!-- ADMINS -->
    <div id="page-admins" class="hidden">
      <div class="page-header"><h1>Admin Users</h1><p>Manage administrators</p></div>
      <div class="flex-between mb-4"><button class="btn-sm" onclick="showCreateAdminForm()">+ Add Admin</button></div>
      <div class="card hidden mb-4" id="createAdminCard"><div class="flex gap-2"><input id="newAdminEmail" placeholder="email" type="email" style="flex:1"><input id="newAdminPassword" placeholder="password" type="password" style="flex:1"><button class="btn-sm" onclick="createAdmin()">Add</button><button class="btn-sm btn-ghost" onclick="document.getElementById('createAdminCard').classList.add('hidden')">Cancel</button></div></div>
      <div class="table-wrap"><table><thead><tr><th>Email</th><th>Role</th><th>Last Login</th><th>Created</th><th></th></tr></thead><tbody id="adminsBody"></tbody></table></div>
    </div>
    <!-- API KEYS -->
    <div id="page-api-keys" class="hidden">
      <div class="page-header"><h1>API Keys</h1><p>Manage API access tokens</p></div>
      <div class="flex-between mb-4"><button class="btn-sm" onclick="showCreateApiKey()">+ Create API Key</button></div>
      <div class="card hidden mb-4" id="createApiKeyCard"><div class="flex gap-2"><input id="newApiKeyName" placeholder="Key name (e.g. production)" style="flex:1"><button class="btn-sm" onclick="createApiKey()">Create</button><button class="btn-sm btn-ghost" onclick="document.getElementById('createApiKeyCard').classList.add('hidden')">Cancel</button></div></div>
      <div id="newApiKeyDisplay" class="hidden card success"><p class="text-sm">⚠️ Copy this key now. You won't see it again.</p><code class="inline-code" id="newApiKeyValue" style="word-break:break-all"></code></div>
      <div class="table-wrap"><table><thead><tr><th>Name</th><th>Prefix</th><th>Created</th><th></th></tr></thead><tbody id="apiKeysBody"></tbody></table></div>
    </div>
    <!-- CERTIFICATES -->
    <div id="page-certificates" class="hidden">
      <div class="page-header"><h1>Certificates</h1><p>Manage TLS certificates via internal ACME CA</p></div>
      <div class="card mb-4"><div class="flex gap-2"><input id="certDomain" placeholder="example.com" style="flex:1"><button class="btn-sm" onclick="issueCertificate()">Issue Certificate</button></div></div>
      <div class="table-wrap"><table><thead><tr><th>Domain</th><th>Status</th><th>Issuer</th><th>Expires</th><th></th></tr></thead><tbody id="certsBody"></tbody></table></div>
    </div>
    <!-- SETTINGS -->
    <div id="page-settings" class="hidden">
      <div class="page-header"><h1>Settings</h1><p>Application configuration</p></div>
      <div class="card"><div class="section-title">SMTP Email</div>
        <div class="flex flex-wrap gap-2 mb-4">
          <div style="flex:1"><label>Host</label><input id="smtpHost" placeholder="smtp.gmail.com"></div>
          <div style="flex:0.5"><label>Port</label><input id="smtpPort" placeholder="587" type="number"></div>
          <div style="flex:1"><label>Username</label><input id="smtpUser" placeholder="user@gmail.com"></div>
          <div style="flex:1"><label>Password</label><input id="smtpPass" type="password" placeholder="app-password"></div>
        </div>
        <button onclick="saveSettings()">Save Settings</button>
      </div>
    </div>
    <!-- API REFERENCE -->
    <div id="page-api" class="hidden">
      <div class="page-header"><h1>API Explorer</h1><p>Interactive API client — try endpoints live <code class="inline-code" id="baseUrlDisplay">-</code></p></div>
      <div class="card" style="margin-bottom:16px">
        <div class="flex gap-2 mb-4" style="flex-wrap:wrap">
          <select id="apiMethod" style="width:110px"><option>GET</option><option>POST</option><option>PUT</option><option>PATCH</option><option>DELETE</option></select>
          <input id="apiPath" placeholder="/collections" style="flex:1;min-width:200px" value="/collections">
          <button class="btn-sm" onclick="sendAPIRequest()" style="margin:0;width:auto">🚀 Send</button>
        </div>
        <div id="apiRequestBody" class="hidden mb-4"><label class="text-sm">Request Body (JSON)</label><textarea id="apiBody" rows="6" style="font-family:'JetBrains Mono',monospace;font-size:12px;width:100%" placeholder='{"name": "posts", "type": "base"}'></textarea></div>
        <div id="apiResponse" class="hidden" style="background:var(--bg);border:1px solid var(--border);border-radius:8px;padding:12px;max-height:400px;overflow:auto">
          <div class="flex-between"><span class="text-sm" id="apiStatus"></span><button class="btn-sm btn-ghost" onclick="document.getElementById('apiResponse').classList.add('hidden')">✕</button></div>
          <pre id="apiResponseBody" style="font-family:'JetBrains Mono',monospace;font-size:12px;white-space:pre-wrap;word-break:break-all;margin-top:8px;color:var(--text)"></pre>
        </div>
      </div>
      <div class="section-title">Quick Endpoints</div>
      <div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(280px,1fr));gap:6px">
        <div class="endpoint-card" onclick="quickAPI('GET','/collections')"><span class="badge success" style="font-size:10px">GET</span> /collections</div>
        <div class="endpoint-card" onclick="quickAPI('GET','/records/posts')"><span class="badge success" style="font-size:10px">GET</span> /records/posts</div>
        <div class="endpoint-card" onclick="quickAPI('POST','/auth/login')"><span class="badge" style="font-size:10px">POST</span> /auth/login</div>
        <div class="endpoint-card" onclick="quickAPI('GET','/health')"><span class="badge success" style="font-size:10px">GET</span> /health</div>
        <div class="endpoint-card" onclick="quickAPI('GET','/admin/users')"><span class="badge success" style="font-size:10px">GET</span> /admin/users</div>
        <div class="endpoint-card" onclick="quickAPI('GET','/logs')"><span class="badge success" style="font-size:10px">GET</span> /logs</div>
        <div class="endpoint-card" onclick="quickAPI('GET','/settings')"><span class="badge success" style="font-size:10px">GET</span> /settings</div>
        <div class="endpoint-card" onclick="quickAPI('GET','/api-keys')"><span class="badge success" style="font-size:10px">GET</span> /api-keys</div>
        <div class="endpoint-card" onclick="quickAPI('GET','/certificates')"><span class="badge success" style="font-size:10px">GET</span> /certificates</div>
        <div class="endpoint-card" onclick="quickAPI('GET','/backups')"><span class="badge success" style="font-size:10px">GET</span> /backups</div>
        <div class="endpoint-card" onclick="quickAPI('GET','/openapi.json')"><span class="badge success" style="font-size:10px">GET</span> /openapi.json</div>
        <div class="endpoint-card" onclick="quickAPI('GET','/metrics')"><span class="badge success" style="font-size:10px">GET</span> /metrics</div>
      </div>
    </div>
  </main>
</div>
<!-- TOAST -->
<div class="toast" id="toastContainer"></div>

<script>
const API = '/api/v1';
let token = null;
let currentPage = 'overview';
let currentCollection = null;
let collViewMode = 'list';

// ─── API ───────────────────────────────────
async function api(path, opts = {}) {
  const headers = { 'Content-Type': 'application/json', ...opts.headers };
  if (token) headers['Authorization'] = 'Bearer ' + token;
  if ((opts.body instanceof FormData)) delete headers['Content-Type'];
  const res = await fetch(API + path, { ...opts, headers, credentials: 'include' });
  const text = await res.text();
  let data;
  try { data = JSON.parse(text); } catch { data = text; }
  if (!res.ok) throw new Error((data && data.message) || 'Request failed (' + res.status + ')');
  return data;
}

// ─── AUTH ──────────────────────────────────
document.getElementById('loginForm').addEventListener('submit', async e => {
  e.preventDefault();
  const errEl = document.getElementById('loginError');
  errEl.style.display = 'none';
  const btn = e.target.querySelector('button');
  btn.disabled = true; btn.textContent = 'Signing in...';
  try {
    const res = await api('/auth/login', { method: 'POST', body: JSON.stringify({
      email: document.getElementById('loginEmail').value,
      password: document.getElementById('loginPassword').value
    })});
    token = res.token;
    localStorage.setItem('gb_token', token);
    localStorage.setItem('gb_admin', JSON.stringify(res.admin));
    showDashboard(res.admin);
  } catch(err) {
    errEl.textContent = err.message; errEl.style.display = 'block';
  } finally { btn.disabled = false; btn.textContent = 'Sign in'; }
});

document.getElementById('registerForm').addEventListener('submit', async e => {
  e.preventDefault();
  const errEl = document.getElementById('registerError');
  errEl.style.display = 'none';
  const pw = document.getElementById('registerPassword').value;
  if (pw !== document.getElementById('registerPasswordConfirm').value) {
    errEl.textContent = 'Passwords do not match'; errEl.style.display = 'block'; return;
  }
  const btn = e.target.querySelector('button');
  btn.disabled = true; btn.textContent = 'Creating...';
  try {
    const res = await api('/auth/register', { method: 'POST', body: JSON.stringify({
      email: document.getElementById('registerEmail').value, password: pw
    })});
    token = res.token;
    localStorage.setItem('gb_token', token);
    localStorage.setItem('gb_admin', JSON.stringify(res.admin));
    showDashboard(res.admin);
  } catch(err) {
    errEl.textContent = err.message; errEl.style.display = 'block';
  } finally { btn.disabled = false; btn.textContent = 'Create Account'; }
});

// ─── NAVIGATION ────────────────────────────
function navigate(page) {
  currentPage = page;
  document.querySelectorAll('[data-nav]').forEach(a => a.classList.remove('active'));
  document.querySelector('[data-nav="' + page + '"]').classList.add('active');
  document.querySelectorAll('[id^="page-"]').forEach(el => el.classList.add('hidden'));
  document.getElementById('page-' + page).classList.remove('hidden');
  if (page === 'overview') loadOverview();
  if (page === 'collections') loadCollections();
  if (page === 'admins') loadAdmins();
  if (page === 'api-keys') loadApiKeys();
  if (page === 'certificates') loadCertificates();
  if (page === 'settings') loadSettings();
  if (page === 'api') document.getElementById('baseUrlDisplay').textContent = window.location.origin + API;
}

function collTab(mode) {
  collViewMode = mode;
  document.getElementById('coll-list-view').classList.toggle('hidden', mode !== 'list');
  document.getElementById('coll-create-view').classList.toggle('hidden', mode !== 'create');
  document.getElementById('coll-records-view').classList.toggle('hidden', mode !== 'records');
  document.querySelectorAll('#page-collections .tab-bar button').forEach((b, i) => b.classList.toggle('active', (i===0 && mode==='list') || (i===1 && mode==='create')));
  if (mode === 'list') loadCollections();
  if (mode === 'create') initSchemaBuilder();
}

// ─── TOAST ─────────────────────────────────
function toast(msg, type) {
  const c = document.getElementById('toastContainer');
  const el = document.createElement('div');
  el.className = 'toast-item ' + (type || '');
  el.textContent = msg;
  c.appendChild(el);
  setTimeout(() => { el.remove(); }, 4000);
}

// ─── OVERVIEW ──────────────────────────────
async function loadOverview() {
  try {
    const h = await api('/health');
    document.getElementById('hVersion').textContent = h.version || '-';
    document.getElementById('hDB').textContent = h.database ? 'healthy' : 'error';
    document.getElementById('hDB').className = h.database ? 'badge success' : 'badge danger';
    document.getElementById('statVersion').textContent = h.version || '-';
  } catch(e) {}
  try {
    const c = await api('/collections');
    document.getElementById('statCollections').textContent = Array.isArray(c) ? c.length : 0;
  } catch(e) { document.getElementById('statCollections').textContent = '-'; }
  try {
    const a = await api('/admin/users');
    document.getElementById('statAdmins').textContent = Array.isArray(a) ? a.length : 0;
  } catch(e) { document.getElementById('statAdmins').textContent = '-'; }
  try {
    const logs = await api('/logs');
    if (logs && logs.items) {
      document.getElementById('logsBody').innerHTML = logs.items.slice(0, 12).map(l =>
        '<tr><td><span class="badge">' + l.action + '</span></td><td>' + l.resource + '</td><td class="text-sm">' + (l.resource_id || '') + '</td><td class="text-sm">' + (l.created_at || '') + '</td></tr>'
      ).join('');
    }
  } catch(e) {}
}

// ─── COLLECTIONS ───────────────────────────
async function loadCollections() {
  try {
    const colls = await api('/collections');
    document.getElementById('collCount').textContent = (Array.isArray(colls) ? colls.length : 0) + ' collection(s)';
    document.getElementById('collectionsBody').innerHTML = (Array.isArray(colls) ? colls : []).map(c =>
      '<tr><td><strong>' + c.name + '</strong></td><td><span class="badge">' + (c.type || 'base') + '</span></td><td class="text-sm">' + ((c.schema && c.schema.length) || 0) + ' fields</td><td class="text-sm">' + (c.list_rule || c.create_rule ? '✓' : '-') + '</td><td class="text-sm">' + (c.created_at ? new Date(c.created_at).toLocaleDateString() : '-') + '</td><td class="flex gap-2"><button class="btn-sm btn-ghost" onclick="viewRecords(\'' + c.name + '\')">📋 Records</button><button class="btn-danger" onclick="deleteCollection(\'' + c.id + '\',\'' + c.name + '\')">🗑</button></td></tr>'
    ).join('') || '<tr><td colspan="6" class="text-sm" style="padding:20px">No collections yet. Create your first one!</td></tr>';
  } catch(e) { toast(e.message, 'error'); }
}

function initSchemaBuilder() {
  const div = document.getElementById('schemaFields');
  div.innerHTML = '';
  addSchemaField();
  addSchemaField();
}

function addSchemaField() {
  const div = document.getElementById('schemaFields');
  const row = document.createElement('div');
  row.className = 'field-row';
  row.innerHTML = '<input class="field-name" placeholder="field_name" value="">' +
    '<select class="field-type"><option value="text">text</option><option value="number">number</option><option value="bool">bool</option><option value="email">email</option><option value="url">url</option><option value="date">date</option><option value="select">select</option><option value="json">json</option><option value="file">file</option><option value="relation">relation</option><option value="password">password</option><option value="editor">editor</option></select>' +
    '<label class="field-required"><input type="checkbox" checked> Required</label>' +
    '<button class="btn-danger" onclick="this.parentElement.remove()" style="font-size:10px;padding:4px 8px">✕</button>';
  div.appendChild(row);
}

async function createCollection() {
  const name = document.getElementById('newCollName').value.trim();
  const type = document.getElementById('newCollType').value;
  if (!name) return toast('Collection name is required', 'error');
  const fields = [];
  document.querySelectorAll('#schemaFields .field-row').forEach(r => {
    const nameEl = r.querySelector('.field-name');
    const typeEl = r.querySelector('.field-type');
    const reqEl = r.querySelector('input[type=checkbox]');
    if (nameEl.value.trim()) {
      fields.push({ name: nameEl.value.trim(), type: typeEl.value, required: reqEl.checked });
    }
  });
  // Auth options for "auth" type collections
  const options = {};
  if (type === 'auth') {
    options.authMethods = {
      password: document.getElementById('authPassword')?.checked ?? true,
      otp: document.getElementById('authOTP')?.checked ?? false,
      oauth2: document.getElementById('authOAuth2')?.checked ?? false,
      mfa: document.getElementById('authMFA')?.checked ?? false,
    };
    options.onlyVerified = document.getElementById('authVerified')?.checked ?? false;
  }
  
  try {
    await api('/collections', { method: 'POST', body: JSON.stringify({
      name, type, schema: fields, options,
      list_rule: document.getElementById('newCollListRule').value || '',
      view_rule: document.getElementById('newCollViewRule').value || '',
      create_rule: document.getElementById('newCollCreateRule').value || '',
      update_rule: document.getElementById('newCollUpdateRule').value || '',
      delete_rule: document.getElementById('newCollDeleteRule').value || '',
    })});
    toast('Collection "' + name + '" created!', 'success');
    collTab('list');
  } catch(e) { toast(e.message, 'error'); }
}

async function deleteCollection(id, name) {
  if (!confirm('Delete "' + name + '" and ALL records? This cannot be undone.')) return;
  try { await api('/collections/' + id, { method: 'DELETE' }); toast('Deleted "' + name + '"', 'success'); loadCollections(); }
  catch(e) { toast(e.message, 'error'); }
}

let currentCollName = '';

async function viewRecords(collName) {
  currentCollName = collName;
  currentCollection = collName;
  collTab('records');
  document.getElementById('recordsTitle').textContent = 'Records: ' + collName;
  await loadRecords();
}

async function loadRecords() {
  if (!currentCollName) return;
  try {
    const filter = document.getElementById('recordSearchFilter').value;
    const params = filter ? { filter: filter } : {};
    const data = await api('/records/' + currentCollName + '?' + new URLSearchParams(params));
    const items = data.items || [];
    window._recordsData = items;
    if (items.length === 0) {
      document.getElementById('recordsHead').innerHTML = '';
      document.getElementById('recordsBody').innerHTML = '<tr><td colspan="10" class="text-sm" style="padding:20px">No records found</td></tr>';
      return;
    }
    const keys = Object.keys(items[0]).filter(k => k !== 'password' && k !== 'password_hash' && k !== 'tokenKey');
    window._recordKeys = keys;
    document.getElementById('recordsHead').innerHTML = '<tr>' + keys.map(k => '<th>' + k + '</th>').join('') + '<th>Actions</th></tr>';
    document.getElementById('recordsBody').innerHTML = items.map((r, idx) =>
      '<tr id="row-' + idx + '">' + keys.map(k => '<td class="text-sm editable-cell" data-key="' + k + '" data-row="' + idx + '" onclick="startEdit(this)">' + formatCell(r[k]) + '</td>').join('') +
      '<td class="flex gap-2"><button class="btn-sm btn-ghost" onclick="saveEdits(\'' + r.id + '\', ' + idx + ')">💾</button><button class="btn-danger" onclick="deleteRecord(\'' + r.id + '\')">🗑</button></td></tr>'
    ).join('');
  } catch(e) { toast(e.message, 'error'); }
}

function formatCell(val) {
  if (val === null || val === undefined) return '<span class="text-sm">null</span>';
  if (typeof val === 'boolean') return val ? '✓ true' : '✗ false';
  if (typeof val === 'object') return JSON.stringify(val).substring(0, 60);
  return String(val).substring(0, 120);
}

function startEdit(cell) {
  if (cell.querySelector('input')) return;
  const key = cell.dataset.key;
  const row = parseInt(cell.dataset.row);
  const currentVal = window._recordsData[row][key];
  const val = currentVal === null || currentVal === undefined ? '' : typeof currentVal === 'object' ? JSON.stringify(currentVal) : String(currentVal);
  cell.innerHTML = '<input value="' + val.replace(/"/g, '&quot;').replace(/</g, '&lt;') + '" data-key="' + key + '" data-row="' + row + '" style="width:100%;padding:4px 6px;font-size:12px" onkeydown="if(event.key===\'Enter\')this.blur()" onblur="commitEdit(this)">';
  cell.querySelector('input').focus();
}

function commitEdit(input) {
  const key = input.dataset.key;
  const row = parseInt(input.dataset.row);
  const newVal = input.value;
  if (!window._editBuffer) window._editBuffer = {};
  if (!window._editBuffer[row]) window._editBuffer[row] = {};
  window._editBuffer[row][key] = newVal;
  // Update display
  const cell = input.parentElement;
  cell.innerHTML = formatCell(newVal === '' ? null : newVal);
  cell.classList.add('text-sm', 'editable-cell');
  cell.setAttribute('data-key', key);
  cell.setAttribute('data-row', row);
  cell.setAttribute('onclick', 'startEdit(this)');
}

async function saveEdits(id, row) {
  if (!window._editBuffer || !window._editBuffer[row]) return toast('No changes to save');
  try {
    await api('/records/' + currentCollName + '/' + id, { method: 'PUT', body: JSON.stringify(window._editBuffer[row]) });
    delete window._editBuffer[row];
    toast('Record updated!', 'success');
    loadRecords();
  } catch(e) { toast(e.message, 'error'); }
}

async function showCreateRecord() {
  document.getElementById('createRecordForm').classList.remove('hidden');
  try {
    const colls = await api('/collections');
    const coll = (Array.isArray(colls) ? colls : []).find(c => c.name === currentCollName);
    if (coll && coll.schema) {
      document.getElementById('createRecordFields').innerHTML = coll.schema.map(f =>
        '<div class="form-group"><label>' + f.name + ' (' + f.type + ')</label><input data-field="' + f.name + '" placeholder="' + f.name + '" ' + (f.required ? 'required' : '') + '></div>'
      ).join('');
    }
  } catch(e) {}
}

async function createRecord() {
  const data = {};
  document.querySelectorAll('#createRecordFields input').forEach(inp => {
    if (inp.value) data[inp.dataset.field] = inp.value;
  });
  try {
    await api('/records/' + currentCollName, { method: 'POST', body: JSON.stringify(data) });
    toast('Record created!', 'success');
    document.getElementById('createRecordForm').classList.add('hidden');
    loadRecords();
  } catch(e) { toast(e.message, 'error'); }
}

async function deleteRecord(id) {
  if (!confirm('Delete this record?')) return;
  try { await api('/records/' + currentCollName + '/' + id, { method: 'DELETE' }); toast('Deleted', 'success'); loadRecords(); }
  catch(e) { toast(e.message, 'error'); }
}

// ─── ADMINS ────────────────────────────────
async function loadAdmins() {
  try {
    const admins = await api('/admin/users');
    document.getElementById('adminsBody').innerHTML = (Array.isArray(admins) ? admins : []).map(a =>
      '<tr><td>' + a.email + '</td><td><span class="badge">' + (a.role || 'admin') + '</span></td><td class="text-sm">' + (a.last_login_at ? new Date(a.last_login_at).toLocaleString() : 'never') + '</td><td class="text-sm">' + (a.created_at ? new Date(a.created_at).toLocaleDateString() : '-') + '</td><td><button class="btn-danger" onclick="deleteAdmin(\'' + a.id + '\',\'' + a.email + '\')">Remove</button></td></tr>'
    ).join('') || '<tr><td colspan="5" class="text-sm" style="padding:20px">No admin users</td></tr>';
  } catch(e) { toast(e.message, 'error'); }
}
function showCreateAdminForm() { document.getElementById('createAdminCard').classList.remove('hidden'); }
async function createAdmin() {
  const email = document.getElementById('newAdminEmail').value.trim();
  const password = document.getElementById('newAdminPassword').value;
  if (!email || !password) return toast('Email and password required', 'error');
  try {
    await api('/admin/users', { method: 'POST', body: JSON.stringify({email, password, role: 'admin'}) });
    toast('Admin created!', 'success');
    document.getElementById('createAdminCard').classList.add('hidden');
    document.getElementById('newAdminEmail').value = '';
    document.getElementById('newAdminPassword').value = '';
    loadAdmins();
  } catch(e) { toast(e.message, 'error'); }
}
async function deleteAdmin(id, email) {
  if (!confirm('Remove admin "' + email + '"?')) return;
  try { await api('/admin/users/' + id, { method: 'DELETE' }); toast('Removed', 'success'); loadAdmins(); }
  catch(e) { toast(e.message, 'error'); }
}

// ─── API KEYS ──────────────────────────────
async function loadApiKeys() {
  try {
    const keys = await api('/api-keys');
    document.getElementById('apiKeysBody').innerHTML = (Array.isArray(keys) ? keys : []).map(k =>
      '<tr><td>' + k.name + '</td><td><code class="inline-code">' + (k.prefix || '') + '...</code></td><td class="text-sm">' + (k.created_at ? new Date(k.created_at).toLocaleDateString() : '-') + '</td><td><button class="btn-danger" onclick="deleteApiKey(\'' + k.id + '\')">Revoke</button></td></tr>'
    ).join('') || '<tr><td colspan="4" class="text-sm" style="padding:20px">No API keys</td></tr>';
  } catch(e) { toast(e.message, 'error'); }
}
function showCreateApiKey() { document.getElementById('createApiKeyCard').classList.remove('hidden'); }
async function createApiKey() {
  const name = document.getElementById('newApiKeyName').value.trim() || 'API Key';
  try {
    const res = await api('/api-keys', { method: 'POST', body: JSON.stringify({name}) });
    document.getElementById('newApiKeyValue').textContent = res.key;
    document.getElementById('newApiKeyDisplay').classList.remove('hidden');
    document.getElementById('createApiKeyCard').classList.add('hidden');
    loadApiKeys();
  } catch(e) { toast(e.message, 'error'); }
}
async function deleteApiKey(id) {
  if (!confirm('Revoke this API key?')) return;
  try { await api('/api-keys/' + id, { method: 'DELETE' }); toast('Revoked', 'success'); loadApiKeys(); }
  catch(e) { toast(e.message, 'error'); }
}

// ─── CERTIFICATES ──────────────────────────
async function loadCertificates() {
  try {
    const certs = await api('/certificates');
    document.getElementById('certsBody').innerHTML = (Array.isArray(certs) ? certs : []).map(c =>
      '<tr><td><strong>' + c.domain + '</strong></td><td><span class="badge ' + (c.status === 'active' ? 'success' : 'danger') + '">' + c.status + '</span></td><td class="text-sm">' + (c.issuer || 'Gresbase CA') + '</td><td class="text-sm">' + (c.not_after ? new Date(c.not_after).toLocaleDateString() : '-') + '</td><td><button class="btn-danger" onclick="revokeCert(\'' + c.id + '\')">Revoke</button></td></tr>'
    ).join('') || '<tr><td colspan="5" class="text-sm" style="padding:20px">No certificates</td></tr>';
  } catch(e) { toast(e.message, 'error'); }
}
async function issueCertificate() {
  const domain = document.getElementById('certDomain').value.trim();
  if (!domain) return toast('Domain required', 'error');
  try {
    await api('/certificates/issue', { method: 'POST', body: JSON.stringify({domain}) });
    toast('Certificate issued for ' + domain + '!', 'success');
    document.getElementById('certDomain').value = '';
    loadCertificates();
  } catch(e) { toast(e.message, 'error'); }
}
async function revokeCert(id) {
  if (!confirm('Revoke this certificate?')) return;
  try { await api('/certificates/' + id, { method: 'DELETE' }); toast('Revoked', 'success'); loadCertificates(); }
  catch(e) { toast(e.message, 'error'); }
}

// ─── SETTINGS ──────────────────────────────
async function loadSettings() {
  try {
    const s = await api('/settings');
    if (s) {
      if (s.SMTP) { document.getElementById('smtpHost').value = s.SMTP.host || ''; document.getElementById('smtpPort').value = s.SMTP.port || ''; document.getElementById('smtpUser').value = s.SMTP.username || ''; }
    }
  } catch(e) {}
}
async function saveSettings() {
  try {
    await api('/settings', { method: 'PUT', body: JSON.stringify({
      SMTP: { host: document.getElementById('smtpHost').value, port: parseInt(document.getElementById('smtpPort').value) || 587, username: document.getElementById('smtpUser').value, password: document.getElementById('smtpPass').value }
    })});
    toast('Settings saved! Restart may be required.', 'success');
  } catch(e) { toast(e.message, 'error'); }
}

// ─── LOGOUT ────────────────────────────────
function logout() {
  token = null;
  localStorage.removeItem('gb_token');
  localStorage.removeItem('gb_admin');
  document.getElementById('dashboard').classList.add('hidden');
  document.getElementById('loginPage').classList.remove('hidden');
  document.getElementById('registerPage').classList.add('hidden');
}
function showRegister() {
  document.getElementById('loginPage').classList.add('hidden');
  document.getElementById('registerPage').classList.remove('hidden');
}
function showLogin() {
  document.getElementById('loginPage').classList.remove('hidden');
  document.getElementById('registerPage').classList.add('hidden');
}

// ─── INIT ──────────────────────────────────
function showDashboard(admin) {
  document.getElementById('loginPage').classList.add('hidden');
  document.getElementById('registerPage').classList.add('hidden');
  document.getElementById('dashboard').classList.remove('hidden');
  document.getElementById('sidebarEmail').textContent = admin.email;
  document.getElementById('baseUrlDisplay').textContent = window.location.origin + API;
  loadOverview();
}

// Auto-restore session
(function() {
  const saved = localStorage.getItem('gb_token');
  const admin = JSON.parse(localStorage.getItem('gb_admin') || 'null');
  if (saved && admin) {
    token = saved;
    api('/admin/me').then(() => showDashboard(admin)).catch(() => { logout(); });
  }
})();

// ─── API EXPLORER ─────────────────────────
function quickAPI(method, path) {
  document.getElementById('apiMethod').value = method;
  document.getElementById('apiPath').value = path;
  navigate('api');
  document.getElementById('apiRequestBody').classList.toggle('hidden', method === 'GET' || method === 'DELETE');
  document.getElementById('apiBody').value = '';
  sendAPIRequest();
}

async function sendAPIRequest() {
  const method = document.getElementById('apiMethod').value;
  const path = document.getElementById('apiPath').value;
  const bodyText = document.getElementById('apiBody').value;
  const respDiv = document.getElementById('apiResponse');
  const statusEl = document.getElementById('apiStatus');
  const bodyEl = document.getElementById('apiResponseBody');
  
  document.getElementById('apiRequestBody').classList.toggle('hidden', method === 'GET' || method === 'DELETE');
  
  respDiv.classList.remove('hidden');
  statusEl.textContent = '⏳ Loading...';
  bodyEl.textContent = '';
  
  const headers = { 'Content-Type': 'application/json' };
  if (token) headers['Authorization'] = 'Bearer ' + token;
  if (method === 'GET' || method === 'DELETE') delete headers['Content-Type'];
  
  try {
    const opts = { method, headers, credentials: 'include' };
    if (bodyText && method !== 'GET' && method !== 'DELETE') {
      opts.body = bodyText;
    }
    const res = await fetch(API + path, opts);
    const text = await res.text();
    let json;
    try { json = JSON.parse(text); } catch { json = text; }
    const pretty = typeof json === 'object' ? JSON.stringify(json, null, 2) : json;
    statusEl.innerHTML = '<span class="badge ' + (res.ok ? 'success' : 'danger') + '">' + res.status + ' ' + res.statusText + '</span>';
    bodyEl.textContent = pretty;
  } catch(err) {
    statusEl.innerHTML = '<span class="badge danger">Error</span>';
    bodyEl.textContent = err.message;
  }
}

document.getElementById('apiMethod').addEventListener('change', function() {
  document.getElementById('apiRequestBody').classList.toggle('hidden', this.value === 'GET' || this.value === 'DELETE');
});

// ─── SSE REALTIME DASHBOARD ───────────────
let sseConnection = null;
function connectSSE() {
  if (sseConnection) return;
  try {
    sseConnection = new EventSource(API + '/sse');
    sseConnection.onmessage = function(e) {
      try {
        const msg = JSON.parse(e.data);
        if (msg.event && msg.event.startsWith('record:')) {
          // Auto-refresh records if viewing a collection
          if (currentPage === 'collections' && collViewMode === 'records' && currentCollName) {
            loadRecords();
          }
        }
        // Update client count
        if (msg.event === 'presence:join' || msg.event === 'presence:leave') {
          document.getElementById('statClients').textContent = '🟢';
        }
      } catch {}
    };
    sseConnection.onerror = function() {
      sseConnection = null;
      setTimeout(connectSSE, 5000);
    };
  } catch(e) {}
}

// ─── COLLECTION EXPORT/IMPORT ──────────────
async function exportCollections() {
  try {
    const colls = await api('/collections');
    const blob = new Blob([JSON.stringify(colls, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url; a.download = 'gresbase_collections.json';
    a.click(); URL.revokeObjectURL(url);
    toast('Collections exported!', 'success');
  } catch(e) { toast(e.message, 'error'); }
}

function importCollections() {
  const input = document.createElement('input');
  input.type = 'file'; input.accept = '.json';
  input.onchange = async function() {
    const file = input.files[0];
    if (!file) return;
    const text = await file.text();
    try {
      const data = JSON.parse(text);
      await api('/collections/import', { method: 'POST', body: JSON.stringify({ collections: Array.isArray(data) ? data : [data] }) });
      toast('Collections imported!', 'success'); loadCollections();
    } catch(e) { toast(e.message, 'error'); }
  };
  input.click();
}

// ─── FILE UPLOAD IN RECORDS ───────────────
function addFileUploadField(fieldName) {
  const div = document.getElementById('createRecordFields');
  if (!div) return;
  const fileDiv = document.createElement('div');
  fileDiv.className = 'form-group';
  fileDiv.innerHTML = '<label>' + fieldName + ' (file)</label><div class="file-upload-area" id="drop-' + fieldName + '" onclick="document.getElementById(\'file-' + fieldName + '\').click()"><input type="file" id="file-' + fieldName + '" data-field="' + fieldName + '" style="display:none" onchange="handleFileSelect(this)"><p class="text-sm">Click or drag file to upload</p></div>';
  div.appendChild(fileDiv);
  
  const dropArea = document.getElementById('drop-' + fieldName);
  dropArea.addEventListener('dragover', e => { e.preventDefault(); dropArea.classList.add('dragover'); });
  dropArea.addEventListener('dragleave', () => dropArea.classList.remove('dragover'));
  dropArea.addEventListener('drop', e => {
    e.preventDefault(); dropArea.classList.remove('dragover');
    document.getElementById('file-' + fieldName).files = e.dataTransfer.files;
    handleFileSelect(document.getElementById('file-' + fieldName));
  });
}

function handleFileSelect(input) {
  const file = input.files[0];
  if (file) {
    input.parentElement.querySelector('p').textContent = '📎 ' + file.name + ' (' + (file.size / 1024).toFixed(1) + ' KB)';
    input.parentElement.style.borderColor = 'var(--success)';
  }
}

async function createRecord() {
  const data = {};
  const files = {};
  document.querySelectorAll('#createRecordFields input[data-field]').forEach(inp => {
    if (inp.type === 'file' && inp.files && inp.files[0]) {
      files[inp.dataset.field] = inp.files[0];
    } else if (inp.value) {
      data[inp.dataset.field] = inp.value;
    }
  });
  
  try {
    // Create record first
    let record = await api('/records/' + currentCollName, { method: 'POST', body: JSON.stringify(data) });
    
    // Upload files if any
    for (const [field, file] of Object.entries(files)) {
      const fd = new FormData();
      fd.append('file', file);
      await fetch(API + '/files/upload?collection=' + currentCollName + '&record=' + record.id, {
        method: 'POST', body: fd,
        headers: token ? { 'Authorization': 'Bearer ' + token } : {}
      });
    }
    
    toast('Record created!', 'success');
    document.getElementById('createRecordForm').classList.add('hidden');
    loadRecords();
  } catch(e) { toast(e.message, 'error'); }
}

// ─── SHOW CREATE RECORD (ENHANCED) ────────
async function showCreateRecord() {
  document.getElementById('createRecordForm').classList.remove('hidden');
  try {
    const colls = await api('/collections');
    const coll = (Array.isArray(colls) ? colls : []).find(c => c.name === currentCollName);
    const div = document.getElementById('createRecordFields');
    if (coll && coll.schema) {
      div.innerHTML = coll.schema.map(f => {
        if (f.type === 'file') {
          return '<div id="field-group-' + f.name + '"></div>';
        }
        return '<div class="form-group"><label>' + f.name + ' (' + f.type + ')</label><input data-field="' + f.name + '" placeholder="' + f.name + '" ' + (f.required ? 'required' : '') + '></div>';
      }).join('');
      // Add file upload fields
      coll.schema.filter(f => f.type === 'file').forEach(f => {
        setTimeout(() => addFileUploadField(f.name), 100);
      });
    }
  } catch(e) {}
}

// Reconnect SSE on dashboard load
const origShowDashboard = showDashboard;
showDashboard = function(admin) {
  origShowDashboard(admin);
  setTimeout(connectSSE, 1000);
};

// Fix: override existing createRecord if it exists
if (typeof createRecord !== 'undefined') {
  // Already defined above, skip old definition
}
</script>
</body>
</html>`
