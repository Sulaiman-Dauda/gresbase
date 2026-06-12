package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gresbase/gresbase/internal/events"
	"github.com/gresbase/gresbase/internal/realtime"
)

// ---------------------------------------------------------------------------
// RealtimeConnect handles WebSocket and SSE connections via the realtime hub.
// WebSocket: ws://host/api/v1/realtime
// SSE: GET /api/v1/sse (primary), POST /api/v1/realtime (subscription management)
func (h *Handlers) RealtimeConnect(w http.ResponseWriter, r *http.Request) {
	action := "connect"
	transport := "websocket"
	if r.Method == http.MethodPost {
		action = "subscribe"
		transport = "sse-subscription"
	} else if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		transport = "sse"
	}

	event := &events.RealtimeRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: action, Transport: transport}
	if err := h.app.OnRealtimeRequest().Trigger(event, func(e events.Event) error {
		if r.Method == http.MethodPost {
			h.app.Realtime().HandleSSESubscription(w, r)
			return nil
		}
		if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			h.app.Realtime().HandleSSE(w, r)
			return nil
		}
		h.app.Realtime().HandleWebSocket(w, r)
		return nil
	}); err != nil {
		writeInternalError(w, "realtime connect", err)
		return
	}
}

// RealtimeSSE is a dedicated SSE endpoint (GET /api/v1/sse).
func (h *Handlers) RealtimeSSE(w http.ResponseWriter, r *http.Request) {
	event := &events.RealtimeRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "sse", Transport: "sse"}
	if err := h.app.OnRealtimeRequest().Trigger(event, func(e events.Event) error {
		h.app.Realtime().HandleSSE(w, r)
		return nil
	}); err != nil {
		writeInternalError(w, "realtime sse", err)
		return
	}
}

// RealtimeBroadcast publishes a client message to a custom realtime channel
// (POST /api/v1/realtime/broadcast). Requires authentication; channels that
// shadow collection record topics and reserved event names are rejected so
// clients cannot spoof server events.
func (h *Handlers) RealtimeBroadcast(w http.ResponseWriter, r *http.Request) {
	info := NewRequestInfo(r)
	if !info.IsAdmin && !info.IsRecordAuth {
		writeError(w, 401, "Authentication required")
		return
	}

	var form struct {
		Channel string          `json:"channel"`
		Event   string          `json:"event"`
		Data    json.RawMessage `json:"data"`
	}
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	form.Channel = strings.TrimSpace(form.Channel)
	form.Event = strings.TrimSpace(form.Event)
	if form.Channel == "" {
		writeError(w, 400, "channel is required")
		return
	}
	if form.Event == "" {
		form.Event = "message"
	}
	if realtime.IsReservedEvent(form.Event) {
		writeError(w, 400, "event name is reserved for server messages")
		return
	}
	if !h.app.Realtime().CanPublishToChannel(form.Channel) {
		writeError(w, 400, "cannot broadcast to this channel")
		return
	}

	h.app.Realtime().BroadcastChannel(form.Channel, &realtime.RealtimeMessage{
		Event:     form.Event,
		Channel:   form.Channel,
		Topic:     form.Channel,
		Data:      form.Data,
		Timestamp: time.Now().UnixMilli(),
	})
	w.WriteHeader(204)
}

func realtimeMsg(event string, data any) realtime.RealtimeMessage {
	payload, _ := json.Marshal(data)
	return realtime.RealtimeMessage{
		Event:     event,
		Timestamp: time.Now().UnixMilli(),
		Data:      payload,
	}
}
