package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/gresbase/gresbase/internal/collection"
	"github.com/gresbase/gresbase/internal/database"
	"github.com/gresbase/gresbase/internal/events"
	"github.com/gresbase/gresbase/internal/forms"
)

// ---------------------------------------------------------------------------
// Collections
// ---------------------------------------------------------------------------

func (h *Handlers) CollectionsList(w http.ResponseWriter, r *http.Request) {
	event := &events.CollectionRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "list"}
	var colls []*collection.Collection
	if err := h.app.OnCollectionRequest().Trigger(event, func(e events.Event) error {
		var err error
		colls, err = h.app.Collections().ListCollections(r.Context())
		return err
	}); err != nil {
		writeError(w, 500, "Failed to list collections")
		return
	}
	writeOK(w, colls)
}

func (h *Handlers) CollectionsExport(w http.ResponseWriter, r *http.Request) {
	event := &events.CollectionRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "export"}
	var collectionsData []map[string]any
	if err := h.app.OnCollectionRequest().Trigger(event, func(e events.Event) error {
		var err error
		collectionsData, err = h.app.Collections().ExportCollections(r.Context())
		return err
	}); err != nil {
		writeError(w, 500, "Failed to export collections")
		return
	}
	writeOK(w, map[string]any{"collections": collectionsData})
}

func (h *Handlers) CollectionsCreate(w http.ResponseWriter, r *http.Request) {
	var form forms.CollectionUpsertForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid collection data")
		return
	}
	if err := form.Validate(h.app.Collections()); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.CollectionRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form.Collection), Action: "create", CollectionName: form.Name, Data: bodyToMap(form.Collection)}
	var coll collection.Collection
	if err := h.app.OnCollectionRequest().Trigger(event, func(e events.Event) error {
		var eventForm forms.CollectionUpsertForm
		if err := remarshalInto(event.Data, &eventForm.Collection); err != nil {
			return err
		}
		if err := eventForm.Validate(h.app.Collections()); err != nil {
			return err
		}
		coll = eventForm.Collection
		return h.app.Collections().CreateCollection(r.Context(), &coll)
	}); err != nil {
		if _, ok := err.(forms.Errors); ok {
			writeValidationError(w, err)
			return
		}
		writeInternalError(w, "Failed to create collection", err)
		return
	}

	postCommitCtx := cloneRequestContext(r.Context(), false)
	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	database.AfterCommit(r.Context(), func() {
		_ = h.app.OnCollectionCreate().Trigger(&events.CollectionEvent{App: h.app, CollectionID: coll.ID, CollectionName: coll.Name, Type: events.ModelEventCreate}, func(e events.Event) error { return e.Next() })
		h.app.Auth().RecordAudit(postCommitCtx, adminID, "collection.create", "_collections", coll.ID, map[string]any{"name": coll.Name}, r)
	})
	writeJSON(w, 201, coll)
}

func (h *Handlers) CollectionsGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	event := &events.CollectionRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "view", CollectionID: id}
	var coll *collection.Collection
	if err := h.app.OnCollectionRequest().Trigger(event, func(e events.Event) error {
		var err error
		coll, err = h.app.Collections().GetCollection(r.Context(), id)
		if err == nil && coll != nil {
			event.CollectionID = coll.ID
			event.CollectionName = coll.Name
		}
		return err
	}); err != nil {
		writeError(w, 404, "Collection not found")
		return
	}
	writeOK(w, coll)
}

