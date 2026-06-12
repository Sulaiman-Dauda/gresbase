package forms

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gresbase/gresbase/internal/collection"
	"github.com/gresbase/gresbase/internal/mailer"
	appsettings "github.com/gresbase/gresbase/internal/settings"
)

// Errors represents structured form validation errors.
type Errors map[string]string

func (e Errors) Error() string {
	if len(e) == 0 {
		return "validation failed"
	}

	keys := make([]string, 0, len(e))
	for k := range e {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s: %s", k, e[k]))
	}

	return strings.Join(parts, ", ")
}

func (e Errors) Add(field, message string) {
	if field == "" || message == "" {
		return
	}
	if _, exists := e[field]; !exists {
		e[field] = message
	}
}

func (e Errors) HasAny() bool {
	return len(e) > 0
}

// Validatable is implemented by forms that can validate themselves.
type Validatable interface {
	Validate() error
}

// LoginForm validates admin login payloads.
type LoginForm struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (f *LoginForm) Validate() error {
	errs := Errors{}
	if strings.TrimSpace(f.Email) == "" {
		errs.Add("email", "email is required")
	}
	if strings.TrimSpace(f.Password) == "" {
		errs.Add("password", "password is required")
	}
	if errs.HasAny() {
		return errs
	}
	return nil
}

// SetupForm validates the first-admin setup payload.
type SetupForm struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (f *SetupForm) Validate() error {
	errs := Errors{}
	if strings.TrimSpace(f.Email) == "" {
		errs.Add("email", "email is required")
	}
	if len(strings.TrimSpace(f.Password)) < 8 {
		errs.Add("password", "password must be at least 8 characters")
	}
	if errs.HasAny() {
		return errs
	}
	return nil
}

// RegisterForm validates admin registration payloads.
type RegisterForm struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (f *RegisterForm) Validate() error {
	setup := &SetupForm{Email: f.Email, Password: f.Password}
	return setup.Validate()
}

// RefreshTokenForm validates refresh token payloads.
type RefreshTokenForm struct {
	RefreshToken string `json:"refreshToken"`
}

func (f *RefreshTokenForm) Validate() error {
	if strings.TrimSpace(f.RefreshToken) == "" {
		return Errors{"refreshToken": "refreshToken is required"}
	}
	return nil
}

// OTPRequestForm validates OTP request payloads.
type OTPRequestForm struct {
	Email string `json:"email"`
}

func (f *OTPRequestForm) Validate() error {
	if strings.TrimSpace(f.Email) == "" {
		return Errors{"email": "email is required"}
	}
	return nil
}

// OTPVerifyForm validates OTP verification payloads.
type OTPVerifyForm struct {
	OTPID string `json:"otpId"`
	Code  string `json:"code"`
}

func (f *OTPVerifyForm) Validate() error {
	errs := Errors{}
	if strings.TrimSpace(f.OTPID) == "" {
		errs.Add("otpId", "otpId is required")
	}
	if strings.TrimSpace(f.Code) == "" {
		errs.Add("code", "code is required")
	}
	if errs.HasAny() {
		return errs
	}
	return nil
}

// AdminUpsertForm validates admin create/update payloads.
type AdminUpsertForm struct {
	Email    string `json:"email"`
	Password string `json:"password,omitempty"`
	Role     string `json:"role,omitempty"`
	Avatar   string `json:"avatar,omitempty"`
}

func (f *AdminUpsertForm) ValidateCreate() error {
	errs := Errors{}
	if strings.TrimSpace(f.Email) == "" {
		errs.Add("email", "email is required")
	}
	if len(strings.TrimSpace(f.Password)) < 8 {
		errs.Add("password", "password must be at least 8 characters")
	}
	if strings.TrimSpace(f.Role) == "" {
		errs.Add("role", "role is required")
	}
	if errs.HasAny() {
		return errs
	}
	return nil
}

func (f *AdminUpsertForm) ValidateUpdate() error {
	errs := Errors{}
	if strings.TrimSpace(f.Email) == "" && strings.TrimSpace(f.Role) == "" && strings.TrimSpace(f.Avatar) == "" && strings.TrimSpace(f.Password) == "" {
		errs.Add("body", "at least one field must be provided")
	}
	if f.Password != "" && len(strings.TrimSpace(f.Password)) < 8 {
		errs.Add("password", "password must be at least 8 characters")
	}
	if errs.HasAny() {
		return errs
	}
	return nil
}

