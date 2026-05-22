// Package api provides the rule evaluation engine that properly compiles
// collection access rules into SQL WHERE clauses using the filter engine.
// This replaces the previous placeholder that only checked for admin presence.
package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gresbase/gresbase/internal/api/middleware"
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
	TenantID     string
	Verified     bool

	// From request
	Method string
	Query  map[string]string
	Body   map[string]any
}

// NewRuleContext extracts rule evaluation context from an HTTP request.
func NewRuleContext(r *http.Request) *RuleContext {
	rc := &RuleContext{
		Method: r.Method,
	}

	// Extract auth info from context
	if adminID, ok := r.Context().Value(middleware.CtxAdminID).(string); ok && adminID != "" {
		rc.IsAdmin = true
		rc.AdminID = adminID
	}
	if role, ok := r.Context().Value(middleware.CtxAdminRole).(string); ok {
		rc.Role = role
	}
	if tenantID, ok := r.Context().Value(middleware.CtxTenantID).(string); ok {
		rc.TenantID = tenantID
	}
	if email, ok := r.Context().Value("email").(string); ok {
		rc.Email = email
	}

	// Record auth context
	if recordID, ok := r.Context().Value("record_id").(string); ok {
		rc.IsRecordAuth = true
		rc.RecordID = recordID
	}
	if verified, ok := r.Context().Value("verified").(bool); ok {
		rc.Verified = verified
	}

	// Parse query parameters
	rc.Query = make(map[string]string)
	for key := range r.URL.Query() {
		rc.Query[key] = r.URL.Query().Get(key)
	}

	return rc
}

// RuleEvaluator evaluates collection access rules using the filter engine.
type RuleEvaluator struct{}

// NewRuleEvaluator creates a new rule evaluator.
func NewRuleEvaluator() *RuleEvaluator {
	return &RuleEvaluator{}
}

// EvaluateRule checks whether a request should be allowed based on the
// collection's access rule. Unlike the previous placeholder, this actually
// parses the rule expression and resolves @request.* macros.
//
// For "list" and "view" rules, this returns a SQL WHERE clause that should
// be AND-ed with the query.
//
// For "create", "update", and "delete" rules, this returns a simple boolean.
func (ev *RuleEvaluator) EvaluateRule(ctx context.Context, rule string, rc *RuleContext) (allowed bool, whereClause string) {
	if strings.TrimSpace(rule) == "" {
		return true, ""
	}

	// Superusers/admins bypass all rules
	if rc.IsAdmin {
		return true, ""
	}

	// Resolve @request.* macros in the rule string
	resolved := ev.resolveMacros(rule, rc)

	// Parse the resolved rule into a filter expression
	expr, err := filter.ParseFilter(resolved)
	if err != nil {
		// If we can't parse the rule, deny access (safe default)
		return false, ""
	}

	// For list/view rules, convert to SQL WHERE clause and allow
	// (the WHERE clause is applied to the query, filtering rows the user can see)
	sql, _, err := filter.FilterToSQL(expr, nil)
	if err != nil {
		return false, ""
	}

	return true, sql
}

// EvaluateRuleBool evaluates a rule and returns a simple true/false.
// Used for create/update/delete rules where we can't filter rows.
func (ev *RuleEvaluator) EvaluateRuleBool(ctx context.Context, rule string, rc *RuleContext) bool {
	allowed, _ := ev.EvaluateRule(ctx, rule, rc)
	return allowed
}

// EvaluateRuleWhere evaluates a list/view rule and returns a SQL WHERE clause.
func (ev *RuleEvaluator) EvaluateRuleWhere(ctx context.Context, rule string, rc *RuleContext) string {
	_, where := ev.EvaluateRule(ctx, rule, rc)
	return where
}

// resolveMacros replaces @request.* macros with actual values from the context.
//
// Supported macros:
//
//	@request.auth.id          → authenticated record ID
//	@request.auth.email       → authenticated email
//	@request.auth.role        → authenticated role
//	@request.auth.verified    → whether email is verified
//	@request.auth.collection  → auth collection ID
//	@request.method           → HTTP method
//	@request.query.xxx        → query parameter value
//
// After resolution, these become literal values suitable for filter expressions.
// Example: '@request.auth.id = owner' → '"rec_abc123" = owner'
func (ev *RuleEvaluator) resolveMacros(rule string, rc *RuleContext) string {
	resolved := rule

	// @request.auth.id
	if strings.Contains(resolved, "@request.auth.id") {
		if rc.IsRecordAuth && rc.RecordID != "" {
			resolved = strings.ReplaceAll(resolved, "@request.auth.id", fmt.Sprintf("%q", rc.RecordID))
		} else {
			// Not authenticated as a record — replace with empty string
			resolved = strings.ReplaceAll(resolved, "@request.auth.id", `""`)
		}
	}

	// @request.auth.email
	if strings.Contains(resolved, "@request.auth.email") {
		resolved = strings.ReplaceAll(resolved, "@request.auth.email", fmt.Sprintf("%q", rc.Email))
	}

	// @request.auth.role
	if strings.Contains(resolved, "@request.auth.role") {
		resolved = strings.ReplaceAll(resolved, "@request.auth.role", fmt.Sprintf("%q", rc.Role))
	}

	// @request.auth.verified
	if strings.Contains(resolved, "@request.auth.verified") {
		if rc.Verified {
			resolved = strings.ReplaceAll(resolved, "@request.auth.verified", "true")
		} else {
			resolved = strings.ReplaceAll(resolved, "@request.auth.verified", "false")
		}
	}

	// @request.auth.collection
	if strings.Contains(resolved, "@request.auth.collection") {
		resolved = strings.ReplaceAll(resolved, "@request.auth.collection", fmt.Sprintf("%q", rc.CollectionID))
	}

	// @request.method
	if strings.Contains(resolved, "@request.method") {
		resolved = strings.ReplaceAll(resolved, "@request.method", fmt.Sprintf("%q", rc.Method))
	}

	// @request.query.xxx
	for {
		start := strings.Index(resolved, "@request.query.")
		if start == -1 {
			break
		}
		end := start + len("@request.query.")
		// Find the end of the macro name (alphanumeric + underscore + dot)
		fieldEnd := end
		for fieldEnd < len(resolved) {
			c := resolved[fieldEnd]
			if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '.' {
				fieldEnd++
			} else {
				break
			}
		}
		fieldName := resolved[end:fieldEnd]
		val, ok := rc.Query[fieldName]
		if !ok {
			val = ""
		}
		resolved = resolved[:start] + fmt.Sprintf("%q", val) + resolved[fieldEnd:]
	}

	return resolved
}
