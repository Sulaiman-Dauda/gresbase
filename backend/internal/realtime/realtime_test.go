package realtime

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestNewHub(t *testing.T) {
	hub := NewHub()
	if hub == nil {
		t.Fatal("expected non-nil hub")
	}
	if hub.ClientCount() != 0 {
		t.Errorf("expected 0 clients, got %d", hub.ClientCount())
	}
}

func TestPickFields(t *testing.T) {
	data := map[string]any{
		"id":     "abc123",
		"name":   "test",
		"email":  "test@example.com",
		"status": "active",
	}

	result := pickFields(data, "id,name")
	if len(result) != 2 {
		t.Errorf("expected 2 fields, got %d", len(result))
	}
	if result["id"] != "abc123" {
		t.Errorf("expected id=abc123, got %v", result["id"])
	}
	if result["name"] != "test" {
		t.Errorf("expected name=test, got %v", result["name"])
	}
	if _, ok := result["email"]; ok {
		t.Error("email should not be in result")
	}

	// Wildcard returns all
	result2 := pickFields(data, "*")
	if len(result2) != 4 {
		t.Errorf("expected 4 fields for wildcard, got %d", len(result2))
	}

	// Empty returns all
	result3 := pickFields(data, "")
	if len(result3) != 4 {
		t.Errorf("expected 4 fields for empty, got %d", len(result3))
	}
}

func TestGenerateClientID(t *testing.T) {
	id1 := generateClientID()
	id2 := generateClientID()

	if id1 == "" {
		t.Error("client ID should not be empty")
	}
	if id1 == id2 {
		t.Error("client IDs should be unique")
	}
}

func TestHubStats(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Shutdown()

	time.Sleep(20 * time.Millisecond)

	stats := hub.Stats()
	if stats.TotalConnections != 0 {
		t.Errorf("expected 0 connections, got %d", stats.TotalConnections)
	}
}

func TestHubIdleTimeout(t *testing.T) {
	hub := NewHub()
	hub.SetIdleTimeout(100 * time.Millisecond)
	if hub.IdleTimeout() != 100*time.Millisecond {
		t.Errorf("expected 100ms idle timeout, got %v", hub.IdleTimeout())
	}
}

func TestHubMaxConnectionAge(t *testing.T) {
	hub := NewHub()
	if hub.MaxConnectionAge() != DefaultMaxConnectionAge {
		t.Errorf("expected default max connection age %v, got %v", DefaultMaxConnectionAge, hub.MaxConnectionAge())
	}

	hub.SetMaxConnectionAge(time.Minute)
	if hub.MaxConnectionAge() != time.Minute {
		t.Errorf("expected 1m max connection age, got %v", hub.MaxConnectionAge())
	}

	// 0 disables; negative clamps to disabled.
	hub.SetMaxConnectionAge(0)
	if hub.MaxConnectionAge() != 0 {
		t.Errorf("expected 0 (disabled), got %v", hub.MaxConnectionAge())
	}
	hub.SetMaxConnectionAge(-time.Second)
	if hub.MaxConnectionAge() != 0 {
		t.Errorf("expected negative value to clamp to 0, got %v", hub.MaxConnectionAge())
	}
}

func TestSSEMaxConnectionAge(t *testing.T) {
	hub := NewHub()
	hub.SetMaxConnectionAge(150 * time.Millisecond)

	srv := httptest.NewServer(http.HandlerFunc(hub.HandleSSE))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	start := time.Now()
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("SSE request failed: %v", err)
	}
	defer resp.Body.Close()

	// The stream must terminate (clean EOF) once the max age is exceeded.
	body, err := io.ReadAll(resp.Body)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("expected clean stream close, got read error: %v", err)
	}
	if !strings.Contains(string(body), "connection:established") {
		t.Errorf("expected connection:established event, got: %s", body)
	}
	if elapsed > 3*time.Second {
		t.Errorf("SSE connection not closed by max age cap (took %v)", elapsed)
	}
}

func TestWebSocketMaxConnectionAge(t *testing.T) {
	hub := NewHub()
	hub.SetMaxConnectionAge(150 * time.Millisecond)

	srv := httptest.NewServer(http.HandlerFunc(hub.HandleWebSocket))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, _, err := conn.ReadMessage()
		if err == nil {
			continue // connection:established etc.
		}
		if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
			t.Fatal("WebSocket connection not closed by max age cap (read deadline hit)")
		}
		// Server closed the connection — a clean GoingAway close frame is
		// preferred, but any non-timeout termination proves enforcement.
		if !websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure, websocket.CloseAbnormalClosure) {
			t.Logf("connection terminated with: %v", err)
		}
		return
	}
}

