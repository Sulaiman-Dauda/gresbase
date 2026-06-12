// Package realtime provides a production-grade realtime engine with SSE (primary)
// and WebSocket (fallback) transports. It supports per-subscription access rule
// evaluation, auth state tracking, field-level picking, expand support, client-side
// filter expressions, dry caching for transactional consistency, and chunked
// message delivery.
//
// Architecture:
//
//	Hub (manages all transports)
//	 ├── SSEClient (Server-Sent Events, primary)
//	 ├── WSClient  (WebSocket, fallback)
//	 └── Registry  (subscription brokering)
//
// Matches PocketBase's realtime capabilities.
package realtime

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/gresbase/gresbase/internal/ctxkeys"
	"github.com/rs/zerolog/log"
)

// Transport type constants.
const (
	TransportSSE = "sse"
	TransportWS  = "ws"
)

const (
	// DefaultIdleTimeout is the time after which an idle connection is closed.
	DefaultIdleTimeout = 5 * time.Minute
	// DefaultMaxConnectionAge is the maximum lifetime of a single realtime
	// connection before it is closed cleanly (clients auto-reconnect).
	// Prevents zombie connections; matches PocketBase's 30 minute default.
	DefaultMaxConnectionAge = 30 * time.Minute
	// DefaultMaxConnections is the maximum number of concurrent realtime clients.
	DefaultMaxConnections = 10000
	// DefaultMaxMessageSize is the max allowed incoming message size.
	DefaultMaxMessageSize = 65536
	// DefaultMaxSubscriptionsPerRequest caps batch subscription churn from a single message.
	DefaultMaxSubscriptionsPerRequest = 128
	// DefaultMaxSubscriptionsPerClient caps the total topics a single client may hold.
	DefaultMaxSubscriptionsPerClient = 1000
	// DefaultMaxTopicLength caps individual topic names.
	DefaultMaxTopicLength = 255
	// ClientsChunkSize is the batch size for concurrent client message processing.
	ClientsChunkSize = 150
	// writeWait is how long a write can take before timing out.
	writeWait = 10 * time.Second
	// pongWait is how long we wait for a pong from the client.
	pongWait = 60 * time.Second
	// pingPeriod is how often we send pings (must be less than pongWait).
	pingPeriod = (pongWait * 9) / 10
)

// RealtimeMessage represents a message exchanged between server and clients.
type RealtimeMessage struct {
	ClientID  string          `json:"client_id,omitempty"`
	Event     string          `json:"event"`
	Channel   string          `json:"channel,omitempty"`
	Topic     string          `json:"topic,omitempty"` // full subscription topic
	Data      json.RawMessage `json:"data,omitempty"`
	Timestamp int64           `json:"timestamp"`
}

// SubscribeRequest is the payload clients send to manage subscriptions.
type SubscribeRequest struct {
	Type          string            `json:"type"` // subscribe, unsubscribe, message, presence, ping
	ClientID      string            `json:"clientId,omitempty"`
	Subscriptions []string          `json:"subscriptions,omitempty"`
	Channel       string            `json:"channel,omitempty"`
	Topic         string            `json:"topic,omitempty"`
	Event         string            `json:"event,omitempty"`
	Data          json.RawMessage   `json:"data,omitempty"`
	Query         map[string]string `json:"query,omitempty"` // ?fields, ?expand, ?filter
	Options       *SubscribeOptions `json:"options,omitempty"`
}

// subscriptionFromRequest builds a Subscription for one topic, merging the
// legacy query map with the structured options object (options win).
func subscriptionFromRequest(topic string, req *SubscribeRequest) *Subscription {
	opts := &SubscribeOptions{
		Filter: req.Query["filter"],
		Fields: req.Query["fields"],
		Expand: req.Query["expand"],
	}
	if req.Options != nil {
		if req.Options.Filter != "" {
			opts.Filter = req.Options.Filter
		}
		if req.Options.Fields != "" {
			opts.Fields = req.Options.Fields
		}
		if req.Options.Expand != "" {
			opts.Expand = req.Options.Expand
		}
		opts.Presence = req.Options.Presence
	}
	return &Subscription{Topic: topic, Query: req.Query, Options: opts}
}

// ---------------------------------------------------------------------------
// Client interface - shared between SSE and WebSocket
// ---------------------------------------------------------------------------

// Client represents any connected realtime client (SSE or WebSocket).
type Client interface {
	// ID returns the unique client identifier.
	ID() string
	// Transport returns the transport type (sse or ws).
	Transport() string
	// ConnectedAt returns when the client connected.
	ConnectedAt() time.Time
	// LastSeen returns when the client was last active.
	LastSeen() time.Time
	// Touch updates the last seen timestamp.
	Touch()
	// Subscriptions returns a copy of the client's subscription topics.
	Subscriptions() map[string]*Subscription
	// Subscribe adds a subscription topic.
	Subscribe(topic string, sub *Subscription)
	// Unsubscribe removes a subscription topic.
	Unsubscribe(topic string)
	// UnsubscribeAll removes all subscriptions.
	UnsubscribeAll()
	// Set sets a key-value pair on the client's store.
	Set(key string, value any)
	// Get retrieves a stored value.
	Get(key string) (any, bool)
	// Unset removes a stored value.
	Unset(key string)
	// Send delivers a message to this client.
	Send(msg *RealtimeMessage) error
	// SendRaw delivers raw bytes.
	SendRaw(data []byte) error
	// Close closes the client connection.
	Close()
	// IsClosed returns whether the client is closed.
	IsClosed() bool
	// AuthRecord returns the auth record associated with this client (if any).
	AuthRecord() *AuthInfo
	// SetAuthRecord sets the auth record.
	SetAuthRecord(auth *AuthInfo)
}

// AuthInfo holds authentication information tracked per realtime client.
type AuthInfo struct {
	AdminID    string `json:"admin_id"`
	Email      string `json:"email"`
	Role       string `json:"role"`
	Collection string `json:"collection,omitempty"`
	RecordID   string `json:"record_id,omitempty"`
	Verified   bool   `json:"verified,omitempty"`
	Anonymous  bool   `json:"anonymous,omitempty"`
}

// IsAdmin reports whether the auth state belongs to an admin (superuser).
func (a *AuthInfo) IsAdmin() bool {
	return a != nil && a.AdminID != ""
}

// Subscription represents a client's subscription to a topic.
type Subscription struct {
	Topic   string            `json:"topic"`
	Query   map[string]string `json:"query,omitempty"` // filter, fields, expand
	Options *SubscribeOptions `json:"options,omitempty"`
}

