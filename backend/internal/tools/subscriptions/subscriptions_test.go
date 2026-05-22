package subscriptions

import (
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	client := NewClient("test-client-1")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.ID != "test-client-1" {
		t.Errorf("expected ID 'test-client-1', got %q", client.ID)
	}
	if client.ConnectedAt.IsZero() {
		t.Error("ConnectedAt should not be zero")
	}
}

func TestClientSubscribe(t *testing.T) {
	client := NewClient("c1")
	client.Subscribe("channel-a")
	client.Subscribe("channel-b")

	if !client.IsSubscribed("channel-a") {
		t.Error("should be subscribed to channel-a")
	}
	if !client.IsSubscribed("channel-b") {
		t.Error("should be subscribed to channel-b")
	}
	if client.IsSubscribed("channel-c") {
		t.Error("should not be subscribed to channel-c")
	}

	channels := client.Channels()
	if len(channels) != 2 {
		t.Errorf("expected 2 channels, got %d", len(channels))
	}
}

func TestClientUnsubscribe(t *testing.T) {
	client := NewClient("c2")
	client.Subscribe("channel-a")
	client.Subscribe("channel-b")
	client.Unsubscribe("channel-a")

	if client.IsSubscribed("channel-a") {
		t.Error("should not be subscribed to channel-a after unsubscribe")
	}
	if !client.IsSubscribed("channel-b") {
		t.Error("should still be subscribed to channel-b")
	}
}

func TestClientMetadata(t *testing.T) {
	client := NewClient("c3")
	client.SetMetadata("auth", map[string]string{"user": "admin"})
	client.SetMetadata("ip", "10.0.0.1")

	auth, ok := client.GetMetadata("auth")
	if !ok {
		t.Error("should have auth metadata")
	}
	if authMap, ok := auth.(map[string]string); !ok || authMap["user"] != "admin" {
		t.Error("auth metadata mismatch")
	}

	ip, ok := client.GetMetadata("ip")
	if !ok || ip != "10.0.0.1" {
		t.Error("ip metadata mismatch")
	}

	_, ok = client.GetMetadata("nonexistent")
	if ok {
		t.Error("should not find nonexistent metadata")
	}
}

func TestClientTouch(t *testing.T) {
	client := NewClient("c4")
	oldSeen := client.LastSeen
	time.Sleep(5 * time.Millisecond)
	client.Touch()
	if !client.LastSeen.After(oldSeen) {
		t.Error("LastSeen should be updated after Touch")
	}
}

func TestNewRegistry(t *testing.T) {
	reg := NewRegistry()
	if reg == nil {
		t.Fatal("expected non-nil registry")
	}
	if reg.ClientCount() != 0 {
		t.Errorf("expected 0 clients, got %d", reg.ClientCount())
	}
}

func TestRegistryRegister(t *testing.T) {
	reg := NewRegistry()
	client := reg.Register("client-1")

	if client == nil {
		t.Fatal("expected non-nil client from Register")
	}
	if reg.ClientCount() != 1 {
		t.Errorf("expected 1 client, got %d", reg.ClientCount())
	}

	// Re-register returns same client
	client2 := reg.Register("client-1")
	if client2.ID != "client-1" {
		t.Error("re-register should return existing client")
	}
	if reg.ClientCount() != 1 {
		t.Errorf("re-register should not change count, got %d", reg.ClientCount())
	}
}

func TestRegistrySubscribe(t *testing.T) {
	reg := NewRegistry()
	reg.Subscribe("client-1", "topic-a")
	reg.Subscribe("client-1", "topic-b")
	reg.Subscribe("client-2", "topic-a")

	if !reg.IsSubscribed("client-1", "topic-a") {
		t.Error("client-1 should be subscribed to topic-a")
	}
	if reg.ChannelCount("topic-a") != 2 {
		t.Errorf("expected 2 subscribers for topic-a, got %d", reg.ChannelCount("topic-a"))
	}
	if reg.ChannelCount("topic-b") != 1 {
		t.Errorf("expected 1 subscriber for topic-b, got %d", reg.ChannelCount("topic-b"))
	}

	clients := reg.GetChannelClients("topic-a")
	if len(clients) != 2 {
		t.Errorf("expected 2 client IDs for topic-a, got %d", len(clients))
	}
}

