package api

import (
	stdsql "database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gresbase/gresbase/internal/auth"
	"github.com/gresbase/gresbase/internal/events"
	"github.com/gresbase/gresbase/internal/forms"
	"github.com/gresbase/gresbase/internal/job"
	"github.com/gresbase/gresbase/internal/mailer"
	"github.com/gresbase/gresbase/internal/settings"
	"github.com/gresbase/gresbase/internal/storage"
)

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

func (h *Handlers) SettingsGet(w http.ResponseWriter, r *http.Request) {
	event := &events.SettingsListRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r)}
	var settings *settings.Settings
	if err := h.app.OnSettingsListRequest().Trigger(event, func(e events.Event) error {
		var err error
		settings, err = h.app.Settings().Get(r.Context())
		return err
	}); err != nil {
		writeError(w, 500, "Failed to load settings")
		return
	}
	// Mask sensitive fields
	settings.SMTP.Password = ""
	settings.S3.SecretKey = ""
	writeOK(w, settings)
}

func (h *Handlers) SettingsUpdate(w http.ResponseWriter, r *http.Request) {
	settings, err := h.app.Settings().Get(r.Context())
	if err != nil {
		writeError(w, 500, "Failed to load settings")
		return
	}
	prevSMTPPassword := settings.SMTP.Password
	prevS3SecretKey := settings.S3.SecretKey

	if err := decodeJSONBody(r, settings); err != nil {
		writeError(w, 400, "Invalid body")
		return
	}
	// Secrets are masked on read; an empty incoming value means "unchanged".
	if settings.SMTP.Password == "" {
		settings.SMTP.Password = prevSMTPPassword
	}
	if settings.S3.SecretKey == "" {
		settings.S3.SecretKey = prevS3SecretKey
	}
	// An entry with empty subject and body means "use the built-in default" —
	// drop it so resets actually clear the stored override.
	for id, tmpl := range settings.EmailTemplates {
		if tmpl.Subject == "" && tmpl.Body == "" {
			delete(settings.EmailTemplates, id)
		}
	}
	form := &forms.SettingsUpdateForm{Settings: settings}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.SettingsUpdateRequestEvent{
		App:      h.app,
		Request:  r,
		Info:     toEventRequestInfoWithBody(r, settings),
		Settings: settings,
	}
	if err := h.app.OnSettingsUpdateRequest().Trigger(event, func(e events.Event) error {
		return h.app.Settings().Save(r.Context(), settings)
	}); err != nil {
		writeError(w, 500, "Failed to save settings")
		return
	}

	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "settings.update", "_settings", "", nil, r)
	writeOK(w, map[string]any{"message": "Settings saved"})
}

// emailTemplateInfo describes one email template for the dashboard: its
// built-in default, the placeholders it understands, and the current override
// (if any).
type emailTemplateInfo struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Placeholders   []string `json:"placeholders"`
	DefaultSubject string   `json:"defaultSubject"`
	DefaultBody    string   `json:"defaultBody"`
	CustomSubject  string   `json:"customSubject,omitempty"`
	CustomBody     string   `json:"customBody,omitempty"`
}

// SettingsEmailTemplates returns the catalog of email templates: defaults,
// placeholders and any stored overrides. Superuser-gated like the rest of
// the settings endpoints.
func (h *Handlers) SettingsEmailTemplates(w http.ResponseWriter, r *http.Request) {
	st, err := h.app.Settings().Get(r.Context())
	if err != nil {
		writeError(w, 500, "Failed to load settings")
		return
	}

	defs := mailer.Templates()
	out := make([]emailTemplateInfo, 0, len(defs))
	for _, def := range defs {
		info := emailTemplateInfo{
			ID:             def.ID,
			Name:           def.Name,
			Description:    def.Description,
			Placeholders:   def.Placeholders,
			DefaultSubject: def.DefaultSubject,
			DefaultBody:    def.DefaultBody,
		}
		if ov, ok := st.EmailTemplates[def.ID]; ok {
			info.CustomSubject = ov.Subject
			info.CustomBody = ov.Body
		}
		out = append(out, info)
	}
	writeOK(w, out)
}

// ---------------------------------------------------------------------------
// Logs
// ---------------------------------------------------------------------------

