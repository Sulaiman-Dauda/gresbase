package realtime

import (
	"testing"
	"time"
)

// TestDirectBroadcastSuppression verifies the health-aware delivery switch:
// while the WAL stream reports healthy, direct BroadcastRecord calls are
// dropped (the WAL stream is the single source of truth); when it reports
// unhealthy, the API-emitted path resumes; and the WAL emission entry point
// always delivers regardless of the switch.
func TestDirectBroadcastSuppression(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Shutdown()
	time.Sleep(10 * time.Millisecond)

	got := make(chan *RealtimeMessage, 8)
	client := &mockClient{
		baseClient: newBaseClient(generateClientID(), TransportSSE),
		onSend: func(msg *RealtimeMessage) error {
			got <- msg
			return nil
		},
	}
	// No rule checker on a bare hub → fail-closed to admins; make the client
	// an admin so we observe the delivery path itself.
	client.SetAuthRecord(&AuthInfo{AdminID: "admin-1", Role: "super_admin"})
	hub.Register(client)
	time.Sleep(10 * time.Millisecond)
	if err := hub.SubscribeClient(client, "posts/*", &Subscription{Topic: "posts/*"}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	expectEvent := func(want bool, context string) {
		t.Helper()
		select {
		case msg := <-got:
			if !want {
				t.Fatalf("%s: unexpected delivery %q", context, msg.Event)
			}
		case <-time.After(500 * time.Millisecond):
			if want {
				t.Fatalf("%s: expected delivery, got none", context)
			}
		}
	}

	// WAL healthy → direct broadcasts suppressed.
	hub.SetDirectRecordBroadcastsSuppressed(true)
	if !hub.DirectRecordBroadcastsSuppressed() {
		t.Fatal("suppression flag not set")
	}
	hub.BroadcastRecord("create", "posts", "r1", map[string]any{"id": "r1"})
	expectEvent(false, "suppressed direct broadcast")

	// The WAL emission path bypasses the switch (it IS the WAL path).
	hub.BroadcastRecordFromWAL("create", "posts", "r2", map[string]any{"id": "r2"})
	expectEvent(true, "WAL emission while suppressed")

	// WAL degraded → API-emitted path resumes.
	hub.SetDirectRecordBroadcastsSuppressed(false)
	hub.BroadcastRecord("update", "posts", "r3", map[string]any{"id": "r3"})
	expectEvent(true, "direct broadcast after WAL degraded")
}