func TestRegistryUnsubscribe(t *testing.T) {
	reg := NewRegistry()
	reg.Subscribe("client-1", "topic-a")
	reg.Subscribe("client-1", "topic-b")

	reg.Unsubscribe("client-1", "topic-a")

	if reg.IsSubscribed("client-1", "topic-a") {
		t.Error("should not be subscribed after unsubscribe")
	}
	if !reg.IsSubscribed("client-1", "topic-b") {
		t.Error("should still be subscribed to topic-b")
	}
	if reg.ChannelCount("topic-a") != 0 {
		t.Errorf("expected 0 subscribers for topic-a, got %d", reg.ChannelCount("topic-a"))
	}
}

func TestRegistryUnregister(t *testing.T) {
	reg := NewRegistry()
	reg.Subscribe("client-1", "topic-a")
	reg.Subscribe("client-1", "topic-b")
	reg.Subscribe("client-2", "topic-a")

	reg.Unregister("client-1")

	if reg.ClientCount() != 1 {
		t.Errorf("expected 1 client after unregister, got %d", reg.ClientCount())
	}
	if reg.ChannelCount("topic-a") != 1 {
		t.Errorf("expected 1 subscriber for topic-a, got %d", reg.ChannelCount("topic-a"))
	}
	if reg.ChannelCount("topic-b") != 0 {
		t.Errorf("expected 0 subscribers for topic-b, got %d", reg.ChannelCount("topic-b"))
	}
}

func TestRegistryGetClientChannels(t *testing.T) {
	reg := NewRegistry()
	reg.Subscribe("client-1", "channel-a")
	reg.Subscribe("client-1", "channel-b")
	reg.Subscribe("client-1", "channel-c")

	channels := reg.GetClientChannels("client-1")
	if len(channels) != 3 {
		t.Errorf("expected 3 channels, got %d", len(channels))
	}
}

func TestRegistryStats(t *testing.T) {
	reg := NewRegistry()
	reg.Subscribe("c1", "topic-a")
	reg.Subscribe("c2", "topic-a")
	reg.Subscribe("c2", "topic-b")

	stats := reg.Stats()
	if stats["total_clients"] != 2 {
		t.Errorf("expected 2 total clients, got %v", stats["total_clients"])
	}
	if stats["total_channels"] != 2 {
		t.Errorf("expected 2 total channels, got %v", stats["total_channels"])
	}
}

func TestRegistryCleanupStale(t *testing.T) {
	reg := NewRegistry()
	reg.Subscribe("c1", "topic-a")

	// Artificially set client LastSeen to old time
	client := reg.GetClient("c1")
	if client != nil {
		client.LastSeen = time.Now().Add(-2 * time.Hour)
	}

	removed := reg.CleanupStale(1 * time.Hour)
	if removed != 1 {
		t.Errorf("expected 1 stale client removed, got %d", removed)
	}
	if reg.ClientCount() != 0 {
		t.Errorf("expected 0 clients after cleanup, got %d", reg.ClientCount())
	}
}

func TestBroker(t *testing.T) {
	reg := NewRegistry()
	reg.Subscribe("c1", "topic-a")
	reg.Subscribe("c2", "topic-a")
	reg.Subscribe("c2", "topic-b")

	sent := make(map[string][]byte)
	broker := NewBroker(reg, func(clientID string, data []byte) error {
		sent[clientID] = data
		return nil
	})

	count := broker.SendToChannel("topic-a", []byte(`{"msg":"hello"}`))
	if count != 2 {
		t.Errorf("expected 2 messages sent to topic-a, got %d", count)
	}

	count = broker.SendToChannel("topic-b", []byte(`{"msg":"world"}`))
	if count != 1 {
		t.Errorf("expected 1 message sent to topic-b, got %d", count)
	}

	count = broker.SendToChannel("nonexistent", []byte(`{}`))
	if count != 0 {
		t.Errorf("expected 0 messages for nonexistent topic, got %d", count)
	}
}

func TestRegistryConcurrent(t *testing.T) {
	reg := NewRegistry()
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			cid := "c" + string(rune('0'+id))
			reg.Subscribe(cid, "shared-topic")
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	if reg.ChannelCount("shared-topic") != 10 {
		t.Errorf("expected 10 subscribers, got %d", reg.ChannelCount("shared-topic"))
	}
}
