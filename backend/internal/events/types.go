package events

import (
	"net/http"
	"time"
)

// -------------------------------------------------------------------
// App-level events
// -------------------------------------------------------------------

// BootstrapEvent is fired when the application bootstraps.
type BootstrapEvent struct {
	BaseEvent
	App interface{}
}

// TerminateEvent is fired when the application is terminating.
type TerminateEvent struct {
	BaseEvent
	App       interface{}
	IsRestart bool
}

// RouteRegistrar is the router surface exposed to plugins/extensions on serve.
type RouteRegistrar interface {
	Use(middlewares ...func(http.Handler) http.Handler)
	Handle(pattern string, handler http.Handler)
	HandleFunc(pattern string, handlerFn http.HandlerFunc)
	Method(method, pattern string, handler http.Handler)
	MethodFunc(method, pattern string, handlerFn http.HandlerFunc)
	Get(pattern string, handlerFn http.HandlerFunc)
	Post(pattern string, handlerFn http.HandlerFunc)
	Put(pattern string, handlerFn http.HandlerFunc)
	Patch(pattern string, handlerFn http.HandlerFunc)
	Delete(pattern string, handlerFn http.HandlerFunc)
	Group(prefix string, fn func(RouteRegistrar))
	Mount(prefix string, fn func(RouteRegistrar))
}

// ServeEvent is fired when the HTTP server starts.
type ServeEvent struct {
	BaseEvent
	App    interface{}
	Addr   string
	Router RouteRegistrar
	Server interface{}
}

// -------------------------------------------------------------------
// Model events
// -------------------------------------------------------------------

// ModelEventType represents the type of model event.
type ModelEventType string

const (
	ModelEventCreate   ModelEventType = "create"
	ModelEventUpdate   ModelEventType = "update"
	ModelEventDelete   ModelEventType = "delete"
	ModelEventValidate ModelEventType = "validate"
)

// ModelEvent is fired for CRUD operations on models.
type ModelEvent struct {
	BaseEvent
	App       interface{}
	Model     interface{}
	TableName string
	Type      ModelEventType
}

// ModelErrorEvent is fired when a model operation fails.
type ModelErrorEvent struct {
	BaseEvent
	App       interface{}
	Model     interface{}
	TableName string
	Type      ModelEventType
	Error     error
}

// -------------------------------------------------------------------
// Record events
// -------------------------------------------------------------------

// RecordEvent is fired for CRUD operations on records.
type RecordEvent struct {
	BaseEvent
	App            interface{}
	Record         map[string]any
	CollectionID   string
	CollectionName string
	RecordID       string
	Type           ModelEventType
}

// RecordErrorEvent is fired when a record operation fails.
type RecordErrorEvent struct {
	BaseEvent
	App          interface{}
	Record       map[string]any
	CollectionID string
	RecordID     string
	Type         ModelEventType
	Error        error
}

// -------------------------------------------------------------------
// Auth events
// -------------------------------------------------------------------

// AuthEvent is fired for authentication operations.
type AuthEvent struct {
	BaseEvent
	App          interface{}
	UserID       string
	Provider     string // "password", "oauth", "otp", "magiclink"
	Token        string
	RefreshToken string
}

// -------------------------------------------------------------------
// Realtime events
// -------------------------------------------------------------------

// RealtimeConnectEvent is fired when a client connects to the realtime service.
type RealtimeConnectEvent struct {
	BaseEvent
	App      interface{}
	ClientID string
}

// RealtimeMessageEvent is fired when a realtime message is received.
type RealtimeMessageEvent struct {
	BaseEvent
	App      interface{}
	ClientID string
	Channel  string
	Event    string
	Payload  []byte
}

// RealtimeSubscribeEvent is fired when a client subscribes to a channel.
type RealtimeSubscribeEvent struct {
	BaseEvent
	App      interface{}
	ClientID string
	Channel  string
}

// -------------------------------------------------------------------
// File events
// -------------------------------------------------------------------

