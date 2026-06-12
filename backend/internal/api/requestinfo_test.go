package api

import (
	"context"
	"net/http/httptest"
	"testing"

	apimw "github.com/gresbase/gresbase/internal/api/middleware"
)

func TestNewRequestInfo(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v1/records/posts?page=2&expand=author", nil)
	req.Header.Set("X-Test", "ok")
	ctx := context.WithValue(req.Context(), apimw.CtxAdminID, "admin1")
	ctx = context.WithValue(ctx, apimw.CtxAdminRole, "super_admin")
	req = req.WithContext(ctx)

	info := NewRequestInfo(req)
	if info.Method != "POST" {
		t.Fatalf("expected POST, got %s", info.Method)
	}
	if !info.IsAdmin || info.AdminID != "admin1" {
		t.Fatalf("expected admin auth info to be extracted")
	}
	if info.Query["page"] != "2" || info.Query["expand"] != "author" {
		t.Fatalf("unexpected query info: %#v", info.Query)
	}
	if info.Headers["X-Test"] != "ok" {
		t.Fatalf("unexpected headers: %#v", info.Headers)
	}
}

func TestToEventRequestInfoWithBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	info := toEventRequestInfoWithBody(req, map[string]any{"email": "admin@example.com"})
	if info == nil {
		t.Fatal("expected event request info")
	}
	if info.Body["email"] != "admin@example.com" {
		t.Fatalf("expected body to be copied, got %#v", info.Body)
	}
}