// CollectionUpsertForm validates collection create/update payloads.
type CollectionUpsertForm struct {
	collection.Collection
}

func (f *CollectionUpsertForm) Normalize() {
	if f.Type == "" {
		f.Type = collection.TypeBase
	}
	if f.Options == nil {
		f.Options = map[string]any{}
	}
	if f.Indexes == nil {
		f.Indexes = []string{}
	}
	if f.Schema == nil {
		f.Schema = []collection.SchemaField{}
	}
}

func (f *CollectionUpsertForm) Validate(svc *collection.Service) error {
	f.Normalize()
	if svc == nil {
		return fmt.Errorf("collection service is required")
	}
	return svc.ValidateCollectionDefinition(&f.Collection)
}

// CollectionsImportForm validates bulk collection imports.
type CollectionsImportForm struct {
	Collections   []map[string]any `json:"collections"`
	DeleteMissing bool             `json:"deleteMissing"`
}

func (f *CollectionsImportForm) Validate() error {
	if len(f.Collections) == 0 {
		return Errors{"collections": "collections are required"}
	}
	return nil
}

// EmailForm validates email-only payloads.
type EmailForm struct {
	Email string `json:"email"`
}

func (f *EmailForm) Validate() error {
	if strings.TrimSpace(f.Email) == "" {
		return Errors{"email": "email is required"}
	}
	return nil
}

// TokenForm validates token-only payloads.
type TokenForm struct {
	Token string `json:"token"`
}

func (f *TokenForm) Validate() error {
	if strings.TrimSpace(f.Token) == "" {
		return Errors{"token": "token is required"}
	}
	return nil
}

// TokenPasswordForm validates token + password reset payloads.
type TokenPasswordForm struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (f *TokenPasswordForm) Validate() error {
	errs := Errors{}
	if strings.TrimSpace(f.Token) == "" {
		errs.Add("token", "token is required")
	}
	if len(strings.TrimSpace(f.Password)) < 8 {
		errs.Add("password", "password must be at least 8 characters")
	}
	if errs.HasAny() {
		return errs
	}
	return nil
}

// NewEmailForm validates email change payloads.
type NewEmailForm struct {
	NewEmail string `json:"newEmail"`
}

func (f *NewEmailForm) Validate() error {
	if strings.TrimSpace(f.NewEmail) == "" {
		return Errors{"newEmail": "newEmail is required"}
	}
	return nil
}

// APIKeyCreateForm validates API key create payloads.
type APIKeyCreateForm struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions,omitempty"`
}

func (f *APIKeyCreateForm) Normalize() {
	if strings.TrimSpace(f.Name) == "" {
		f.Name = "API Key"
	}

	seen := make(map[string]struct{}, len(f.Permissions))
	normalized := make([]string, 0, len(f.Permissions))
	for _, permission := range f.Permissions {
		permission = strings.ToLower(strings.TrimSpace(permission))
		permission = strings.Trim(permission, ".")
		if permission == "" {
			continue
		}
		if permission == "all" {
			permission = "*"
		}
		if _, ok := seen[permission]; ok {
			continue
		}
		seen[permission] = struct{}{}
		normalized = append(normalized, permission)
	}
	sort.Strings(normalized)
	f.Permissions = normalized
}

func (f *APIKeyCreateForm) Validate() error {
	f.Normalize()
	errs := Errors{}
	if len(f.Name) > 120 {
		errs.Add("name", "name must be at most 120 characters")
	}
	if len(f.Permissions) > 64 {
		errs.Add("permissions", "permissions must contain at most 64 entries")
	}
	for _, permission := range f.Permissions {
		if !isValidAPIKeyPermission(permission) {
			errs.Add("permissions", "permissions must use scope syntax like collections.read, records.*, or *")
			break
		}
	}
	if errs.HasAny() {
		return errs
	}
	return nil
}

