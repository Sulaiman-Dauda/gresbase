package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestNewRateLimiter(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		MaxRequests: 5,
		Window:      time.Second,
	})

	if rl == nil {
		t.Fatal("expected non-nil rate limiter")
	}
}

func TestRateLimiterAllow(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		MaxRequests: 3,
		Window:      100 * time.Millisecond,
	})

	key := "test-key"

	// First 3 requests should be allowed
	for i := 0; i < 3; i++ {
		if !rl.Allow(key) {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// 4th request should be denied
	if rl.Allow(key) {
		t.Error("4th request should be denied")
	}

	// Remaining should be 0
	if rl.Remaining(key) != 0 {
		t.Errorf("expected 0 remaining, got %d", rl.Remaining(key))
	}
}

func TestRateLimiterWindowReset(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		MaxRequests: 2,
		Window:      50 * time.Millisecond,
	})

	key := "window-key"

	// Use up all requests
	rl.Allow(key)
	rl.Allow(key)

	if rl.Allow(key) {
		t.Error("should be rate limited")
	}

	// Wait for window to pass
	time.Sleep(60 * time.Millisecond)

	// Should be allowed again
	if !rl.Allow(key) {
		t.Error("should be allowed after window reset")
	}
}

func TestRateLimiterDifferentKeys(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		MaxRequests: 1,
		Window:      time.Minute,
	})

	// Key 1
	if !rl.Allow("key1") {
		t.Error("key1 first request should be allowed")
	}
	if rl.Allow("key1") {
		t.Error("key1 second request should be denied")
	}

	// Key 2 (different key, should not be affected)
	if !rl.Allow("key2") {
		t.Error("key2 first request should be allowed")
	}
}

func TestRateLimiterReset(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		MaxRequests: 1,
		Window:      time.Hour,
	})

	key := "reset-key"

	rl.Allow(key)
	if rl.Allow(key) {
		t.Error("should be rate limited")
	}

	rl.Reset(key)

	if !rl.Allow(key) {
		t.Error("should be allowed after reset")
	}
}

func TestRateLimiterConcurrency(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		MaxRequests: 100,
		Window:      time.Minute,
	})

	var wg sync.WaitGroup
	allowed := make(chan bool, 200)

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed <- rl.Allow("concurrent-key")
		}()
	}

	wg.Wait()
	close(allowed)

	count := 0
	for a := range allowed {
		if a {
			count++
		}
	}

	if count > 100 {
		t.Errorf("expected at most 100 allowed, got %d", count)
	}
}

func TestRateLimitByIPMiddleware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := RateLimitByIP(2, time.Minute)
	wrapped := middleware(handler)

	// First request
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "192.0.2.1:12345"
	rec1 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec1, req1)
	if rec1.Code != 200 {
		t.Errorf("first request should succeed, got %d", rec1.Code)
	}

	// Second request
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.0.2.1:12345"
	rec2 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Errorf("second request should succeed, got %d", rec2.Code)
	}

	// Third request (should be rate limited)
	req3 := httptest.NewRequest("GET", "/test", nil)
	req3.RemoteAddr = "192.0.2.1:12345"
	rec3 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec3, req3)
	if rec3.Code != 429 {
		t.Errorf("third request should be rate limited, got %d", rec3.Code)
	}

	// Different IP should work
	req4 := httptest.NewRequest("GET", "/test", nil)
	req4.RemoteAddr = "192.0.2.2:12345"
	rec4 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec4, req4)
	if rec4.Code != 200 {
		t.Errorf("different IP should succeed, got %d", rec4.Code)
	}
}

func TestTimeoutExceptSkipsLongLivedRequests(t *testing.T) {
	mw := TimeoutExcept(10*time.Millisecond, IsLongLivedRequest)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasDeadline := r.Context().Deadline()
		writeCode := http.StatusNoContent
		if hasDeadline {
			writeCode = http.StatusAccepted
		}
		w.WriteHeader(writeCode)
	}))

	sseReq := httptest.NewRequest(http.MethodGet, "/api/v1/sse", nil)
	sseRec := httptest.NewRecorder()
	h.ServeHTTP(sseRec, sseReq)
	if sseRec.Code != http.StatusNoContent {
		t.Fatalf("expected long-lived SSE route to skip timeout deadline, got %d", sseRec.Code)
	}

	healthReq := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	healthRec := httptest.NewRecorder()
	h.ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusAccepted {
		t.Fatalf("expected regular route to carry timeout deadline, got %d", healthRec.Code)
	}
}

func TestIsLongLivedRequest(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   bool
	}{
		{http.MethodGet, "/api/v1/realtime", true},
		{http.MethodGet, "/api/v1/sse", true},
		{http.MethodPost, "/api/v1/realtime", false},
		{http.MethodGet, "/api/v1/health", false},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, nil)
		if got := IsLongLivedRequest(req); got != tt.want {
			t.Fatalf("IsLongLivedRequest(%s %s) = %v, want %v", tt.method, tt.path, got, tt.want)
		}
	}
}