// SubscribeOptions holds parsed subscription preferences.
type SubscribeOptions struct {
	Filter string `json:"filter,omitempty"`
	Fields string `json:"fields,omitempty"`
	Expand string `json:"expand,omitempty"`
	// Presence is arbitrary client-declared state for this subscription. When
	// set, the hub announces presence:join/presence:leave on the topic and
	// includes this client in presence member listings.
	Presence map[string]any `json:"presence,omitempty"`
}

// PresenceMember is one entry in a topic's presence listing.
type PresenceMember struct {
	ClientID string         `json:"client_id"`
	State    map[string]any `json:"state"`
}

// ---------------------------------------------------------------------------
// baseClient provides common client functionality
// ---------------------------------------------------------------------------

type baseClient struct {
	id          string
	transport   string
	connectedAt time.Time
	lastSeen    time.Time
	subs        map[string]*Subscription // topic -> subscription
	store       map[string]any
	authRecord  *AuthInfo
	closed      atomic.Bool
	mu          sync.RWMutex
}

func newBaseClient(id, transport string) *baseClient {
	now := time.Now()
	return &baseClient{
		id:          id,
		transport:   transport,
		connectedAt: now,
		lastSeen:    now,
		subs:        make(map[string]*Subscription),
		store:       make(map[string]any),
	}
}

func (c *baseClient) ID() string             { return c.id }
func (c *baseClient) Transport() string      { return c.transport }
func (c *baseClient) ConnectedAt() time.Time { return c.connectedAt }
func (c *baseClient) LastSeen() time.Time    { c.mu.RLock(); defer c.mu.RUnlock(); return c.lastSeen }
func (c *baseClient) Touch()                 { c.mu.Lock(); defer c.mu.Unlock(); c.lastSeen = time.Now() }
func (c *baseClient) IsClosed() bool         { return c.closed.Load() }

func (c *baseClient) Subscriptions() map[string]*Subscription {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]*Subscription, len(c.subs))
	for k, v := range c.subs {
		result[k] = v
	}
	return result
}

func (c *baseClient) Subscribe(topic string, sub *Subscription) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subs[topic] = sub
	c.lastSeen = time.Now()
}

func (c *baseClient) Unsubscribe(topic string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.subs, topic)
}

func (c *baseClient) UnsubscribeAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subs = make(map[string]*Subscription)
}

func (c *baseClient) Set(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if value == nil {
		delete(c.store, key)
	} else {
		c.store[key] = value
	}
}

func (c *baseClient) Get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.store[key]
	return v, ok
}

func (c *baseClient) Unset(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.store, key)
}

func (c *baseClient) AuthRecord() *AuthInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.authRecord
}

func (c *baseClient) SetAuthRecord(auth *AuthInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.authRecord = auth
}

// ---------------------------------------------------------------------------
// Hub — central realtime management
// ---------------------------------------------------------------------------

// TransactionContext represents an in-flight realtime transaction.
// Messages during the transaction are dry-cached and sent only on commit.
type TransactionContext struct {
	ID        string
	messages  []*BroadcastRequest
	mu        sync.Mutex
	committed bool
}

// RuleChecker decides whether a subscriber may receive a record event.
// It is implemented by the API layer, where collection access rules and the
// filter engine live. The hub fails closed: when no checker is set, record
// events are delivered to admin clients only.
type RuleChecker interface {
	// CanReceiveRecord reports whether a client with the given auth state may
	// receive the record event. wildcard indicates a collection-wide
	// subscription ({collection}/*, evaluated against the list rule) versus a
	// single-record subscription (evaluated against the view rule).
	// filterExpr is the subscriber's optional client-side filter expression,
	// which must also match the record for delivery.
	CanReceiveRecord(collection string, recordID string, record map[string]any, auth *AuthInfo, wildcard bool, filterExpr string) bool
}

// Hub is the central realtime management system.
// It handles all client connections, subscriptions, and message brokering.
type Hub struct {
	clients        map[string]Client          // all connected clients by ID
	topicIndex     map[string]map[string]bool // topic -> client IDs
	register       chan Client
	unregister     chan Client
	broadcast      chan *BroadcastRequest
	ruleChecker    RuleChecker
	channelGuard   func(topic string) bool
	idleTimeout    time.Duration
	maxConnAge     time.Duration
	maxConnections int
	maxMessageSize int64

	// Cluster mode: when crossNode is set, record events are published via
	// publish (a Postgres NOTIFY) and delivered on every node by the cluster
	// listener, rather than fanned out locally at broadcast time.
	crossNode bool
	publish   func(ctx context.Context, payload string) error

	// directSuppressed gates the API-emitted record event path. While the WAL
	// change-capture stream is healthy it is the single source of truth for
	// record events, so direct BroadcastRecord calls are dropped to avoid
	// double delivery. The WAL consumer flips this off the moment the stream
	// degrades, so the API path resumes automatically.
	directSuppressed atomic.Bool

	mu      sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
	runDone chan struct{} // closed when Run() returns
	stats   HubStats

	// Transactional dryCache support
	transactions map[string]*TransactionContext
	txMu         sync.RWMutex
	activeTx     string // current active transaction ID set via context
}

// BroadcastRequest is a request to broadcast to a specific topic or channel.
type BroadcastRequest struct {
	Topic   string
	Channel string // deprecated alias for topic
	Message *RealtimeMessage
	Data    map[string]any // record data for access rule evaluation

	// Collection and RecordID identify the record event source. When
	// Collection is set, per-subscriber access rules are enforced before
	// delivery. Wildcard marks a {collection}/* topic (list-rule semantics).
	Collection string
	RecordID   string
	Wildcard   bool

	// DryCache, if true, stores messages instead of sending immediately
	// (for transactional consistency — sends on commit).
	DryCache bool
}

// HubStats holds runtime statistics.
type HubStats struct {
	TotalConnections int64 `json:"total_connections"`
	TotalTopics      int64 `json:"total_topics"`
	MessagesSent     int64 `json:"messages_sent"`
	MessagesDropped  int64 `json:"messages_dropped"`
}

// NewHub creates a new realtime Hub.
func NewHub() *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	return &Hub{
		clients:        make(map[string]Client),
		topicIndex:     make(map[string]map[string]bool),
		register:       make(chan Client, 256),
		unregister:     make(chan Client, 256),
		broadcast:      make(chan *BroadcastRequest, 1024),
		idleTimeout:    DefaultIdleTimeout,
		maxConnAge:     DefaultMaxConnectionAge,
		maxConnections: DefaultMaxConnections,
		maxMessageSize: DefaultMaxMessageSize,
		ctx:            ctx,
		cancel:         cancel,
		runDone:        make(chan struct{}),
	}
}

