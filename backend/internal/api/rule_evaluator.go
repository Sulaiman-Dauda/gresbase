// Package api provides the rule evaluation engine used for collection access
// rules. It resolves request macros for list/view filters and evaluates
// per-record mutation rules against request + record context.
package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gresbase/gresbase/internal/filter"
)

// RuleContext holds all the information needed to evaluate access rules.
type RuleContext struct {
	// From auth token
	IsAdmin      bool
	IsRecordAuth bool
	AdminID      string
	RecordID     string
	CollectionID string
	Role         string
	Email        string
	Verified     bool
	Anonymous    bool

	// From request
	Method string
	Query  map[string]string
	Body   map[string]any
}

// NewRuleContext extracts rule evaluation context from an HTTP request.
func NewRuleContext(r *http.Request) *RuleContext {
	return NewRuleContextFromInfo(NewRequestInfo(r))
}

// NewRuleContextFromInfo converts normalized request info into a rule context.
func NewRuleContextFromInfo(info *RequestInfo) *RuleContext {
	if info == nil {
		return &RuleContext{Query: map[string]string{}, Body: map[string]any{}}
	}

	return &RuleContext{
		IsAdmin:      info.IsAdmin,
		IsRecordAuth: info.IsRecordAuth,
		AdminID:      info.AdminID,
		RecordID:     info.RecordID,
		CollectionID: info.CollectionID,
		Role:         info.Role,
		Email:        info.Email,
		Verified:     info.Verified,
		Anonymous:    info.Anonymous,
		Method:       info.Method,
		Query:        cloneStringMap(info.Query),
		Body:         cloneAnyMap(info.Body),
	}
}

// RuleEvaluator evaluates collection access rules using the filter engine.
type RuleEvaluator struct{}

// NewRuleEvaluator creates a new rule evaluator.
func NewRuleEvaluator() *RuleEvaluator {
	return &RuleEvaluator{}
}

// EvaluateRule validates and resolves a list/view rule into a filter expression
// that can later be compiled by the query builder. It intentionally returns the
// resolved filter expression instead of raw SQL so the rest of the stack uses a
// single compilation path.
func (ev *RuleEvaluator) EvaluateRule(ctx context.Context, rule string, rc *RuleContext) (allowed bool, resolvedFilter string) {
	if strings.TrimSpace(rule) == "" {
		return true, ""
	}

	if rc != nil && rc.IsAdmin {
		return true, ""
	}

	resolved := ev.resolveMacros(rule, rc)
	if _, err := filter.ParseFilter(resolved); err != nil {
		return false, ""
	}

	return true, resolved
}

// EvaluateRuleBool evaluates a rule against the provided record/body context.
// This is used for create/update/delete/view checks where a per-record decision
// is required.
func (ev *RuleEvaluator) EvaluateRuleBool(ctx context.Context, rule string, rc *RuleContext, record map[string]any) bool {
	if strings.TrimSpace(rule) == "" {
		return true
	}

	if rc != nil && rc.IsAdmin {
		return true
	}

	expr, err := filter.ParseFilter(rule)
	if err != nil {
		return false
	}

	matched, err := filter.FilterMatchesWithResolver(expr, record, func(key string) (any, error) {
		return ev.resolveRequestValue(key, rc), nil
	})
	if err != nil {
		return false
	}

	return matched
}

// EvaluateRuleWhere resolves a list/view rule into a filter expression ready to
// be passed to the query builder.
func (ev *RuleEvaluator) EvaluateRuleWhere(ctx context.Context, rule string, rc *RuleContext) string {
	_, resolved := ev.EvaluateRule(ctx, rule, rc)
	return resolved
}

// resolveRequestValue resolves a supported @request.* macro to a concrete value.
func (ev *RuleEvaluator) resolveRequestValue(key string, rc *RuleContext) any {
	if rc == nil {
		return nil
	}

	switch key {
	case "@request.auth.id":
		if rc.IsRecordAuth && rc.RecordID != "" {
			return rc.RecordID
		}
		if rc.IsAdmin && rc.AdminID != "" {
			return rc.AdminID
		}
		return ""
	case "@request.auth.email":
		return rc.Email
	case "@request.auth.role":
		return rc.Role
	case "@request.auth.verified":
		return rc.Verified
	case "@request.auth.anonymous":
		return rc.Anonymous
	case "@request.auth.collection":
		return rc.CollectionID
	case "@request.method":
		return rc.Method
	}

	if strings.HasPrefix(key, "@request.query.") {
		return rc.Query[strings.TrimPrefix(key, "@request.query.")]
	}
	if strings.HasPrefix(key, "@request.body.") {
		return rc.Body[strings.TrimPrefix(key, "@request.body.")]
	}

	return nil
}

// resolveMacros replaces supported @request.* macros with literal filter values.
// This is used only for list/view rule compilation where the resolved result is
// re-parsed by the query builder.
func (ev *RuleEvaluator) resolveMacros(rule string, rc *RuleContext) string {
	resolved := rule
	for _, macro := range []string{
		"@request.auth.id",
		"@request.auth.email",
		"@request.auth.role",
		"@request.auth.verified",
		"@request.auth.anonymous",
		"@request.auth.collection",
		"@request.method",
	} {
		if !strings.Contains(resolved, macro) {
			continue
		}
		resolved = strings.ReplaceAll(resolved, macro, literalForFilter(ev.resolveRequestValue(macro, rc)))
	}

	for _, prefix := range []string{"@request.query.", "@request.body."} {
		for {
			start := strings.Index(resolved, prefix)
			if start == -1 {
				break
			}
			end := start + len(prefix)
			fieldEnd := end
			for fieldEnd < len(resolved) {
				c := resolved[fieldEnd]
				if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '.' {
					fieldEnd++
				} else {
					break
				}
			}
			macro := resolved[start:fieldEnd]
			resolved = resolved[:start] + literalForFilter(ev.resolveRequestValue(macro, rc)) + resolved[fieldEnd:]
		}
	}

	return resolved
}

func literalForFilter(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case bool:
		if v {
			return "true"
		}
		return "false"
	case string:
		return fmt.Sprintf("%q", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}
