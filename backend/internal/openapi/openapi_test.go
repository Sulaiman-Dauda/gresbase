package openapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGenerate_Basic(t *testing.T) {
	routes := []RouteDef{
		{
			Method:         "GET",
			Pattern:        "/api/v1/health",
			Summary:        "Health check",
			OperationID:    "getHealth",
			Tag:            "Health",
			AuthRequired:   false,
			ResponseSchema: &Schema{Type: "object"},
		},
		{
			Method:         "POST",
			Pattern:        "/api/v1/auth/login",
			Summary:        "Admin login",
			OperationID:    "postAuthLogin",
			Tag:            "Auth",
			AuthRequired:   true,
			RequestSchema:  &Schema{Type: "object", Properties: map[string]*Schema{
				"email":    {Type: "string", Format: "email"},
				"password": {Type: "string", Format: "password"},
			}},
			ResponseSchema: &Schema{Type: "object"},
		},
	}

	spec := Generate("0.1.0", "http://localhost:8080", routes)

	if spec.OpenAPI != "3.1.0" {
		t.Errorf("expected OpenAPI 3.1.0, got %s", spec.OpenAPI)
	}
	if spec.Info.Title != "Gresbase API" {
		t.Errorf("expected Gresbase API title")
	}
	if spec.Info.Version != "0.1.0" {
		t.Errorf("expected version 0.1.0")
	}
	if len(spec.Servers) != 1 {
		t.Errorf("expected 1 server")
	}
	if spec.Servers[0].URL != "http://localhost:8080" {
		t.Errorf("expected server URL")
	}
	if len(spec.Paths) != 2 {
		t.Errorf("expected 2 paths, got %d", len(spec.Paths))
	}

	// Check auth route has security and request body
	authPath := spec.Paths["/api/v1/auth/login"]
	if authPath.Post == nil {
		t.Fatal("expected POST for /api/v1/auth/login")
	}
	if len(authPath.Post.Security) == 0 {
		t.Error("expected security on auth route")
	}
	if authPath.Post.RequestBody == nil {
		t.Error("expected request body on auth route")
	}

	// Check health route doesn't require auth
	healthPath := spec.Paths["/api/v1/health"]
	if healthPath.Get == nil {
		t.Fatal("expected GET for /api/v1/health")
	}
	if len(healthPath.Get.Security) > 0 {
		t.Error("expected no security on health route")
	}

	// Check components
	if spec.Components == nil {
		t.Fatal("expected components")
	}
	if len(spec.Components.SecuritySchemes) != 2 {
		t.Errorf("expected 2 security schemes")
	}
	if spec.Components.SecuritySchemes["bearerAuth"].Type != "http" {
		t.Errorf("expected http bearer")
	}
	if spec.Components.SecuritySchemes["apiKey"].Type != "apiKey" {
		t.Errorf("expected apiKey auth")
	}
}

func TestGenerate_Tags(t *testing.T) {
	routes := []RouteDef{
		{Method: "GET", Pattern: "/api/v1/health", Tag: "Health"},
		{Method: "POST", Pattern: "/api/v1/auth/login", Tag: "Auth"},
	}

	spec := Generate("1.0.0", "http://localhost:8080", routes)
	if len(spec.Tags) == 0 {
		t.Error("expected tags to be present")
	}

	// Check that our tags are included
	healthTagFound := false
	authTagFound := false
	for _, tag := range spec.Tags {
		if tag.Name == "Health" {
			healthTagFound = true
		}
		if tag.Name == "Auth" {
			authTagFound = true
		}
	}
	if !healthTagFound {
		t.Error("expected Health tag")
	}
	if !authTagFound {
		t.Error("expected Auth tag")
	}
}

func TestGenerate_MultipleMethods(t *testing.T) {
	routes := []RouteDef{
		{Method: "GET", Pattern: "/api/v1/records/{collection}", Tag: "Records"},
		{Method: "POST", Pattern: "/api/v1/records/{collection}", Tag: "Records"},
		{Method: "PUT", Pattern: "/api/v1/records/{collection}/{id}", Tag: "Records"},
		{Method: "DELETE", Pattern: "/api/v1/records/{collection}/{id}", Tag: "Records"},
	}

	spec := Generate("1.0.0", "http://localhost:8080", routes)
	// 2 unique path patterns: /records/{collection} and /records/{collection}/{id}
	if len(spec.Paths) != 2 {
		t.Errorf("expected 2 paths, got %d", len(spec.Paths))
	}
}

