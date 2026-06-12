package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiter implements a sliding window rate limiter.
type RateLimiter struct {
	mu      sync.Mutex
	windows map[string]*window
	config  RateLimitConfig
}

// RateLimitConfig holds rate limiting parameters.
type RateLimitConfig struct {
	MaxRequests int           // max requests in the window
	Window      time.Duration // sliding window duration
	ExcludeIPs  []string      // IPs/CIDRs to exclude from rate limiting
}

// window tracks requests in a sliding window.
type window struct {
	timestamps []time.Time
	hits       int
}

// NewRateLimiter creates a rate limiter with periodic cleanup.
func NewRateLimiter(cfg RateLimitConfig) *RateLimiter {
	if cfg.MaxRequests <= 0 {
		cfg.MaxRequests = 100
	}
	if cfg.Window <= 0 {
		cfg.Window = time.Minute
	}

	rl := &RateLimiter{
		windows: make(map[string]*window),
		config:  cfg,
	}

	// Periodic cleanup of expired windows
	go func() {
		ticker := time.NewTicker(cfg.Window)
		defer ticker.Stop()
		for range ticker.C {
			rl.cleanup()
		}
	}()

	return rl
}

// Allow checks if a request is allowed for the given key.
// Returns true if allowed, false if rate limited.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	w, exists := rl.windows[key]
	if !exists {
		w = &window{}
		rl.windows[key] = w
	}

	// Remove timestamps outside the window
	cutoff := now.Add(-rl.config.Window)
	var filtered []time.Time
	for _, ts := range w.timestamps {
		if ts.After(cutoff) {
			filtered = append(filtered, ts)
		}
	}
	w.timestamps = filtered
	w.hits = len(filtered)

	if w.hits >= rl.config.MaxRequests {
		return false
	}

	w.timestamps = append(w.timestamps, now)
	w.hits++
	return true
}

// Remaining returns the number of remaining requests in the current window.
func (rl *RateLimiter) Remaining(key string) int {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	w, exists := rl.windows[key]
	if !exists {
		return rl.config.MaxRequests
	}

	cutoff := time.Now().Add(-rl.config.Window)
	count := 0
	for _, ts := range w.timestamps {
		if ts.After(cutoff) {
			count++
		}
	}

	remaining := rl.config.MaxRequests - count
	if remaining < 0 {
		remaining = 0
	}
	return remaining
}

// Reset clears the rate limit for a key.
func (rl *RateLimiter) Reset(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.windows, key)
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-rl.config.Window)
	for key, w := range rl.windows {
		var filtered []time.Time
		for _, ts := range w.timestamps {
			if ts.After(cutoff) {
				filtered = append(filtered, ts)
			}
		}
		if len(filtered) == 0 {
			delete(rl.windows, key)
		} else {
			w.timestamps = filtered
			w.hits = len(filtered)
		}
	}
}

// IsExcluded checks if an IP is in the exclusion list.
func (rl *RateLimiter) IsExcluded(ip string) bool {
	for _, excluded := range rl.config.ExcludeIPs {
		if excluded == ip {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// HTTP Middleware
// ---------------------------------------------------------------------------

// RateLimitByIP creates a middleware that rate limits by client IP.
func RateLimitByIP(maxRequests int, window time.Duration) func(http.Handler) http.Handler {
	limiter := NewRateLimiter(RateLimitConfig{
		MaxRequests: maxRequests,
		Window:      window,
	})

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := GetClientIP(r)
			key := "ip:" + ip

			if !limiter.Allow(key) {
				w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", maxRequests))
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(window).Unix()))
				w.Header().Set("Retry-After", fmt.Sprintf("%.0f", window.Seconds()))
				http.Error(w, `{"message":"rate limit exceeded","status":429}`, http.StatusTooManyRequests)
				return
			}

			remaining := limiter.Remaining(key)
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", maxRequests))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(window).Unix()))

			next.ServeHTTP(w, r)
		})
	}
}

// RateLimitByKey creates a middleware that rate limits by a custom key extractor.
func RateLimitByKey(maxRequests int, window time.Duration, keyFunc func(r *http.Request) string) func(http.Handler) http.Handler {
	limiter := NewRateLimiter(RateLimitConfig{
		MaxRequests: maxRequests,
		Window:      window,
	})

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFunc(r)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}

			if !limiter.Allow(key) {
				w.Header().Set("Retry-After", fmt.Sprintf("%.0f", window.Seconds()))
				http.Error(w, `{"message":"rate limit exceeded","status":429}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// GetClientIP extracts the real client IP from the request.
func GetClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			if ip := strings.TrimSpace(parts[0]); ip != "" {
				return ip
			}
		}
	}

	// Check X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to remote address
	return r.RemoteAddr
}

// ---------------------------------------------------------------------------
// Hardened auth rate limiter
// ---------------------------------------------------------------------------

// AuthRateLimiter provides specialized rate limiting for auth endpoints.
type AuthRateLimiter struct {
	loginLimiter    *RateLimiter
	registerLimiter *RateLimiter
	otpLimiter      *RateLimiter
	refreshLimiter  *RateLimiter
}

// NewAuthRateLimiter creates rate limiters for auth endpoints.
func NewAuthRateLimiter() *AuthRateLimiter {
	return &AuthRateLimiter{
		loginLimiter:    NewRateLimiter(RateLimitConfig{MaxRequests: 10, Window: time.Minute}),
		registerLimiter: NewRateLimiter(RateLimitConfig{MaxRequests: 5, Window: time.Minute}),
		otpLimiter:      NewRateLimiter(RateLimitConfig{MaxRequests: 5, Window: time.Minute}),
		refreshLimiter:  NewRateLimiter(RateLimitConfig{MaxRequests: 30, Window: time.Minute}),
	}
}

// AllowLogin checks login rate limit.
func (l *AuthRateLimiter) AllowLogin(ip, email string) bool {
	return l.loginLimiter.Allow("login:" + ip + ":" + hash(email))
}

// AllowRegister checks registration rate limit.
func (l *AuthRateLimiter) AllowRegister(ip string) bool {
	return l.registerLimiter.Allow("register:" + ip)
}

// AllowOTP checks OTP request rate limit.
func (l *AuthRateLimiter) AllowOTP(ip, email string) bool {
	return l.otpLimiter.Allow("otp:" + ip + ":" + hash(email))
}

// AllowRefresh checks token refresh rate limit.
func (l *AuthRateLimiter) AllowRefresh(ip string) bool {
	return l.refreshLimiter.Allow("refresh:" + ip)
}

func hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:16]
}

// Ensure unused import compiles
var _ = fmt.Sprintf