// FileUploadEvent is fired when a file is uploaded.
type FileUploadEvent struct {
	BaseEvent
	App      interface{}
	Filename string
	Size     int64
	MimeType string
	Path     string
}

// FileDeleteEvent is fired when a file is deleted.
type FileDeleteEvent struct {
	BaseEvent
	App      interface{}
	Filename string
	Path     string
}

// -------------------------------------------------------------------
// Certificate events
// -------------------------------------------------------------------

// CertIssueEvent is fired when a certificate is issued.
type CertIssueEvent struct {
	BaseEvent
	App      interface{}
	Domain   string
	NotAfter time.Time
}

// CertRenewalEvent is fired when a certificate is renewed.
type CertRenewalEvent struct {
	BaseEvent
	App      interface{}
	Domain   string
	NotAfter time.Time
}

// -------------------------------------------------------------------
// Request events
// -------------------------------------------------------------------

// HTTPRequestInfo is a normalized request snapshot suitable for hooks.
type HTTPRequestInfo struct {
	Method       string            `json:"method"`
	Path         string            `json:"path"`
	Query        map[string]string `json:"query,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	Body         map[string]any    `json:"body,omitempty"`
	IsAdmin      bool              `json:"is_admin"`
	IsRecordAuth bool              `json:"is_record_auth"`
	AdminID      string            `json:"admin_id,omitempty"`
	RecordID     string            `json:"record_id,omitempty"`
	CollectionID string            `json:"collection_id,omitempty"`
	TenantID     string            `json:"tenant_id,omitempty"`
	Role         string            `json:"role,omitempty"`
	Email        string            `json:"email,omitempty"`
	Verified     bool              `json:"verified,omitempty"`
}

// CollectionsImportRequestEvent is fired when collection snapshots are imported.
type CollectionsImportRequestEvent struct {
	BaseEvent
	App             interface{}
	Request         *http.Request
	Info            *HTTPRequestInfo
	CollectionsData []map[string]any
	DeleteMissing   bool
}

// SettingsListRequestEvent is fired before settings are returned.
type SettingsListRequestEvent struct {
	BaseEvent
	App     interface{}
	Request *http.Request
	Info    *HTTPRequestInfo
}

// SettingsUpdateRequestEvent is fired when settings are updated.
type SettingsUpdateRequestEvent struct {
	BaseEvent
	App      interface{}
	Request  *http.Request
	Info     *HTTPRequestInfo
	Settings any
}

// BatchRequestEvent is fired before a /batch request is executed.
type BatchRequestEvent struct {
	BaseEvent
	App           interface{}
	Request       *http.Request
	Info          *HTTPRequestInfo
	RequestsCount int
	Transactional bool
}

// RecordListRequestEvent is fired before a record list query is executed.
type RecordListRequestEvent struct {
	BaseEvent
	App            interface{}
	Request        *http.Request
	Info           *HTTPRequestInfo
	CollectionID   string
	CollectionName string
	Filter         string
	Sort           string
	Expand         string
	Fields         string
	Page           int
	PerPage        int
}

// RecordViewRequestEvent is fired before a single record is fetched.
type RecordViewRequestEvent struct {
	BaseEvent
	App            interface{}
	Request        *http.Request
	Info           *HTTPRequestInfo
	CollectionID   string
	CollectionName string
	RecordID       string
	Expand         string
	Fields         string
}

// RecordCreateRequestEvent is fired before a record is created.
type RecordCreateRequestEvent struct {
	BaseEvent
	App            interface{}
	Request        *http.Request
	Info           *HTTPRequestInfo
	CollectionID   string
	CollectionName string
	Data           map[string]any
}

// RecordUpdateRequestEvent is fired before a record is updated.
type RecordUpdateRequestEvent struct {
	BaseEvent
	App            interface{}
	Request        *http.Request
	Info           *HTTPRequestInfo
	CollectionID   string
	CollectionName string
	RecordID       string
	Data           map[string]any
}

// RecordDeleteRequestEvent is fired before a record is deleted.
type RecordDeleteRequestEvent struct {
	BaseEvent
	App            interface{}
	Request        *http.Request
	Info           *HTTPRequestInfo
	CollectionID   string
	CollectionName string
	RecordID       string
}

// AdminUserRequestEvent is fired for admin user management requests.
type AdminUserRequestEvent struct {
	BaseEvent
	App           interface{}
	Request       *http.Request
	Info          *HTTPRequestInfo
	Action        string
	TargetAdminID string
	Data          map[string]any
}

// CollectionRequestEvent is fired for collection management requests.
type CollectionRequestEvent struct {
	BaseEvent
	App            interface{}
	Request        *http.Request
	Info           *HTTPRequestInfo
	Action         string
	CollectionID   string
	CollectionName string
	Data           map[string]any
}

// AdminAuthRequestEvent is fired for admin auth request flows.
type AdminAuthRequestEvent struct {
	BaseEvent
	App          interface{}
	Request      *http.Request
	Info         *HTTPRequestInfo
	Action       string
	Provider     string
	Email        string
	NewEmail     string
	Token        string
	RefreshToken string
	OTPID        string
	Code         string
}

// RecordAuthRequestEvent is fired for auth-collection request flows.
type RecordAuthRequestEvent struct {
	BaseEvent
	App            interface{}
	Request        *http.Request
	Info           *HTTPRequestInfo
	Action         string
	CollectionID   string
	CollectionName string
	RecordID       string
	Provider       string
	Identity       string
	Email          string
	NewEmail       string
	Token          string
	RefreshToken   string
	OTPID          string
	Code           string
}

// RealtimeRequestEvent is fired before a realtime HTTP request is handled.
type RealtimeRequestEvent struct {
	BaseEvent
	App       interface{}
	Request   *http.Request
	Info      *HTTPRequestInfo
	Action    string
	Transport string
}

// FileDownloadRequestEvent is fired before a file download is served.
type FileDownloadRequestEvent struct {
	BaseEvent
	App            interface{}
	Request        *http.Request
	Info           *HTTPRequestInfo
	CollectionName string
	RecordID       string
	Filename       string
	Path           string
	Thumb          string
}

// FileUploadRequestEvent is fired before a file upload is processed.
type FileUploadRequestEvent struct {
	BaseEvent
	App            interface{}
	Request        *http.Request
	Info           *HTTPRequestInfo
	CollectionName string
	RecordID       string
	Filename       string
	Size           int64
}

// FileDeleteRequestEvent is fired before a file delete is processed.
type FileDeleteRequestEvent struct {
	BaseEvent
	App            interface{}
	Request        *http.Request
	Info           *HTTPRequestInfo
	CollectionName string
	RecordID       string
	Filename       string
	Path           string
}

// BackupListRequestEvent is fired before backups are listed.
type BackupListRequestEvent struct {
	BaseEvent
	App     interface{}
	Request *http.Request
	Info    *HTTPRequestInfo
}

// BackupCreateRequestEvent is fired before a backup is created.
type BackupCreateRequestEvent struct {
	BaseEvent
	App          interface{}
	Request      *http.Request
	Info         *HTTPRequestInfo
	Name         string
	IncludeFiles bool
}

// BackupRestoreRequestEvent is fired before a backup restore starts.
type BackupRestoreRequestEvent struct {
	BaseEvent
	App        interface{}
	Request    *http.Request
	Info       *HTTPRequestInfo
	BackupID   string
	BackupName string
}

// BackupDeleteRequestEvent is fired before a backup is deleted.
type BackupDeleteRequestEvent struct {
	BaseEvent
	App        interface{}
	Request    *http.Request
	Info       *HTTPRequestInfo
	BackupName string
}

// BackupDownloadRequestEvent is fired before a backup is downloaded.
type BackupDownloadRequestEvent struct {
	BaseEvent
	App        interface{}
	Request    *http.Request
	Info       *HTTPRequestInfo
	BackupName string
	BackupPath string
}

// LogsListRequestEvent is fired before audit logs are listed.
type LogsListRequestEvent struct {
	BaseEvent
	App      interface{}
	Request  *http.Request
	Info     *HTTPRequestInfo
	Action   string
	Resource string
	DateFrom string
	DateTo   string
	Page     int
	PerPage  int
}

// APIKeyRequestEvent is fired for API key request flows.
type APIKeyRequestEvent struct {
	BaseEvent
	App      interface{}
	Request  *http.Request
	Info     *HTTPRequestInfo
	Action   string
	APIKeyID string
	Name     string
}

// SearchRequestEvent is fired before a full-text search query is executed.
type SearchRequestEvent struct {
	BaseEvent
	App            interface{}
	Request        *http.Request
	Info           *HTTPRequestInfo
	CollectionName string
	Query          string
	Language       string
	Page           int
	PerPage        int
	Highlight      bool
	Rank           bool
}

// FTSIndexRequestEvent is fired for FTS index create/remove requests.
type FTSIndexRequestEvent struct {
	BaseEvent
	App            interface{}
	Request        *http.Request
	Info           *HTTPRequestInfo
	Action         string
	CollectionName string
	Fields         []string
	Language       string
	Weight         string
}

// JobRequestEvent is fired for cron job management requests.
type JobRequestEvent struct {
	BaseEvent
	App      interface{}
	Request  *http.Request
	Info     *HTTPRequestInfo
	Action   string
	JobID    string
	Name     string
	CronExpr string
	Handler  string
	Data     map[string]any
}

// JSPluginRequestEvent is fired for JS plugin management requests.
type JSPluginRequestEvent struct {
	BaseEvent
	App        interface{}
	Request    *http.Request
	Info       *HTTPRequestInfo
	Action     string
	PluginID   string
	Name       string
	Script     string
	Expression string
	Priority   int
}

// -------------------------------------------------------------------
// Collection events
// -------------------------------------------------------------------

// CollectionEvent is fired when a collection is modified.
type CollectionEvent struct {
	BaseEvent
	App            interface{}
	CollectionID   string
	CollectionName string
	Type           ModelEventType
}

func (e *ModelEvent) EventTags() []string {
	tags := []string{}
	if e.TableName != "" {
		tags = append(tags, e.TableName)
	}
	return tags
}

func (e *RecordEvent) EventTags() []string {
	tags := []string{}
	if e.CollectionID != "" {
		tags = append(tags, e.CollectionID)
	}
	if e.CollectionName != "" {
		tags = append(tags, e.CollectionName)
	}
	if e.RecordID != "" {
		tags = append(tags, e.RecordID)
	}
	return tags
}

func (e *CollectionEvent) EventTags() []string {
	tags := []string{}
	if e.CollectionID != "" {
		tags = append(tags, e.CollectionID)
	}
	if e.CollectionName != "" {
		tags = append(tags, e.CollectionName)
	}
	return tags
}

func (e *CollectionsImportRequestEvent) EventTags() []string {
	return []string{"collections:import"}
}

func (e *SettingsListRequestEvent) EventTags() []string {
	return []string{"settings:list"}
}

func (e *SettingsUpdateRequestEvent) EventTags() []string {
	return []string{"settings:update"}
}

func (e *BatchRequestEvent) EventTags() []string {
	tags := []string{"batch"}
	if e.Transactional {
		tags = append(tags, "transactional")
	}
	return tags
}

func (e *RecordListRequestEvent) EventTags() []string {
	return requestCollectionTags("records:list", e.CollectionID, e.CollectionName, "")
}

func (e *RecordViewRequestEvent) EventTags() []string {
	return requestCollectionTags("records:view", e.CollectionID, e.CollectionName, e.RecordID)
}

func (e *RecordCreateRequestEvent) EventTags() []string {
	return requestCollectionTags("records:create", e.CollectionID, e.CollectionName, "")
}

func (e *RecordUpdateRequestEvent) EventTags() []string {
	return requestCollectionTags("records:update", e.CollectionID, e.CollectionName, e.RecordID)
}

func (e *RecordDeleteRequestEvent) EventTags() []string {
	return requestCollectionTags("records:delete", e.CollectionID, e.CollectionName, e.RecordID)
}

func (e *AdminUserRequestEvent) EventTags() []string {
	return uniqueNonEmpty([]string{"admins:" + e.Action, e.TargetAdminID})
}

func (e *CollectionRequestEvent) EventTags() []string {
	return requestCollectionTags("collections:"+e.Action, e.CollectionID, e.CollectionName, "")
}

func (e *AdminAuthRequestEvent) EventTags() []string {
	tags := []string{"auth:" + e.Action}
	if e.Provider != "" {
		tags = append(tags, e.Provider)
	}
	return uniqueNonEmpty(tags)
}

func (e *RecordAuthRequestEvent) EventTags() []string {
	tags := []string{"record_auth:" + e.Action}
	if e.CollectionID != "" {
		tags = append(tags, e.CollectionID)
	}
	if e.CollectionName != "" {
		tags = append(tags, e.CollectionName)
	}
	if e.RecordID != "" {
		tags = append(tags, e.RecordID)
	}
	if e.Provider != "" {
		tags = append(tags, e.Provider)
	}
	return uniqueNonEmpty(tags)
}

func (e *RealtimeRequestEvent) EventTags() []string {
	return uniqueNonEmpty([]string{"realtime:" + e.Action, e.Transport})
}

func (e *FileDownloadRequestEvent) EventTags() []string {
	return requestCollectionTags("files:download", "", e.CollectionName, e.RecordID)
}

func (e *FileUploadRequestEvent) EventTags() []string {
	return requestCollectionTags("files:upload", "", e.CollectionName, e.RecordID)
}

func (e *FileDeleteRequestEvent) EventTags() []string {
	return requestCollectionTags("files:delete", "", e.CollectionName, e.RecordID)
}

func (e *BackupListRequestEvent) EventTags() []string {
	return []string{"backups:list"}
}

func (e *BackupCreateRequestEvent) EventTags() []string {
	return uniqueNonEmpty([]string{"backups:create", e.Name})
}

func (e *BackupRestoreRequestEvent) EventTags() []string {
	return uniqueNonEmpty([]string{"backups:restore", e.BackupID, e.BackupName})
}

func (e *BackupDeleteRequestEvent) EventTags() []string {
	return uniqueNonEmpty([]string{"backups:delete", e.BackupName})
}

func (e *BackupDownloadRequestEvent) EventTags() []string {
	return uniqueNonEmpty([]string{"backups:download", e.BackupName})
}

func (e *LogsListRequestEvent) EventTags() []string {
	return []string{"logs:list"}
}

func (e *APIKeyRequestEvent) EventTags() []string {
	return uniqueNonEmpty([]string{"api_keys:" + e.Action, e.APIKeyID})
}

func (e *SearchRequestEvent) EventTags() []string {
	return uniqueNonEmpty([]string{"search", e.CollectionName})
}

func (e *FTSIndexRequestEvent) EventTags() []string {
	return uniqueNonEmpty([]string{"fts:" + e.Action, e.CollectionName})
}

func (e *JobRequestEvent) EventTags() []string {
	return uniqueNonEmpty([]string{"jobs:" + e.Action, e.JobID, e.Name})
}

func (e *JSPluginRequestEvent) EventTags() []string {
	return uniqueNonEmpty([]string{"jsplugins:" + e.Action, e.PluginID, e.Name})
}

func requestCollectionTags(action, collectionID, collectionName, recordID string) []string {
	tags := []string{action}
	if collectionID != "" {
		tags = append(tags, collectionID)
	}
	if collectionName != "" {
		tags = append(tags, collectionName)
	}
	if recordID != "" {
		tags = append(tags, recordID)
	}
	return uniqueNonEmpty(tags)
}

func uniqueNonEmpty(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