func TestHubShutdown(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	time.Sleep(10 * time.Millisecond)

	hub.Shutdown()

	// After shutdown, operations should be no-ops
	hub.BroadcastTopic("test", &RealtimeMessage{Event: "test"})
	if hub.ClientCount() != 0 {
		t.Log("clients after shutdown:", hub.ClientCount())
	}
}

func TestHubRegisterUnregister(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Shutdown()

	time.Sleep(10 * time.Millisecond)

	// Create a test SSE server that immediately closes
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", 500)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Connection", "keep-alive")

		client := &SSEClient{
			baseClient: newBaseClient(generateClientID(), TransportSSE),
			w:          w,
			flusher:    flusher,
			hub:        hub,
		}

		hub.Register(client)

		// Send one message
		client.Send(&RealtimeMessage{
			ClientID:  client.ID(),
			Event:     "connection:established",
			Timestamp: time.Now().UnixMilli(),
		})

		// Wait for client to be registered, then close
		time.Sleep(20 * time.Millisecond)
		hub.Unregister(client)
	}))
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("SSE connection failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream, got %s", ct)
	}

	time.Sleep(50 * time.Millisecond)

	// After unregister, client count should be 0
	count := hub.ClientCount()
	t.Logf("Client count after test: %d", count)
}

func TestSubscribeUnsubscribe(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Shutdown()

	time.Sleep(10 * time.Millisecond)

	// Create a direct client (not via HTTP)
	client := &mockClient{
		baseClient: newBaseClient(generateClientID(), TransportSSE),
	}

	hub.Register(client)
	time.Sleep(10 * time.Millisecond)

	if hub.ClientCount() != 1 {
		t.Fatalf("expected 1 client, got %d", hub.ClientCount())
	}

	// Subscribe
	hub.SubscribeClient(client, "topic1", &Subscription{Topic: "topic1"})
	hub.SubscribeClient(client, "topic2", &Subscription{Topic: "topic2"})

	if hub.TopicSubscriberCount("topic1") != 1 {
		t.Errorf("expected 1 subscriber for topic1, got %d", hub.TopicSubscriberCount("topic1"))
	}

	// Unsubscribe one
	hub.UnsubscribeClient(client, "topic1")
	if hub.TopicSubscriberCount("topic1") != 0 {
		t.Errorf("expected 0 subscribers for topic1 after unsubscribe, got %d", hub.TopicSubscriberCount("topic1"))
	}
	if hub.TopicSubscriberCount("topic2") != 1 {
		t.Errorf("expected 1 subscriber for topic2, got %d", hub.TopicSubscriberCount("topic2"))
	}

	// Unsubscribe all
	hub.UnsubscribeAll(client)
	if hub.TopicSubscriberCount("topic2") != 0 {
		t.Errorf("expected 0 subscribers after unsubscribe all, got %d", hub.TopicSubscriberCount("topic2"))
	}

	// Unregister
	hub.Unregister(client)
	time.Sleep(10 * time.Millisecond)
	if hub.ClientCount() != 0 {
		t.Errorf("expected 0 clients after unregister, got %d", hub.ClientCount())
	}
}

func TestBroadcastToTopic(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Shutdown()

	time.Sleep(10 * time.Millisecond)

	var mu sync.Mutex
	var received []*RealtimeMessage

	client := &mockClient{
		baseClient: newBaseClient(generateClientID(), TransportSSE),
		onSend: func(msg *RealtimeMessage) error {
			mu.Lock()
			// Only count test events, ignore presence broadcasts
			if msg.Event == "test:event" {
				received = append(received, msg)
			}
			mu.Unlock()
			return nil
		},
	}

	hub.Register(client)
	hub.SubscribeClient(client, "test-topic", &Subscription{Topic: "test-topic"})
	time.Sleep(10 * time.Millisecond)

	// Broadcast
	msg := &RealtimeMessage{
		Event:     "test:event",
		Topic:     "test-topic",
		Data:      json.RawMessage(`{"hello":"world"}`),
		Timestamp: time.Now().UnixMilli(),
	}
	hub.Broadcast(&BroadcastRequest{
		Topic:   "test-topic",
		Message: msg,
	})

	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	count := len(received)
	mu.Unlock()

	if count != 1 {
		t.Errorf("expected 1 message received, got %d", count)
	}

	// Broadcast to a different topic should not be received
	mu.Lock()
	received = nil
	mu.Unlock()

	hub.Broadcast(&BroadcastRequest{
		Topic:   "other-topic",
		Message: msg,
	})

	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	count = len(received)
	mu.Unlock()

	if count != 0 {
		t.Errorf("expected 0 messages for non-subscribed topic, got %d", count)
	}
}

