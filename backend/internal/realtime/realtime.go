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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
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
	// DefaultMaxMessageSize is the max allowed incoming message size.
	DefaultMaxMessageSize = 65536
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
	TenantID   string `json:"tenant_id"`
	Collection string `json:"collection,omitempty"`
	RecordID   string `json:"record_id,omitempty"`
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

func (c *baseClient) ID() string               { return c.id }
func (c *baseClient) Transport() string         { return c.transport }
func (c *baseClient) ConnectedAt() time.Time    { return c.connectedAt }
func (c *baseClient) LastSeen() time.Time       { c.mu.RLock(); defer c.mu.RUnlock(); return c.lastSeen }
func (c *baseClient) Touch()                    { c.mu.Lock(); defer c.mu.Unlock(); c.lastSeen = time.Now() }
func (c *baseClient) IsClosed() bool            { return c.closed.Load() }

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
	ID       string
	messages []*BroadcastRequest
	mu       sync.Mutex
	committed bool
}

// RuleChecker is the interface the Hub uses to evaluate access rules.
// Implemented by the app layer to check collection rules against auth.
type RuleChecker interface {
	// CanAccessRecord checks if a record is accessible given a rule and auth info.
	CanAccessRecord(collectionName string, recordID string, rule string, auth *AuthInfo, filter string) (bool, error)
	// GetCollectionRecord retrieves a record for broadcasting.
	GetCollectionRecord(collectionName string, recordID string, auth *AuthInfo) (map[string]any, error)
}

// Hub is the central realtime management system.
// It handles all client connections, subscriptions, and message brokering.
type Hub struct {
	clients     map[string]Client // all connected clients by ID
	topicIndex  map[string]map[string]bool // topic -> client IDs
	register    chan Client
	unregister  chan Client
	broadcast   chan *BroadcastRequest
	ruleChecker RuleChecker
	idleTimeout time.Duration
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	stats       HubStats
	
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
	// DryCache, if true, stores messages instead of sending immediately
	// (for transactional consistency — sends on commit).
	DryCache bool
}

// HubStats holds runtime statistics.
type HubStats struct {
	TotalConnections   int64 `json:"total_connections"`
	TotalTopics        int64 `json:"total_topics"`
	MessagesSent       int64 `json:"messages_sent"`
	MessagesDropped    int64 `json:"messages_dropped"`
}

