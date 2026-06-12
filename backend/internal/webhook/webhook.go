// Package webhook implements admin-configured outbound webhooks: when record
// or collection events fire, matching endpoints receive an HMAC-SHA256-signed
// JSON POST with retries and a persisted delivery log.
package webhook

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gresbase/gresbase/internal/database"
)

// Webhook is an admin-configured endpoint subscribed to events.
type Webhook struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	URL         string            `json:"url"`
	Secret      string            `json:"secret"`
	Events      []string          `json:"events"`      // e.g. "record.create", or "*" for all
	Collections []string          `json:"collections"` // empty = all collections
	Enabled     bool              `json:"enabled"`
	Headers     map[string]string `json:"headers"` // extra static headers sent with each delivery
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// Delivery is one attempt at POSTing an event to a webhook endpoint.
type Delivery struct {
	ID         int64           `json:"id"`
	WebhookID  string          `json:"webhook_id"`
	Event      string          `json:"event"`
	Payload    json.RawMessage `json:"payload"`
	StatusCode int             `json:"status_code"`
	Attempt    int             `json:"attempt"`
	Success    bool            `json:"success"`
	Error      string          `json:"error,omitempty"`
	DurationMS int             `json:"duration_ms"`
	CreatedAt  time.Time       `json:"created_at"`
}

// Service manages webhook configuration and dispatches deliveries through a
// small in-process worker pool — no external queue required.
type Service struct {
	db         *database.DB
	client     *http.Client
	queue      chan task
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	pruneLimit int

	// onDelivery is a test hook invoked for every recorded delivery attempt.
	onDelivery func(*Delivery)
}

// NewService creates a webhook service. Call EnsureTable then Start.
func NewService(db *database.DB) *Service {
	return &Service{
		db:         db,
		client:     &http.Client{Timeout: requestTimeout},
		queue:      make(chan task, 256),
		pruneLimit: 1000,
	}
}

