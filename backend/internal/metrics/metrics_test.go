package metrics

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewCollector(t *testing.T) {
	c := NewCollector()
	if c == nil {
		t.Fatal("expected non-nil Collector")
	}
	if c.startTime.IsZero() {
		t.Error("expected startTime to be set")
	}
}

func TestCollector_RecordQuery(t *testing.T) {
	c := NewCollector()

	c.RecordQuery(5*time.Millisecond, false)
	c.RecordQuery(15*time.Millisecond, false)
	c.RecordQuery(200*time.Millisecond, true) // slow

	if c.queryCount.Load() != 3 {
		t.Errorf("expected queryCount 3, got %d", c.queryCount.Load())
	}
	if c.slowQueryCount.Load() != 1 {
		t.Errorf("expected slowQueryCount 1, got %d", c.slowQueryCount.Load())
	}
}

func TestCollector_Generate(t *testing.T) {
	c := NewCollector()

	// Set up stats callbacks
	c.DBStats = func() (int32, int32, int32) {
		return 5, 2, 25
	}
	c.DBMode = func() string {
		return "embedded"
	}
	c.RealtimeStats = func() (int, int64, int64, int64) {
		return 10, 5, 1000, 0
	}

	// Record some queries
	c.RecordQuery(10*time.Millisecond, false)
	c.RecordQuery(5*time.Millisecond, false)

	report := c.Generate("0.1.0")
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if report.Version != "0.1.0" {
		t.Errorf("expected version 0.1.0, got %s", report.Version)
	}
	if report.Status != StatusHealthy {
		t.Errorf("expected status healthy, got %s", report.Status)
	}
	if report.UptimeMs < 0 {
		t.Error("expected non-negative uptime")
	}
	if report.Uptime == "" {
		t.Error("expected non-empty uptime string")
	}
	if report.Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}

	// Database metrics
	if report.Database.Status != StatusHealthy {
		t.Errorf("expected db healthy")
	}
	if report.Database.Mode != "embedded" {
		t.Errorf("expected embedded mode, got %s", report.Database.Mode)
	}
	if report.Database.OpenConnections != 5 {
		t.Errorf("expected open 5, got %d", report.Database.OpenConnections)
	}
	if report.Database.IdleConnections != 2 {
		t.Errorf("expected idle 2, got %d", report.Database.IdleConnections)
	}
	if report.Database.MaxConnections != 25 {
		t.Errorf("expected max 25, got %d", report.Database.MaxConnections)
	}
	if report.Database.TotalQueries != 2 {
		t.Errorf("expected total queries 2, got %d", report.Database.TotalQueries)
	}
	if report.Database.SlowQueries != 0 {
		t.Errorf("expected slow queries 0")
	}
	if !(report.Database.AvgQueryMs > 0) {
		t.Error("expected positive avg query ms")
	}

	// Realtime metrics
	if report.Realtime.TotalClients != 10 {
		t.Errorf("expected 10 clients, got %d", report.Realtime.TotalClients)
	}
	if report.Realtime.TotalTopics != 5 {
		t.Errorf("expected 5 topics, got %d", report.Realtime.TotalTopics)
	}
	if report.Realtime.MessagesSent != 1000 {
		t.Errorf("expected 1000 sent")
	}

	// System info
	if report.System.GoVersion == "" {
		t.Error("expected go version")
	}
	if report.System.NumCPU <= 0 {
		t.Error("expected positive num CPU")
	}
	if report.System.NumGoroutines <= 0 {
		t.Error("expected positive goroutines")
	}

	// Components
	if report.Components["api"].Status != StatusHealthy {
		t.Error("expected api healthy")
	}
	if report.Components["database"].Status != StatusHealthy {
		t.Error("expected db healthy")
	}
}

func TestCollector_Generate_NoCallbacks(t *testing.T) {
	c := NewCollector()
	report := c.Generate("1.0.0")

	// Without callbacks, database and realtime should be omitted
	if report.Database.TotalQueries != 0 {
		t.Errorf("expected 0 queries without callbacks")
	}
}

func TestCollector_Generate_NoRealtime(t *testing.T) {
	c := NewCollector()
	c.DBStats = func() (int32, int32, int32) {
		return 1, 1, 10
	}
	c.DBMode = func() string {
		return "external"
	}
	// RealtimeStats not set

	report := c.Generate("1.0.0")
	if report.Database.Mode != "external" {
		t.Errorf("expected external mode")
	}
	// Realtime should not be set
	// (the struct will be zero-valued but not populated)
}

func TestCollector_Handler(t *testing.T) {
	c := NewCollector()
	handler := c.Handler("0.3.0")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected json content type, got %s", contentType)
	}

	// Parse response
	var report HealthReport
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if report.Version != "0.3.0" {
		t.Errorf("expected version 0.3.0")
	}
}

func TestCollector_Handler_CORS(t *testing.T) {
	c := NewCollector()
	handler := c.Handler("0.1.0")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	cors := rec.Header().Get("Access-Control-Allow-Origin")
	if cors != "*" {
		t.Errorf("expected CORS *, got %s", cors)
	}

	cache := rec.Header().Get("Cache-Control")
	if cache != "no-cache" {
		t.Errorf("expected no-cache, got %s", cache)
	}
}

func TestCollector_WithSlowQueries(t *testing.T) {
	c := NewCollector()
	c.DBStats = func() (int32, int32, int32) { return 3, 1, 20 }

	// Mix of normal and slow queries
	c.RecordQuery(1*time.Millisecond, false)
	c.RecordQuery(2*time.Millisecond, false)
	c.RecordQuery(500*time.Millisecond, true)
	c.RecordQuery(1*time.Second, true)

	report := c.Generate("1.0.0")
	if report.Database.TotalQueries != 4 {
		t.Errorf("expected 4 total queries")
	}
	if report.Database.SlowQueries != 2 {
		t.Errorf("expected 2 slow queries, got %d", report.Database.SlowQueries)
	}
}

func TestStatusConstants(t *testing.T) {
	if StatusHealthy != "healthy" {
		t.Errorf("expected healthy=%q", StatusHealthy)
	}
	if StatusDegraded != "degraded" {
		t.Errorf("expected degraded=%q", StatusDegraded)
	}
	if StatusDown != "down" {
		t.Errorf("expected down=%q", StatusDown)
	}
}

func TestComponentHealth(t *testing.T) {
	ch := ComponentHealth{
		Status:  StatusHealthy,
		Message: "All good",
	}
	data, _ := json.Marshal(ch)
	var parsed ComponentHealth
	json.Unmarshal(data, &parsed)
	if parsed.Status != StatusHealthy {
		t.Errorf("expected healthy after round-trip")
	}
}
