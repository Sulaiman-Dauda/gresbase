package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/gresbase/gresbase/internal/webhook"
)

// ---------------------------------------------------------------------------
// Webhooks (admin-only; routes mounted under /api/v1/webhooks)
// ---------------------------------------------------------------------------

// webhookForm uses pointers so updates can distinguish "omitted" from "zero".
type webhookForm struct {
	Name        *string           `json:"name"`
	URL         *string           `json:"url"`
	Secret      *string           `json:"secret"`
	Events      []string          `json:"events"`
	Collections []string          `json:"collections"`
	Enabled     *bool             `json:"enabled"`
	Headers     map[string]string `json:"headers"`
}

// maskedWebhook returns a copy safe for listing responses: the secret is only
// ever shown in full once, on create.
func maskedWebhook(hook *webhook.Webhook) *webhook.Webhook {
	c := *hook
	c.Secret = webhook.MaskSecret(c.Secret)
	return &c
}

func (h *Handlers) WebhooksList(w http.ResponseWriter, r *http.Request) {
	hooks, err := h.app.Webhooks().List(r.Context())
	if err != nil {
		writeError(w, 500, "Failed to list webhooks: "+err.Error())
		return
	}
	masked := make([]*webhook.Webhook, 0, len(hooks))
	for _, hook := range hooks {
		masked = append(masked, maskedWebhook(hook))
	}
	writeOK(w, map[string]any{"webhooks": masked})
}

func (h *Handlers) WebhooksCreate(w http.ResponseWriter, r *http.Request) {
	var form webhookForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if form.Name == nil || *form.Name == "" {
		writeError(w, 400, "Webhook name is required")
		return
	}
	if form.URL == nil || *form.URL == "" {
		writeError(w, 400, "Webhook url is required")
		return
	}

	hook := &webhook.Webhook{
		Name:        *form.Name,
		URL:         *form.URL,
		Events:      form.Events,
		Collections: form.Collections,
		Enabled:     true,
		Headers:     form.Headers,
	}
	if form.Secret != nil {
		hook.Secret = *form.Secret
	}
	if form.Enabled != nil {
		hook.Enabled = *form.Enabled
	}

	created, err := h.app.Webhooks().Create(r.Context(), hook)
	if err != nil {
		writeError(w, 400, "Failed to create webhook: "+err.Error())
		return
	}

	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "webhook.create", "webhook", created.ID,
		map[string]any{"name": created.Name, "url": created.URL, "events": created.Events}, r)

	// Full secret in this response only; every later read is masked.
	writeJSON(w, 201, created)
}

func (h *Handlers) WebhooksGet(w http.ResponseWriter, r *http.Request) {
	hook, err := h.app.Webhooks().Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 404, "Webhook not found")
		return
	}
	writeOK(w, maskedWebhook(hook))
}

func (h *Handlers) WebhooksUpdate(w http.ResponseWriter, r *http.Request) {
	hook, err := h.app.Webhooks().Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 404, "Webhook not found")
		return
	}

	var form webhookForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if form.Name != nil {
		hook.Name = *form.Name
	}
	if form.URL != nil {
		hook.URL = *form.URL
	}
	if form.Secret != nil {
		hook.Secret = *form.Secret // empty string keeps the stored secret
	}
	if form.Events != nil {
		hook.Events = form.Events
	}
	if form.Collections != nil {
		hook.Collections = form.Collections
	}
	if form.Enabled != nil {
		hook.Enabled = *form.Enabled
	}
	if form.Headers != nil {
		hook.Headers = form.Headers
	}

	updated, err := h.app.Webhooks().Update(r.Context(), hook)
	if err != nil {
		writeError(w, 400, "Failed to update webhook: "+err.Error())
		return
	}

	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "webhook.update", "webhook", updated.ID,
		map[string]any{"name": updated.Name, "url": updated.URL, "enabled": updated.Enabled}, r)
	writeOK(w, maskedWebhook(updated))
}

func (h *Handlers) WebhooksDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := h.app.Webhooks().Get(r.Context(), id); err != nil {
		writeError(w, 404, "Webhook not found")
		return
	}
	if err := h.app.Webhooks().Delete(r.Context(), id); err != nil {
		writeError(w, 500, "Failed to delete webhook: "+err.Error())
		return
	}

	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "webhook.delete", "webhook", id, nil, r)
	writeOK(w, map[string]any{"deleted": id})
}

func (h *Handlers) WebhooksDeliveries(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	deliveries, err := h.app.Webhooks().ListDeliveries(r.Context(), id, limit)
	if err != nil {
		writeError(w, 500, "Failed to list deliveries: "+err.Error())
		return
	}
	writeOK(w, map[string]any{"deliveries": deliveries})
}

func (h *Handlers) WebhooksTest(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	delivery, err := h.app.Webhooks().TestSend(r.Context(), id)
	if err != nil {
		writeError(w, 404, "Webhook not found")
		return
	}

	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "webhook.test", "webhook", id,
		map[string]any{"success": delivery.Success, "status_code": delivery.StatusCode}, r)
	writeOK(w, delivery)
}