func (h *Handlers) LogsList(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	perPage, _ := strconv.Atoi(r.URL.Query().Get("perPage"))
	form := &forms.LogsQueryForm{
		Action:   r.URL.Query().Get("action"),
		Resource: r.URL.Query().Get("resource"),
		DateFrom: r.URL.Query().Get("date_from"),
		DateTo:   r.URL.Query().Get("date_to"),
		Page:     page,
		PerPage:  perPage,
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.LogsListRequestEvent{
		App:      h.app,
		Request:  r,
		Info:     toEventRequestInfo(r),
		Action:   form.Action,
		Resource: form.Resource,
		DateFrom: form.DateFrom,
		DateTo:   form.DateTo,
		Page:     form.Page,
		PerPage:  form.PerPage,
	}
	var entries any
	var total int
	if err := h.app.OnLogsListRequest().Trigger(event, func(e events.Event) error {
		var err error
		entries, total, err = h.app.Settings().ListLogs(r.Context(), settings.LogQueryParams{
			Action:   event.Action,
			Resource: event.Resource,
			DateFrom: event.DateFrom,
			DateTo:   event.DateTo,
			Page:     event.Page,
			PerPage:  event.PerPage,
		})
		return err
	}); err != nil {
		writeError(w, 500, "Failed to fetch logs")
		return
	}

	writeOK(w, map[string]any{
		"page":       event.Page,
		"perPage":    event.PerPage,
		"totalItems": total,
		"totalPages": maxInt(1, (total+event.PerPage-1)/event.PerPage),
		"items":      entries,
	})
}

// ---------------------------------------------------------------------------
// API Keys
// ---------------------------------------------------------------------------

func (h *Handlers) APIKeysList(w http.ResponseWriter, r *http.Request) {
	adminID := r.Context().Value(contextKeyAdminID).(string)
	event := &events.APIKeyRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "list"}
	var keys []map[string]any
	if err := h.app.OnAPIKeyRequest().Trigger(event, func(e events.Event) error {
		rows, err := h.app.DB().Query(r.Context(),
			`SELECT id, admin_id, name, prefix, permissions, last_used_at, expires_at, created_at FROM _api_keys WHERE admin_id = $1 ORDER BY created_at DESC`, adminID)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var id, aID, name, prefix string
			var permissionsJSON []byte
			var lastUsedAt stdsql.NullTime
			var expiresAt stdsql.NullTime
			var createdAt time.Time
			if err := rows.Scan(&id, &aID, &name, &prefix, &permissionsJSON, &lastUsedAt, &expiresAt, &createdAt); err != nil {
				return err
			}
			item := map[string]any{
				"id":          id,
				"admin_id":    aID,
				"name":        name,
				"prefix":      prefix,
				"permissions": auth.NormalizeAPIKeyPermissions(decodeStringSliceJSON(permissionsJSON)),
				"created_at":  createdAt,
			}
			if lastUsedAt.Valid {
				item["last_used_at"] = lastUsedAt.Time.UTC()
			}
			if expiresAt.Valid {
				item["expires_at"] = expiresAt.Time.UTC()
			}
			keys = append(keys, item)
		}
		return nil
	}); err != nil {
		writeError(w, 500, "Failed to list API keys")
		return
	}
	writeOK(w, keys)
}

func (h *Handlers) APIKeysCreate(w http.ResponseWriter, r *http.Request) {
	var form forms.APIKeyCreateForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}
	adminID := r.Context().Value(contextKeyAdminID).(string)
	event := &events.APIKeyRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "create", Name: form.Name}
	var key string
	var apiKey *auth.APIKey
	if err := h.app.OnAPIKeyRequest().Trigger(event, func(e events.Event) error {
		var err error
		key, apiKey, err = h.app.Auth().GenerateAPIKey(r.Context(), adminID, form.Name, form.Permissions)
		return err
	}); err != nil {
		writeError(w, 500, "Failed to create API key")
		return
	}
	h.app.Auth().RecordAudit(r.Context(), adminID, "api_key.create", "_api_keys", apiKey.ID, map[string]any{"name": apiKey.Name, "permissions": apiKey.Permissions}, r)
	writeJSON(w, 201, map[string]any{"key": key, "apiKey": apiKey})
}