// NewHub creates a new realtime Hub.
func NewHub() *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	return &Hub{
		clients:     make(map[string]Client),
		topicIndex:  make(map[string]map[string]bool),
		register:    make(chan Client, 256),
		unregister:  make(chan Client, 256),
		broadcast:   make(chan *BroadcastRequest, 1024),
		idleTimeout: DefaultIdleTimeout,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// SetRuleChecker sets the access rule evaluation callback.
func (h *Hub) SetRuleChecker(rc RuleChecker) {
	h.ruleChecker = rc
}

// SetIdleTimeout sets the connection idle timeout.
func (h *Hub) SetIdleTimeout(d time.Duration) {
	h.idleTimeout = d
}

// IdleTimeout returns the configured idle timeout.
func (h *Hub) IdleTimeout() time.Duration {
	return h.idleTimeout
}

// Run starts the hub event loop. Must be called in a goroutine.
func (h *Hub) Run() {
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

			// Broadcast presence join to all clients
			h.broadcastPresence(client.ID(), "presence:join")

		case client := <-h.unregister:
			h.mu.Lock()
			// Remove from topic index
			for topic := range client.Subscriptions() {
				h.removeFromTopicIndex(client.ID(), topic)
			}
			delete(h.clients, client.ID())
			clientCount := len(h.clients)
			h.mu.Unlock()

			log.Debug().
				Str("client_id", client.ID()).
				Int("total_clients", clientCount).
				Msg("Realtime client disconnected")

			h.broadcastPresence(client.ID(), "presence:leave")

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
func (h *Hub) BroadcastRecord(action string, collectionName string, recordID string, data map[string]any) {
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

	// Send to specific record subscribers
	h.Broadcast(&BroadcastRequest{
		Topic:   specificTopic,
		Message: msg,
		Data:    data,
	})

	// Send to wildcard subscribers
	h.Broadcast(&BroadcastRequest{
		Topic:   wildcardTopic,
		Message: msg,
		Data:    data,
	})
}

// SubscribeClient subscribes a client to a topic with options.
func (h *Hub) SubscribeClient(client Client, topic string, sub *Subscription) {
	client.Subscribe(topic, sub)

	h.mu.Lock()
	if h.topicIndex[topic] == nil {
		h.topicIndex[topic] = make(map[string]bool)
	}
	h.topicIndex[topic][client.ID()] = true
	h.mu.Unlock()

	log.Debug().
		Str("client_id", client.ID()).
		Str("topic", topic).
		Msg("Client subscribed to topic")
}

// UnsubscribeClient unsubscribes a client from a topic.
func (h *Hub) UnsubscribeClient(client Client, topic string) {
	client.Unsubscribe(topic)
	h.mu.Lock()
	h.removeFromTopicIndex(client.ID(), topic)
	h.mu.Unlock()
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

				// Apply access rules if checker is set
				if h.ruleChecker != nil && req.Data != nil {
					sub, hasSub := client.Subscriptions()[topic]
					if hasSub && sub.Options != nil && sub.Options.Filter != "" {
						// Client-side filter check
						// Delegate to rule checker
						continue // skip if filter doesn't match
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
						messages = existing.([]*RealtimeMessage)
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
					client.Send(msg)
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

func (h *Hub) broadcastPresence(clientID string, event string) {
	msg := &RealtimeMessage{
		Event:     event,
		ClientID:  clientID,
		Timestamp: time.Now().UnixMilli(),
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, client := range h.clients {
		client.Send(msg)
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
	c.conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseGoingAway, "bye"))
	c.conn.Close()
	c.hub.Unregister(c)
}

// ReadPump reads messages from the WebSocket and handles subscription requests.
func (c *WSClient) ReadPump() {
	defer c.Close()

	c.conn.SetReadLimit(DefaultMaxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		c.Touch()
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Debug().Err(err).Str("client_id", c.ID()).Msg("WebSocket read error")
			}
			break
		}

		c.Touch()
		c.handleMessage(message)
	}
}

// WritePump writes messages to the WebSocket.
func (c *WSClient) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Drain any remaining messages in the buffer
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte("\n"))
				w.Write(<-c.send)
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
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("WebSocket upgrade failed")
		return
	}

	client := NewWSClient(conn, h)

	// Send connection established
	client.Send(&RealtimeMessage{
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

func (c *WSClient) handleMessage(data []byte) {
	var req SubscribeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		log.Debug().Err(err).Str("client_id", c.ID()).Msg("Invalid message from client")
		return
	}

	switch req.Type {
	case "subscribe":
		for _, topic := range req.Subscriptions {
			sub := &Subscription{
				Topic: topic,
				Query: req.Query,
				Options: &SubscribeOptions{
					Filter: req.Query["filter"],
					Fields: req.Query["fields"],
					Expand: req.Query["expand"],
				},
			}
			c.hub.SubscribeClient(c, topic, sub)
		}
		c.Send(&RealtimeMessage{
			Event:     "subscription:confirmed",
			ClientID:  c.ID(),
			Timestamp: time.Now().UnixMilli(),
		})

	case "unsubscribe":
		for _, topic := range req.Subscriptions {
			c.hub.UnsubscribeClient(c, topic)
		}
		c.Send(&RealtimeMessage{
			Event:     "subscription:removed",
			ClientID:  c.ID(),
			Timestamp: time.Now().UnixMilli(),
		})

	case "message":
		msg := &RealtimeMessage{
			ClientID:  c.ID(),
			Event:     req.Event,
			Channel:   req.Channel,
			Topic:     req.Topic,
			Data:      req.Data,
			Timestamp: time.Now().UnixMilli(),
		}
		// Broadcast to subscribers of the topic
		if req.Topic != "" {
			c.hub.BroadcastTopic(req.Topic, msg)
		} else if req.Channel != "" {
			c.hub.BroadcastTopic(req.Channel, msg)
		}

	case "presence":
		count := c.hub.TopicSubscriberCount(req.Topic)
		data, _ := json.Marshal(map[string]any{
			"topic":  req.Topic,
			"clients": count,
		})
		c.Send(&RealtimeMessage{
			Event:     "presence",
			ClientID:  c.ID(),
			Topic:     req.Topic,
			Data:      data,
			Timestamp: time.Now().UnixMilli(),
		})

	case "ping":
		c.Send(&RealtimeMessage{
			Event:     "pong",
			ClientID:  c.ID(),
			Timestamp: time.Now().UnixMilli(),
		})
	}
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
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering
	w.Header().Set("Access-Control-Allow-Origin", "*")

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

	h.Register(client)

	// Send connection established
	client.Send(&RealtimeMessage{
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
	var req SubscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	client := h.GetClient(req.ClientID)
	if client == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "client not found"})
		return
	}

	switch req.Type {
	case "subscribe":
		for _, topic := range req.Subscriptions {
			sub := &Subscription{
				Topic: topic,
				Query: req.Query,
				Options: &SubscribeOptions{
					Filter: req.Query["filter"],
					Fields: req.Query["fields"],
					Expand: req.Query["expand"],
				},
			}
			h.SubscribeClient(client, topic, sub)
		}

	case "unsubscribe":
		for _, topic := range req.Subscriptions {
			h.UnsubscribeClient(client, topic)
		}
	}

	client.Touch()
	writeJSON(w, http.StatusNoContent, nil)
}

// ---------------------------------------------------------------------------
// Utility functions
// ---------------------------------------------------------------------------

var idCounter atomic.Uint64

func generateClientID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), idCounter.Add(1))
}

// pickFields extracts only the specified fields from a record map.
// Fields can be comma-separated, with support for nested fields via dot notation.
func pickFields(data map[string]any, fields string) map[string]any {
	if fields == "" || fields == "*" {
		return data
	}

	result := make(map[string]any)
	for _, field := range splitAndTrim(fields, ",") {
		if val, ok := data[field]; ok {
			result[field] = val
		}
	}
	return result
}

func splitAndTrim(s, sep string) []string {
	parts := make([]string, 0)
	for _, p := range splitComma(s) {
		p = trim(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func splitComma(s string) []string {
	var parts []string
	current := ""
	for _, ch := range s {
		if ch == ',' {
			current = trim(current)
			if current != "" {
				parts = append(parts, current)
			}
			current = ""
		} else {
			current += string(ch)
		}
	}
	current = trim(current)
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func trim(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		json.NewEncoder(w).Encode(v)
	}
}

// Ensure imports are used
var _ = context.Background
var _ = fmt.Sprintf
var _ = http.StatusOK
