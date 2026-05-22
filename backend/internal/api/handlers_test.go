package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gresbase/gresbase/internal/app"
	"github.com/gresbase/gresbase/internal/config"
)

func TestBatchHandler_Validation(t *testing.T) {
	a, _ := app.New(config.Load())
	// We don't need to bootstrap for this test - just check handler logic
	h := NewHandlers(a)

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "empty batch",
			body:       `{"requests": []}`,
			wantStatus: 400,
		},
		{
			name:       "invalid JSON",
			body:       `not json`,
			wantStatus: 400,
		},
		{
			name: "batch limit exceeded",
			body: func() string {
				reqs := make([]BatchRequest, 101)
				b, _ := json.Marshal(BatchPayload{Requests: reqs})
				return string(b)
			}(),
			wantStatus: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/v1/batch", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.HandleBatch(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestBatchRequest_Struct(t *testing.T) {
	req := BatchRequest{
		Method: "GET",
		URL:    "/api/v1/collections",
		Headers: map[string]string{
			"X-Custom": "test",
		},
		Body: json.RawMessage(`{"name":"test"}`),
	}

	if req.Method != "GET" {
		t.Error("method mismatch")
	}
	if req.URL != "/api/v1/collections" {
		t.Error("URL mismatch")
	}
}

func TestBatchResponse_Struct(t *testing.T) {
	resp := BatchResponse{
		Status: 200,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: json.RawMessage(`{"ok":true}`),
	}

	if resp.Status != 200 {
		t.Error("status mismatch")
	}
}

func TestExecuteSingleRequest_ForbiddenURL(t *testing.T) {
	a, _ := app.New(config.Load())
	req := BatchRequest{
		Method: "GET",
		URL:    "/admin/dashboard",
	}
	httpReq := httptest.NewRequest("GET", "/", nil)
	resp := executeSingleRequest(a, httpReq, req)

	if resp.Status != 403 {
		t.Errorf("non-API URL should be forbidden, got %d", resp.Status)
	}
}

func TestHealthEndpoint(t *testing.T) {
	a, _ := app.New(config.Load())
	h := NewHandlers(a)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	h.Health(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("health check should return 200, got %d", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "healthy" && resp["status"] != "degraded" {
		t.Errorf("unexpected health status: %v", resp["status"])
	}
}

func TestLoginValidation(t *testing.T) {
	a, _ := app.New(config.Load())
	h := NewHandlers(a)

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "empty body",
			body:       `{}`,
			wantStatus: 400,
		},
		{
			name:       "missing password",
			body:       `{"email":"test@test.com"}`,
			wantStatus: 400,
		},
		{
			name:       "invalid JSON",
			body:       `not json`,
			wantStatus: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.Login(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, 200, map[string]string{"hello": "world"})

	if w.Code != 200 {
		t.Errorf("status = %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %s", ct)
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["hello"] != "world" {
		t.Error("response body mismatch")
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, 404, "Not found")

	if w.Code != 404 {
		t.Errorf("status = %d", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["message"] != "Not found" {
		t.Errorf("error message mismatch: %v", resp)
	}
}

func TestWriteOK(t *testing.T) {
	w := httptest.NewRecorder()
	writeOK(w, map[string]int{"count": 42})

	if w.Code != 200 {
		t.Errorf("status = %d", w.Code)
	}

	var resp map[string]int
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["count"] != 42 {
		t.Error("response mismatch")
	}
}
