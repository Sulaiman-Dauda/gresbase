// Package metrics provides runtime metrics and health information
// for the Gresbase server, including database pool stats, realtime
// connection counts, uptime, and storage usage.
package metrics

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Status represents the current health status of a component.
type Status string

const (
	StatusHealthy  Status = "healthy"
	StatusDegraded Status = "degraded"
	StatusDown     Status = "down"
)

// ComponentHealth describes the health of a subsystem.
type ComponentHealth struct {
	Status  Status `json:"status"`
	Message string `json:"message,omitempty"`
}

// HealthReport is a comprehensive health check response.
type HealthReport struct {
	Status     Status                     `json:"status"`
	Version    string                     `json:"version"`
	Uptime     string                     `json:"uptime"`
	UptimeMs   int64                      `json:"uptime_ms"`
	Components map[string]ComponentHealth `json:"components"`
	System     SystemInfo                 `json:"system"`
	Database   DatabaseMetrics            `json:"database,omitempty"`
	Realtime   RealtimeMetrics            `json:"realtime,omitempty"`
	Storage    StorageMetrics             `json:"storage,omitempty"`
	Timestamp  string                     `json:"timestamp"`
}

// SystemInfo contains Go runtime stats.
type SystemInfo struct {
	GoVersion     string  `json:"go_version"`
	NumCPU        int     `json:"num_cpu"`
	NumGoroutines int     `json:"num_goroutines"`
	AllocMB       float64 `json:"alloc_mb"`
	TotalAllocMB  float64 `json:"total_alloc_mb"`
}

// DatabaseMetrics holds database connection pool statistics.
type DatabaseMetrics struct {
	Status          Status  `json:"status"`
	Mode            string  `json:"mode"` // "embedded" or "external"
	OpenConnections int32   `json:"open_connections"`
	IdleConnections int32   `json:"idle_connections"`
	MaxConnections  int32   `json:"max_connections"`
	TotalQueries    int64   `json:"total_queries,omitempty"`
	SlowQueries     int64   `json:"slow_queries,omitempty"`
	AvgQueryMs      float64 `json:"avg_query_ms,omitempty"`
}

// RealtimeMetrics holds realtime hub statistics.
type RealtimeMetrics struct {
	Status          Status `json:"status"`
	TotalClients    int    `json:"total_clients"`
	TotalTopics     int64  `json:"total_topics"`
	MessagesSent    int64  `json:"messages_sent"`
	MessagesDropped int64  `json:"messages_dropped"`
}

// StorageMetrics holds storage system information.
type StorageMetrics struct {
	Status    Status `json:"status"`
	Backend   string `json:"backend"`
	FileCount int    `json:"file_count,omitempty"`
	TotalSize int64  `json:"total_size_bytes,omitempty"`
}

// httpDurationBuckets are the cumulative upper bounds (in seconds) for the
// HTTP request-duration histogram. They follow the conventional Prometheus
// client default latency buckets.
var httpDurationBuckets = []float64{
	0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10,
}

// httpStatusKey identifies a request-count series by method and status class.
type httpStatusKey struct {
	method string
	status string
}

// Collector gathers and reports system metrics.
type Collector struct {
	startTime      time.Time
	queryCount     atomic.Int64
	slowQueryCount atomic.Int64
	totalQueryTime atomic.Int64 // nanoseconds

	// HTTP request metrics.
	httpMu         sync.Mutex
	httpRequests   map[httpStatusKey]int64
	httpDurSum     float64
	httpDurCount   int64
	httpDurBuckets []int64 // cumulative counts aligned with httpDurationBuckets

	// Callbacks set by the App layer
	DBStats       func() (open, idle, max int32)
	DBMode        func() string
	RealtimeStats func() (clients int, topics, sent, dropped int64)
	// WALStats reports WAL change-capture counters. Nil when WAL capture is
	// not enabled.
	WALStats func() (eventsDecoded, streamRestarts int64, healthy bool)
}

// NewCollector creates a metrics collector with the current time as start.
func NewCollector() *Collector {
	return &Collector{
		startTime:      time.Now(),
		httpRequests:   make(map[httpStatusKey]int64),
		httpDurBuckets: make([]int64, len(httpDurationBuckets)),
	}
}

