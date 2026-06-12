package realtime

import (
	"encoding/json"
	"testing"
)

func TestSubscriptionFromRequestMergesOptions(t *testing.T) {
	req := &SubscribeRequest{
		Type:          "subscribe",
		Subscriptions: []string{"room1"},
		Query:         map[string]string{"filter": "a = 1", "fields": "id"},
		Options: &SubscribeOptions{
			Filter:   "b = 2",
			Presence: map[string]any{"name": "alice"},
		},
	}

	sub := subscriptionFromRequest("room1", req)
	if sub.Options.Filter != "b = 2" {
		t.Fatalf("options filter should win over query: %q", sub.Options.Filter)
	}
	if sub.Options.Fields != "id" {
		t.Fatalf("query fields should survive when options omit them: %q", sub.Options.Fields)
	}
	if sub.Options.Presence == nil || sub.Options.Presence["name"] != "alice" {
		t.Fatalf("presence state lost: %#v", sub.Options.Presence)
	}
}

func TestDecodeSubscribeRequestAcceptsOptions(t *testing.T) {
	raw := []byte(`{"type":"subscribe","clientId":"c1","subscriptions":["room1"],"options":{"presence":{"name":"bob"}}}`)
	req, err := decodeSubscribeRequest(raw, true)
	if err != nil {
		t.Fatalf("decode with options must succeed (SDK sends this envelope): %v", err)
	}
	if req.Options == nil || req.Options.Presence["name"] != "bob" {
		t.Fatalf("options not decoded: %#v", req.Options)
	}
}

func TestPresenceMembers(t *testing.T) {
	hub := NewHub()

	withPresence := newBaseClient("client_a", "test")
	withPresence.Subscribe("room1", &Subscription{
		Topic:   "room1",
		Options: &SubscribeOptions{Presence: map[string]any{"name": "alice"}},
	})
	plain := newBaseClient("client_b", "test")
	plain.Subscribe("room1", &Subscription{Topic: "room1", Options: &SubscribeOptions{}})

	hub.clients["client_a"] = &fakeClient{withPresence}
	hub.clients["client_b"] = &fakeClient{plain}
	hub.topicIndex["room1"] = map[string]bool{"client_a": true, "client_b": true}

	members := hub.PresenceMembers("room1")
	if len(members) != 1 {
		t.Fatalf("expected exactly the presence-enabled member, got %d", len(members))
	}
	if members[0].ClientID != "client_a" || members[0].State["name"] != "alice" {
		t.Fatalf("unexpected member: %#v", members[0])
	}
}

func TestClusterBroadcastEventRoundTrip(t *testing.T) {
	evt := clusterEvent{Type: "broadcast", Topic: "room1", Event: "cursor", ClientID: "c1", Payload: json.RawMessage(`{"x":1}`)}
	payload, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded clusterEvent
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Type != "broadcast" || decoded.Topic != "room1" || decoded.Event != "cursor" {
		t.Fatalf("round trip lost fields: %#v", decoded)
	}

	// Legacy record events (no type field) must keep decoding as records.
	var legacy clusterEvent
	if err := json.Unmarshal([]byte(`{"action":"create","collection":"posts","record_id":"r1"}`), &legacy); err != nil {
		t.Fatalf("legacy decode: %v", err)
	}
	if legacy.Type != "" || legacy.Action != "create" {
		t.Fatalf("legacy event mis-decoded: %#v", legacy)
	}
}

func TestIsReservedEventExported(t *testing.T) {
	for _, name := range []string{"record:create", "presence:join", "connection:established", "subscription:confirmed"} {
		if !IsReservedEvent(name) {
			t.Errorf("%q should be reserved", name)
		}
	}
	if IsReservedEvent("cursor-moved") {
		t.Errorf("custom event names must not be reserved")
	}
}

// fakeClient wraps baseClient with no-op transport methods.
type fakeClient struct{ *baseClient }

func (f *fakeClient) Send(msg *RealtimeMessage) error { return nil }
func (f *fakeClient) SendRaw(data []byte) error       { return nil }
func (f *fakeClient) Close()                          {}
