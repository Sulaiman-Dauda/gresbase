package events

import "time"

// -------------------------------------------------------------------
// App-level events
// -------------------------------------------------------------------

// BootstrapEvent is fired when the application bootstraps.
type BootstrapEvent struct {
	BaseEvent
	App interface{}
}

// TerminateEvent is fired when the application is terminating.
type TerminateEvent struct {
	BaseEvent
	App       interface{}
	IsRestart bool
}

// ServeEvent is fired when the HTTP server starts.
type ServeEvent struct {
	BaseEvent
	App   interface{}
	Addr  string
}

// -------------------------------------------------------------------
// Model events
// -------------------------------------------------------------------

// ModelEventType represents the type of model event.
type ModelEventType string

const (
	ModelEventCreate   ModelEventType = "create"
	ModelEventUpdate   ModelEventType = "update"
	ModelEventDelete   ModelEventType = "delete"
	ModelEventValidate ModelEventType = "validate"
)

// ModelEvent is fired for CRUD operations on models.
type ModelEvent struct {
	BaseEvent
	App       interface{}
	Model     interface{}
	TableName string
	Type      ModelEventType
}

// ModelErrorEvent is fired when a model operation fails.
type ModelErrorEvent struct {
	BaseEvent
	App       interface{}
	Model     interface{}
	TableName string
	Type      ModelEventType
	Error     error
}

// -------------------------------------------------------------------
// Record events
// -------------------------------------------------------------------

// RecordEvent is fired for CRUD operations on records.
type RecordEvent struct {
	BaseEvent
	App          interface{}
	Record       map[string]any
	CollectionID string
	RecordID     string
	Type         ModelEventType
}

// RecordErrorEvent is fired when a record operation fails.
type RecordErrorEvent struct {
	BaseEvent
	App          interface{}
	Record       map[string]any
	CollectionID string
	RecordID     string
	Type         ModelEventType
	Error        error
}

// -------------------------------------------------------------------
// Auth events
// -------------------------------------------------------------------

// AuthEvent is fired for authentication operations.
type AuthEvent struct {
	BaseEvent
	App          interface{}
	UserID       string
	Provider     string // "password", "oauth", "otp", "magiclink"
	Token        string
	RefreshToken string
}

// -------------------------------------------------------------------
// Realtime events
// -------------------------------------------------------------------

// RealtimeConnectEvent is fired when a client connects to the realtime service.
type RealtimeConnectEvent struct {
	BaseEvent
	App       interface{}
	ClientID  string
}

// RealtimeMessageEvent is fired when a realtime message is received.
type RealtimeMessageEvent struct {
	BaseEvent
	App       interface{}
	ClientID  string
	Channel   string
	Event     string
	Payload   []byte
}

// RealtimeSubscribeEvent is fired when a client subscribes to a channel.
type RealtimeSubscribeEvent struct {
	BaseEvent
	App        interface{}
	ClientID   string
	Channel    string
}

// -------------------------------------------------------------------
// File events
// -------------------------------------------------------------------

// FileUploadEvent is fired when a file is uploaded.
type FileUploadEvent struct {
	BaseEvent
	App       interface{}
	Filename  string
	Size      int64
	MimeType  string
	Path      string
}

// FileDeleteEvent is fired when a file is deleted.
type FileDeleteEvent struct {
	BaseEvent
	App       interface{}
	Filename  string
	Path      string
}

// -------------------------------------------------------------------
// Certificate events
// -------------------------------------------------------------------

// CertIssueEvent is fired when a certificate is issued.
type CertIssueEvent struct {
	BaseEvent
	App      interface{}
	Domain   string
	NotAfter time.Time
}

// CertRenewalEvent is fired when a certificate is renewed.
type CertRenewalEvent struct {
	BaseEvent
	App      interface{}
	Domain   string
	NotAfter time.Time
}

// -------------------------------------------------------------------
// Collection events
// -------------------------------------------------------------------

// CollectionEvent is fired when a collection is modified.
type CollectionEvent struct {
	BaseEvent
	App          interface{}
	CollectionID string
	Type         ModelEventType
}