// SetRuleChecker sets the access rule evaluation callback.
func (h *Hub) SetRuleChecker(rc RuleChecker) {
	h.ruleChecker = rc
}

// SetChannelGuard sets a callback deciding whether clients may publish to a
// topic. Used to keep client broadcasts off collection record topics so they
// cannot spoof record events.
func (h *Hub) SetChannelGuard(guard func(topic string) bool) {
	h.channelGuard = guard
}

// CanPublishToChannel reports whether client publishes to the topic are
// allowed. Without a guard configured it fails closed.
func (h *Hub) CanPublishToChannel(topic string) bool {
	if h.channelGuard == nil {
		return false
	}
	return h.channelGuard(topic)
}

// BroadcastChannel delivers a client/broadcast message to a custom channel.
// In cluster mode the message travels over Postgres LISTEN/NOTIFY so every
// node fans it out; otherwise it is delivered locally.
func (h *Hub) BroadcastChannel(topic string, msg *RealtimeMessage) {
	h.mu.RLock()
	crossNode := h.crossNode
	h.mu.RUnlock()
	if crossNode {
		h.publishChannelEvent(topic, msg)
		return
	}
	h.Broadcast(&BroadcastRequest{Topic: topic, Message: msg})
}

// IsReservedEvent reports whether an event name is reserved for
// server-generated messages and may not be published by clients.
func IsReservedEvent(event string) bool { return isReservedEvent(event) }

// SetIdleTimeout sets the connection idle timeout.
func (h *Hub) SetIdleTimeout(d time.Duration) {
	h.idleTimeout = d
}

// IdleTimeout returns the configured idle timeout.
func (h *Hub) IdleTimeout() time.Duration {
	return h.idleTimeout
}

// SetMaxConnectionAge caps the lifetime of a single realtime connection.
// Connections older than the cap are closed cleanly and clients auto-reconnect.
// d <= 0 disables the cap.
func (h *Hub) SetMaxConnectionAge(d time.Duration) {
	if d < 0 {
		d = 0
	}
	h.maxConnAge = d
}

// MaxConnectionAge returns the configured connection lifetime cap (0 = disabled).
func (h *Hub) MaxConnectionAge() time.Duration {
	return h.maxConnAge
}

// maxConnAgeChan returns a channel firing when the connection age cap is
// reached, or a nil channel (never fires) when the cap is disabled. The
// returned stop func must be called to release the timer.
func (h *Hub) maxConnAgeChan() (<-chan time.Time, func()) {
	if h.maxConnAge <= 0 {
		return nil, func() {}
	}
	timer := time.NewTimer(h.maxConnAge)
	return timer.C, func() { timer.Stop() }
}

// SetMaxConnections sets the maximum number of concurrent realtime clients.
func (h *Hub) SetMaxConnections(limit int) {
	h.maxConnections = limit
}

// MaxConnections returns the configured realtime client limit.
func (h *Hub) MaxConnections() int {
	return h.maxConnections
}

// SetMaxMessageSize sets the maximum inbound realtime message size.
func (h *Hub) SetMaxMessageSize(limit int64) {
	if limit <= 0 {
		limit = DefaultMaxMessageSize
	}
	h.maxMessageSize = limit
}

// MaxMessageSize returns the configured realtime message size limit.
func (h *Hub) MaxMessageSize() int64 {
	if h.maxMessageSize <= 0 {
		return DefaultMaxMessageSize
	}
	return h.maxMessageSize
}

func (h *Hub) canAcceptConnection() bool {
	if h.maxConnections <= 0 {
		return true
	}
	return h.ClientCount() < h.maxConnections
}

func authInfoFromContext(ctx context.Context) *AuthInfo {
	if ctx == nil {
		return nil
	}
	if adminID, _ := ctx.Value(ctxkeys.AdminID).(string); adminID != "" {
		email, _ := ctx.Value(ctxkeys.AdminEmail).(string)
		role, _ := ctx.Value(ctxkeys.AdminRole).(string)
		return &AuthInfo{AdminID: adminID, Email: email, Role: role, Verified: true}
	}
	if recordID, _ := ctx.Value(ctxkeys.RecordID).(string); recordID != "" {
		collectionID, _ := ctx.Value(ctxkeys.CollectionID).(string)
		email, _ := ctx.Value(ctxkeys.Email).(string)
		verified, _ := ctx.Value(ctxkeys.Verified).(bool)
		anonymous, _ := ctx.Value(ctxkeys.Anonymous).(bool)
		return &AuthInfo{RecordID: recordID, Collection: collectionID, Email: email, Verified: verified, Anonymous: anonymous}
	}
	return nil
}

// isReservedEvent reports whether an event name is reserved for
// server-generated messages and may not be published by clients.
func isReservedEvent(event string) bool {
	lower := strings.ToLower(event)
	for _, prefix := range []string{"record:", "connection:", "subscription:", "presence"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func decodeSubscribeRequest(data []byte, requireClientID bool) (*SubscribeRequest, error) {
	var req SubscribeRequest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("unexpected trailing data")
		}
		return nil, err
	}
	if err := validateSubscribeRequest(&req, requireClientID); err != nil {
		return nil, err
	}
	return &req, nil
}

func validateSubscribeRequest(req *SubscribeRequest, requireClientID bool) error {
	if req == nil {
		return fmt.Errorf("missing request")
	}
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	req.ClientID = strings.TrimSpace(req.ClientID)
	req.Topic = strings.TrimSpace(req.Topic)
	req.Channel = strings.TrimSpace(req.Channel)
	req.Event = strings.TrimSpace(req.Event)
	req.Subscriptions = normalizeSubscriptions(req.Subscriptions)

	if requireClientID && req.ClientID == "" {
		return fmt.Errorf("clientId is required")
	}
	if len(req.Subscriptions) > DefaultMaxSubscriptionsPerRequest {
		return fmt.Errorf("too many subscriptions")
	}
	if len(req.Topic) > DefaultMaxTopicLength || len(req.Channel) > DefaultMaxTopicLength || strings.ContainsAny(req.Topic+req.Channel, "\r\n") {
		return fmt.Errorf("invalid topic or channel")
	}

	switch req.Type {
	case "subscribe", "unsubscribe":
		if len(req.Subscriptions) == 0 {
			return fmt.Errorf("subscriptions are required")
		}
	case "message":
		if req.Topic == "" && req.Channel == "" {
			return fmt.Errorf("topic or channel is required")
		}
	case "presence":
		if req.Topic == "" && req.Channel == "" {
			return fmt.Errorf("topic or channel is required")
		}
	case "ping":
		return nil
	default:
		return fmt.Errorf("unsupported request type")
	}

	return nil
}

