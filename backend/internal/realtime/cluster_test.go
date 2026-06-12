package realtime

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// TestClusterPublishesInsteadOfLocalDelivery verifies that in cluster mode a
// record broadcast is published over the cluster channel and is NOT delivered
// directly to local subscribers (the listener does that on every node).
func TestClusterPublishesInsteadOfLocalDelivery(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Shutdown()
	time.Sleep(10 * time.Millisecond)

	var mu sync.Mutex
	delivered := 0
	client := &mockClient{
		baseClient: newBaseClient(generateClientID(), TransportSSE),
		onSend: func(msg *RealtimeMessage) error {
			if msg.Event == "record:create" {
				mu.Lock()
				delivered++
				mu.Unlock()
			}
			return nil
		},
	}
	hub.Register(client)
	time.Sleep(10 * time.Millisecond)
	hub.SubscribeClient(client, "posts/*", &Subscription{Topic: "posts/*"})

	var published []string
	hub.EnableCluster(func(ctx context.Context, payload string) error {
		mu.Lock()
		published = append(published, payload)
		mu.Unlock()
		return nil
	})

	hub.BroadcastRecord("create", "posts", "rec1", map[string]any{"id": "rec1", "title": "hi"})
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if delivered != 0 {
		t.Fatalf("cluster mode must not deliver locally at broadcast time, delivered=%d", delivered)
	}
	if len(published) != 1 {
		t.Fatalf("expected 1 cluster publish, got %d", len(published))
	}
	var evt clusterEvent
	if err := json.Unmarshal([]byte(published[0]), &evt); err != nil {
		t.Fatalf("payload not valid clusterEvent JSON: %v", err)
	}
	if evt.Action != "create" || evt.Collection != "posts" || evt.RecordID != "rec1" {
		t.Fatalf("unexpected cluster event: %+v", evt)
	}
}

// TestClusterListenerDeliversLocally verifies that the path the listener uses
// (localBroadcastRecord) delivers to this node's subscribers.
func TestClusterListenerDeliversLocally(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Shutdown()
	time.Sleep(10 * time.Millisecond)

	got := make(chan *RealtimeMessage, 4)
	client := &mockClient{
		baseClient: newBaseClient(generateClientID(), TransportSSE),
		onSend: func(msg *RealtimeMessage) error {
			if msg.Event == "record:update" {
				got <- msg
			}
			return nil
		},
	}
	// No rule checker is set on this bare hub, so delivery is fail-closed to
	// admins only; mark the client as an admin so we test the delivery path.
	client.SetAuthRecord(&AuthInfo{AdminID: "admin-1", Role: "super_admin"})
	hub.Register(client)
	time.Sleep(10 * time.Millisecond)
	hub.SubscribeClient(client, "posts/*", &Subscription{Topic: "posts/*"})

	// Simulate the cluster listener receiving an event from another node.
	hub.localBroadcastRecord("update", "posts", "rec9", map[string]any{"id": "rec9"})

	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("expected listener delivery path to reach local subscriber")
	}
}
