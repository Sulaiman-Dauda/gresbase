// Package ctxkeys defines the typed context keys shared across the HTTP
// middleware, request handlers, and the realtime hub.
//
// These were historically plain string keys. A dedicated unexported key type
// avoids the collision risk that staticcheck's SA1029 warns about while keeping
// the keys usable from packages that must not import one another (notably the
// realtime hub, which is a leaf package and cannot import the api/middleware
// packages without creating an import cycle).
package ctxkeys

// Key is the type for all request-scoped context keys in Gresbase.
type Key string

// Admin authentication context keys.
const (
	AdminID         Key = "admin_id"
	AdminRole       Key = "admin_role"
	AdminEmail      Key = "admin_email"
	RequestID       Key = "request_id"
	AdminAuthMethod Key = "admin_auth_method"
	APIKeyID        Key = "api_key_id"
	APIKeyPerms     Key = "api_key_permissions" //nolint:gosec // G101 false positive: context-key name, not a credential.
)

// Record (end-user) authentication context keys.
const (
	RecordID     Key = "record_id"
	CollectionID Key = "collection_id"
	Email        Key = "email"
	Verified     Key = "verified"
	Anonymous    Key = "anonymous"
)