func normalizeSubscriptions(subscriptions []string) []string {
	if len(subscriptions) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(subscriptions))
	result := make([]string, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		subscription = strings.TrimSpace(subscription)
		if subscription == "" || len(subscription) > DefaultMaxTopicLength || strings.ContainsAny(subscription, "\r\n") {
			continue
		}
		if _, exists := seen[subscription]; exists {
			continue
		}
		seen[subscription] = struct{}{}
		result = append(result, subscription)
	}
	return result
}

// Run starts the hub event loop. Must be called in a goroutine.
func (h *Hub) Run() {
	// Signal Shutdown() once the loop has actually exited, so the goroutine's
	// lifecycle is deterministic and it cannot keep logging/working after
	// Shutdown returns.
	defer close(h.runDone)

	// Start stale connection cleanup ticker
	cleanupTicker := time.NewTicker(30 * time.Second)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-h.ctx.Done():
			return

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.ID()] = client
			clientCount := len(h.clients)
			h.mu.Unlock()

			log.Debug().
				Str("client_id", client.ID()).
				Str("transport", client.Transport()).
				Int("total_clients", clientCount).
				Msg("Realtime client connected")

		case client := <-h.unregister:
			subs := client.Subscriptions()
			h.mu.Lock()
			// Remove from topic index
			for topic := range subs {
				h.removeFromTopicIndex(client.ID(), topic)
			}
			delete(h.clients, client.ID())
			clientCount := len(h.clients)
			h.mu.Unlock()

			// Announce departure on presence-enabled subscriptions.
			for topic, sub := range subs {
				if sub != nil && sub.Options != nil && sub.Options.Presence != nil {
					h.emitPresence("presence:leave", topic, client.ID(), sub.Options.Presence)
				}
			}

			log.Debug().
				Str("client_id", client.ID()).
				Int("total_clients", clientCount).
				Msg("Realtime client disconnected")

		case req := <-h.broadcast:
			h.handleBroadcast(req)

		case <-cleanupTicker.C:
			h.cleanupStale()
		}
	}
}

// Register adds a client to the hub.
func (h *Hub) Register(client Client) {
	select {
	case h.register <- client:
	case <-h.ctx.Done():
	}
}

// Unregister removes a client from the hub.
func (h *Hub) Unregister(client Client) {
	select {
	case h.unregister <- client:
	case <-h.ctx.Done():
	}
}

// Broadcast queues a broadcast request. If a transaction is active,
// messages are dry-cached and sent only on commit.
func (h *Hub) Broadcast(req *BroadcastRequest) {
	// Check for active transaction
	h.txMu.RLock()
	activeTx := h.activeTx
	if activeTx != "" {
		if tx, ok := h.transactions[activeTx]; ok && !tx.committed {
			h.txMu.RUnlock()
			req.DryCache = true
			tx.mu.Lock()
			tx.messages = append(tx.messages, req)
			tx.mu.Unlock()
			return
		}
	}
	h.txMu.RUnlock()

	select {
	case h.broadcast <- req:
	default:
		atomic.AddInt64(&h.stats.MessagesDropped, 1)
	}
}

// BroadcastTopic sends a message to all clients subscribed to a topic.
func (h *Hub) BroadcastTopic(topic string, msg *RealtimeMessage) {
	h.Broadcast(&BroadcastRequest{Topic: topic, Message: msg})
}

// BroadcastRecord sends a record change event to the appropriate topics.
// This is the main entry point for collection record create/update/delete events.
//
// In single-node mode it fans out to local subscribers immediately. In cluster
// mode it publishes the event over Postgres LISTEN/NOTIFY; the cluster listener
// on every node (including this one) then performs the local fan-out, so the
// app tier scales horizontally with no extra infrastructure.
func (h *Hub) BroadcastRecord(action string, collectionName string, recordID string, data map[string]any) {
	// While WAL change capture is healthy it is the single source of truth
	// for record events; the same mutation will arrive via the WAL stream.
	if h.directSuppressed.Load() {
		return
	}
	h.mu.RLock()
	crossNode := h.crossNode
	h.mu.RUnlock()
	if crossNode {
		h.publishRecordEvent(action, collectionName, recordID, data)
		return
	}
	h.localBroadcastRecord(action, collectionName, recordID, data)
}

// SetDirectRecordBroadcastsSuppressed toggles the API-emitted record event
// path. The WAL change-capture consumer suppresses direct broadcasts only
// while its stream is healthy; any stream failure re-enables them so events
// keep flowing (degraded to API-emitted coverage) instead of going silent.
func (h *Hub) SetDirectRecordBroadcastsSuppressed(suppressed bool) {
	h.directSuppressed.Store(suppressed)
}

// DirectRecordBroadcastsSuppressed reports whether direct record broadcasts
// are currently dropped in favor of the WAL stream.
func (h *Hub) DirectRecordBroadcastsSuppressed() bool {
	return h.directSuppressed.Load()
}

// BroadcastRecordFromWAL delivers a record event sourced from the WAL
// change-capture stream. It bypasses the direct-broadcast suppression gate
// (it IS the WAL path) and always fans out locally: with one replication
// slot per node, every node receives every change from its own stream, so
// cross-node publishing would duplicate events.
func (h *Hub) BroadcastRecordFromWAL(action string, collectionName string, recordID string, data map[string]any) {
	h.localBroadcastRecord(action, collectionName, recordID, data)
}

// localBroadcastRecord fans a record event out to this node's local
// subscribers. It is the delivery path for both single-node mode and the
// cluster listener.
func (h *Hub) localBroadcastRecord(action string, collectionName string, recordID string, data map[string]any) {
	// Generate topics that match PocketBase format:
	// "{collection_name}/{record_id}" for specific record
	// "{collection_name}/*" for any record in collection

	specificTopic := fmt.Sprintf("%s/%s", collectionName, recordID)
	wildcardTopic := fmt.Sprintf("%s/*", collectionName)

	dataBytes, _ := json.Marshal(map[string]any{
		"action": action,
		"record": data,
	})

	msg := &RealtimeMessage{
		Event:     fmt.Sprintf("record:%s", action),
		Data:      dataBytes,
		Timestamp: time.Now().UnixMilli(),
	}

	// Send to specific record subscribers (view-rule semantics)
	h.Broadcast(&BroadcastRequest{
		Topic:      specificTopic,
		Message:    msg,
		Data:       data,
		Collection: collectionName,
		RecordID:   recordID,
	})

	// Send to wildcard subscribers (list-rule semantics)
	h.Broadcast(&BroadcastRequest{
		Topic:      wildcardTopic,
		Message:    msg,
		Data:       data,
		Collection: collectionName,
		RecordID:   recordID,
		Wildcard:   true,
	})
}