func (h *Handlers) APIKeysDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	role, _ := r.Context().Value(contextKeyAdminRole).(string)

	var ownerID string
	if err := h.app.DB().QueryRow(r.Context(), `SELECT admin_id FROM _api_keys WHERE id = $1`, id).Scan(&ownerID); err != nil {
		writeError(w, 404, "API key not found")
		return
	}
	if !strings.EqualFold(role, "super_admin") && ownerID != adminID {
		writeError(w, 403, "Access denied")
		return
	}

	event := &events.APIKeyRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "delete", APIKeyID: id}
	if err := h.app.OnAPIKeyRequest().Trigger(event, func(e events.Event) error {
		return h.app.DB().Exec(r.Context(), "DELETE FROM _api_keys WHERE id = $1", id)
	}); err != nil {
		writeError(w, 500, "Failed to delete API key")
		return
	}
	h.app.Auth().RecordAudit(r.Context(), adminID, "api_key.delete", "_api_keys", id, map[string]any{"owner_id": ownerID}, r)
	writeOK(w, map[string]any{"deleted": id})
}

// ---------------------------------------------------------------------------
// Backups
// ---------------------------------------------------------------------------

func (h *Handlers) BackupsList(w http.ResponseWriter, r *http.Request) {
	event := &events.BackupListRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r)}
	var backups []*storage.BackupInfo
	if err := h.app.OnBackupListRequest().Trigger(event, func(e events.Event) error {
		var err error
		backups, err = h.app.Backup().ListBackups()
		return err
	}); err != nil {
		writeError(w, 500, "Failed to list backups")
		return
	}
	if backups == nil {
		backups = []*storage.BackupInfo{}
	}
	writeOK(w, backups)
}

func (h *Handlers) BackupsCreate(w http.ResponseWriter, r *http.Request) {
	var form forms.BackupCreateForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.BackupCreateRequestEvent{
		App:          h.app,
		Request:      r,
		Info:         toEventRequestInfoWithBody(r, form),
		Name:         form.Name,
		IncludeFiles: form.IncludeFiles,
	}
	var info *storage.BackupInfo
	if err := h.app.OnBackupCreateRequest().Trigger(event, func(e events.Event) error {
		var err error
		info, err = h.app.Backup().CreateBackup(r.Context(), event.Name, event.IncludeFiles)
		return err
	}); err != nil {
		writeInternalError(w, "Backup failed", err)
		return
	}

	if h.app.Mailer() != nil {
		// Notify admins about backup completion
	}

	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "backup.create", "_backups", info.ID, map[string]any{"name": info.Name}, r)
	writeJSON(w, 201, info)
}

func (h *Handlers) BackupsRestore(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	backups, _ := h.app.Backup().ListBackups()
	var backupID string
	for _, b := range backups {
		if b.Name == name || b.ID == name {
			backupID = b.ID
			break
		}
	}
	if backupID == "" {
		writeError(w, 404, "Backup not found")
		return
	}

	event := &events.BackupRestoreRequestEvent{
		App:        h.app,
		Request:    r,
		Info:       toEventRequestInfoWithBody(r, map[string]any{"backup": name}),
		BackupID:   backupID,
		BackupName: name,
	}
	if err := h.app.OnBackupRestoreRequest().Trigger(event, func(e events.Event) error {
		return h.app.Backup().RestoreBackup(r.Context(), event.BackupID)
	}); err != nil {
		writeInternalError(w, "Restore failed", err)
		return
	}

	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "backup.restore", "_backups", event.BackupID, map[string]any{"name": event.BackupName}, r)
	writeOK(w, map[string]any{"message": "Restore completed — restart the server to apply changes"})
}

func (h *Handlers) BackupsDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	event := &events.BackupDeleteRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), BackupName: name}
	if err := h.app.OnBackupDeleteRequest().Trigger(event, func(e events.Event) error {
		return h.app.Backup().DeleteBackup(name)
	}); err != nil {
		writeInternalError(w, "Delete failed", err)
		return
	}
	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "backup.delete", "_backups", name, nil, r)
	writeOK(w, map[string]any{"deleted": name})
}

