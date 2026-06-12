package realtime

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"
)

// ClusterChannel is the Postgres LISTEN/NOTIFY channel used to fan record
// events across application nodes.
const ClusterChannel = "gresbase_realtime"

// maxNotifyPayload is the safety threshold below Postgres' 8000-byte NOTIFY
// limit. Events whose JSON exceeds this are published without the record body;
// subscribers still learn the record changed and can refetch.
const maxNotifyPayload = 7000

// clusterEvent is the wire format published over LISTEN/NOTIFY. Type "" or
// "record" carries a record mutation; "broadcast" carries a custom-channel
// message (client broadcast or presence announcement).
type clusterEvent struct {
	Type       string          `json:"type,omitempty"`
	Action     string          `json:"action,omitempty"`
	Collection string          `json:"collection,omitempty"`
	RecordID   string          `json:"record_id,omitempty"`
	Data       map[string]any  `json:"data,omitempty"`
	Topic      string          `json:"topic,omitempty"`
	Event      string          `json:"event,omitempty"`
	ClientID   string          `json:"client_id,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

// Connector opens a fresh dedicated database connection (used for the
// long-lived LISTEN connection). Implemented by *database.DB.
type Connector interface {
	Connect(ctx context.Context) (*pgx.Conn, error)
}

// EnableCluster switches the hub into multi-node mode. Record events are then
// published via the provided publish function (a Postgres NOTIFY) instead of
// being delivered to local subscribers directly; the cluster listener delivers
// them on every node, including this one. This makes the app tier horizontally
// scalable using only PostgreSQL.
func (h *Hub) EnableCluster(publish func(ctx context.Context, payload string) error) {
	h.mu.Lock()
	h.crossNode = true
	h.publish = publish
	h.mu.Unlock()
}

// publishRecordEvent serializes and publishes a record event to the cluster.
func (h *Hub) publishRecordEvent(action, collectionName, recordID string, data map[string]any) {
	h.mu.RLock()
	publish := h.publish
	h.mu.RUnlock()
	if publish == nil {
		return
	}

	evt := clusterEvent{Action: action, Collection: collectionName, RecordID: recordID, Data: data}
	payload, err := json.Marshal(evt)
	if err != nil {
		return
	}
	if len(payload) > maxNotifyPayload {
		// Too large for NOTIFY: drop the body but still announce the change.
		evt.Data = nil
		payload, err = json.Marshal(evt)
		if err != nil {
			return
		}
		log.Warn().Str("collection", collectionName).Str("record", recordID).
			Msg("realtime event exceeds NOTIFY payload limit; delivered without record body")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := publish(ctx, string(payload)); err != nil {
		log.Warn().Err(err).Msg("failed to publish realtime cluster event")
	}
}

// publishChannelEvent publishes a custom-channel broadcast to the cluster.
func (h *Hub) publishChannelEvent(topic string, msg *RealtimeMessage) {
	h.mu.RLock()
	publish := h.publish
	h.mu.RUnlock()
	if publish == nil {
		return
	}

	evt := clusterEvent{Type: "broadcast", Topic: topic, Event: msg.Event, ClientID: msg.ClientID, Payload: msg.Data}
	payload, err := json.Marshal(evt)
	if err != nil {
		return
	}
	if len(payload) > maxNotifyPayload {
		evt.Payload = nil
		payload, err = json.Marshal(evt)
		if err != nil {
			return
		}
		log.Warn().Str("topic", topic).Str("event", msg.Event).
			Msg("realtime broadcast exceeds NOTIFY payload limit; delivered without body")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := publish(ctx, string(payload)); err != nil {
		log.Warn().Err(err).Msg("failed to publish realtime cluster broadcast")
	}
}

// RunClusterListener opens a dedicated LISTEN connection and delivers every
// record event it receives to this node's local subscribers. It reconnects
// with backoff until ctx is cancelled. Run in a goroutine.
func (h *Hub) RunClusterListener(ctx context.Context, connector Connector) {
	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := h.listenOnce(ctx, connector); err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Warn().Err(err).Dur("retry_in", backoff).Msg("realtime cluster listener disconnected")
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
	}
}

func (h *Hub) listenOnce(ctx context.Context, connector Connector) error {
	conn, err := connector.Connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	if _, err := conn.Exec(ctx, "LISTEN "+ClusterChannel); err != nil {
		return err
	}
	log.Info().Str("channel", ClusterChannel).Msg("realtime cluster listener connected")

	for {
		notification, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var evt clusterEvent
		if err := json.Unmarshal([]byte(notification.Payload), &evt); err != nil {
			log.Debug().Err(err).Msg("ignoring malformed realtime cluster event")
			continue
		}
		switch evt.Type {
		case "broadcast":
			h.Broadcast(&BroadcastRequest{Topic: evt.Topic, Message: &RealtimeMessage{
				Event:     evt.Event,
				ClientID:  evt.ClientID,
				Topic:     evt.Topic,
				Data:      evt.Payload,
				Timestamp: time.Now().UnixMilli(),
			}})
		default:
			h.localBroadcastRecord(evt.Action, evt.Collection, evt.RecordID, evt.Data)
		}
	}
}
