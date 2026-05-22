// Package tools/subscriptions provides client subscription tracking for the realtime engine.
package subscriptions

import (
	"sync"
	"time"
)

// Client represents a connected client with active subscriptions.
type Client struct {
	ID          string
	ConnectedAt time.Time
	LastSeen    time.Time
	Metadata    map[string]any
	channels    map[string]bool
	mu          sync.RWMutex
}

// NewClient creates a subscription client.
func NewClient(id string) *Client {
	now := time.Now()
	return &Client{
		ID:          id,
		ConnectedAt: now,
		LastSeen:    now,
		Metadata:    make(map[string]any),
		channels:    make(map[string]bool),
	}
}

// Subscribe adds a channel subscription.
func (c *Client) Subscribe(channel string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.channels[channel] = true
	c.LastSeen = time.Now()
}

// Unsubscribe removes a channel subscription.
func (c *Client) Unsubscribe(channel string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.channels, channel)
}

// IsSubscribed checks if client is subscribed to a channel.
func (c *Client) IsSubscribed(channel string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.channels[channel]
}

// Channels returns all subscribed channels.
func (c *Client) Channels() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	channels := make([]string, 0, len(c.channels))
	for ch := range c.channels {
		channels = append(channels, ch)
	}
	return channels
}

// SetMetadata sets client metadata.
func (c *Client) SetMetadata(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Metadata[key] = value
}

// GetMetadata gets client metadata.
func (c *Client) GetMetadata(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.Metadata[key]
	return v, ok
}

// Touch updates the LastSeen timestamp.
func (c *Client) Touch() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.LastSeen = time.Now()
}

// Registry tracks all subscription clients and their channel mappings.
type Registry struct {
	clients  map[string]*Client
	channels map[string]map[string]bool // channel -> client IDs
	mu       sync.RWMutex
}

// NewRegistry creates a subscription registry.
func NewRegistry() *Registry {
	return &Registry{
		clients:  make(map[string]*Client),
		channels: make(map[string]map[string]bool),
	}
}

// Register adds a client to the registry.
func (r *Registry) Register(clientID string) *Client {
	r.mu.Lock()
	defer r.mu.Unlock()

	if client, ok := r.clients[clientID]; ok {
		client.Touch()
		return client
	}

	client := NewClient(clientID)
	r.clients[clientID] = client
	return client
}

// Unregister removes a client and all its subscriptions.
func (r *Registry) Unregister(clientID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	client, ok := r.clients[clientID]
	if !ok {
		return
	}

	// Remove from channel mappings
	for _, ch := range client.Channels() {
		if clients, ok := r.channels[ch]; ok {
			delete(clients, clientID)
			if len(clients) == 0 {
				delete(r.channels, ch)
			}
		}
	}

	delete(r.clients, clientID)
}

// Subscribe subscribes a client to a channel.
func (r *Registry) Subscribe(clientID, channel string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	client, ok := r.clients[clientID]
	if !ok {
		client = NewClient(clientID)
		r.clients[clientID] = client
	}

	client.Subscribe(channel)

	if r.channels[channel] == nil {
		r.channels[channel] = make(map[string]bool)
	}
	r.channels[channel][clientID] = true
}

// Unsubscribe removes a client subscription.
func (r *Registry) Unsubscribe(clientID, channel string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if client, ok := r.clients[clientID]; ok {
		client.Unsubscribe(channel)
	}

	if clients, ok := r.channels[channel]; ok {
		delete(clients, clientID)
		if len(clients) == 0 {
			delete(r.channels, channel)
		}
	}
}

// GetChannelClients returns all client IDs subscribed to a channel.
func (r *Registry) GetChannelClients(channel string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	clients, ok := r.channels[channel]
	if !ok {
		return nil
	}

	ids := make([]string, 0, len(clients))
	for id := range clients {
		ids = append(ids, id)
	}
	return ids
}

// GetClientChannels returns all channels a client is subscribed to.
func (r *Registry) GetClientChannels(clientID string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	client, ok := r.clients[clientID]
	if !ok {
		return nil
	}

	return client.Channels()
}

// GetClient returns a client by ID.
func (r *Registry) GetClient(clientID string) *Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.clients[clientID]
}

// ClientCount returns the total number of connected clients.
func (r *Registry) ClientCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.clients)
}

// ChannelCount returns the number of clients subscribed to a channel.
func (r *Registry) ChannelCount(channel string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	clients, ok := r.channels[channel]
	if !ok {
		return 0
	}
	return len(clients)
}

// IsSubscribed checks if a client is subscribed to a channel.
func (r *Registry) IsSubscribed(clientID, channel string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	client, ok := r.clients[clientID]
	if !ok {
		return false
	}

	return client.IsSubscribed(channel)
}

// Stats returns registry statistics.
func (r *Registry) Stats() map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()

	channelStats := make(map[string]int)
	for ch, clients := range r.channels {
		channelStats[ch] = len(clients)
	}

	return map[string]any{
		"total_clients":  len(r.clients),
		"total_channels": len(r.channels),
		"channels":       channelStats,
	}
}

// CleanupStale removes clients that haven't been seen within the given duration.
func (r *Registry) CleanupStale(maxAge time.Duration) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	var removed int

	for id, client := range r.clients {
		client.mu.RLock()
		lastSeen := client.LastSeen
		client.mu.RUnlock()

		if lastSeen.Before(cutoff) {
			// Remove from channel mappings
			for _, ch := range client.Channels() {
				if clients, ok := r.channels[ch]; ok {
					delete(clients, id)
					if len(clients) == 0 {
						delete(r.channels, ch)
					}
				}
			}
			delete(r.clients, id)
			removed++
		}
	}

	return removed
}

// Broker delivers messages to clients based on channel subscriptions.
type Broker struct {
	registry *Registry
	sendFn   func(clientID string, data []byte) error
}

// NewBroker creates a message broker.
func NewBroker(registry *Registry, sendFn func(clientID string, data []byte) error) *Broker {
	return &Broker{
		registry: registry,
		sendFn:   sendFn,
	}
}

// SendToChannel sends a message to all clients subscribed to a channel.
func (b *Broker) SendToChannel(channel string, data []byte) int {
	clientIDs := b.registry.GetChannelClients(channel)
	sent := 0
	for _, id := range clientIDs {
		if err := b.sendFn(id, data); err == nil {
			sent++
		}
	}
	return sent
}

// SendToClient sends a message to a specific client.
func (b *Broker) SendToClient(clientID string, data []byte) error {
	return b.sendFn(clientID, data)
}