// EnsureTable creates the webhook tables if they don't exist.
func (s *Service) EnsureTable(ctx context.Context) error {
	return s.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS _webhooks (
			id          TEXT PRIMARY KEY,
			name        TEXT NOT NULL,
			url         TEXT NOT NULL,
			secret      TEXT NOT NULL,
			events      JSONB NOT NULL DEFAULT '[]',
			collections JSONB NOT NULL DEFAULT '[]',
			enabled     BOOLEAN DEFAULT TRUE,
			headers     JSONB NOT NULL DEFAULT '{}',
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE TABLE IF NOT EXISTS _webhook_deliveries (
			id          BIGSERIAL PRIMARY KEY,
			webhook_id  TEXT REFERENCES _webhooks(id) ON DELETE CASCADE,
			event       TEXT NOT NULL,
			payload     JSONB,
			status_code INT DEFAULT 0,
			attempt     INT DEFAULT 1,
			success     BOOLEAN DEFAULT FALSE,
			error       TEXT DEFAULT '',
			duration_ms INT DEFAULT 0,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_webhook_id ON _webhook_deliveries(webhook_id);
	`)
}

// ValidateURL rejects webhook URLs that aren't plain http/https. Private and
// loopback addresses are deliberately allowed: Gresbase is self-hosted and
// operators commonly forward events to internal services (n8n, local
// functions, etc.), so the usual cloud SSRF blocklist would be a footgun here.
func ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid webhook url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("webhook url must use http or https")
	}
	if u.Host == "" {
		return fmt.Errorf("webhook url is missing a host")
	}
	return nil
}

// MaskSecret returns a display-safe form of a webhook secret: the prefix plus
// a few identifying characters, never the full value.
func MaskSecret(secret string) string {
	if len(secret) <= 12 {
		return "…"
	}
	return secret[:12] + "…"
}

func generateSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return "whsec_" + hex.EncodeToString(b)
}

// Create persists a new webhook. If no secret is provided a random one is
// generated; the full secret is returned here once and masked in listings.
func (s *Service) Create(ctx context.Context, hook *Webhook) (*Webhook, error) {
	if hook.Name == "" {
		return nil, fmt.Errorf("webhook name is required")
	}
	if err := ValidateURL(hook.URL); err != nil {
		return nil, err
	}
	hook.ID = uuid.New().String()
	if hook.Secret == "" {
		hook.Secret = generateSecret()
	}
	if len(hook.Events) == 0 {
		hook.Events = []string{"*"}
	}
	if hook.Collections == nil {
		hook.Collections = []string{}
	}
	if hook.Headers == nil {
		hook.Headers = map[string]string{}
	}
	now := time.Now()
	hook.CreatedAt = now
	hook.UpdatedAt = now

	eventsJSON, _ := json.Marshal(hook.Events)
	collectionsJSON, _ := json.Marshal(hook.Collections)
	headersJSON, _ := json.Marshal(hook.Headers)

	err := s.db.Exec(ctx, `
		INSERT INTO _webhooks (id, name, url, secret, events, collections, enabled, headers, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		hook.ID, hook.Name, hook.URL, hook.Secret, eventsJSON, collectionsJSON, hook.Enabled, headersJSON,
		hook.CreatedAt, hook.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create webhook: %w", err)
	}
	return hook, nil
}

// Update overwrites an existing webhook's configuration. An empty Secret
// keeps the stored one (so masked values round-trip safely).
func (s *Service) Update(ctx context.Context, hook *Webhook) (*Webhook, error) {
	existing, err := s.Get(ctx, hook.ID)
	if err != nil {
		return nil, err
	}
	if hook.Name == "" {
		return nil, fmt.Errorf("webhook name is required")
	}
	if err := ValidateURL(hook.URL); err != nil {
		return nil, err
	}
	if hook.Secret == "" {
		hook.Secret = existing.Secret
	}
	if len(hook.Events) == 0 {
		hook.Events = []string{"*"}
	}
	if hook.Collections == nil {
		hook.Collections = []string{}
	}
	if hook.Headers == nil {
		hook.Headers = map[string]string{}
	}
	hook.CreatedAt = existing.CreatedAt
	hook.UpdatedAt = time.Now()

	eventsJSON, _ := json.Marshal(hook.Events)
	collectionsJSON, _ := json.Marshal(hook.Collections)
	headersJSON, _ := json.Marshal(hook.Headers)

	err = s.db.Exec(ctx, `
		UPDATE _webhooks
		SET name = $1, url = $2, secret = $3, events = $4, collections = $5, enabled = $6, headers = $7, updated_at = $8
		WHERE id = $9`,
		hook.Name, hook.URL, hook.Secret, eventsJSON, collectionsJSON, hook.Enabled, headersJSON,
		hook.UpdatedAt, hook.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to update webhook: %w", err)
	}
	return hook, nil
}

// Delete removes a webhook and (via cascade) its delivery log.
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.db.Exec(ctx, "DELETE FROM _webhooks WHERE id = $1", id)
}

// Get returns a single webhook with its full (unmasked) secret.
func (s *Service) Get(ctx context.Context, id string) (*Webhook, error) {
	row := s.db.QueryRow(ctx, selectWebhookSQL+" WHERE id = $1", id)
	hook, err := scanWebhook(row)
	if err != nil {
		return nil, fmt.Errorf("webhook not found: %s", id)
	}
	return hook, nil
}

// List returns all webhooks with full secrets; callers serving HTTP responses
// are expected to mask them (see MaskSecret).
func (s *Service) List(ctx context.Context) ([]*Webhook, error) {
	rows, err := s.db.Query(ctx, selectWebhookSQL+" ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hooks := []*Webhook{}
	for rows.Next() {
		hook, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		hooks = append(hooks, hook)
	}
	return hooks, rows.Err()
}

// ListDeliveries returns the most recent delivery attempts for a webhook.
func (s *Service) ListDeliveries(ctx context.Context, webhookID string, limit int) ([]*Delivery, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, webhook_id, event, payload, status_code, attempt, success, error, duration_ms, created_at
		FROM _webhook_deliveries WHERE webhook_id = $1 ORDER BY id DESC LIMIT $2`, webhookID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	deliveries := []*Delivery{}
	for rows.Next() {
		d := &Delivery{}
		var payload []byte
		if err := rows.Scan(&d.ID, &d.WebhookID, &d.Event, &payload, &d.StatusCode, &d.Attempt,
			&d.Success, &d.Error, &d.DurationMS, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.Payload = payload
		deliveries = append(deliveries, d)
	}
	return deliveries, rows.Err()
}

// Prune keeps only the most recent pruneLimit deliveries for a webhook. It is
// called opportunistically after inserts so the log can't grow unbounded.
func (s *Service) Prune(ctx context.Context, webhookID string) error {
	return s.db.Exec(ctx, `
		DELETE FROM _webhook_deliveries
		WHERE webhook_id = $1 AND id NOT IN (
			SELECT id FROM _webhook_deliveries WHERE webhook_id = $1 ORDER BY id DESC LIMIT $2
		)`, webhookID, s.pruneLimit)
}

const selectWebhookSQL = `
	SELECT id, name, url, secret, events, collections, enabled, headers, created_at, updated_at
	FROM _webhooks`

type scannable interface {
	Scan(dest ...any) error
}

func scanWebhook(row scannable) (*Webhook, error) {
	hook := &Webhook{}
	var eventsJSON, collectionsJSON, headersJSON []byte
	if err := row.Scan(&hook.ID, &hook.Name, &hook.URL, &hook.Secret, &eventsJSON, &collectionsJSON,
		&hook.Enabled, &headersJSON, &hook.CreatedAt, &hook.UpdatedAt); err != nil {
		return nil, err
	}
	json.Unmarshal(eventsJSON, &hook.Events)
	json.Unmarshal(collectionsJSON, &hook.Collections)
	json.Unmarshal(headersJSON, &hook.Headers)
	return hook, nil
}