func isValidAPIKeyPermission(permission string) bool {
	if permission == "*" {
		return true
	}
	if permission == "" || strings.Contains(permission, "..") || strings.ContainsAny(permission, " /\\,") {
		return false
	}
	if strings.Count(permission, "*") > 1 {
		return false
	}
	if strings.Contains(permission, "*") && !strings.HasSuffix(permission, ".*") {
		return false
	}
	for _, ch := range permission {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '.' || ch == '_' || ch == '*' {
			continue
		}
		return false
	}
	return true
}

// BackupCreateForm validates backup creation payloads.
type BackupCreateForm struct {
	Name         string `json:"name"`
	IncludeFiles bool   `json:"includeFiles"`
}

func (f *BackupCreateForm) Validate() error {
	if len(strings.TrimSpace(f.Name)) > 200 {
		return Errors{"name": "name must be at most 200 characters"}
	}
	return nil
}

// OAuthCallbackForm validates OAuth callback query params.
type OAuthCallbackForm struct {
	Code  string `json:"code"`
	State string `json:"state"`
}

func (f *OAuthCallbackForm) Validate() error {
	errs := Errors{}
	if strings.TrimSpace(f.Code) == "" {
		errs.Add("code", "code is required")
	}
	if strings.TrimSpace(f.State) == "" {
		errs.Add("state", "state is required")
	}
	if errs.HasAny() {
		return errs
	}
	return nil
}

// RecordAuthOAuth2Form validates PocketBase-style record OAuth2 auth payloads.
type RecordAuthOAuth2Form struct {
	Provider     string         `json:"provider"`
	Code         string         `json:"code"`
	State        string         `json:"state"`
	CodeVerifier string         `json:"codeVerifier,omitempty"`
	RedirectURL  string         `json:"redirectURL"`
	RedirectUrl  string         `json:"redirectUrl,omitempty"`
	CreateData   map[string]any `json:"createData,omitempty"`
}

func (f *RecordAuthOAuth2Form) Normalize() {
	if strings.TrimSpace(f.RedirectURL) == "" {
		f.RedirectURL = strings.TrimSpace(f.RedirectUrl)
	}
}

func (f *RecordAuthOAuth2Form) Validate() error {
	f.Normalize()
	errs := Errors{}
	if strings.TrimSpace(f.Provider) == "" {
		errs.Add("provider", "provider is required")
	}
	if strings.TrimSpace(f.Code) == "" {
		errs.Add("code", "code is required")
	}
	if strings.TrimSpace(f.State) == "" {
		errs.Add("state", "state is required")
	}
	if strings.TrimSpace(f.RedirectURL) == "" {
		errs.Add("redirectURL", "redirectURL is required")
	}
	if errs.HasAny() {
		return errs
	}
	return nil
}

// SettingsUpdateForm validates settings update payloads.
type SettingsUpdateForm struct {
	*appsettings.Settings
}

func (f *SettingsUpdateForm) Validate() error {
	if f == nil || f.Settings == nil {
		return Errors{"settings": "settings are required"}
	}
	errs := Errors{}
	if strings.TrimSpace(f.AppName) == "" {
		errs.Add("app_name", "app_name is required")
	}
	if strings.TrimSpace(f.AppURL) != "" {
		u, err := url.Parse(f.AppURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			errs.Add("app_url", "app_url must be a valid absolute URL")
		}
	}
	if f.Security.MinPasswordLength < 6 {
		errs.Add("security.min_password_length", "min password length must be at least 6")
	}
	if f.Security.AuthTokenExpiry <= 0 {
		errs.Add("security.auth_token_expiry", "auth token expiry must be positive")
	}
	if f.Security.RefreshTokenExpiry <= 0 {
		errs.Add("security.refresh_token_expiry", "refresh token expiry must be positive")
	}
	// Email template overrides must reference a known template id and parse as
	// valid Go templates — a broken override is rejected here, at save time,
	// so it can never reach the mailer.
	for id, tmpl := range f.EmailTemplates {
		if tmpl.Subject == "" && tmpl.Body == "" {
			continue // empty entry = use built-in default
		}
		if err := mailer.ValidateOverride(id, tmpl.Subject, tmpl.Body); err != nil {
			errs.Add("email_templates."+id, fmt.Sprintf("invalid template: %v", err))
		}
	}
	if errs.HasAny() {
		return errs
	}
	return nil
}

