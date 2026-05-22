// Package tenant provides multi-tenant isolation and management.
package tenant

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gresbase/gresbase/internal/database"
)

// Tenant represents a tenant in a multi-tenant deployment.
type Tenant struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Slug      string         `json:"slug"`
	Settings  map[string]any `json:"settings"`
	Active    bool           `json:"active"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// Service manages tenant operations.
type Service struct {
	db *database.DB
}

// NewService creates a tenant service.
func NewService(db *database.DB) *Service {
	return &Service{db: db}
}

// EnsureTable creates the tenants table.
func (s *Service) EnsureTable(ctx context.Context) error {
	return s.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS _tenants (
			id          TEXT PRIMARY KEY,
			name        TEXT NOT NULL,
			slug        TEXT NOT NULL UNIQUE,
			settings    JSONB DEFAULT '{}',
			active      BOOLEAN DEFAULT TRUE,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
}

// CreateTenant creates a new tenant. Returns error if slug exists.
func (s *Service) CreateTenant(ctx context.Context, name, slug string) (*Tenant, error) {
	if slug == "" {
		slug = slugify(name)
	}

	t := &Tenant{
		ID:        uuid.New().String(),
		Name:      name,
		Slug:      slug,
		Settings:  make(map[string]any),
		Active:    true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	settingsJSON, _ := json.Marshal(t.Settings)
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO _tenants (id, name, slug, settings, active)
		VALUES ($1, $2, $3, $4, $5)`,
		t.ID, t.Name, t.Slug, settingsJSON, t.Active)
	if err != nil {
		return nil, fmt.Errorf("failed to create tenant: %w", err)
	}

	return t, nil
}

// GetTenant retrieves a tenant by ID.
func (s *Service) GetTenant(ctx context.Context, id string) (*Tenant, error) {
	t := &Tenant{}
	var settingsJSON []byte

	err := s.db.Pool.QueryRow(ctx, `
		SELECT id, name, slug, settings, active, created_at, updated_at
		FROM _tenants WHERE id = $1`, id).Scan(
		&t.ID, &t.Name, &t.Slug, &settingsJSON, &t.Active, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}

	json.Unmarshal(settingsJSON, &t.Settings)
	return t, nil
}

// GetTenantBySlug retrieves a tenant by slug.
func (s *Service) GetTenantBySlug(ctx context.Context, slug string) (*Tenant, error) {
	t := &Tenant{}
	var settingsJSON []byte

	err := s.db.Pool.QueryRow(ctx, `
		SELECT id, name, slug, settings, active, created_at, updated_at
		FROM _tenants WHERE slug = $1`, slug).Scan(
		&t.ID, &t.Name, &t.Slug, &settingsJSON, &t.Active, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}

	json.Unmarshal(settingsJSON, &t.Settings)
	return t, nil
}

// ListTenants lists all tenants.
func (s *Service) ListTenants(ctx context.Context) ([]*Tenant, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, name, slug, settings, active, created_at, updated_at
		FROM _tenants ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tenants []*Tenant
	for rows.Next() {
		t := &Tenant{}
		var settingsJSON []byte
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &settingsJSON, &t.Active, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal(settingsJSON, &t.Settings)
		tenants = append(tenants, t)
	}

	return tenants, nil
}

// UpdateTenant updates a tenant's information.
func (s *Service) UpdateTenant(ctx context.Context, id string, updates map[string]any) error {
	if name, ok := updates["name"]; ok {
		_, err := s.db.Pool.Exec(ctx, "UPDATE _tenants SET name = $1, updated_at = NOW() WHERE id = $2", name, id)
		if err != nil {
			return err
		}
	}
	if active, ok := updates["active"]; ok {
		_, err := s.db.Pool.Exec(ctx, "UPDATE _tenants SET active = $1, updated_at = NOW() WHERE id = $2", active, id)
		if err != nil {
			return err
		}
	}
	return nil
}

// DeleteTenant removes a tenant and all its data (CASCADE).
func (s *Service) DeleteTenant(ctx context.Context, id string) error {
	result, err := s.db.Pool.Exec(ctx, "DELETE FROM _tenants WHERE id = $1", id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("tenant not found: %s", id)
	}
	return nil
}

// Exists checks if a tenant slug exists.
func (s *Service) Exists(ctx context.Context, slug string) bool {
	var exists bool
	s.db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM _tenants WHERE slug = $1)", slug).Scan(&exists)
	return exists
}

// slugify creates a URL-safe slug from a name.
func slugify(name string) string {
	slug := ""
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			slug += string(r | 32) // lowercase
		} else if r == ' ' || r == '-' || r == '_' {
			slug += "-"
		}
	}
	if slug == "" {
		slug = "tenant"
	}
	return slug
}
