package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gresbase/gresbase/internal/metrics"
)

// metricsResponseWriter captures the status code written to the response so
// the HTTP metrics middleware can label series by status class. It forwards
// the optional Flusher/Hijacker/Pusher interfaces used by streaming routes
// (SSE, WebSocket upgrade) so wrapping them does not break those handlers.
type metricsResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *metricsResponseWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *metricsResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.status = http.StatusOK
		w.wroteHeader = true
	}
	return w.ResponseWriter.Write(b)
}

func (w *metricsResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// statusClass collapses a status code into a Prometheus-friendly class label
// (2xx, 3xx, 4xx, 5xx, 1xx) to bound cardinality.
func statusClass(code int) string {
	switch {
	case code >= 500:
		return "5xx"
	case code >= 400:
		return "4xx"
	case code >= 300:
		return "3xx"
	case code >= 200:
		return "2xx"
	case code >= 100:
		return "1xx"
	default:
		return strconv.Itoa(code)
	}
}

// HTTPMetrics records request counts and latency into the metrics collector.
// It is a no-op when the collector is nil so wiring is safe in any config.
func HTTPMetrics(coll *metrics.Collector) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if coll == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			mw := &metricsResponseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(mw, r)
			coll.RecordHTTPRequest(r.Method, statusClass(mw.status), time.Since(start))
		})
	}
}
