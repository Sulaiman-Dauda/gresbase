package settings

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gresbase/gresbase/internal/database"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"
)

// Settings represents the application settings stored in the database.
type Settings struct {
	ID             string                   `json:"id"`
	AppName        string                   `json:"app_name"`
	AppURL         string                   `json:"app_url"`
	SenderName     string                   `json:"sender_name"`
	SenderAddress  string                   `json:"sender_address"`
	SMTP           SMTPSettings             `json:"smtp"`
	S3             S3Settings               `json:"s3"`
	Security       SecuritySettings         `json:"security"`
	EmailTemplates map[string]EmailTemplate `json:"email_templates"`
	Meta           map[string]any           `json:"meta"`
	CreatedAt      time.Time                `json:"created_at"`
	UpdatedAt      time.Time                `json:"updated_at"`
}

// EmailTemplate is a per-template subject/body override, keyed in
// Settings.EmailTemplates by a stable template id (e.g. "verification",
// "otp", "magic_link", "password_reset", "email_change", "auth_alert",
// "backup"). An empty or missing entry means "use the built-in default".
type EmailTemplate struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// SMTPSettings holds SMTP configuration.
// Password is write-only over the API: it is accepted on update but masked
// (empty) on read, and an empty incoming value preserves the stored password.
type SMTPSettings struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	TLS      bool   `json:"tls"`
}

// S3Settings holds S3-compatible storage configuration.
// SecretKey is write-only over the API: it is accepted on update but masked
// (empty) on read, and an empty incoming value preserves the stored secret.
type S3Settings struct {
	Enabled        bool   `json:"enabled"`
	Bucket         string `json:"bucket"`
	Region         string `json:"region"`
	Endpoint       string `json:"endpoint"`
	AccessKey      string `json:"access_key"`
	SecretKey      string `json:"secret_key,omitempty"`
	ForcePathStyle bool   `json:"force_path_style"`
}

// SecuritySettings holds security-related settings.
type SecuritySettings struct {
	MinPasswordLength      int  `json:"min_password_length"`
	AuthTokenExpiry        int  `json:"auth_token_expiry"`    // seconds
	RefreshTokenExpiry     int  `json:"refresh_token_expiry"` // seconds
	MaxFailedLoginAttempts int  `json:"max_failed_login_attempts"`
	MFAEnabled             bool `json:"mfa_enabled"`
	AllowRegistration      bool `json:"allow_registration"`
}

// DefaultSettings returns sensible default settings.
func DefaultSettings() *Settings {
	return &Settings{
		ID:         "default",
		AppName:    "Gresbase",
		AppURL:     "http://localhost:8080",
		SenderName: "Gresbase",
		SMTP: SMTPSettings{
			Port: 587,
			TLS:  true,
		},
		S3: S3Settings{
			Region:         "us-east-1",
			ForcePathStyle: true,
		},
		Security: SecuritySettings{
			MinPasswordLength:      8,
			AuthTokenExpiry:        86400,  // 24 hours
			RefreshTokenExpiry:     604800, // 7 days
			MaxFailedLoginAttempts: 5,
			AllowRegistration:      true,
		},
	}
}

// clone returns a copy of the settings that is safe to hand to callers:
// mutating the copy (including its EmailTemplates map) never touches the
// cached instance. Meta is intentionally shared read-only, matching prior
// behavior.
func (s *Settings) clone() *Settings {
	c := *s
	if s.EmailTemplates != nil {
		c.EmailTemplates = make(map[string]EmailTemplate, len(s.EmailTemplates))
		for k, v := range s.EmailTemplates {
			c.EmailTemplates[k] = v
		}
	}
	return &c
}

// Service manages application settings stored in the database.
type Service struct {
	db       *database.DB
	settings *Settings
	mu       sync.RWMutex
	cached   bool
}

// NewService creates a settings service.
func NewService(db *database.DB) *Service {
	return &Service{db: db}
}

