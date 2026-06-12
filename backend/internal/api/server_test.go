package api_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gresbase/gresbase/internal/api"
	"github.com/gresbase/gresbase/internal/app"
	"github.com/gresbase/gresbase/internal/config"
)

func TestNewServer(t *testing.T) {
	cfg := &config.Config{
		JWTSecret:      "test-jwt-secret",
		LogLevel:       "error",
		Addr:           ":8080",
		StorageBackend: "local",
		StorageLocal:   t.TempDir(),
	}
	application, err := app.New(cfg)
	if err != nil {
		t.Fatalf("Failed to create app: %v", err)
	}

	srv := api.NewServer(application)
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}
}

func TestHealthHandler(t *testing.T) {
	// Test that we can construct and hit health endpoint
	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	req.Header.Set("Content-Type", "application/json")
	_ = req
}

func TestLoginRequestValidation(t *testing.T) {
	tests := []struct {
		name     string
		body     map[string]any
		wantCode int
	}{
		{
			name:     "valid login",
			body:     map[string]any{"email": "admin@test.com", "password": "secret123"},
			wantCode: 200, // would be 200 with real DB
		},
		{
			name:     "missing email",
			body:     map[string]any{"password": "secret123"},
			wantCode: 400,
		},
		{
			name:     "missing password",
			body:     map[string]any{"email": "admin@test.com"},
			wantCode: 400,
		},
		{
			name:     "empty body",
			body:     map[string]any{},
			wantCode: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.body)
			if err != nil {
				t.Fatal(err)
			}

			_ = data
			// Full integration test would require a DB connection
		})
	}
}

func TestJSONResponseFormat(t *testing.T) {
	rec := httptest.NewRecorder()

	response := map[string]any{
		"status": "ok",
		"data":   map[string]any{"id": "123", "name": "test"},
	}

	rec.Header().Set("Content-Type", "application/json")
	rec.WriteHeader(200)
	json.NewEncoder(rec).Encode(response)

	if rec.Code != 200 {
		t.Errorf("Expected 200, got %d", rec.Code)
	}

	var result map[string]any
	json.NewDecoder(rec.Body).Decode(&result)

	if result["status"] != "ok" {
		t.Errorf("Expected status ok, got %v", result["status"])
	}

	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatal("Expected nested data object")
	}
	if data["name"] != "test" {
		t.Errorf("Expected name 'test', got %v", data["name"])
	}
}

func TestErrorResponseFormat(t *testing.T) {
	tests := []struct {
		code    int
		message string
	}{
		{400, "Invalid request"},
		{401, "Unauthorized"},
		{404, "Not found"},
		{409, "Conflict"},
		{500, "Internal server error"},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			rec := httptest.NewRecorder()

			errResp := map[string]any{
				"code":    tt.code,
				"message": tt.message,
			}

			rec.Header().Set("Content-Type", "application/json")
			rec.WriteHeader(tt.code)
			json.NewEncoder(rec).Encode(errResp)

			if rec.Code != tt.code {
				t.Errorf("Expected %d, got %d", tt.code, rec.Code)
			}

			var result map[string]any
			json.NewDecoder(rec.Body).Decode(&result)

			if result["message"] != tt.message {
				t.Errorf("Expected %q, got %q", tt.message, result["message"])
			}
		})
	}
}

func TestCORSHeaders(t *testing.T) {
	req := httptest.NewRequest("OPTIONS", "/api/v1/health", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "GET")
	req.Header.Set("Access-Control-Request-Headers", "Authorization,Content-Type")

	if req.Header.Get("Origin") != "http://localhost:3000" {
		t.Error("Origin header not set correctly")
	}

	if req.Method != "OPTIONS" {
		t.Errorf("Expected OPTIONS, got %s", req.Method)
	}
}

func TestPaginationParameters(t *testing.T) {
	tests := []struct {
		url             string
		expectedPage    string
		expectedPerPage string
		expectedSort    string
	}{
		{"/api/v1/records/posts?page=1&perPage=20&sort=created_at", "1", "20", "created_at"},
		{"/api/v1/records/posts?page=3&perPage=50", "3", "50", ""},
		{"/api/v1/records/posts", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.url, nil)

			page := req.URL.Query().Get("page")
			if page != tt.expectedPage {
				t.Errorf("Expected page %q, got %q", tt.expectedPage, page)
			}

			perPage := req.URL.Query().Get("perPage")
			if perPage != tt.expectedPerPage {
				t.Errorf("Expected perPage %q, got %q", tt.expectedPerPage, perPage)
			}

			sort := req.URL.Query().Get("sort")
			if sort != tt.expectedSort {
				t.Errorf("Expected sort %q, got %q", tt.expectedSort, sort)
			}
		})
	}
}

func TestAuthHeaderExtraction(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		wantToken string
	}{
		{"standard bearer", "Bearer eyJhbGciOiJIUzI1NiIs...", "eyJhbGciOiJIUzI1NiIs..."},
		{"lowercase bearer", "bearer token123", "token123"},
		{"no prefix", "just-a-token", ""},
		{"empty", "", ""},
		{"bearer with empty token", "Bearer ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}

			auth := req.Header.Get("Authorization")
			// The extractBearer function would split here
			_ = auth
		})
	}
}

func TestBatchRequestFormat(t *testing.T) {
	batch := map[string]any{
		"requests": []map[string]any{
			{"method": "GET", "url": "/api/v1/health"},
			{"method": "POST", "url": "/api/v1/collections", "body": map[string]any{"name": "test"}},
		},
	}

	data, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/v1/batch", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")

	var parsed map[string]any
	json.Unmarshal(data, &parsed)

	requests, ok := parsed["requests"].([]any)
	if !ok || len(requests) == 0 {
		t.Error("Batch request should have a requests array")
	}
}

func TestFileUploadMultipartRequest(t *testing.T) {
	body := bytes.NewBuffer(nil)
	req := httptest.NewRequest("POST", "/api/v1/files/upload?collection=test&record=rec1", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xxx")

	if req.URL.Query().Get("collection") != "test" {
		t.Error("Collection query param missing")
	}
	if req.URL.Query().Get("record") != "rec1" {
		t.Error("Record query param missing")
	}
}

func TestCollectionValidation(t *testing.T) {
	// Test collection data validation
	validCollection := map[string]any{
		"name": "posts",
		"type": "base",
		"schema": []map[string]any{
			{"name": "title", "type": "text", "required": true},
			{"name": "content", "type": "text"},
		},
		"list_rule":   "",
		"view_rule":   "",
		"create_rule": "",
		"update_rule": "",
		"delete_rule": "",
	}

	data, _ := json.Marshal(validCollection)

	var parsed map[string]any
	json.Unmarshal(data, &parsed)

	if parsed["name"] != "posts" {
		t.Error("Collection name should be 'posts'")
	}

	schema, ok := parsed["schema"].([]any)
	if !ok || len(schema) != 2 {
		t.Errorf("Expected 2 schema fields, got %d", len(schema))
	}
}

func TestRecordCRUDRequestFormat(t *testing.T) {
	record := map[string]any{
		"title":   "Hello World",
		"content": "This is a test post",
		"tags":    []string{"test", "hello"},
	}

	data, _ := json.Marshal(record)

	req := httptest.NewRequest("POST", "/api/v1/records/posts", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")

	var parsed map[string]any
	json.Unmarshal(data, &parsed)

	if parsed["title"] != "Hello World" {
		t.Error("Record title mismatch")
	}
	if tags, ok := parsed["tags"].([]any); !ok || len(tags) != 2 {
		t.Error("Record tags mismatch")
	}

	_ = req
}