// SubscribeClient subscribes a client to a topic with options. It returns an
// error when the client already holds the maximum number of subscriptions.
func (h *Hub) SubscribeClient(client Client, topic string, sub *Subscription) error {
	subs := client.Subscriptions()
	if _, exists := subs[topic]; !exists && len(subs) >= DefaultMaxSubscriptionsPerClient {
		return fmt.Errorf("subscription limit of %d topics reached", DefaultMaxSubscriptionsPerClient)
	}

	client.Subscribe(topic, sub)

	h.mu.Lock()
	if h.topicIndex[topic] == nil {
		h.topicIndex[topic] = make(map[string]bool)
	}
	h.topicIndex[topic][client.ID()] = true
	h.mu.Unlock()

	if sub != nil && sub.Options != nil && sub.Options.Presence != nil {
		h.emitPresence("presence:join", topic, client.ID(), sub.Options.Presence)
	}

	log.Debug().
		Str("client_id", client.ID()).
		Str("topic", topic).
		Msg("Client subscribed to topic")
	return nil
}

// emitPresence announces a presence change to a topic's subscribers.
func (h *Hub) emitPresence(event, topic, clientID string, state map[string]any) {
	data, _ := json.Marshal(map[string]any{"client_id": clientID, "state": state})
	h.Broadcast(&BroadcastRequest{Topic: topic, Message: &RealtimeMessage{
		Event:     event,
		ClientID:  clientID,
		Topic:     topic,
		Data:      data,
		Timestamp: time.Now().UnixMilli(),
	}})
}

// presenceState returns the presence state a client declared for a topic, or
// nil when the subscription did not opt into presence.
func presenceState(client Client, topic string) map[string]any {
	if sub, ok := client.Subscriptions()[topic]; ok && sub != nil && sub.Options != nil {
		return sub.Options.Presence
	}
	return nil
}

// PresenceMembers lists the presence-enabled subscribers of a topic on this node.
func (h *Hub) PresenceMembers(topic string) []PresenceMember {
	h.mu.RLock()
	ids := make([]string, 0)
	if clients, ok := h.topicIndex[topic]; ok {
		for id := range clients {
			ids = append(ids, id)
		}
	}
	h.mu.RUnlock()

	members := []PresenceMember{}
	for _, id := range ids {
		client := h.GetClient(id)
		if client == nil {
			continue
		}
		if state := presenceState(client, topic); state != nil {
			members = append(members, PresenceMember{ClientID: id, State: state})
		}
	}
	return members
}

// UnsubscribeClient unsubscribes a client from a topic.
func (h *Hub) UnsubscribeClient(client Client, topic string) {
	state := presenceState(client, topic)
	client.Unsubscribe(topic)
	h.mu.Lock()
	h.removeFromTopicIndex(client.ID(), topic)
	h.mu.Unlock()
	if state != nil {
		h.emitPresence("presence:leave", topic, client.ID(), state)
	}
}

// UnsubscribeAll removes all of a client's subscriptions.
func (h *Hub) UnsubscribeAll(client Client) {
	subs := client.Subscriptions()
	client.UnsubscribeAll()

	h.mu.Lock()
	for topic := range subs {
		h.removeFromTopicIndex(client.ID(), topic)
	}
	h.mu.Unlock()

	for topic, sub := range subs {
		if sub != nil && sub.Options != nil && sub.Options.Presence != nil {
			h.emitPresence("presence:leave", topic, client.ID(), sub.Options.Presence)
		}
	}
}

// TopicSubscriberCount returns how many clients are subscribed to a topic.
func (h *Hub) TopicSubscriberCount(topic string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if clients, ok := h.topicIndex[topic]; ok {
		return len(clients)
	}
	return 0
}

// Stats returns current hub statistics.
func (h *Hub) Stats() HubStats {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return HubStats{
		TotalConnections: int64(len(h.clients)),
		TotalTopics:      int64(len(h.topicIndex)),
		MessagesSent:     atomic.LoadInt64(&h.stats.MessagesSent),
		MessagesDropped:  atomic.LoadInt64(&h.stats.MessagesDropped),
	}
}

// ClientCount returns the number of connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// GetClient retrieves a client by ID.
func (h *Hub) GetClient(id string) Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.clients[id]
}

// BeginTransaction starts a transactional dryCache context.
// All subsequent Broadcast calls will be cached until CommitTransaction is called.
func (h *Hub) BeginTransaction() *TransactionContext {
	tx := &TransactionContext{
		ID: generateClientID(),
	}
	h.txMu.Lock()
	if h.transactions == nil {
		h.transactions = make(map[string]*TransactionContext)
	}
	h.transactions[tx.ID] = tx
	h.activeTx = tx.ID
	h.txMu.Unlock()
	return tx
}

// CommitTransaction sends all dry-cached messages and ends the transaction.
func (h *Hub) CommitTransaction(tx *TransactionContext) {
	if tx == nil || tx.committed {
		return
	}
	tx.mu.Lock()
	messages := tx.messages
	tx.committed = true
	tx.mu.Unlock()

	h.txMu.Lock()
	delete(h.transactions, tx.ID)
	if h.activeTx == tx.ID {
		h.activeTx = ""
	}
	h.txMu.Unlock()

	// Send all cached messages
	for _, req := range messages {
		req.DryCache = false
		h.Broadcast(req)
	}
}

// RollbackTransaction discards all dry-cached messages and ends the transaction.
func (h *Hub) RollbackTransaction(tx *TransactionContext) {
	if tx == nil || tx.committed {
		return
	}
	tx.mu.Lock()
	tx.committed = true
	tx.messages = nil
	tx.mu.Unlock()

	h.txMu.Lock()
	delete(h.transactions, tx.ID)
	if h.activeTx == tx.ID {
		h.activeTx = ""
	}
	h.txMu.Unlock()
}

