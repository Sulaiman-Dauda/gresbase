package api

import (
	"encoding/json"
	"net/http"

	apimw "github.com/gresbase/gresbase/internal/api/middleware"
	"github.com/gresbase/gresbase/internal/events"
)

// RequestInfo is a normalized representation of a request, suitable for rule
// evaluation, hooks, plugins and future request-scoped APIs.
type RequestInfo struct {
	Method       string            `json:"method"`
	Query        map[string]string `json:"query"`
	Headers      map[string]string `json:"headers"`
	Body         map[string]any    `json:"body,omitempty"`
	IsAdmin      bool              `json:"isAdmin"`
	IsRecordAuth bool              `json:"isRecordAuth"`
	AdminID      string            `json:"adminId,omitempty"`
	RecordID     string            `json:"recordId,omitempty"`
	CollectionID string            `json:"collectionId,omitempty"`
	Role         string            `json:"role,omitempty"`
	Email        string            `json:"email,omitempty"`
	Verified     bool              `json:"verified,omitempty"`
	Anonymous    bool              `json:"anonymous,omitempty"`
}

// NewRequestInfo extracts normalized request information from an HTTP request.
func NewRequestInfo(r *http.Request) *RequestInfo {
	info := &RequestInfo{
		Method:  r.Method,
		Query:   map[string]string{},
		Headers: map[string]string{},
		Body:    map[string]any{},
	}

	for key := range r.URL.Query() {
		info.Query[key] = r.URL.Query().Get(key)
	}

	for key, values := range r.Header {
		if len(values) > 0 {
			info.Headers[key] = values[0]
		}
	}

	if adminID, ok := r.Context().Value(apimw.CtxAdminID).(string); ok && adminID != "" {
		info.IsAdmin = true
		info.AdminID = adminID
	}
	if role, ok := r.Context().Value(apimw.CtxAdminRole).(string); ok {
		info.Role = role
	}
	if email, ok := r.Context().Value("email").(string); ok {
		info.Email = email
	}
	if recordID, ok := r.Context().Value("record_id").(string); ok && recordID != "" {
		info.IsRecordAuth = true
		info.RecordID = recordID
	}
	if collectionID, ok := r.Context().Value("collection_id").(string); ok {
		info.CollectionID = collectionID
	}
	if verified, ok := r.Context().Value("verified").(bool); ok {
		info.Verified = verified
	}
	if anonymous, ok := r.Context().Value("anonymous").(bool); ok {
		info.Anonymous = anonymous
	}

	return info
}

// WithBody clones info and attaches a body payload.
func (ri *RequestInfo) WithBody(body map[string]any) *RequestInfo {
	if ri == nil {
		return nil
	}
	clone := *ri
	clone.Query = cloneStringMap(ri.Query)
	clone.Headers = cloneStringMap(ri.Headers)
	clone.Body = cloneAnyMap(body)
	return &clone
}

func cloneStringMap(src map[string]string) map[string]string {
	if src == nil {
		return map[string]string{}
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func cloneAnyMap(src map[string]any) map[string]any {
	if src == nil {
		return map[string]any{}
	}
	raw, err := json.Marshal(src)
	if err != nil {
		return map[string]any{}
	}
	var dst map[string]any
	if err := json.Unmarshal(raw, &dst); err != nil {
		return map[string]any{}
	}
	return dst
}

func toEventRequestInfo(r *http.Request) *events.HTTPRequestInfo {
	return toEventRequestInfoWithBody(r, nil)
}

func toEventRequestInfoWithBody(r *http.Request, body any) *events.HTTPRequestInfo {
	info := NewRequestInfo(r)
	if info == nil {
		return nil
	}
	return &events.HTTPRequestInfo{
		Method:       info.Method,
		Path:         r.URL.Path,
		Query:        cloneStringMap(info.Query),
		Headers:      cloneStringMap(info.Headers),
		Body:         bodyToMap(body),
		IsAdmin:      info.IsAdmin,
		IsRecordAuth: info.IsRecordAuth,
		AdminID:      info.AdminID,
		RecordID:     info.RecordID,
		CollectionID: info.CollectionID,
		Role:         info.Role,
		Email:        info.Email,
		Verified:     info.Verified,
	}
}

func bodyToMap(body any) map[string]any {
	if body == nil {
		return map[string]any{}
	}
	if direct, ok := body.(map[string]any); ok {
		return cloneAnyMap(direct)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return map[string]any{}
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return map[string]any{}
	}
	return result
}