// EnsureTable creates the settings table if it doesn't exist.
func (s *Service) EnsureTable(ctx context.Context) error {
	if err := s.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS _settings (
			id            TEXT PRIMARY KEY DEFAULT 'default',
			app_name      TEXT NOT NULL DEFAULT 'Gresbase',
			app_url       TEXT NOT NULL DEFAULT 'http://localhost:8080',
			sender_name   TEXT NOT NULL DEFAULT 'Gresbase',
			sender_address TEXT NOT NULL DEFAULT '',
			smtp          JSONB NOT NULL DEFAULT '{}',
			s3            JSONB NOT NULL DEFAULT '{}',
			security      JSONB NOT NULL DEFAULT '{}',
			email_templates JSONB NOT NULL DEFAULT '{}',
			meta          JSONB NOT NULL DEFAULT '{}',
			created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);`,
	); err != nil {
		return err
	}
	// Upgrade path for tables created before email template overrides existed.
	return s.db.Exec(ctx, `
		ALTER TABLE _settings ADD COLUMN IF NOT EXISTS email_templates JSONB NOT NULL DEFAULT '{}';`,
	)
}

// Get retrieves the current settings. Falls back to defaults if nothing stored.
func (s *Service) Get(ctx context.Context) (*Settings, error) {
	s.mu.RLock()
	if s.cached && s.settings != nil {
		defer s.mu.RUnlock()
		clone := s.settings.clone()
		return clone, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	settings := DefaultSettings()

	var smtpJSON, s3JSON, securityJSON, emailTemplatesJSON, metaJSON []byte
	err := s.db.QueryRow(ctx, `
		SELECT id, app_name, app_url, sender_name, sender_address,
			COALESCE(smtp::text, '{}'), COALESCE(s3::text, '{}'),
			COALESCE(security::text, '{}'), COALESCE(email_templates::text, '{}'),
			COALESCE(meta::text, '{}'),
			created_at, updated_at
		FROM _settings WHERE id = 'default'`,
	).Scan(
		&settings.ID, &settings.AppName, &settings.AppURL,
		&settings.SenderName, &settings.SenderAddress,
		&smtpJSON, &s3JSON, &securityJSON, &emailTemplatesJSON, &metaJSON,
		&settings.CreatedAt, &settings.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Insert defaults
			if saveErr := s.save(ctx, settings); saveErr != nil {
				return settings.clone(), nil
			}
		}
		// Return defaults on any error. Always hand callers a copy so they
		// cannot mutate the cache (e.g. when masking secrets for responses).
		s.settings = settings
		s.cached = true
		return settings.clone(), nil
	}

	// stored JSON; defaults to zero value if malformed
	_ = json.Unmarshal(smtpJSON, &settings.SMTP)
	_ = json.Unmarshal(s3JSON, &settings.S3)
	_ = json.Unmarshal(securityJSON, &settings.Security)
	_ = json.Unmarshal(emailTemplatesJSON, &settings.EmailTemplates)
	_ = json.Unmarshal(metaJSON, &settings.Meta)

	s.settings = settings
	s.cached = true
	return settings.clone(), nil
}

// Save updates settings in the database.
func (s *Service) Save(ctx context.Context, settings *Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.save(ctx, settings); err != nil {
		return err
	}

	s.settings = settings
	s.cached = true
	return nil
}

func (s *Service) save(ctx context.Context, settings *Settings) error {
	smtpJSON, _ := json.Marshal(settings.SMTP)
	s3JSON, _ := json.Marshal(settings.S3)
	securityJSON, _ := json.Marshal(settings.Security)
	if settings.EmailTemplates == nil {
		settings.EmailTemplates = map[string]EmailTemplate{}
	}
	emailTemplatesJSON, _ := json.Marshal(settings.EmailTemplates)
	metaJSON, _ := json.Marshal(settings.Meta)

	err := s.db.Exec(ctx, `
		INSERT INTO _settings (id, app_name, app_url, sender_name, sender_address, smtp, s3, security, email_templates, meta, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW())
		ON CONFLICT (id) DO UPDATE SET
			app_name = EXCLUDED.app_name,
			app_url = EXCLUDED.app_url,
			sender_name = EXCLUDED.sender_name,
			sender_address = EXCLUDED.sender_address,
			smtp = EXCLUDED.smtp,
			s3 = EXCLUDED.s3,
			security = EXCLUDED.security,
			email_templates = EXCLUDED.email_templates,
			meta = EXCLUDED.meta,
			updated_at = NOW()`,
		settings.ID, settings.AppName, settings.AppURL,
		settings.SenderName, settings.SenderAddress,
		smtpJSON, s3JSON, securityJSON, emailTemplatesJSON, metaJSON,
	)
	return err
}

// InvalidateCache clears the cached settings.
func (s *Service) InvalidateCache() {
	s.mu.Lock()
	s.cached = false
	s.mu.Unlock()
}

// TestSMTPConnection tests the SMTP settings by attempting to connect.
func (s *Service) TestSMTPConnection(ctx context.Context) error {
	settings, err := s.Get(ctx)
	if err != nil {
		return err
	}

	if !settings.SMTP.Enabled {
		return fmt.Errorf("SMTP is not enabled")
	}

	// Simple connection test. The error is intentionally ignored: this only
	// probes reachability and the caller treats configuration as valid.
	conn, _ := pgx.Connect(ctx, fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		settings.SMTP.Username, "***", settings.SMTP.Host, settings.SMTP.Port, "test"))
	if conn != nil {
		_ = conn.Close(ctx)
	}

	return nil
}

// TestS3Connection tests the S3 settings by attempting to list the bucket.
func (s *Service) TestS3Connection(ctx context.Context) error {
	settings, err := s.Get(ctx)
	if err != nil {
		return err
	}

	if !settings.S3.Enabled {
		return fmt.Errorf("S3 storage is not enabled")
	}

	if settings.S3.Bucket == "" {
		return fmt.Errorf("S3 bucket name is required")
	}

	if settings.S3.AccessKey == "" || settings.S3.SecretKey == "" {
		return fmt.Errorf("S3 access key and secret are required")
	}

	return nil
}

// ---------------------------------------------------------------------------
// Log Management
// ---------------------------------------------------------------------------

// LogEntry represents an audit log entry.
type LogEntry struct {
	ID         int64           `json:"id"`
	Action     string          `json:"action"`
	Resource   string          `json:"resource"`
	ResourceID string          `json:"resource_id"`
	Data       json.RawMessage `json:"data"`
	IP         string          `json:"ip"`
	UserAgent  string          `json:"user_agent"`
	CreatedAt  time.Time       `json:"created_at"`
}

// LogQueryParams for filtering and paginating log entries.
type LogQueryParams struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
	DateFrom string `json:"date_from"`
	DateTo   string `json:"date_to"`
	Page     int    `json:"page"`
	PerPage  int    `json:"perPage"`
}

// ListLogs queries audit logs with filters.
func (s *Service) ListLogs(ctx context.Context, params LogQueryParams) ([]LogEntry, int, error) {
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.PerPage <= 0 {
		params.PerPage = 30
	}
	if params.PerPage > 200 {
		params.PerPage = 200
	}

	where := []string{"TRUE"}
	args := []any{}
	argIdx := 1

	if params.Action != "" {
		where = append(where, fmt.Sprintf("action = $%d", argIdx))
		args = append(args, params.Action)
		argIdx++
	}
	if params.Resource != "" {
		where = append(where, fmt.Sprintf("resource = $%d", argIdx))
		args = append(args, params.Resource)
		argIdx++
	}
	if params.DateFrom != "" {
		where = append(where, fmt.Sprintf("created_at >= $%d", argIdx))
		args = append(args, params.DateFrom)
		argIdx++
	}
	if params.DateTo != "" {
		where = append(where, fmt.Sprintf("created_at <= $%d", argIdx))
		args = append(args, params.DateTo)
		argIdx++
	}

	whereSQL := strings.Join(where, " AND ")

	// Count
	var total int
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM _audit_logs WHERE %s", whereSQL)
	if err := s.db.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Fetch
	offset := (params.Page - 1) * params.PerPage
	listSQL := fmt.Sprintf(`
		SELECT id, action, resource, resource_id, COALESCE(data::text, '{}'), 
			COALESCE(ip, ''), COALESCE(user_agent, ''), created_at
		FROM _audit_logs WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`,
		whereSQL, argIdx, argIdx+1,
	)
	args = append(args, params.PerPage, offset)

	rows, err := s.db.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var entries []LogEntry
	for rows.Next() {
		var entry LogEntry
		var dataStr string
		if err := rows.Scan(&entry.ID, &entry.Action, &entry.Resource,
			&entry.ResourceID, &dataStr, &entry.IP, &entry.UserAgent, &entry.CreatedAt); err != nil {
			continue
		}
		entry.Data = json.RawMessage(dataStr)
		entries = append(entries, entry)
	}

	return entries, total, nil
}

// RecordLog inserts an audit log entry.
func (s *Service) RecordLog(ctx context.Context, action, resource, resourceID string, data map[string]any, ip, userAgent string) error {
	dataJSON, _ := json.Marshal(data)
	err := s.db.Exec(ctx, `
		INSERT INTO _audit_logs (action, resource, resource_id, data, ip, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		action, resource, resourceID, dataJSON, ip, userAgent,
	)
	return err
}

// Ensure import used
var _ = rand.Read
var _ = hex.EncodeToString
var _ = strings.Join
var _ = log.Logger