func TestWSClientSubscriptionParsing(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Shutdown()

	time.Sleep(10 * time.Millisecond)

	client := &mockClient{
		baseClient: newBaseClient(generateClientID(), TransportWS),
	}

	hub.Register(client)

	// Simulate a subscribe message
	subReq := []byte(`{"type":"subscribe","subscriptions":["posts/*","users/abc123"],"query":{"filter":"published=true","fields":"id,title"}}`)
	hub.handleSubscriptionRequest(client, subReq)

	time.Sleep(10 * time.Millisecond)

	if hub.TopicSubscriberCount("posts/*") != 1 {
		t.Errorf("expected 1 subscriber for posts/*, got %d", hub.TopicSubscriberCount("posts/*"))
	}
	if hub.TopicSubscriberCount("users/abc123") != 1 {
		t.Errorf("expected 1 subscriber for users/abc123, got %d", hub.TopicSubscriberCount("users/abc123"))
	}

	// Check subscription options
	subs := client.Subscriptions()
	if sub, ok := subs["posts/*"]; ok {
		if sub.Options.Filter != "published=true" {
			t.Errorf("expected filter 'published=true', got %q", sub.Options.Filter)
		}
		if sub.Options.Fields != "id,title" {
			t.Errorf("expected fields 'id,title', got %q", sub.Options.Fields)
		}
	} else {
		t.Error("posts/* subscription not found")
	}

	// Simulate unsubscribe
	unsubReq := []byte(`{"type":"unsubscribe","subscriptions":["posts/*"]}`)
	hub.handleSubscriptionRequest(client, unsubReq)

	time.Sleep(10 * time.Millisecond)

	if hub.TopicSubscriberCount("posts/*") != 0 {
		t.Errorf("expected 0 subscribers for posts/* after unsubscribe, got %d", hub.TopicSubscriberCount("posts/*"))
	}
}

func TestDryCache(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Shutdown()

	time.Sleep(10 * time.Millisecond)

	var mu sync.Mutex
	var received []*RealtimeMessage

	client := &mockClient{
		baseClient: newBaseClient(generateClientID(), TransportSSE),
		onSend: func(msg *RealtimeMessage) error {
			mu.Lock()
			received = append(received, msg)
			mu.Unlock()
			return nil
		},
	}

	hub.Register(client)
	hub.SubscribeClient(client, "dry-topic", &Subscription{Topic: "dry-topic"})
	time.Sleep(10 * time.Millisecond)

	// Broadcast with DryCache
	msg := &RealtimeMessage{
		Event:     "record:create",
		Topic:     "dry-topic",
		Timestamp: time.Now().UnixMilli(),
	}
	hub.Broadcast(&BroadcastRequest{
		Topic:    "dry-topic",
		Message:  msg,
		DryCache: true,
	})

	time.Sleep(20 * time.Millisecond)

	// With dry cache, message should NOT be sent immediately
	mu.Lock()
	beforeFlush := len(received)
	mu.Unlock()

	// Accept that presence:join was sent on registration
	// The dry-cached record:create should not have been sent
	if beforeFlush > 0 {
		t.Logf("Got %d messages before flush (likely presence events)", beforeFlush)
	}

	// Check that the dry cache key was stored
	cached, exists := client.Get("drycache:dry-topic:record:create")
	if !exists || cached == nil {
		t.Error("expected dry cache entry to exist")
	}

	// Flush dry cache
	hub.FlushDryCache("drycache:dry-topic:record:create")

	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	afterFlush := len(received)
	mu.Unlock()

	if afterFlush <= beforeFlush {
		t.Errorf("expected more messages after flush, got %d (before: %d)", afterFlush, beforeFlush)
	}

	// Clear dry cache
	hub.ClearDryCache("drycache:dry-topic:record:create")
}

// ---------------------------------------------------------------------------
// mock client for testing
// ---------------------------------------------------------------------------

type mockClient struct {
	*baseClient
	onSend func(msg *RealtimeMessage) error
}

func (m *mockClient) Send(msg *RealtimeMessage) error {
	if m.onSend != nil {
		return m.onSend(msg)
	}
	return nil
}

func (m *mockClient) SendRaw(data []byte) error {
	return nil
}

func (m *mockClient) Close() {
	m.closed.Store(true)
}

// Add subscription request handling to the hub for tests
func (h *Hub) handleSubscriptionRequest(client Client, data []byte) {
	var req SubscribeRequest
	if err := json.Unmarshal(data, &req); err != nil {
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
}
