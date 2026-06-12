package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

const (
	workerCount    = 4
	maxAttempts    = 3
	requestTimeout = 10 * time.Second
)

// retryBackoff is the wait schedule between failed attempts. A var so tests
// can shrink it.
var retryBackoff = []time.Duration{time.Second, 5 * time.Second, 25 * time.Second}

type task struct {
	hook  *Webhook
	event string
	body  []byte
}

// Start launches the delivery worker pool.
func (s *Service) Start(ctx context.Context) {
	s.ctx, s.cancel = context.WithCancel(ctx)
	for i := 0; i < workerCount; i++ {
		s.wg.Add(1)
		go s.worker()
	}
	log.Info().Msg("Webhook dispatcher started")
}

// Stop cancels in-flight deliveries and waits for the workers to exit.
func (s *Service) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
	log.Info().Msg("Webhook dispatcher stopped")
}

func (s *Service) worker() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case t := <-s.queue:
			s.deliver(s.ctx, t.hook, t.event, t.body)
		}
	}
}

// Fire enqueues deliveries for every enabled webhook matching the event and
// collection. It never blocks the caller: if the queue is full the delivery
// is dropped (and logged) rather than stalling the request path.
func (s *Service) Fire(ctx context.Context, eventName, collectionName string, payload any) {
	hooks, err := s.List(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Webhook dispatch: failed to load webhooks")
		return
	}

	var body []byte
	for _, hook := range hooks {
		if !Matches(hook, eventName, collectionName) {
			continue
		}
		if body == nil {
			body, err = BuildBody(eventName, collectionName, payload)
			if err != nil {
				log.Error().Err(err).Str("event", eventName).Msg("Webhook dispatch: failed to encode payload")
				return
			}
		}
		select {
		case s.queue <- task{hook: hook, event: eventName, body: body}:
		default:
			log.Warn().Str("webhook", hook.ID).Str("event", eventName).Msg("Webhook queue full, delivery dropped")
		}
	}
}

// Matches reports whether a webhook is subscribed to the given event and
// collection. "*" matches any event; an empty collections list matches all.
func Matches(hook *Webhook, event, collection string) bool {
	if !hook.Enabled {
		return false
	}
	eventMatch := false
	for _, e := range hook.Events {
		if e == "*" || e == event {
			eventMatch = true
			break
		}
	}
	if !eventMatch {
		return false
	}
	if len(hook.Collections) == 0 {
		return true
	}
	for _, c := range hook.Collections {
		if c == collection {
			return true
		}
	}
	return false
}

// BuildBody encodes the delivery envelope sent to webhook endpoints.
func BuildBody(event, collection string, payload any) ([]byte, error) {
	return json.Marshal(map[string]any{
		"event":      event,
		"collection": collection,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
		"data":       payload,
	})
}

// Sign computes the Stripe-style signature header value
// "t=<unix>,v1=<hex hmac-sha256 of '<t>.<body>'>". Including the timestamp in
// the signed material lets receivers reject replayed deliveries.
func Sign(secret string, t time.Time, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", t.Unix())
	mac.Write(body)
	return fmt.Sprintf("t=%d,v1=%s", t.Unix(), hex.EncodeToString(mac.Sum(nil)))
}

// deliver POSTs the body with up to maxAttempts attempts, recording each one.
func (s *Service) deliver(ctx context.Context, hook *Webhook, event string, body []byte) *Delivery {
	var last *Delivery
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		last = s.attempt(ctx, hook, event, body, attempt)
		s.recordDelivery(ctx, last)
		if last.Success {
			return last
		}
		if attempt < maxAttempts && !sleepCtx(ctx, backoffFor(attempt)) {
			return last
		}
	}
	log.Warn().Str("webhook", hook.ID).Str("event", event).Str("error", last.Error).
		Msg("Webhook delivery failed after all attempts")
	return last
}

// attempt performs a single signed POST and returns the result without
// recording it.
func (s *Service) attempt(ctx context.Context, hook *Webhook, event string, body []byte, attempt int) *Delivery {
	d := &Delivery{
		WebhookID: hook.ID,
		Event:     event,
		Payload:   body,
		Attempt:   attempt,
		CreatedAt: time.Now(),
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, hook.URL, bytes.NewReader(body))
	if err != nil {
		d.Error = err.Error()
		return d
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gresbase-Event", event)
	req.Header.Set("X-Gresbase-Delivery", uuid.New().String())
	req.Header.Set("X-Gresbase-Signature", Sign(hook.Secret, time.Now(), body))
	for k, v := range hook.Headers {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := s.client.Do(req)
	d.DurationMS = int(time.Since(start).Milliseconds())
	if err != nil {
		d.Error = err.Error()
		return d
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	d.StatusCode = resp.StatusCode
	d.Success = resp.StatusCode >= 200 && resp.StatusCode < 300
	if !d.Success {
		d.Error = "non-2xx response: " + resp.Status
	}
	return d
}

// TestSend fires a synthetic {"event":"test"} delivery synchronously (single
// attempt, no retries) and returns the result — backs the dashboard's "send
// test" button.
func (s *Service) TestSend(ctx context.Context, webhookID string) (*Delivery, error) {
	hook, err := s.Get(ctx, webhookID)
	if err != nil {
		return nil, err
	}
	body, err := BuildBody("test", "", map[string]any{"message": "Test delivery from Gresbase"})
	if err != nil {
		return nil, err
	}
	d := s.attempt(ctx, hook, "test", body, 1)
	s.recordDelivery(ctx, d)
	return d, nil
}

func (s *Service) recordDelivery(ctx context.Context, d *Delivery) {
	if s.onDelivery != nil {
		s.onDelivery(d)
	}
	if s.db == nil {
		return
	}
	err := s.db.QueryRow(ctx, `
		INSERT INTO _webhook_deliveries (webhook_id, event, payload, status_code, attempt, success, error, duration_ms, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		d.WebhookID, d.Event, []byte(d.Payload), d.StatusCode, d.Attempt, d.Success, d.Error, d.DurationMS, d.CreatedAt).Scan(&d.ID)
	if err != nil {
		log.Error().Err(err).Str("webhook", d.WebhookID).Msg("Failed to record webhook delivery")
		return
	}
	if err := s.Prune(ctx, d.WebhookID); err != nil {
		log.Debug().Err(err).Str("webhook", d.WebhookID).Msg("Webhook delivery prune failed")
	}
}

func backoffFor(attempt int) time.Duration {
	idx := attempt - 1
	if idx >= len(retryBackoff) {
		idx = len(retryBackoff) - 1
	}
	return retryBackoff[idx]
}

// sleepCtx waits for d or until ctx is canceled; returns false on cancel.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
