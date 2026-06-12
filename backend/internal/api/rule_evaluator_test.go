package api

import "testing"

func TestRuleEvaluatorEvaluateRuleBool(t *testing.T) {
	ev := NewRuleEvaluator()
	rc := &RuleContext{
		Role:         "admin",
		Email:        "owner@example.com",
		Method:       "POST",
		Query:        map[string]string{"status": "draft"},
		Body:         map[string]any{"title": "Hello world"},
		IsRecordAuth: true,
		RecordID:     "rec_123",
	}

	record := map[string]any{
		"owner":  "rec_123",
		"status": "draft",
	}

	if !ev.EvaluateRuleBool(nil, `owner = @request.auth.id && @request.auth.role = "admin"`, rc, record) {
		t.Fatal("expected record rule to match request auth macros")
	}

	if ev.EvaluateRuleBool(nil, `owner = @request.auth.id && @request.auth.role = "viewer"`, rc, record) {
		t.Fatal("expected role mismatch to deny access")
	}

	if !ev.EvaluateRuleBool(nil, `@request.body.title = "Hello world"`, rc, record) {
		t.Fatal("expected body macro rule to match")
	}
}

func TestRuleEvaluatorEvaluateRuleWhere(t *testing.T) {
	ev := NewRuleEvaluator()
	rc := &RuleContext{
		Role:         "member",
		Query:        map[string]string{"status": "published"},
		IsRecordAuth: true,
		RecordID:     "rec_456",
	}

	resolved := ev.EvaluateRuleWhere(nil, `owner = @request.auth.id && status = @request.query.status`, rc)
	if resolved != `owner = "rec_456" && status = "published"` {
		t.Fatalf("unexpected resolved filter: %s", resolved)
	}
}