func (h *Handlers) CollectionsUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var form forms.CollectionUpsertForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid collection data")
		return
	}
	form.ID = id
	if err := form.Validate(h.app.Collections()); err != nil {
		writeValidationError(w, err)
		return
	}
	event := &events.CollectionRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form.Collection), Action: "update", CollectionID: id, CollectionName: form.Name, Data: bodyToMap(form.Collection)}
	var coll collection.Collection
	if err := h.app.OnCollectionRequest().Trigger(event, func(e events.Event) error {
		var eventForm forms.CollectionUpsertForm
		if err := remarshalInto(event.Data, &eventForm.Collection); err != nil {
			return err
		}
		eventForm.ID = id
		if err := eventForm.Validate(h.app.Collections()); err != nil {
			return err
		}
		coll = eventForm.Collection
		return h.app.Collections().UpdateCollection(r.Context(), &coll)
	}); err != nil {
		if _, ok := err.(forms.Errors); ok {
			writeValidationError(w, err)
			return
		}
		writeInternalError(w, "Failed to update collection", err)
		return
	}
	postCommitCtx := cloneRequestContext(r.Context(), false)
	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	database.AfterCommit(r.Context(), func() {
		_ = h.app.OnCollectionUpdate().Trigger(&events.CollectionEvent{App: h.app, CollectionID: coll.ID, CollectionName: coll.Name, Type: events.ModelEventUpdate}, func(e events.Event) error { return e.Next() })
		h.app.Auth().RecordAudit(postCommitCtx, adminID, "collection.update", "_collections", coll.ID, map[string]any{"name": coll.Name}, r)
	})
	writeOK(w, coll)
}

func (h *Handlers) CollectionsDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	coll, _ := h.app.Collections().GetCollection(r.Context(), id)
	event := &events.CollectionRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "delete", CollectionID: id}
	if coll != nil {
		event.CollectionName = coll.Name
	}
	if err := h.app.OnCollectionRequest().Trigger(event, func(e events.Event) error {
		return h.app.Collections().DeleteCollection(r.Context(), id)
	}); err != nil {
		writeInternalError(w, "Failed to delete collection", err)
		return
	}
	if coll != nil {
		postCommitCtx := cloneRequestContext(r.Context(), false)
		adminID, _ := r.Context().Value(contextKeyAdminID).(string)
		deletedPrefix := coll.Name + "/"
		database.AfterCommit(r.Context(), func() {
			_ = h.app.OnCollectionDelete().Trigger(&events.CollectionEvent{App: h.app, CollectionID: coll.ID, CollectionName: coll.Name, Type: events.ModelEventDelete}, func(e events.Event) error { return e.Next() })
			_ = h.app.Storage().DeletePrefix(postCommitCtx, deletedPrefix)
			h.app.Auth().RecordAudit(postCommitCtx, adminID, "collection.delete", "_collections", coll.ID, map[string]any{"name": coll.Name, "deleted_storage_prefix": deletedPrefix}, r)
		})
	}
	writeOK(w, map[string]any{"deleted": id})
}

func (h *Handlers) CollectionsImport(w http.ResponseWriter, r *http.Request) {
	var form forms.CollectionsImportForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid import data")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}
	event := &events.CollectionsImportRequestEvent{
		App:             h.app,
		Request:         r,
		Info:            toEventRequestInfoWithBody(r, form),
		CollectionsData: form.Collections,
		DeleteMissing:   form.DeleteMissing,
	}
	if err := h.app.OnCollectionsImportRequest().Trigger(event, func(e events.Event) error {
		return h.app.Collections().ImportCollections(r.Context(), event.CollectionsData, event.DeleteMissing)
	}); err != nil {
		writeInternalError(w, "Failed to import collections", err)
		return
	}
	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "collection.import", "_collections", "", map[string]any{"imported": len(event.CollectionsData), "deleteMissing": event.DeleteMissing}, r)
	writeOK(w, map[string]any{"imported": len(event.CollectionsData), "deleteMissing": event.DeleteMissing})
}

// CollectionsRefreshView refreshes a materialized view collection.
func (h *Handlers) CollectionsRefreshView(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	coll, err := h.app.Collections().GetCollection(r.Context(), id)
	if err != nil {
		writeError(w, 404, "Collection not found")
		return
	}
	if err := h.app.Collections().RefreshView(r.Context(), coll); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "collection.refresh_view", "_collections", coll.ID, nil, r)
	writeOK(w, map[string]any{"refreshed": coll.Name})
}
