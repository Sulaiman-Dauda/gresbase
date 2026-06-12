package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gresbase/gresbase/internal/filter"
)

// rulePreset describes a ready-to-use access rule for a collection. The catalog
// exists to attack the single worst footgun of policy-based access control:
// having to hand-author expressions with no idea whether they parse, what they
// resolve to, or who they let through.
type rulePreset struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	// Value is the rule expression. A nil pointer represents a locked rule
	// (null), an empty string represents a public rule, and any other string is
	// a filtered rule.
	Value *string `json:"value"`
	Kind  string  `json:"kind"` // "locked" | "public" | "filtered"
}

// strPtr returns a pointer to the given string. Used so preset values can
// distinguish "" (public) from null (locked).
func strPtr(s string) *string { return &s }

// rulePresetCatalog returns the static list of rule presets surfaced in the UI.
func rulePresetCatalog() []rulePreset {
	return []rulePreset{
		{
			Key:         "locked",
			Label:       "Locked (superusers only)",
			Description: "Only superusers (admins) can perform this action. Everyone else is denied. This is the safest default.",
			Value:       nil,
			Kind:        "locked",
		},
		{
			Key:         "public",
			Label:       "Public (anyone)",
			Description: "Anyone can perform this action, including unauthenticated requests. Use with care.",
			Value:       strPtr(""),
			Kind:        "public",
		},
		{
			Key:         "authenticated",
			Label:       "Authenticated users",
			Description: "Any signed-in auth record can perform this action. Unauthenticated requests are denied.",
			Value:       strPtr(`@request.auth.id != ""`),
			Kind:        "filtered",
		},
		{
			Key:         "owner-only",
			Label:       "Owner only",
			Description: "Only the record's owner can perform this action. Requires the collection to have an `owner` field referencing the auth record id.",
			Value:       strPtr(`owner = @request.auth.id`),
			Kind:        "filtered",
		},
		{
			Key:         "verified-only",
			Label:       "Verified users only",
			Description: "Only auth records with a verified email/account can perform this action.",
			Value:       strPtr(`@request.auth.verified = true`),
			Kind:        "filtered",
		},
		{
			Key:         "admin-or-owner",
			Label:       "Admin role or owner",
			Description: "Auth records with the `admin` role, or the record's owner, can perform this action. Requires an `owner` field on the collection.",
			Value:       strPtr(`@request.auth.role = "admin" || owner = @request.auth.id`),
			Kind:        "filtered",
		},
	}
}

// RulePresets returns the catalog of ready-to-use access rule presets.
//
// Route: GET /collections/meta/rule-presets
func (h *Handlers) RulePresets(w http.ResponseWriter, r *http.Request) {
	writeOK(w, rulePresetCatalog())
}

// ruleSimulateAuth mirrors the auth-token fields of a RuleContext for the
// simulator request body. All fields are optional and default sensibly.
type ruleSimulateAuth struct {
	IsAdmin      bool   `json:"isAdmin"`
	IsRecordAuth bool   `json:"isRecordAuth"`
	ID           string `json:"id"`
	Role         string `json:"role"`
	Email        string `json:"email"`
	Collection   string `json:"collection"`
	Verified     bool   `json:"verified"`
}

// ruleSimulateRequest is the request body for RuleSimulate. Rule is a
// json.RawMessage so we can distinguish a JSON null (locked) from an empty
// string (public) from an expression.
type ruleSimulateRequest struct {
	Rule   json.RawMessage   `json:"rule"`
	Auth   *ruleSimulateAuth `json:"auth"`
	Record map[string]any    `json:"record"`
	Body   map[string]any    `json:"body"`
	Query  map[string]string `json:"query"`
}

// RuleSimulate evaluates an access rule against a hypothetical request context
// and reports whether it parses, who it allows, and the resolved filter. This
// lets policy authors test rules before saving them, instead of discovering
// breakage in production.
//
// Route: POST /collections/meta/rule-simulate
func (h *Handlers) RuleSimulate(w http.ResponseWriter, r *http.Request) {
	var req ruleSimulateRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	auth := req.Auth
	if auth == nil {
		auth = &ruleSimulateAuth{}
	}

	// Determine the tri-state of the rule from the raw JSON.
	//   - missing or JSON null  -> locked
	//   - "" (empty string)     -> public
	//   - "<expr>"              -> filtered
	trimmed := strings.TrimSpace(string(req.Rule))
	if len(req.Rule) == 0 || trimmed == "null" {
		writeOK(w, map[string]any{
			"allowed":        auth.IsAdmin,
			"locked":         true,
			"resolvedFilter": "",
			"note":           "locked to superusers",
		})
		return
	}

	var rule string
	if err := json.Unmarshal(req.Rule, &rule); err != nil {
		writeError(w, http.StatusBadRequest, "rule must be a string or null")
		return
	}

	rc := &RuleContext{
		IsAdmin:      auth.IsAdmin,
		IsRecordAuth: auth.IsRecordAuth,
		AdminID:      auth.ID,
		RecordID:     auth.ID,
		CollectionID: auth.Collection,
		Role:         auth.Role,
		Email:        auth.Email,
		Verified:     auth.Verified,
		Method:       r.Method,
		Query:        req.Query,
		Body:         req.Body,
	}
	if rc.Query == nil {
		rc.Query = map[string]string{}
	}
	if rc.Body == nil {
		rc.Body = map[string]any{}
	}

	record := req.Record
	if record == nil {
		record = map[string]any{}
	}

	// An empty (public) rule always parses and always allows.
	if strings.TrimSpace(rule) == "" {
		writeOK(w, map[string]any{
			"valid":          true,
			"allowed":        true,
			"resolvedFilter": "",
			"locked":         false,
		})
		return
	}

	// Validate the rule expression up front so we can report a parse error even
	// for admins (EvaluateRule short-circuits admins before parsing).
	if _, err := filter.ParseFilter(rule); err != nil {
		writeOK(w, map[string]any{
			"valid": false,
			"error": err.Error(),
		})
		return
	}

	_, resolvedFilter := h.rules.EvaluateRule(r.Context(), rule, rc)
	allowed := h.rules.EvaluateRuleBool(r.Context(), rule, rc, record)

	writeOK(w, map[string]any{
		"valid":          true,
		"allowed":        allowed,
		"resolvedFilter": resolvedFilter,
		"locked":         false,
	})
}