// Shutdown gracefully closes all connections and stops the hub.
func (h *Hub) Shutdown() {
	log.Info().Msg("Shutting down realtime hub...")
	h.cancel()

	// Wait for Run() to actually exit so no hub goroutine outlives Shutdown
	// (bounded, so a stuck loop can never hang teardown).
	select {
	case <-h.runDone:
	case <-time.After(5 * time.Second):
	}

	h.mu.Lock()
	clients := make([]Client, 0, len(h.clients))
	for _, c := range h.clients {
		clients = append(clients, c)
	}
	h.clients = make(map[string]Client)
	h.topicIndex = make(map[string]map[string]bool)
	h.mu.Unlock()

	for _, c := range clients {
		c.Close()
	}

	log.Info().Msg("Realtime hub shut down")
}

// handleBroadcast processes a broadcast request, applying access rules per subscriber.
func (h *Hub) handleBroadcast(req *BroadcastRequest) {
	topic := req.Topic
	if topic == "" {
		topic = req.Channel // backward compat
	}
	if topic == "" {
		return
	}

	h.mu.RLock()
	clientIDs := make([]string, 0)
	if clients, ok := h.topicIndex[topic]; ok {
		for id := range clients {
			clientIDs = append(clientIDs, id)
		}
	}
	h.mu.RUnlock()

	if len(clientIDs) == 0 {
		return
	}

	// Process clients in chunks for concurrency
	var wg sync.WaitGroup
	chunkSize := ClientsChunkSize
	for i := 0; i < len(clientIDs); i += chunkSize {
		end := i + chunkSize
		if end > len(clientIDs) {
			end = len(clientIDs)
		}
		chunk := clientIDs[i:end]

		wg.Add(1)
		go func(ids []string) {
			defer wg.Done()
			for _, cid := range ids {
				h.mu.RLock()
				client, ok := h.clients[cid]
				h.mu.RUnlock()
				if !ok {
					continue
				}

				// Enforce collection access rules on record events.
				if req.Collection != "" {
					auth := client.AuthRecord()
					var filterExpr string
					if sub, ok := client.Subscriptions()[topic]; ok && sub != nil && sub.Options != nil {
						filterExpr = sub.Options.Filter
					}
					if h.ruleChecker == nil {
						// Fail closed: without a rule checker, record events
						// are delivered to admin clients only.
						if !auth.IsAdmin() {
							continue
						}
					} else if !h.ruleChecker.CanReceiveRecord(req.Collection, req.RecordID, req.Data, auth, req.Wildcard, filterExpr) {
						continue
					}
				}

				// Apply field picking and expand
				msg := h.applySubscriptionOptions(client, topic, req.Message)

				if req.DryCache {
					// Store for later delivery (after transaction commit)
					key := "drycache:" + topic + ":" + req.Message.Event
					existing, _ := client.Get(key)
					var messages []*RealtimeMessage
					if existing != nil {
						messages, _ = existing.([]*RealtimeMessage)
					}
					messages = append(messages, msg)
					client.Set(key, messages)
				} else {
					if err := client.Send(msg); err != nil {
						log.Debug().
							Err(err).
							Str("client_id", cid).
							Str("topic", topic).
							Msg("Failed to send realtime message")
						client.Close()
					} else {
						atomic.AddInt64(&h.stats.MessagesSent, 1)
					}
				}
			}
		}(chunk)
	}
	wg.Wait()
}

// FlushDryCache sends all dry-cached messages for a specific key (exact key as stored).
func (h *Hub) FlushDryCache(key string) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, client := range h.clients {
		if cached, ok := client.Get(key); ok {
			if messages, ok := cached.([]*RealtimeMessage); ok {
				for _, msg := range messages {
					_ = client.Send(msg) // realtime delivery is lossy by design
					atomic.AddInt64(&h.stats.MessagesSent, 1)
				}
			}
			client.Unset(key)
		}
	}
}

// ClearDryCache removes dry-cached messages without sending (exact key).
func (h *Hub) ClearDryCache(key string) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, client := range h.clients {
		client.Unset(key)
	}
}

// applySubscriptionOptions applies field picking based on subscription options.
func (h *Hub) applySubscriptionOptions(client Client, topic string, msg *RealtimeMessage) *RealtimeMessage {
	sub, ok := client.Subscriptions()[topic]
	if !ok || sub.Options == nil || sub.Options.Fields == "" {
		return msg
	}

	// Parse the message data
	var data map[string]any
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return msg
	}

	// Apply field picking
	picked := pickFields(data, sub.Options.Fields)

	newData, _ := json.Marshal(picked)
	return &RealtimeMessage{
		ClientID:  msg.ClientID,
		Event:     msg.Event,
		Channel:   msg.Channel,
		Topic:     msg.Topic,
		Data:      newData,
		Timestamp: msg.Timestamp,
	}
}

func (h *Hub) removeFromTopicIndex(clientID, topic string) {
	if clients, ok := h.topicIndex[topic]; ok {
		delete(clients, clientID)
		if len(clients) == 0 {
			delete(h.topicIndex, topic)
		}
	}
}

func (h *Hub) cleanupStale() {
	h.mu.Lock()
	defer h.mu.Unlock()

	cutoff := time.Now().Add(-h.idleTimeout)
	for id, client := range h.clients {
		if client.LastSeen().Before(cutoff) {
			log.Debug().Str("client_id", id).Msg("Cleaning up stale realtime client")
			for topic := range client.Subscriptions() {
				h.removeFromTopicIndex(id, topic)
			}
			client.Close()
			delete(h.clients, id)
		}
	}
}

// ---------------------------------------------------------------------------
// WebSocket transport
// ---------------------------------------------------------------------------

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins; auth handled separately
	},
}

// WSClient wraps a WebSocket connection as a Client.
type WSClient struct {
	*baseClient
	conn *websocket.Conn
	send chan []byte
	hub  *Hub
}

// NewWSClient creates a WebSocket client from an upgraded HTTP connection.
func NewWSClient(conn *websocket.Conn, hub *Hub) *WSClient {
	id := generateClientID()
	c := &WSClient{
		baseClient: newBaseClient(id, TransportWS),
		conn:       conn,
		send:       make(chan []byte, 256),
		hub:        hub,
	}
	hub.Register(c)
	return c
}

// Send delivers a message to the WebSocket client.
func (c *WSClient) Send(msg *RealtimeMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return c.SendRaw(data)
}

// SendRaw sends raw bytes through the WebSocket.
func (c *WSClient) SendRaw(data []byte) error {
	if c.IsClosed() {
		return fmt.Errorf("client closed")
	}
	select {
	case c.send <- data:
		return nil
	default:
		return fmt.Errorf("send buffer full")
	}
}

