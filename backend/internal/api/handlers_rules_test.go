package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newRuleHandlers() *Handlers {
	return &Handlers{rules: NewRuleEvaluator()}
}

func decodeRuleResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("failed to decode response: %v (body=%s)", err, rec.Body.String())
	}
	return out
}

func simulate(t *testing.T, h *Handlers, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/collections/meta/rule-simulate", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.RuleSimulate(rec, req)
	return decodeRuleResponse(t, rec)
}

func TestRulePresets(t *testing.T) {
	h := newRuleHandlers()
	req := httptest.NewRequest(http.MethodGet, "/collections/meta/rule-presets", nil)
	rec := httptest.NewRecorder()
	h.RulePresets(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var presets []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &presets); err != nil {
		t.Fatalf("failed to decode presets: %v", err)
	}

	wantKinds := map[string]string{
		"locked":         "locked",
		"public":         "public",
		"authenticated":  "filtered",
		"owner-only":     "filtered",
		"verified-only":  "filtered",
		"admin-or-owner": "filtered",
	}

	got := map[string]map[string]any{}
	for _, p := range presets {
		key, _ := p["key"].(string)
		got[key] = p
	}

	for key, kind := range wantKinds {
		p, ok := got[key]
		if !ok {
			t.Errorf("missing preset %q", key)
			continue
		}
		if p["kind"] != kind {
			t.Errorf("preset %q: expected kind %q, got %v", key, kind, p["kind"])
		}
	}

	// locked must carry a null value, public must carry "".
	if v, ok := got["locked"]; ok {
		if v["value"] != nil {
			t.Errorf("locked preset value should be null, got %v", v["value"])
		}
	}
	if v, ok := got["public"]; ok {
		if v["value"] != "" {
			t.Errorf("public preset value should be empty string, got %v", v["value"])
		}
	}
	if v, ok := got["authenticated"]; ok {
		if v["value"] != `@request.auth.id != ""` {
			t.Errorf("authenticated preset value mismatch, got %v", v["value"])
		}
	}
}

func TestRuleSimulateLocked(t *testing.T) {
	h := newRuleHandlers()

	// Locked rule (null) denies anonymous.
	anon := simulate(t, h, `{"rule": null}`)
	if anon["locked"] != true {
		t.Errorf("expected locked=true, got %v", anon["locked"])
	}
	if anon["allowed"] != false {
		t.Errorf("expected anon allowed=false for locked rule, got %v", anon["allowed"])
	}
	if anon["resolvedFilter"] != "" {
		t.Errorf("expected empty resolvedFilter, got %v", anon["resolvedFilter"])
	}

	// Locked rule (null) allows admin.
	admin := simulate(t, h, `{"rule": null, "auth": {"isAdmin": true}}`)
	if admin["allowed"] != true {
		t.Errorf("expected admin allowed=true for locked rule, got %v", admin["allowed"])
	}
	if admin["locked"] != true {
		t.Errorf("expected locked=true for admin, got %v", admin["locked"])
	}

	// Missing rule field behaves as locked.
	missing := simulate(t, h, `{}`)
	if missing["locked"] != true {
		t.Errorf("expected missing rule to be treated as locked, got %v", missing["locked"])
	}
}

func TestRuleSimulateAuthenticated(t *testing.T) {
	h := newRuleHandlers()

	// Record-auth user is allowed.
	authed := simulate(t, h, `{"rule": "@request.auth.id != \"\"", "auth": {"isRecordAuth": true, "id": "user123"}}`)
	if authed["valid"] != true {
		t.Fatalf("expected valid=true, got %v (resp=%v)", authed["valid"], authed)
	}
	if authed["allowed"] != true {
		t.Errorf("expected record-auth user allowed=true, got %v", authed["allowed"])
	}

	// Anonymous request is denied.
	anon := simulate(t, h, `{"rule": "@request.auth.id != \"\""}`)
	if anon["valid"] != true {
		t.Fatalf("expected valid=true, got %v", anon["valid"])
	}
	if anon["allowed"] != false {
		t.Errorf("expected anon allowed=false, got %v", anon["allowed"])
	}
}

func TestRuleSimulateOwnerFilter(t *testing.T) {
	h := newRuleHandlers()

	resp := simulate(t, h, `{"rule": "owner = @request.auth.id", "auth": {"isRecordAuth": true, "id": "owner-42"}, "record": {"owner": "owner-42"}}`)
	if resp["valid"] != true {
		t.Fatalf("expected valid=true, got %v (resp=%v)", resp["valid"], resp)
	}
	rf, _ := resp["resolvedFilter"].(string)
	if rf == "" {
		t.Errorf("expected a non-empty resolvedFilter for owner rule, got %q", rf)
	}
	if !strings.Contains(rf, "owner") {
		t.Errorf("expected resolvedFilter to reference owner, got %q", rf)
	}
	if resp["allowed"] != true {
		t.Errorf("expected matching owner record allowed=true, got %v", resp["allowed"])
	}

	// A non-owner record should not be allowed.
	denied := simulate(t, h, `{"rule": "owner = @request.auth.id", "auth": {"isRecordAuth": true, "id": "owner-42"}, "record": {"owner": "someone-else"}}`)
	if denied["allowed"] != false {
		t.Errorf("expected non-owner allowed=false, got %v", denied["allowed"])
	}
}

func TestRuleSimulateInvalid(t *testing.T) {
	h := newRuleHandlers()

	resp := simulate(t, h, `{"rule": "owner = = bogus &&"}`)
	if resp["valid"] != false {
		t.Errorf("expected valid=false for malformed rule, got %v (resp=%v)", resp["valid"], resp)
	}
	if _, ok := resp["error"]; !ok {
		t.Errorf("expected an error message for invalid rule, got %v", resp)
	}
}

func TestRuleSimulatePublic(t *testing.T) {
	h := newRuleHandlers()

	resp := simulate(t, h, `{"rule": ""}`)
	if resp["valid"] != true {
		t.Errorf("expected valid=true for public rule, got %v", resp["valid"])
	}
	if resp["allowed"] != true {
		t.Errorf("expected public rule allowed=true, got %v", resp["allowed"])
	}
	if resp["locked"] != false {
		t.Errorf("expected locked=false for public rule, got %v", resp["locked"])
	}
}
