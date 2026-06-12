package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSignFormat(t *testing.T) {
	secret := "whsec_testsecret"
	body := []byte(`{"event":"record.create","data":{"id":"abc"}}`)
	now := time.Now()

	sig := Sign(secret, now, body)

	var ts int64
	var v1 string
	if _, err := fmt.Sscanf(sig, "t=%d,v1=%s", &ts, &v1); err != nil {
		t.Fatalf("unexpected signature format %q: %v", sig, err)
	}
	if ts != now.Unix() {
		t.Errorf("timestamp mismatch: got %d, want %d", ts, now.Unix())
	}

	// Recompute the HMAC independently and verify.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d.", ts)))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	if v1 != expected {
		t.Errorf("hmac mismatch: got %s, want %s", v1, expected)
	}

	// Different secret must produce a different signature.
	if Sign("whsec_other", now, body) == sig {
		t.Error("signatures with different secrets should differ")
	}
}

func TestMatches(t *testing.T) {
	cases := []struct {
		name        string
		events      []string
		collections []string
		enabled     bool
		event       string
		collection  string
		want        bool
	}{
		{"exact event, all collections", []string{"record.create"}, nil, true, "record.create", "posts", true},
		{"wildcard event", []string{"*"}, nil, true, "collection.delete", "posts", true},
		{"event not subscribed", []string{"record.create"}, nil, true, "record.delete", "posts", false},
		{"collection match", []string{"*"}, []string{"posts", "users"}, true, "record.update", "users", true},
		{"collection mismatch", []string{"*"}, []string{"posts"}, true, "record.update", "users", false},
		{"disabled", []string{"*"}, nil, false, "record.create", "posts", false},
		{"empty events", nil, nil, true, "record.create", "posts", false},
	}

	for _, tc := range cases {
		hook := &Webhook{Events: tc.events, Collections: tc.collections, Enabled: tc.enabled}
		if got := Matches(hook, tc.event, tc.collection); got != tc.want {
			t.Errorf("%s: Matches = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestBuildBodyShape(t *testing.T) {
	body, err := BuildBody("record.create", "posts", map[string]any{"id": "r1", "title": "hello"})
	if err != nil {
		t.Fatalf("BuildBody failed: %v", err)
	}

	var envelope struct {
		Event      string         `json:"event"`
		Collection string         `json:"collection"`
		Timestamp  string         `json:"timestamp"`
		Data       map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if envelope.Event != "record.create" {
		t.Errorf("event = %q", envelope.Event)
	}
	if envelope.Collection != "posts" {
		t.Errorf("collection = %q", envelope.Collection)
	}
	if _, err := time.Parse(time.RFC3339, envelope.Timestamp); err != nil {
		t.Errorf("timestamp %q is not RFC3339: %v", envelope.Timestamp, err)
	}
	if envelope.Data["id"] != "r1" || envelope.Data["title"] != "hello" {
		t.Errorf("data mismatch: %v", envelope.Data)
	}
}

func TestValidateURL(t *testing.T) {
	valid := []string{
		"http://localhost:8080/hook",
		"https://example.com/webhooks/gresbase",
		"http://192.168.1.10/internal", // private addresses allowed by design
	}
	for _, u := range valid {
		if err := ValidateURL(u); err != nil {
			t.Errorf("ValidateURL(%q) = %v, want nil", u, err)
		}
	}

	invalid := []string{
		"ftp://example.com/hook",
		"file:///etc/passwd",
		"not a url at all%%%",
		"",
		"http://",
	}
	for _, u := range invalid {
		if err := ValidateURL(u); err == nil {
			t.Errorf("ValidateURL(%q) = nil, want error", u)
		}
	}
}

func TestGenerateAndMaskSecret(t *testing.T) {
	secret, err := generateSecret()
	if err != nil {
		t.Fatalf("generateSecret: %v", err)
	}
	if !strings.HasPrefix(secret, "whsec_") {
		t.Errorf("secret %q missing whsec_ prefix", secret)
	}
	if len(secret) != len("whsec_")+64 {
		t.Errorf("secret length = %d, want %d", len(secret), len("whsec_")+64)
	}

	masked := MaskSecret(secret)
	if strings.Contains(masked, secret[12:]) || !strings.HasSuffix(masked, "…") {
		t.Errorf("masked secret %q leaks material", masked)
	}
	if MaskSecret("short") != "…" {
		t.Error("short secrets should be fully masked")
	}
}

// TestDeliverRetryThenSucceed exercises the full retry path against a real
// HTTP server that fails twice, then accepts. No DB: deliveries are captured
// via the onDelivery hook.
func TestDeliverRetryThenSucceed(t *testing.T) {
	oldBackoff := retryBackoff
	retryBackoff = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	defer func() { retryBackoff = oldBackoff }()

	secret := "whsec_retrytest"
	var calls atomic.Int32
	var gotSig, gotEvent, gotStatic string
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		gotSig = r.Header.Get("X-Gresbase-Signature")
		gotEvent = r.Header.Get("X-Gresbase-Event")
		gotStatic = r.Header.Get("X-Custom")
		if r.Header.Get("X-Gresbase-Delivery") == "" {
			t.Error("missing X-Gresbase-Delivery header")
		}
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		gotBody = body
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := NewService(nil)
	var recorded []*Delivery
	s.onDelivery = func(d *Delivery) { recorded = append(recorded, d) }

	hook := &Webhook{
		ID:      "wh1",
		URL:     srv.URL,
		Secret:  secret,
		Events:  []string{"*"},
		Enabled: true,
		Headers: map[string]string{"X-Custom": "static-value"},
	}
	body, _ := BuildBody("record.create", "posts", map[string]any{"id": "r1"})

	last := s.deliver(context.Background(), hook, "record.create", body)

	if !last.Success {
		t.Fatalf("final delivery should succeed: %+v", last)
	}
	if len(recorded) != 3 {
		t.Fatalf("expected 3 recorded attempts, got %d", len(recorded))
	}
	for i, d := range recorded[:2] {
		if d.Success || d.StatusCode != 500 || d.Attempt != i+1 {
			t.Errorf("attempt %d: %+v", i+1, d)
		}
	}
	if recorded[2].StatusCode != 200 || recorded[2].Attempt != 3 {
		t.Errorf("final attempt: %+v", recorded[2])
	}

	if gotEvent != "record.create" {
		t.Errorf("X-Gresbase-Event = %q", gotEvent)
	}
	if gotStatic != "static-value" {
		t.Errorf("static header = %q", gotStatic)
	}

	// Verify the received signature against the received body.
	var ts int64
	var v1 string
	if _, err := fmt.Sscanf(gotSig, "t=%d,v1=%s", &ts, &v1); err != nil {
		t.Fatalf("bad signature header %q: %v", gotSig, err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d.", ts)))
	mac.Write(gotBody)
	if v1 != hex.EncodeToString(mac.Sum(nil)) {
		t.Error("signature does not verify against received body")
	}
}

// TestDeliverAllAttemptsFail asserts the dispatcher stops after maxAttempts.
func TestDeliverAllAttemptsFail(t *testing.T) {
	oldBackoff := retryBackoff
	retryBackoff = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	defer func() { retryBackoff = oldBackoff }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	s := NewService(nil)
	var recorded []*Delivery
	s.onDelivery = func(d *Delivery) { recorded = append(recorded, d) }

	hook := &Webhook{ID: "wh2", URL: srv.URL, Secret: "whsec_x", Events: []string{"*"}, Enabled: true}
	last := s.deliver(context.Background(), hook, "record.delete", []byte(`{}`))

	if last.Success {
		t.Error("delivery should have failed")
	}
	if len(recorded) != maxAttempts {
		t.Errorf("expected %d attempts, got %d", maxAttempts, len(recorded))
	}
	if last.Error == "" {
		t.Error("failed delivery should carry an error message")
	}
}