// LogsQueryForm validates audit log query parameters.
type LogsQueryForm struct {
	Action   string
	Resource string
	DateFrom string
	DateTo   string
	Page     int
	PerPage  int
}

func (f *LogsQueryForm) Normalize() {
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PerPage <= 0 {
		f.PerPage = 50
	}
	if f.PerPage > 200 {
		f.PerPage = 200
	}
}

func (f *LogsQueryForm) Validate() error {
	f.Normalize()
	errs := Errors{}
	if f.DateFrom != "" && !isValidDateOrTimestamp(f.DateFrom) {
		errs.Add("date_from", "date_from must be RFC3339 or YYYY-MM-DD")
	}
	if f.DateTo != "" && !isValidDateOrTimestamp(f.DateTo) {
		errs.Add("date_to", "date_to must be RFC3339 or YYYY-MM-DD")
	}
	if errs.HasAny() {
		return errs
	}
	return nil
}

// SearchForm validates search payloads.
type SearchForm struct {
	Query     string `json:"query"`
	Language  string `json:"language"`
	Page      int    `json:"page"`
	PerPage   int    `json:"per_page"`
	Highlight bool   `json:"highlight"`
	Rank      bool   `json:"rank"`
}

func (f *SearchForm) Normalize() {
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PerPage <= 0 {
		f.PerPage = 30
	}
	if f.PerPage > 200 {
		f.PerPage = 200
	}
}

func (f *SearchForm) Validate() error {
	f.Normalize()
	if strings.TrimSpace(f.Query) == "" {
		return Errors{"query": "query is required"}
	}
	return nil
}

// FTSIndexForm validates full-text index creation payloads.
type FTSIndexForm struct {
	Fields   []string `json:"fields"`
	Language string   `json:"language"`
	Weight   string   `json:"weight"`
}

func (f *FTSIndexForm) Validate() error {
	if len(f.Fields) == 0 {
		return Errors{"fields": "fields are required"}
	}
	return nil
}

// FileUploadForm validates file upload query parameters.
type FileUploadForm struct {
	Collection string
	RecordID   string
}

func (f *FileUploadForm) Validate() error {
	errs := Errors{}
	if strings.TrimSpace(f.Collection) == "" {
		errs.Add("collection", "collection is required")
	}
	if strings.TrimSpace(f.RecordID) == "" {
		errs.Add("record", "record is required")
	}
	if errs.HasAny() {
		return errs
	}
	return nil
}

// PromoteFilesForm validates staged file promotion requests.
type PromoteFilesForm struct {
	Collection string   `json:"collection"`
	RecordID   string   `json:"recordId"`
	Files      []string `json:"files"`
}

func (f *PromoteFilesForm) Validate() error {
	errs := Errors{}
	if strings.TrimSpace(f.Collection) == "" {
		errs.Add("collection", "collection is required")
	}
	if strings.TrimSpace(f.RecordID) == "" {
		errs.Add("recordId", "recordId is required")
	}
	if len(f.Files) == 0 {
		errs.Add("files", "files are required")
	}
	for _, file := range f.Files {
		if strings.TrimSpace(file) == "" {
			errs.Add("files", "files must not contain empty values")
			break
		}
	}
	if errs.HasAny() {
		return errs
	}
	return nil
}

// RecordAuthPasswordForm validates auth collection password login payloads.
type RecordAuthPasswordForm struct {
	Identity string `json:"identity"`
	Password string `json:"password"`
}

func (f *RecordAuthPasswordForm) Validate() error {
	errs := Errors{}
	if strings.TrimSpace(f.Identity) == "" {
		errs.Add("identity", "identity is required")
	}
	if strings.TrimSpace(f.Password) == "" {
		errs.Add("password", "password is required")
	}
	if errs.HasAny() {
		return errs
	}
	return nil
}

// BatchRequestForm validates a single API batch subrequest.
type BatchRequestForm struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

func (f *BatchRequestForm) Normalize() {
	if strings.TrimSpace(f.Method) == "" {
		f.Method = "GET"
	}
}