func (h *Handlers) BackupsDownload(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var path string
	event := &events.BackupDownloadRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), BackupName: name}
	if err := h.app.OnBackupDownloadRequest().Trigger(event, func(e events.Event) error {
		var err error
		path, err = h.app.Backup().GetBackupPath(name)
		if err == nil {
			event.BackupPath = path
		}
		return err
	}); err != nil {
		writeError(w, 404, "Backup not found")
		return
	}
	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "backup.download", "_backups", name, nil, r)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, name))
	http.ServeFile(w, r, path)
}

// ---------------------------------------------------------------------------
// Cron Jobs
// ---------------------------------------------------------------------------

func (h *Handlers) JobsList(w http.ResponseWriter, r *http.Request) {
	event := &events.JobRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "list"}
	var jobs any
	if err := h.app.OnJobRequest().Trigger(event, func(e events.Event) error {
		var err error
		jobs, err = h.app.Jobs().ListJobs(r.Context())
		return err
	}); err != nil {
		writeError(w, 500, "Failed to list jobs")
		return
	}
	writeOK(w, jobs)
}

func (h *Handlers) JobsCreate(w http.ResponseWriter, r *http.Request) {
	var form forms.JobCreateForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}
	if err := h.app.Jobs().ValidateCron(form.CronExpr); err != nil {
		writeError(w, 400, "Invalid cron expression: "+err.Error())
		return
	}

	event := &events.JobRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "create", Name: form.Name, CronExpr: form.CronExpr, Handler: form.Handler, Data: form.Data}
	var jobInfo *job.Job
	if err := h.app.OnJobRequest().Trigger(event, func(e events.Event) error {
		var err error
		jobInfo, err = h.app.Jobs().AddJob(r.Context(), form.Name, form.CronExpr, form.Handler, form.Data)
		return err
	}); err != nil {
		writeInternalError(w, "Failed to create job", err)
		return
	}
	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "job.create", "_jobs", jobInfo.ID, map[string]any{"name": form.Name}, r)
	writeJSON(w, 201, jobInfo)
}

func (h *Handlers) JobsRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	event := &events.JobRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "run", JobID: id}
	if err := h.app.OnJobRequest().Trigger(event, func(e events.Event) error {
		return h.app.Jobs().RunJob(r.Context(), id)
	}); err != nil {
		writeInternalError(w, "Job run failed", err)
		return
	}
	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "job.run", "_jobs", id, nil, r)
	writeOK(w, map[string]any{"message": "Job triggered", "id": id})
}

// JobsUpdate toggles a job's enabled state.
func (h *Handlers) JobsUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var form struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeJSONBody(r, &form); err != nil || form.Enabled == nil {
		writeError(w, 400, `Body must include "enabled"`)
		return
	}
	event := &events.JobRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "update", JobID: id}
	if err := h.app.OnJobRequest().Trigger(event, func(e events.Event) error {
		return h.app.Jobs().SetJobEnabled(r.Context(), id, *form.Enabled)
	}); err != nil {
		writeInternalError(w, "Update failed", err)
		return
	}
	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "job.update", "_jobs", id, map[string]any{"enabled": *form.Enabled}, r)
	writeOK(w, map[string]any{"id": id, "enabled": *form.Enabled})
}

// JobsRuns returns the recent execution history of a job.
func (h *Handlers) JobsRuns(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := h.app.Jobs().ListRuns(r.Context(), id, limit)
	if err != nil {
		writeInternalError(w, "Failed to list runs", err)
		return
	}
	writeOK(w, runs)
}

func (h *Handlers) JobsDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	event := &events.JobRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "delete", JobID: id}
	if err := h.app.OnJobRequest().Trigger(event, func(e events.Event) error {
		return h.app.Jobs().DeleteJob(r.Context(), id)
	}); err != nil {
		writeInternalError(w, "Delete failed", err)
		return
	}
	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "job.delete", "_jobs", id, nil, r)
	writeOK(w, map[string]any{"deleted": id})
}

func decodeStringSliceJSON(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var result []string
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil
	}
	return result
}

func generateID() string {
	return fmt.Sprintf("%x", time.Now().UnixNano())
}
