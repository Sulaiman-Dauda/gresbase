package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func doPrometheusRequest(t *testing.T, c *Collector, version string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	c.PrometheusHandler(version)(rec, req)
	return rec
}

// assertLine checks that body contains the exact line.
func assertLine(t *testing.T, body, line string) {
	t.Helper()
	if !strings.Contains(body, line+"\n") {
		t.Errorf("expected line %q in output:\n%s", line, body)
	}
}

func TestPrometheusHandlerFull(t *testing.T) {
	c := NewCollector()
	c.DBStats = func() (open, idle, max int32) { return 7, 3, 25 }
	c.DBMode = func() string { return "embedded" }
	c.RealtimeStats = func() (clients int, topics, sent, dropped int64) {
		return 4, 2, 100, 5
	}
	c.RecordQuery(50*time.Millisecond, false)
	c.RecordQuery(250*time.Millisecond, true)

	rec := doPrometheusRequest(t, c, "1.2.3")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain; version=0.0.4; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}

	body := rec.Body.String()

	assertLine(t, body, "gresbase_up 1")
	assertLine(t, body, `gresbase_db_connections{state="open"} 7`)
	assertLine(t, body, `gresbase_db_connections{state="idle"} 3`)
	assertLine(t, body, "gresbase_db_max_connections 25")
	assertLine(t, body, "gresbase_db_queries_total 2")
	assertLine(t, body, "gresbase_db_slow_queries_total 1")
	assertLine(t, body, "gresbase_db_query_duration_seconds_sum 0.3")
	assertLine(t, body, "gresbase_realtime_clients 4")
	assertLine(t, body, "gresbase_realtime_topics 2")
	assertLine(t, body, "gresbase_realtime_messages_sent_total 100")
	assertLine(t, body, "gresbase_realtime_messages_dropped_total 5")

	if !strings.Contains(body, `gresbase_info{version="1.2.3",go_version="`) {
		t.Errorf("missing gresbase_info with version label:\n%s", body)
	}
}

func TestPrometheusHandlerHelpTypePrecedeSamples(t *testing.T) {
	c := NewCollector()
	c.DBStats = func() (open, idle, max int32) { return 1, 1, 10 }
	c.RealtimeStats = func() (clients int, topics, sent, dropped int64) {
		return 0, 0, 0, 0
	}

	body := doPrometheusRequest(t, c, "test").Body.String()
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")

	seenHelp := map[string]bool{}
	seenType := map[string]bool{}
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "# HELP "):
			name := strings.Fields(line)[2]
			if seenHelp[name] {
				t.Errorf("duplicate HELP for %s", name)
			}
			seenHelp[name] = true
		case strings.HasPrefix(line, "# TYPE "):
			name := strings.Fields(line)[2]
			if !seenHelp[name] {
				t.Errorf("TYPE before HELP for %s", name)
			}
			seenType[name] = true
		default:
			name := line
			if i := strings.IndexAny(line, "{ "); i >= 0 {
				name = line[:i]
			}
			if !seenHelp[name] || !seenType[name] {
				t.Errorf("sample %q not preceded by HELP/TYPE", line)
			}
		}
	}
	if len(seenHelp) == 0 {
		t.Fatal("no metric families emitted")
	}
}

func TestPrometheusHandlerNilCallbacks(t *testing.T) {
	c := NewCollector()

	rec := doPrometheusRequest(t, c, "0.0.1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	if strings.Contains(body, "gresbase_db_") {
		t.Errorf("db metrics should be omitted with nil DBStats:\n%s", body)
	}
	if strings.Contains(body, "gresbase_realtime_") {
		t.Errorf("realtime metrics should be omitted with nil RealtimeStats:\n%s", body)
	}

	assertLine(t, body, "gresbase_up 1")
	for _, want := range []string{
		"gresbase_uptime_seconds ",
		"gresbase_goroutines ",
		"gresbase_memory_alloc_bytes ",
		"gresbase_memory_total_alloc_bytes ",
		"gresbase_cpu_count ",
	} {
		if !strings.Contains(body, "\n"+want) {
			t.Errorf("missing %q in output:\n%s", want, body)
		}
	}
}

func TestEscapeLabelValue(t *testing.T) {
	got := escapeLabelValue("a\"b\\c\nd")
	want := `a\"b\\c\nd`
	if got != want {
		t.Errorf("escapeLabelValue = %q, want %q", got, want)
	}
}