func (f *BatchRequestForm) Validate() error {
	f.Normalize()
	errs := Errors{}
	if strings.TrimSpace(f.URL) == "" {
		errs.Add("url", "url is required")
	}
	switch strings.ToUpper(strings.TrimSpace(f.Method)) {
	case httpMethodGet, httpMethodPost, httpMethodPut, httpMethodPatch, httpMethodDelete:
	default:
		errs.Add("method", "method must be GET, POST, PUT, PATCH, or DELETE")
	}
	if errs.HasAny() {
		return errs
	}
	return nil
}

// BatchPayloadForm validates the /batch payload.
type BatchPayloadForm struct {
	Requests []BatchRequestForm `json:"requests"`
}

func (f *BatchPayloadForm) Validate() error {
	errs := Errors{}
	if len(f.Requests) == 0 {
		errs.Add("requests", "batch must contain at least one request")
	}
	if len(f.Requests) > 100 {
		errs.Add("requests", "batch limit is 100 requests")
	}
	for i := range f.Requests {
		if err := f.Requests[i].Validate(); err != nil {
			if verr, ok := err.(Errors); ok {
				for field, message := range verr {
					errs.Add(fmt.Sprintf("requests.%d.%s", i, field), message)
				}
			} else {
				errs.Add(fmt.Sprintf("requests.%d", i), err.Error())
			}
		}
	}
	if errs.HasAny() {
		return errs
	}
	return nil
}

// BatchRecordsForm validates collection-scoped batch record operations.
type BatchRecordsForm struct {
	Creates []map[string]any          `json:"creates"`
	Updates map[string]map[string]any `json:"updates"`
	Deletes []string                  `json:"deletes"`
}

func (f *BatchRecordsForm) Validate() error {
	errs := Errors{}
	if len(f.Creates) == 0 && len(f.Updates) == 0 && len(f.Deletes) == 0 {
		errs.Add("body", "at least one of creates, updates, or deletes must be provided")
	}
	for id := range f.Updates {
		if strings.TrimSpace(id) == "" {
			errs.Add("updates", "update record ids must not be empty")
			break
		}
	}
	for _, id := range f.Deletes {
		if strings.TrimSpace(id) == "" {
			errs.Add("deletes", "delete record ids must not be empty")
			break
		}
	}
	if errs.HasAny() {
		return errs
	}
	return nil
}

// JSPluginCreateForm validates JS plugin creation payloads.
type JSPluginCreateForm struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Script   string `json:"script"`
	Priority int    `json:"priority"`
}

func (f *JSPluginCreateForm) Validate() error {
	errs := Errors{}
	if strings.TrimSpace(f.Name) == "" {
		errs.Add("name", "name is required")
	}
	if strings.TrimSpace(f.Script) == "" {
		errs.Add("script", "script is required")
	}
	if errs.HasAny() {
		return errs
	}
	return nil
}

// JSPluginExecuteForm validates JS plugin execution payloads.
type JSPluginExecuteForm struct {
	Expression string `json:"expression"`
}

func (f *JSPluginExecuteForm) Validate() error {
	if strings.TrimSpace(f.Expression) == "" {
		return Errors{"expression": "expression is required"}
	}
	return nil
}

// JobCreateForm validates cron job creation payloads.
type JobCreateForm struct {
	Name     string         `json:"name"`
	CronExpr string         `json:"cron_expr"`
	Handler  string         `json:"handler"`
	Data     map[string]any `json:"data"`
}

func (f *JobCreateForm) Validate() error {
	errs := Errors{}
	if strings.TrimSpace(f.Name) == "" {
		errs.Add("name", "name is required")
	}
	if strings.TrimSpace(f.CronExpr) == "" {
		errs.Add("cron_expr", "cron_expr is required")
	}
	if errs.HasAny() {
		return errs
	}
	return nil
}

const (
	httpMethodGet    = "GET"
	httpMethodPost   = "POST"
	httpMethodPut    = "PUT"
	httpMethodPatch  = "PATCH"
	httpMethodDelete = "DELETE"
)

func isValidDateOrTimestamp(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if _, err := time.Parse(layout, value); err == nil {
			return true
		}
	}
	return false
}