// RecordHTTPRequest records one completed HTTP request for metrics: it bumps
// the per-method/status counter and the request-duration histogram.
func (c *Collector) RecordHTTPRequest(method, status string, duration time.Duration) {
	if method == "" {
		method = "UNKNOWN"
	}
	secs := duration.Seconds()

	c.httpMu.Lock()
	defer c.httpMu.Unlock()
	c.httpRequests[httpStatusKey{method: method, status: status}]++
	c.httpDurSum += secs
	c.httpDurCount++
	for i, ub := range httpDurationBuckets {
		if secs <= ub {
			c.httpDurBuckets[i]++
		}
	}
}

// httpSnapshot is a consistent point-in-time copy of the HTTP metrics.
type httpSnapshot struct {
	requests map[httpStatusKey]int64
	durSum   float64
	durCount int64
	buckets  []int64
}

func (c *Collector) httpStats() httpSnapshot {
	c.httpMu.Lock()
	defer c.httpMu.Unlock()
	reqs := make(map[httpStatusKey]int64, len(c.httpRequests))
	for k, v := range c.httpRequests {
		reqs[k] = v
	}
	buckets := make([]int64, len(c.httpDurBuckets))
	copy(buckets, c.httpDurBuckets)
	return httpSnapshot{
		requests: reqs,
		durSum:   c.httpDurSum,
		durCount: c.httpDurCount,
		buckets:  buckets,
	}
}

// sortedHTTPKeys returns request-count keys in a stable order so exposition
// output is deterministic.
func sortedHTTPKeys(m map[httpStatusKey]int64) []httpStatusKey {
	keys := make([]httpStatusKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].method != keys[j].method {
			return keys[i].method < keys[j].method
		}
		return keys[i].status < keys[j].status
	})
	return keys
}

// RecordQuery records a query execution for metrics.
func (c *Collector) RecordQuery(duration time.Duration, isSlow bool) {
	c.queryCount.Add(1)
	c.totalQueryTime.Add(duration.Nanoseconds())
	if isSlow {
		c.slowQueryCount.Add(1)
	}
}

// Generate produces a comprehensive health report.
func (c *Collector) Generate(version string) *HealthReport {
	uptime := time.Since(c.startTime)
	uptimeMs := uptime.Milliseconds()

	report := &HealthReport{
		Status:   StatusHealthy,
		Version:  version,
		Uptime:   uptime.Round(time.Second).String(),
		UptimeMs: uptimeMs,
		Components: map[string]ComponentHealth{
			"api":      {Status: StatusHealthy, Message: "HTTP server running"},
			"database": {Status: StatusHealthy},
			"realtime": {Status: StatusHealthy},
			"storage":  {Status: StatusHealthy},
		},
		System: SystemInfo{
			GoVersion:     runtime.Version(),
			NumCPU:        runtime.NumCPU(),
			NumGoroutines: runtime.NumGoroutine(),
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	// Memory stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	report.System.AllocMB = float64(m.Alloc) / 1024 / 1024
	report.System.TotalAllocMB = float64(m.TotalAlloc) / 1024 / 1024

	// Database metrics
	if c.DBStats != nil {
		open, idle, max := c.DBStats()
		dbMetrics := DatabaseMetrics{
			Status:          StatusHealthy,
			OpenConnections: open,
			IdleConnections: idle,
			MaxConnections:  max,
			TotalQueries:    c.queryCount.Load(),
			SlowQueries:     c.slowQueryCount.Load(),
		}
		if c.DBMode != nil {
			dbMetrics.Mode = c.DBMode()
		}
		queryCount := c.queryCount.Load()
		if queryCount > 0 {
			dbMetrics.AvgQueryMs = float64(c.totalQueryTime.Load()) / float64(queryCount) / 1e6
		}
		report.Database = dbMetrics
	}

	// Realtime metrics
	if c.RealtimeStats != nil {
		clients, topics, sent, dropped := c.RealtimeStats()
		report.Realtime = RealtimeMetrics{
			Status:          StatusHealthy,
			TotalClients:    clients,
			TotalTopics:     topics,
			MessagesSent:    sent,
			MessagesDropped: dropped,
		}
	}

	return report
}

// Handler returns an HTTP handler for the metrics endpoint.
func (c *Collector) Handler(version string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "no-cache")

		report := c.Generate(version)

		// Encode JSON manually to avoid circular imports
		data, err := json.Marshal(report)
		if err != nil {
			http.Error(w, `{"status":"error"}`, http.StatusInternalServerError)
			return
		}
		w.Write(data)
	}
}

// Ensure imports used
var _ = context.Background