func TestAuthRateLimiter(t *testing.T) {
	rl := NewAuthRateLimiter()

	// Login rate limiting
	ip := "10.0.0.1"
	email := "test@example.com"

	for i := 0; i < 10; i++ {
		if !rl.AllowLogin(ip, email) {
			t.Errorf("login %d should be allowed", i+1)
		}
	}

	// 11th login should be denied
	if rl.AllowLogin(ip, email) {
		t.Error("11th login should be denied")
	}

	// Different email, same IP
	if !rl.AllowLogin(ip, "other@example.com") {
		t.Error("different email should be allowed")
	}

	// OTP rate limiting
	for i := 0; i < 5; i++ {
		if !rl.AllowOTP(ip, email) {
			t.Errorf("OTP %d should be allowed", i+1)
		}
	}
	if rl.AllowOTP(ip, email) {
		t.Error("6th OTP should be denied")
	}

	// Register rate limiting
	for i := 0; i < 5; i++ {
		if !rl.AllowRegister(ip) {
			t.Errorf("register %d should be allowed", i+1)
		}
	}
	if rl.AllowRegister(ip) {
		t.Error("6th register should be denied")
	}
}

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		remote   string
		expected string
	}{
		{
			name:     "remote addr only",
			remote:   "192.0.2.1:12345",
			expected: "192.0.2.1:12345",
		},
		{
			name:     "X-Forwarded-For",
			headers:  map[string]string{"X-Forwarded-For": "10.0.0.1, 10.0.0.2"},
			remote:   "192.0.2.1:12345",
			expected: "10.0.0.1",
		},
		{
			name:     "X-Real-IP",
			headers:  map[string]string{"X-Real-IP": "10.0.0.3"},
			remote:   "192.0.2.1:12345",
			expected: "10.0.0.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remote
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			ip := GetClientIP(req)
			if ip != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, ip)
			}
		})
	}
}

func TestSecurityHeaders(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := SecurityHeaders(handler)

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	headers := []string{
		"X-Content-Type-Options",
		"X-Frame-Options",
		"Referrer-Policy",
		"Content-Security-Policy",
	}

	for _, h := range headers {
		if rec.Header().Get(h) == "" {
			t.Errorf("expected %s header to be set", h)
		}
	}
}

func TestCSRFMiddleware(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := CSRF(ok)

	newReq := func(method string, cookies map[string]string, header string) *http.Request {
		r := httptest.NewRequest(method, "/api/v1/collections/abc", nil)
		for k, v := range cookies {
			r.AddCookie(&http.Cookie{Name: k, Value: v})
		}
		if header != "" {
			r.Header.Set(CSRFHeaderName, header)
		}
		return r
	}

	// Safe method: always allowed.
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newReq(http.MethodGet, map[string]string{AccessCookieName: "tok", CSRFCookieName: "csrf"}, ""))
	if rr.Code != http.StatusOK {
		t.Errorf("GET should pass: got %d", rr.Code)
	}

	// Cookie-auth POST without CSRF header: rejected.
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, newReq(http.MethodPost, map[string]string{AccessCookieName: "tok", CSRFCookieName: "csrf"}, ""))
	if rr.Code != http.StatusForbidden {
		t.Errorf("POST without CSRF header should be 403: got %d", rr.Code)
	}

	// Cookie-auth POST with matching CSRF header: allowed.
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, newReq(http.MethodPost, map[string]string{AccessCookieName: "tok", CSRFCookieName: "csrf"}, "csrf"))
	if rr.Code != http.StatusOK {
		t.Errorf("POST with matching CSRF should pass: got %d", rr.Code)
	}

	// Cookie-auth POST with mismatched CSRF header: rejected.
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, newReq(http.MethodPost, map[string]string{AccessCookieName: "tok", CSRFCookieName: "csrf"}, "wrong"))
	if rr.Code != http.StatusForbidden {
		t.Errorf("POST with mismatched CSRF should be 403: got %d", rr.Code)
	}

	// Authorization-header POST: exempt even without CSRF.
	rr = httptest.NewRecorder()
	req := newReq(http.MethodPost, nil, "")
	req.Header.Set("Authorization", "Bearer xyz")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Authorization-header POST should be exempt: got %d", rr.Code)
	}

	// No auth cookie at all: nothing to protect, passes through.
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, newReq(http.MethodPost, nil, ""))
	if rr.Code != http.StatusOK {
		t.Errorf("POST without auth cookie should pass through: got %d", rr.Code)
	}

	// Exempt path (login) with cookie present: passes.
	rr = httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	loginReq.AddCookie(&http.Cookie{Name: AccessCookieName, Value: "tok"})
	h.ServeHTTP(rr, loginReq)
	if rr.Code != http.StatusOK {
		t.Errorf("exempt login path should pass: got %d", rr.Code)
	}
}
