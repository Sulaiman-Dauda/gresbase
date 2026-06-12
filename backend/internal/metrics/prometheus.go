package metrics

import (
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const prometheusContentType = "text/plain; version=0.0.4; charset=utf-8"

// escapeLabelValue escapes a label value per the Prometheus text format:
// backslash, double-quote, and newline must be escaped.
func escapeLabelValue(v string) string {
	var b strings.Builder
	for _, r := range v {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// promWriter accumulates Prometheus text-format output.
type promWriter struct {
	b strings.Builder
}

func (p *promWriter) family(name, help, typ string) {
	p.b.WriteString("# HELP " + name + " " + help + "\n")
	p.b.WriteString("# TYPE " + name + " " + typ + "\n")
}

// sample writes a single sample line. labels must be pre-formatted
// pairs of key, value; values are escaped here.
func (p *promWriter) sample(name string, labels []string, value string) {
	p.b.WriteString(name)
	if len(labels) > 0 {
		p.b.WriteByte('{')
		for i := 0; i+1 < len(labels); i += 2 {
			if i > 0 {
				p.b.WriteByte(',')
			}
			p.b.WriteString(labels[i])
			p.b.WriteString(`="`)
			p.b.WriteString(escapeLabelValue(labels[i+1]))
			p.b.WriteByte('"')
		}
		p.b.WriteByte('}')
	}
	p.b.WriteByte(' ')
	p.b.WriteString(value)
	p.b.WriteByte('\n')
}

func (p *promWriter) gaugeInt(name, help string, v int64) {
	p.family(name, help, "gauge")
	p.sample(name, nil, strconv.FormatInt(v, 10))
}

func (p *promWriter) counterInt(name, help string, v int64) {
	p.family(name, help, "counter")
	p.sample(name, nil, strconv.FormatInt(v, 10))
}

// PrometheusHandler returns an HTTP handler that exposes collector
// metrics in the Prometheus text exposition format (version 0.0.4).
func (c *Collector) PrometheusHandler(version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var p promWriter

		p.family("gresbase_up", "Whether the Gresbase server is up.", "gauge")
		p.sample("gresbase_up", nil, "1")

		p.family("gresbase_info", "Build and runtime information.", "gauge")
		p.sample("gresbase_info", []string{"version", version, "go_version", runtime.Version()}, "1")

		uptime := time.Since(c.startTime).Seconds()
		p.family("gresbase_uptime_seconds", "Time since the server started, in seconds.", "gauge")
		p.sample("gresbase_uptime_seconds", nil, formatFloat(uptime))

		if c.DBStats != nil {
			open, idle, max := c.DBStats()
			p.family("gresbase_db_connections", "Database connections by state.", "gauge")
			p.sample("gresbase_db_connections", []string{"state", "open"}, strconv.FormatInt(int64(open), 10))
			p.sample("gresbase_db_connections", []string{"state", "idle"}, strconv.FormatInt(int64(idle), 10))
			p.gaugeInt("gresbase_db_max_connections", "Maximum allowed database connections.", int64(max))
			// Pool utilization: acquired (open) connections as a fraction of the
			// max. 0 when no max is reported.
			util := 0.0
			if max > 0 {
				util = float64(open) / float64(max)
			}
			p.family("gresbase_db_pool_utilization", "Database connection-pool utilization (acquired/max), 0..1.", "gauge")
			p.sample("gresbase_db_pool_utilization", nil, formatFloat(util))
			p.counterInt("gresbase_db_queries_total", "Total number of database queries executed.", c.queryCount.Load())
			p.counterInt("gresbase_db_slow_queries_total", "Total number of slow database queries.", c.slowQueryCount.Load())
			querySeconds := float64(c.totalQueryTime.Load()) / 1e9
			p.family("gresbase_db_query_duration_seconds_sum", "Cumulative time spent executing database queries, in seconds.", "counter")
			p.sample("gresbase_db_query_duration_seconds_sum", nil, formatFloat(querySeconds))
		}

		if c.RealtimeStats != nil {
			clients, topics, sent, dropped := c.RealtimeStats()
			p.gaugeInt("gresbase_realtime_clients", "Number of connected realtime clients.", int64(clients))
			p.gaugeInt("gresbase_realtime_topics", "Number of active realtime topics.", topics)
			p.counterInt("gresbase_realtime_messages_sent_total", "Total realtime messages sent.", sent)
			p.counterInt("gresbase_realtime_messages_dropped_total", "Total realtime messages dropped.", dropped)
		}

		if c.WALStats != nil {
			decoded, restarts, healthy := c.WALStats()
			p.counterInt("gresbase_realtime_wal_events_decoded_total", "Total record events decoded from the WAL stream.", decoded)
			p.counterInt("gresbase_realtime_wal_stream_restarts_total", "Total WAL replication stream restarts.", restarts)
			healthyVal := int64(0)
			if healthy {
				healthyVal = 1
			}
			p.gaugeInt("gresbase_realtime_wal_healthy", "Whether the WAL change-capture stream is established (1) or degraded to API-emitted events (0).", healthyVal)
		}

		// HTTP request metrics.
		hs := c.httpStats()
		p.family("gresbase_http_requests_total", "Total HTTP requests by method and status class.", "counter")
		for _, key := range sortedHTTPKeys(hs.requests) {
			p.sample("gresbase_http_requests_total",
				[]string{"method", key.method, "status", key.status},
				strconv.FormatInt(hs.requests[key], 10))
		}
		// The histogram is exposed as three derived series. This codebase
		// declares a HELP/TYPE family for every distinct sample name (see the
		// db_query_duration_seconds_sum counter above), so do the same here.
		p.family("gresbase_http_request_duration_seconds_bucket", "HTTP request latency in seconds (cumulative histogram buckets).", "histogram")
		for i, ub := range httpDurationBuckets {
			p.sample("gresbase_http_request_duration_seconds_bucket",
				[]string{"le", formatFloat(ub)}, strconv.FormatInt(hs.buckets[i], 10))
		}
		p.sample("gresbase_http_request_duration_seconds_bucket",
			[]string{"le", "+Inf"}, strconv.FormatInt(hs.durCount, 10))
		p.family("gresbase_http_request_duration_seconds_sum", "Cumulative HTTP request latency in seconds.", "counter")
		p.sample("gresbase_http_request_duration_seconds_sum", nil, formatFloat(hs.durSum))
		p.family("gresbase_http_request_duration_seconds_count", "Total observed HTTP requests in the latency histogram.", "counter")
		p.sample("gresbase_http_request_duration_seconds_count", nil, strconv.FormatInt(hs.durCount, 10))

		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		p.gaugeInt("gresbase_goroutines", "Number of running goroutines.", int64(runtime.NumGoroutine()))
		p.gaugeInt("gresbase_memory_alloc_bytes", "Bytes of allocated heap objects.", int64(m.Alloc))
		p.gaugeInt("gresbase_memory_total_alloc_bytes", "Cumulative bytes allocated for heap objects.", int64(m.TotalAlloc))
		p.gaugeInt("gresbase_cpu_count", "Number of logical CPUs.", int64(runtime.NumCPU()))

		w.Header().Set("Content-Type", prometheusContentType)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(p.b.String()))
	}
}