// Close closes the WebSocket connection.
func (c *WSClient) Close() {
	if c.closed.Swap(true) {
		return // already closed
	}
	close(c.send)
	// best-effort close frame; connection is being torn down regardless
	_ = c.conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseGoingAway, "bye"))
	c.conn.Close()
	c.hub.Unregister(c)
}

// ReadPump reads messages from the WebSocket and handles subscription requests.
func (c *WSClient) ReadPump() {
	defer c.Close()

	c.conn.SetReadLimit(c.hub.MaxMessageSize())
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		c.Touch()
		return nil
	})

	for {
		messageType, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Debug().Err(err).Str("client_id", c.ID()).Msg("WebSocket read error")
			}
			break
		}
		if messageType != websocket.TextMessage {
			_ = c.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseUnsupportedData, "text messages only"), time.Now().Add(writeWait))
			break
		}

		c.Touch()
		if err := c.handleMessage(message); err != nil {
			c.sendError(err.Error())
		}
	}
}

// WritePump writes messages to the WebSocket.
func (c *WSClient) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Close()
	}()

	// Cap the total connection lifetime; clients auto-reconnect.
	maxAgeC, stopMaxAge := c.hub.maxConnAgeChan()
	defer stopMaxAge()

	for {
		select {
		case <-maxAgeC:
			log.Debug().Str("client_id", c.ID()).Dur("max_age", c.hub.maxConnAge).Msg("WebSocket connection closed (max connection age reached)")
			// Returning triggers the deferred Close, which sends a clean
			// CloseGoingAway frame so well-behaved clients reconnect.
			return

		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// best-effort close frame on a closed send channel
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			_, _ = w.Write(message) // errors surface on w.Close() below

			// Drain any remaining messages in the buffer
			n := len(c.send)
			for i := 0; i < n; i++ {
				_, _ = w.Write([]byte("\n")) // errors surface on w.Close() below
				_, _ = w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// HandleWebSocket upgrades an HTTP request to a WebSocket connection.
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	if !h.canAcceptConnection() {
		http.Error(w, "realtime capacity reached", http.StatusServiceUnavailable)
		return
	}
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("WebSocket upgrade failed")
		return
	}

	client := NewWSClient(conn, h)
	if auth := authInfoFromContext(r.Context()); auth != nil {
		client.SetAuthRecord(auth)
	}

	// Send connection established (lossy by design)
	_ = client.Send(&RealtimeMessage{
		ClientID:  client.ID(),
		Event:     "connection:established",
		Timestamp: time.Now().UnixMilli(),
	})

	log.Info().
		Str("client_id", client.ID()).
		Str("transport", "ws").
		Str("remote_addr", r.RemoteAddr).
		Msg("WebSocket connection established")

	go client.WritePump()
	client.ReadPump()
}

func (c *WSClient) handleMessage(data []byte) error {
	req, err := decodeSubscribeRequest(data, false)
	if err != nil {
		log.Debug().Err(err).Str("client_id", c.ID()).Msg("Invalid message from client")
		return err
	}

	switch req.Type {
	case "subscribe":
		for _, topic := range req.Subscriptions {
			if err := c.hub.SubscribeClient(c, topic, subscriptionFromRequest(topic, req)); err != nil {
				return err
			}
		}
		return c.Send(&RealtimeMessage{
			Event:     "subscription:confirmed",
			ClientID:  c.ID(),
			Timestamp: time.Now().UnixMilli(),
		})

	case "unsubscribe":
		for _, topic := range req.Subscriptions {
			c.hub.UnsubscribeClient(c, topic)
		}
		return c.Send(&RealtimeMessage{
			Event:     "subscription:removed",
			ClientID:  c.ID(),
			Timestamp: time.Now().UnixMilli(),
		})

	case "message":
		event := strings.TrimSpace(req.Event)
		if event == "" {
			event = "message"
		}
		// Client-published events may not impersonate server-generated ones
		// (record mutations, connection lifecycle, etc.).
		if isReservedEvent(event) {
			return fmt.Errorf("event name %q is reserved", event)
		}
		// Publishing requires authentication and a channel that does not
		// shadow a collection record topic.
		if c.AuthRecord() == nil {
			return fmt.Errorf("authentication required to publish messages")
		}
		topic := req.Topic
		if topic == "" {
			topic = req.Channel
		}
		if !c.hub.CanPublishToChannel(topic) {
			return fmt.Errorf("publishing to topic %q is not allowed", topic)
		}
		c.hub.BroadcastChannel(topic, &RealtimeMessage{
			ClientID:  c.ID(),
			Event:     event,
			Channel:   req.Channel,
			Topic:     topic,
			Data:      req.Data,
			Timestamp: time.Now().UnixMilli(),
		})
		return nil

	case "presence":
		topic := req.Topic
		if topic == "" {
			topic = req.Channel
		}
		data, _ := json.Marshal(map[string]any{
			"topic":   topic,
			"clients": c.hub.TopicSubscriberCount(topic),
			"members": c.hub.PresenceMembers(topic),
		})
		return c.Send(&RealtimeMessage{
			Event:     "presence",
			ClientID:  c.ID(),
			Topic:     topic,
			Data:      data,
			Timestamp: time.Now().UnixMilli(),
		})

	case "ping":
		return c.Send(&RealtimeMessage{
			Event:     "pong",
			ClientID:  c.ID(),
			Timestamp: time.Now().UnixMilli(),
		})
	default:
		return fmt.Errorf("unsupported request type")
	}
}

func (c *WSClient) sendError(message string) {
	data, err := json.Marshal(map[string]any{"message": message})
	if err != nil {
		return
	}
	_ = c.Send(&RealtimeMessage{
		Event:     "error",
		ClientID:  c.ID(),
		Data:      data,
		Timestamp: time.Now().UnixMilli(),
	})
}

// ---------------------------------------------------------------------------
// SSE (Server-Sent Events) transport — PRIMARY
// ---------------------------------------------------------------------------

// SSEClient wraps an HTTP response writer as a Client using SSE.
type SSEClient struct {
	*baseClient
	w       http.ResponseWriter
	flusher http.Flusher
	hub     *Hub
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewSSEClient creates a new SSE client from an HTTP connection.
// The caller is responsible for setting SSE headers before calling this.
func NewSSEClient(w http.ResponseWriter, r *http.Request, hub *Hub) *SSEClient {
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Error().Msg("ResponseWriter does not support flushing — SSE requires it")
		return nil
	}

	ctx, cancel := context.WithCancel(r.Context())
	id := generateClientID()

	client := &SSEClient{
		baseClient: newBaseClient(id, TransportSSE),
		w:          w,
		flusher:    flusher,
		hub:        hub,
		ctx:        ctx,
		cancel:     cancel,
	}

	hub.Register(client)
	return client
}

// Send delivers a message as an SSE event.
func (c *SSEClient) Send(msg *RealtimeMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return c.SendRaw(data)
}

// SendRaw writes raw bytes as an SSE data event.
func (c *SSEClient) SendRaw(data []byte) error {
	if c.IsClosed() {
		return fmt.Errorf("client closed")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// SSE format: "id: <id>\nevent: <event>\ndata: <json>\n\n"
	_, err := fmt.Fprintf(c.w, "id: %d\ndata: %s\n\n", time.Now().UnixMilli(), string(data))
	if err != nil {
		return err
	}
	c.flusher.Flush()
	return nil
}

// Close terminates the SSE connection.
func (c *SSEClient) Close() {
	if c.closed.Swap(true) {
		return
	}
	c.cancel()
	c.hub.Unregister(c)
}

// Context returns the SSE client context (cancelled on close).
func (c *SSEClient) Context() context.Context {
	return c.ctx
}

// HandleSSE establishes an SSE connection and manages the client lifecycle.
// This is the recommended primary realtime transport.
func (h *Hub) HandleSSE(w http.ResponseWriter, r *http.Request) {
	if !h.canAcceptConnection() {
		http.Error(w, "realtime capacity reached", http.StatusServiceUnavailable)
		return
	}
	// Check if the response writer supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Set SSE headers (matching PocketBase)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering; CORS is handled by the router middleware

	// Disable write deadline for the SSE connection
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		log.Warn().Err(err).Msg("SetWriteDeadline not supported, SSE may be affected")
	}

	client := &SSEClient{
		baseClient: newBaseClient(generateClientID(), TransportSSE),
		w:          w,
		flusher:    flusher,
		hub:        h,
		ctx:        r.Context(),
		cancel:     func() {}, // will be overwritten
	}

	ctx, cancel := context.WithCancel(r.Context())
	client.ctx = ctx
	client.cancel = cancel
	if auth := authInfoFromContext(r.Context()); auth != nil {
		client.SetAuthRecord(auth)
	}

	h.Register(client)

	// Send connection established (lossy by design)
	_ = client.Send(&RealtimeMessage{
		ClientID:  client.ID(),
		Event:     "connection:established",
		Timestamp: time.Now().UnixMilli(),
	})

	log.Info().
		Str("client_id", client.ID()).
		Str("transport", "sse").
		Str("remote_addr", r.RemoteAddr).
		Msg("SSE connection established")

	// Set up idle timer
	idleTimer := time.NewTimer(h.idleTimeout)
	defer idleTimer.Stop()

	// Cap the total connection lifetime; clients auto-reconnect.
	maxAgeC, stopMaxAge := h.maxConnAgeChan()
	defer stopMaxAge()

	// Keep connection alive with periodic pings
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	// SSE is one-directional (server->client), so we use a separate endpoint for
	// subscription management (POST /api/v1/realtime). The client sends subscription
	// requests via HTTP POST with their clientId.

	for {
		select {
		case <-ctx.Done():
			log.Debug().Str("client_id", client.ID()).Msg("SSE connection closed (context cancelled)")
			h.Unregister(client)
			return

		case <-idleTimer.C:
			log.Debug().Str("client_id", client.ID()).Msg("SSE connection closed (idle timeout)")
			h.Unregister(client)
			return

		case <-maxAgeC:
			log.Debug().Str("client_id", client.ID()).Dur("max_age", h.maxConnAge).Msg("SSE connection closed (max connection age reached)")
			h.Unregister(client)
			return

		case <-pingTicker.C:
			// Send heartbeat comment (SSE comment, not parsed by clients)
			client.mu.Lock()
			fmt.Fprintf(w, ": heartbeat\n\n")
			client.flusher.Flush()
			client.mu.Unlock()
		}
	}
}

// HandleSSESubscription handles the POST endpoint where SSE clients manage their subscriptions.
// Clients send their clientId and list of subscriptions.
func (h *Hub) HandleSSESubscription(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, h.MaxMessageSize())
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	req, err := decodeSubscribeRequest(data, true)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	client := h.GetClient(req.ClientID)
	if client == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "client not found"})
		return
	}
	if auth := authInfoFromContext(r.Context()); auth != nil {
		client.SetAuthRecord(auth)
	}

	switch req.Type {
	case "subscribe":
		for _, topic := range req.Subscriptions {
			if err := h.SubscribeClient(client, topic, subscriptionFromRequest(topic, req)); err != nil {
				writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
				return
			}
		}

	case "unsubscribe":
		for _, topic := range req.Subscriptions {
			h.UnsubscribeClient(client, topic)
		}

	case "presence":
		topic := req.Topic
		if topic == "" {
			topic = req.Channel
		}
		client.Touch()
		writeJSON(w, http.StatusOK, map[string]any{
			"topic":   topic,
			"clients": h.TopicSubscriberCount(topic),
			"members": h.PresenceMembers(topic),
		})
		return

	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported SSE subscription request"})
		return
	}

	client.Touch()
	writeJSON(w, http.StatusNoContent, nil)
}

// ---------------------------------------------------------------------------
// Utility functions
// ---------------------------------------------------------------------------

// generateClientID returns a cryptographically random client identifier.
// Client IDs act as capability tokens for SSE subscription management
// (POST /api/v1/realtime), so they must be unguessable.
func generateClientID() string {
	buf := make([]byte, 20)
	if _, err := cryptorand.Read(buf); err != nil {
		// crypto/rand failing means the platform RNG is broken; do not fall
		// back to a guessable ID.
		panic(fmt.Sprintf("realtime: crypto/rand unavailable: %v", err))
	}
	return hex.EncodeToString(buf)
}

// pickFields extracts only the specified fields from a record map.
// Fields can be comma-separated, with support for nested fields via dot notation.
func pickFields(data map[string]any, fields string) map[string]any {
	if fields == "" || fields == "*" {
		return data
	}

	result := make(map[string]any)
	for _, raw := range strings.Split(fields, ",") {
		field := strings.TrimSpace(raw)
		if field == "" {
			continue
		}
		if val, ok := data[field]; ok {
			result[field] = val
		}
	}
	return result
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v) // best-effort write to response
	}
}

// Ensure imports are used
var _ = context.Background
var _ = fmt.Sprintf
var _ = http.StatusOK