func TestHandler(t *testing.T) {
	routes := []RouteDef{
		{Method: "GET", Pattern: "/api/v1/health", Tag: "Health"},
	}
	spec := Generate("1.0.0", "http://localhost:8080", routes)
	handler := Handler(spec)

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected json content type, got %s", ct)
	}

	cors := rec.Header().Get("Access-Control-Allow-Origin")
	if cors != "*" {
		t.Errorf("expected CORS *, got %s", cors)
	}

	var spec2 Spec
	if err := json.Unmarshal(rec.Body.Bytes(), &spec2); err != nil {
		t.Fatalf("failed to parse spec: %v", err)
	}
	if spec2.OpenAPI != "3.1.0" {
		t.Errorf("expected 3.1.0")
	}
}

func TestSummarizeRoute(t *testing.T) {
	tests := []struct {
		method, pattern, expected string
	}{
		{"GET", "/api/v1/health", "Health check"},
		{"POST", "/api/v1/auth/login", "Admin login"},
		{"POST", "/api/v1/auth/refresh", "Refresh access token"},
		{"POST", "/api/v1/auth/register", "Register new admin"},
		{"POST", "/api/v1/auth/otp/request", "OTP authentication"},
		{"POST", "/api/v1/auth/magic-link", "Magic link authentication"},
		{"GET", "/api/v1/collections", "List collections"},
		{"POST", "/api/v1/collections", "Create collection"},
		{"GET", "/api/v1/records/posts", "List records"},
		{"POST", "/api/v1/records/posts", "Create record"},
		{"PUT", "/api/v1/records/posts/abc", "Update record"},
		{"DELETE", "/api/v1/records/posts/abc", "Delete record"},
		{"GET", "/api/v1/realtime", "Realtime subscription"},
		{"GET", "/api/v1/backups", "Backup management"},
		{"GET", "/api/v1/unknown", "GET /api/v1/unknown"},
	}

	for _, tt := range tests {
		got := summarizeRoute(tt.method, tt.pattern)
		if got != tt.expected {
			t.Errorf("summarizeRoute(%s, %s) = %q, want %q", tt.method, tt.pattern, got, tt.expected)
		}
	}
}

func TestOperationIDFromRoute(t *testing.T) {
	tests := []struct {
		method, pattern, expected string
	}{
		{"GET", "/api/v1/health", "getApiV1Health"},
		{"POST", "/api/v1/auth/login", "postApiV1AuthLogin"},
		{"GET", "/api/v1/collections/abc", "getApiV1CollectionsAbc"},
	}

	for _, tt := range tests {
		got := operationIDFromRoute(tt.method, tt.pattern)
		if got != tt.expected {
			t.Errorf("operationIDFromRoute(%s, %s) = %q, want %q", tt.method, tt.pattern, got, tt.expected)
		}
	}
}

func TestTagFromRoute(t *testing.T) {
	tests := []struct {
		pattern, expected string
	}{
		{"/api/v1/health", "Health"},
		{"/api/v1/auth/login", "Auth"},
		{"/api/v1/auth/oauth/google", "Auth"},
		{"/api/v1/admin/users", "Admin"},
		{"/api/v1/collections", "Collections"},
		{"/api/v1/records/posts", "Records"},
		{"/api/v1/files/a/b/c", "Files"},
		{"/api/v1/realtime", "Realtime"},
		{"/api/v1/sse", "Realtime"},
		{"/api/v1/acme/directory", "ACME"},
		{"/api/v1/certificates", "Certificates"},
		{"/api/v1/settings", "Settings"},
		{"/api/v1/logs", "Logs"},
		{"/api/v1/api-keys", "API Keys"},
		{"/api/v1/backups", "Backups"},
		{"/api/v1/search", "Search"},
		{"/api/v1/jobs", "Jobs"},
		{"/api/v1/plugins", "Plugins"},
		{"/api/v1/unknown", "General"},
	}

	for _, tt := range tests {
		got := tagFromRoute(tt.pattern)
		if got != tt.expected {
			t.Errorf("tagFromRoute(%s) = %q, want %q", tt.pattern, got, tt.expected)
		}
	}
}

func TestRouteDef_WithAllMethods(t *testing.T) {
	routes := []RouteDef{
		{Method: "PATCH", Pattern: "/api/v1/collections/{id}", Tag: "Collections", Summary: "Update collection"},
	}
	spec := Generate("1.0.0", "http://localhost:8080", routes)

	p := spec.Paths["/api/v1/collections/{id}"]
	if p.Patch == nil {
		t.Error("expected PATCH operation")
	}
}

func TestGenerate_EmptyRoutes(t *testing.T) {
	spec := Generate("1.0.0", "http://localhost:8080", nil)
	if spec.OpenAPI != "3.1.0" {
		t.Errorf("expected 3.1.0")
	}
	if len(spec.Paths) != 0 {
		t.Errorf("expected 0 paths")
	}
}
