package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/gresbase/gresbase/internal/collection"
	"github.com/gresbase/gresbase/internal/database"
	"github.com/gresbase/gresbase/internal/events"
	"github.com/gresbase/gresbase/internal/filter"
	"github.com/gresbase/gresbase/internal/forms"
	"github.com/gresbase/gresbase/internal/query"
	"github.com/gresbase/gresbase/internal/tools/search"
	"github.com/rs/zerolog/log"
)

// ---------------------------------------------------------------------------
// Records (with validation and rules)
// ---------------------------------------------------------------------------

func (h *Handlers) RecordsList(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	coll, err := h.app.Collections().GetCollectionByName(r.Context(), collName)
	if err != nil {
		writeError(w, 404, "Collection not found: "+collName)
		return
	}
	if !requireAPIKeyPermission(w, r, "records.read") {
		return
	}

	params := query.ParseParams(r.URL.RawQuery)
	ruleFilter, allowed := h.evaluateRuleWhere(r, coll.ListRule, nil)
	if !allowed {
		writeError(w, 403, "Access denied")
		return
	}

	event := &events.RecordListRequestEvent{
		App:            h.app,
		Request:        r,
		Info:           toEventRequestInfo(r),
		CollectionID:   coll.ID,
		CollectionName: coll.Name,
		Filter:         params.Filter,
		Sort:           params.Sort,
		Expand:         params.Expand,
		Fields:         params.Fields,
		Page:           params.Page,
		PerPage:        params.PerPage,
	}
	var records []map[string]any
	var total int
	if err := h.app.OnRecordsListRequest().Trigger(event, func(e events.Event) error {
		// Expansion is intentionally left out of the builder: applyExpand
		// below resolves it with target-collection rules enforced.
		params = query.Params{
			Filter:  event.Filter,
			Sort:    event.Sort,
			Fields:  event.Fields,
			Page:    event.Page,
			PerPage: event.PerPage,
		}
		qb := query.NewBuilder(h.app.DB(), coll).WithParams(params)
		if ruleFilter != "" {
			qb.WithAccessRule(ruleFilter)
		}
		var err error
		records, total, err = qb.List(r.Context())
		return err
	}); err != nil {
		writeInternalError(w, "Failed to list records", err)
		return
	}

	if records == nil {
		records = []map[string]any{}
	}

	if event.Expand != "" {
		h.applyExpand(r, coll, records, event.Expand)
	}

	if event.Fields != "" && event.Fields != "*" {
		records = pickFieldsFromRecords(records, event.Fields)
	}

	writeOK(w, map[string]any{
		"items":      records,
		"page":       event.Page,
		"perPage":    event.PerPage,
		"totalItems": total,
		"totalPages": maxInt(1, (total+event.PerPage-1)/event.PerPage),
	})
}

func (h *Handlers) RecordsCreate(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	coll, err := h.app.Collections().GetCollectionByName(r.Context(), collName)
	if err != nil {
		writeError(w, 404, "Collection not found: "+collName)
		return
	}
	if !requireAPIKeyPermission(w, r, "records.write") {
		return
	}

	var data map[string]any
	if err := decodeJSONBody(r, &data); err != nil {
		writeError(w, 400, "Invalid record data")
		return
	}

	if !h.checkRule(r, coll.CreateRule, data, data) {
		writeError(w, 403, "Access denied")
		return
	}

	event := &events.RecordCreateRequestEvent{
		App:            h.app,
		Request:        r,
		Info:           toEventRequestInfoWithBody(r, data),
		CollectionID:   coll.ID,
		CollectionName: coll.Name,
		Data:           data,
	}
	var record map[string]any
	if err := h.app.OnRecordCreateRequest().Trigger(event, func(e events.Event) error {
		if err := h.app.Collections().ValidateRecord(coll, event.Data); err != nil {
			return err
		}
		var err error
		record, err = h.app.Collections().CreateRecord(r.Context(), coll, event.Data)
		return err
	}); err != nil {
		if errors.Is(err, collection.ErrViewReadOnly) {
			writeError(w, 400, collection.ErrViewReadOnly.Error())
			return
		}
		if strings.HasPrefix(err.Error(), "field") || strings.Contains(err.Error(), "validation") || strings.Contains(err.Error(), "required") {
			writeError(w, 400, "Validation failed: "+err.Error())
			return
		}
		writeInternalError(w, "Failed to create record", err)
		return
	}

	if rid, ok := record["id"].(string); ok {
		postCommitCtx := cloneRequestContext(r.Context(), false)
		adminID, _ := r.Context().Value(contextKeyAdminID).(string)
		database.AfterCommit(r.Context(), func() {
			_ = h.app.OnRecordCreate().Trigger(&events.RecordEvent{App: h.app, Record: record, CollectionID: coll.ID, CollectionName: coll.Name, RecordID: rid, Type: events.ModelEventCreate}, func(e events.Event) error { return e.Next() })
			h.app.Realtime().BroadcastRecord("create", collName, rid, record)
			h.app.Auth().RecordAudit(postCommitCtx, adminID, "record.create", collName, rid, nil, r)
		})
	}

	writeJSON(w, 201, record)
}

func (h *Handlers) RecordsGet(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	recordID := chi.URLParam(r, "recordId")

	coll, err := h.app.Collections().GetCollectionByName(r.Context(), collName)
	if err != nil {
		writeError(w, 404, "Collection not found")
		return
	}
	if !requireAPIKeyPermission(w, r, "records.read") {
		return
	}

	record, err := h.app.Collections().GetRecord(r.Context(), coll, recordID)
	if err != nil {
		writeError(w, 404, "Record not found")
		return
	}

	if !h.checkRule(r, coll.ViewRule, nil, record) {
		writeError(w, 403, "Access denied")
		return
	}

	event := &events.RecordViewRequestEvent{
		App:            h.app,
		Request:        r,
		Info:           toEventRequestInfo(r),
		CollectionID:   coll.ID,
		CollectionName: coll.Name,
		RecordID:       recordID,
		Expand:         r.URL.Query().Get("expand"),
		Fields:         r.URL.Query().Get("fields"),
	}
	if err := h.app.OnRecordViewRequest().Trigger(event, func(e events.Event) error {
		if event.Expand != "" {
			h.applyExpand(r, coll, []map[string]any{record}, event.Expand)
		}
		if event.Fields != "" && event.Fields != "*" {
			record = pickSingleRecordFields(record, event.Fields)
		}
		return nil
	}); err != nil {
		writeError(w, 404, "Record not found")
		return
	}

	writeOK(w, record)
}

func (h *Handlers) RecordsUpdate(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	recordID := chi.URLParam(r, "recordId")

	coll, err := h.app.Collections().GetCollectionByName(r.Context(), collName)
	if err != nil {
		writeError(w, 404, "Collection not found")
		return
	}
	if !requireAPIKeyPermission(w, r, "records.write") {
		return
	}

	var data map[string]any
	if err := decodeJSONBody(r, &data); err != nil {
		writeError(w, 400, "Invalid body")
		return
	}

	existing, err := h.app.Collections().GetRecord(r.Context(), coll, recordID)
	if err != nil {
		writeError(w, 404, "Record not found")
		return
	}

	if !h.checkRule(r, coll.UpdateRule, data, existing) {
		writeError(w, 403, "Access denied")
		return
	}

	event := &events.RecordUpdateRequestEvent{
		App:            h.app,
		Request:        r,
		Info:           toEventRequestInfoWithBody(r, data),
		CollectionID:   coll.ID,
		CollectionName: coll.Name,
		RecordID:       recordID,
		Data:           data,
	}
	var record map[string]any
	if err := h.app.OnRecordUpdateRequest().Trigger(event, func(e events.Event) error {
		if err := h.app.Collections().ValidateRecordPatch(coll, event.Data); err != nil {
			return err
		}
		if err := h.app.Collections().UpdateRecord(r.Context(), coll, recordID, event.Data); err != nil {
			return err
		}
		record, _ = h.app.Collections().GetRecord(r.Context(), coll, recordID)
		return nil
	}); err != nil {
		if errors.Is(err, collection.ErrViewReadOnly) {
			writeError(w, 400, collection.ErrViewReadOnly.Error())
			return
		}
		if strings.HasPrefix(err.Error(), "field") || strings.Contains(err.Error(), "validation") || strings.Contains(err.Error(), "required") {
			writeError(w, 400, "Validation failed: "+err.Error())
			return
		}
		writeInternalError(w, "Failed to update", err)
		return
	}

	removedFilePaths := removedRecordFilePaths(coll, recordID, existing, record)
	postCommitCtx := cloneRequestContext(r.Context(), false)
	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	database.AfterCommit(r.Context(), func() {
		// Fire-and-forget lifecycle notification; the write already committed.
		_ = h.app.OnRecordUpdate().Trigger(&events.RecordEvent{App: h.app, Record: record, CollectionID: coll.ID, CollectionName: coll.Name, RecordID: recordID, Type: events.ModelEventUpdate}, func(e events.Event) error { return e.Next() })
		h.app.Realtime().BroadcastRecord("update", collName, recordID, record)
		for _, path := range removedFilePaths {
			if err := h.app.Storage().Delete(postCommitCtx, path); err != nil {
				log.Warn().Err(err).Str("collection", collName).Str("record", recordID).Str("path", path).Msg("Failed to delete orphaned file after record update")
			}
		}
		h.app.Auth().RecordAudit(postCommitCtx, adminID, "record.update", collName, recordID, map[string]any{"deleted_files": removedFilePaths}, r)
	})
	writeOK(w, record)
}

func (h *Handlers) RecordsDelete(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	recordID := chi.URLParam(r, "recordId")

	coll, err := h.app.Collections().GetCollectionByName(r.Context(), collName)
	if err != nil {
		writeError(w, 404, "Collection not found")
		return
	}
	if !requireAPIKeyPermission(w, r, "records.write") {
		return
	}

	existing, err := h.app.Collections().GetRecord(r.Context(), coll, recordID)
	if err != nil {
		writeError(w, 404, "Record not found")
		return
	}

	if !h.checkRule(r, coll.DeleteRule, nil, existing) {
		writeError(w, 403, "Access denied")
		return
	}

	event := &events.RecordDeleteRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), CollectionID: coll.ID, CollectionName: coll.Name, RecordID: recordID}
	if err := h.app.OnRecordDeleteRequest().Trigger(event, func(e events.Event) error {
		return h.app.Collections().DeleteRecord(r.Context(), coll, recordID)
	}); err != nil {
		if errors.Is(err, collection.ErrViewReadOnly) {
			writeError(w, 400, collection.ErrViewReadOnly.Error())
			return
		}
		writeInternalError(w, "Failed to delete", err)
		return
	}

	deleted := map[string]any{"id": recordID}
	filePathsToDelete := recordFilePaths(coll, recordID, existing)
	postCommitCtx := cloneRequestContext(r.Context(), false)
	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	database.AfterCommit(r.Context(), func() {
		// Fire-and-forget lifecycle notification; the delete already committed.
		_ = h.app.OnRecordDelete().Trigger(&events.RecordEvent{App: h.app, Record: deleted, CollectionID: coll.ID, CollectionName: coll.Name, RecordID: recordID, Type: events.ModelEventDelete}, func(e events.Event) error { return e.Next() })
		h.app.Realtime().BroadcastRecord("delete", collName, recordID, deleted)
		for _, path := range filePathsToDelete {
			if err := h.app.Storage().Delete(postCommitCtx, path); err != nil {
				log.Warn().Err(err).Str("collection", collName).Str("record", recordID).Str("path", path).Msg("Failed to delete file after record delete")
			}
		}
		h.app.Auth().RecordAudit(postCommitCtx, adminID, "record.delete", collName, recordID, map[string]any{"deleted_files": filePathsToDelete}, r)
	})
	writeOK(w, map[string]any{"deleted": recordID})
}

// ---------------------------------------------------------------------------
// Batch
// ---------------------------------------------------------------------------

func (h *Handlers) Batch(w http.ResponseWriter, r *http.Request) {
	h.HandleBatch(w, r)
}

// ---------------------------------------------------------------------------
// Search (Full-Text Search)
// ---------------------------------------------------------------------------

func (h *Handlers) SearchRecords(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	if !requireAPIKeyPermission(w, r, "search.read") {
		return
	}
	coll, err := h.app.Collections().GetCollectionByName(r.Context(), collName)
	if err != nil {
		writeError(w, 404, "Collection not found: "+collName)
		return
	}
	// Full-text search is a read; it must honor the collection list rule.
	ruleFilter, allowed := h.evaluateRuleWhere(r, coll.ListRule, nil)
	if !allowed {
		writeError(w, 403, "Access denied")
		return
	}
	var ruleExpr *filter.Expr
	if ruleFilter != "" {
		ruleExpr, err = filter.ParseFilter(ruleFilter)
		if err != nil {
			writeError(w, 403, "Access denied")
			return
		}
	}

	var form forms.SearchForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	event := &events.SearchRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), CollectionName: collName, Query: form.Query, Language: form.Language, Page: form.Page, PerPage: form.PerPage, Highlight: form.Highlight, Rank: form.Rank}
	var results []search.SearchResult
	var total int64
	if err := h.app.OnSearchRequest().Trigger(event, func(e events.Event) error {
		var err error
		results, total, err = h.app.Search().Search(r.Context(), search.SearchQuery{
			Collection: collName,
			Query:      event.Query,
			Language:   event.Language,
			Page:       event.Page,
			PerPage:    event.PerPage,
			Highlight:  event.Highlight,
			Rank:       event.Rank,
		})
		return err
	}); err != nil {
		writeInternalError(w, "Search failed", err)
		return
	}

	// Apply the resolved list rule to each hit. This filters post-query, so
	// totals reflect the unfiltered count; secure-by-default wins over exact
	// pagination for rule-restricted search.
	if ruleExpr != nil {
		kept := results[:0]
		for _, res := range results {
			if matched, err := filter.FilterMatches(ruleExpr, res.Record); err == nil && matched {
				kept = append(kept, res)
			}
		}
		results = kept
	}
	if results == nil {
		results = []search.SearchResult{}
	}

	writeOK(w, map[string]any{
		"items":      results,
		"page":       event.Page,
		"perPage":    event.PerPage,
		"totalItems": total,
		"totalPages": maxInt(1, int((total+int64(event.PerPage)-1)/int64(event.PerPage))),
	})
}

func (h *Handlers) FTSCreateIndex(w http.ResponseWriter, r *http.Request) {
	collection := chi.URLParam(r, "collection")
	var form forms.FTSIndexForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}
	event := &events.FTSIndexRequestEvent{App: h.app, Request: r, Info: toEventRequestInfoWithBody(r, form), Action: "create", CollectionName: collection, Fields: form.Fields, Language: form.Language, Weight: form.Weight}
	if err := h.app.OnFTSIndexRequest().Trigger(event, func(e events.Event) error {
		return h.app.Search().CreateIndex(r.Context(), search.IndexConfig{
			Collection: collection,
			Fields:     event.Fields,
			Language:   event.Language,
			Weight:     event.Weight,
		})
	}); err != nil {
		writeInternalError(w, "Failed to create index", err)
		return
	}
	writeJSON(w, 201, map[string]any{"message": "FTS index created", "collection": collection})
}

func (h *Handlers) FTSRemoveIndex(w http.ResponseWriter, r *http.Request) {
	collection := chi.URLParam(r, "collection")
	event := &events.FTSIndexRequestEvent{App: h.app, Request: r, Info: toEventRequestInfo(r), Action: "delete", CollectionName: collection}
	if err := h.app.OnFTSIndexRequest().Trigger(event, func(e events.Event) error {
		return h.app.Search().RemoveIndex(r.Context(), collection)
	}); err != nil {
		writeInternalError(w, "Failed to remove index", err)
		return
	}
	writeOK(w, map[string]any{"message": "FTS index removed", "collection": collection})
}

// ---------------------------------------------------------------------------
// Batch Record Operations (Transactional)
// ---------------------------------------------------------------------------

func (h *Handlers) BatchRecords(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	coll, err := h.app.Collections().GetCollectionByName(r.Context(), collName)
	if err != nil {
		writeError(w, 404, "Collection not found: "+collName)
		return
	}
	if !requireAPIKeyPermission(w, r, "records.write") {
		return
	}

	var form forms.BatchRecordsForm
	if err := decodeJSONBody(r, &form); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if err := form.Validate(); err != nil {
		writeValidationError(w, err)
		return
	}

	// Enforce collection access rules per operation before mutating anything.
	for _, data := range form.Creates {
		if !h.checkRule(r, coll.CreateRule, data, data) {
			writeError(w, 403, "Access denied")
			return
		}
	}
	for id, patch := range form.Updates {
		existing, err := h.app.Collections().GetRecord(r.Context(), coll, id)
		if err != nil {
			writeError(w, 404, "Record not found: "+id)
			return
		}
		if !h.checkRule(r, coll.UpdateRule, patch, existing) {
			writeError(w, 403, "Access denied")
			return
		}
	}
	for _, id := range form.Deletes {
		existing, err := h.app.Collections().GetRecord(r.Context(), coll, id)
		if err != nil {
			writeError(w, 404, "Record not found: "+id)
			return
		}
		if !h.checkRule(r, coll.DeleteRule, nil, existing) {
			writeError(w, 403, "Access denied")
			return
		}
	}

	var created []collection.Record
	var updated, deleted int

	if len(form.Creates) > 0 {
		created, err = h.app.Collections().CreateBatch(r.Context(), coll, form.Creates)
		if err != nil {
			writeInternalError(w, "Batch create failed", err)
			return
		}
	}

	if len(form.Updates) > 0 {
		if err := h.app.Collections().UpdateBatch(r.Context(), coll, form.Updates); err != nil {
			writeInternalError(w, "Batch update failed", err)
			return
		}
		updated = len(form.Updates)
	}

	if len(form.Deletes) > 0 {
		if err := h.app.Collections().DeleteBatch(r.Context(), coll, form.Deletes); err != nil {
			writeInternalError(w, "Batch delete failed", err)
			return
		}
		deleted = len(form.Deletes)
	}

	postCommitCtx := cloneRequestContext(r.Context(), false)
	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	database.AfterCommit(r.Context(), func() {
		for _, rec := range created {
			if rid, ok := rec["id"].(string); ok {
				_ = h.app.OnRecordCreate().Trigger(&events.RecordEvent{App: h.app, Record: rec, CollectionID: coll.ID, CollectionName: coll.Name, RecordID: rid, Type: events.ModelEventCreate}, func(e events.Event) error { return e.Next() })
				h.app.Realtime().BroadcastRecord("create", collName, rid, rec)
				h.app.Auth().RecordAudit(postCommitCtx, adminID, "record.batch_create", collName, rid, nil, r)
			}
		}
		for id := range form.Updates {
			rec, _ := h.app.Collections().GetRecord(postCommitCtx, coll, id)
			_ = h.app.OnRecordUpdate().Trigger(&events.RecordEvent{App: h.app, Record: rec, CollectionID: coll.ID, CollectionName: coll.Name, RecordID: id, Type: events.ModelEventUpdate}, func(e events.Event) error { return e.Next() })
			h.app.Realtime().BroadcastRecord("update", collName, id, rec)
			h.app.Auth().RecordAudit(postCommitCtx, adminID, "record.batch_update", collName, id, nil, r)
		}
		for _, id := range form.Deletes {
			payload := map[string]any{"id": id}
			_ = h.app.OnRecordDelete().Trigger(&events.RecordEvent{App: h.app, Record: payload, CollectionID: coll.ID, CollectionName: coll.Name, RecordID: id, Type: events.ModelEventDelete}, func(e events.Event) error { return e.Next() })
			h.app.Realtime().BroadcastRecord("delete", collName, id, payload)
			h.app.Auth().RecordAudit(postCommitCtx, adminID, "record.batch_delete", collName, id, nil, r)
		}
	})

	writeOK(w, map[string]any{
		"created": created,
		"updated": updated,
		"deleted": deleted,
	})
}

// checkRule evaluates a collection access rule against the request and record
// context. For create/update rules, body should contain the submitted payload.
// For view/update/delete rules, record should contain the existing record data.
//
// Rule semantics (locked by default):
//   - nil   → locked: only superusers (admins) pass
//   - ""    → public: everyone passes
//   - "..." → filter expression evaluated against request + record
func (h *Handlers) checkRule(r *http.Request, rule *string, body map[string]any, record map[string]any) bool {
	info := NewRequestInfo(r)
	if body != nil {
		info = info.WithBody(body)
	}
	rc := NewRuleContextFromInfo(info)
	if rc.IsAdmin {
		return true
	}
	if rule == nil {
		return false
	}
	if strings.TrimSpace(*rule) == "" {
		return true
	}
	return h.rules.EvaluateRuleBool(r.Context(), *rule, rc, record)
}

// evaluateRuleWhere resolves a list/view rule into a filter expression ready to
// be passed to the query builder. It returns allowed=false when the requester
// may not access the collection at all (locked rule for non-superusers, or a
// rule that failed to resolve).
func (h *Handlers) evaluateRuleWhere(r *http.Request, rule *string, body map[string]any) (ruleFilter string, allowed bool) {
	info := NewRequestInfo(r)
	if body != nil {
		info = info.WithBody(body)
	}
	rc := NewRuleContextFromInfo(info)
	if rc.IsAdmin {
		return "", true
	}
	if rule == nil {
		return "", false
	}
	if strings.TrimSpace(*rule) == "" {
		return "", true
	}
	resolved := h.rules.EvaluateRuleWhere(r.Context(), *rule, rc)
	if resolved == "" {
		// Non-empty rule that failed to resolve/parse: fail closed.
		return "", false
	}
	return resolved, true
}

func removedRecordFilePaths(coll *collection.Collection, recordID string, before, after map[string]any) []string {
	beforeSet := make(map[string]struct{})
	for _, path := range recordFilePaths(coll, recordID, before) {
		beforeSet[path] = struct{}{}
	}
	for _, path := range recordFilePaths(coll, recordID, after) {
		delete(beforeSet, path)
	}
	result := make([]string, 0, len(beforeSet))
	for path := range beforeSet {
		result = append(result, path)
	}
	return result
}

func recordFilePaths(coll *collection.Collection, recordID string, record map[string]any) []string {
	if coll == nil || record == nil || recordID == "" {
		return nil
	}

	var paths []string
	for _, field := range coll.Schema {
		if field.Type != collection.FieldFile {
			continue
		}
		for _, name := range fileNamesFromValue(record[field.Name]) {
			if strings.HasPrefix(name, "_tmp/") {
				continue
			}
			paths = append(paths, fmt.Sprintf("%s/%s/%s", coll.Name, recordID, filepath.Base(name)))
		}
	}
	return paths
}

func fileNamesFromValue(value any) []string {
	switch v := value.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return nil
		}
		if strings.HasPrefix(trimmed, "[") {
			var arr []string
			if err := json.Unmarshal([]byte(trimmed), &arr); err == nil {
				return arr
			}
		}
		return []string{trimmed}
	case []string:
		return v
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if name := strings.TrimSpace(fmt.Sprint(item)); name != "" {
				result = append(result, name)
			}
		}
		return result
	default:
		return nil
	}
}

// Field picking: reduce records to only specified fields (?fields=).
func pickFieldsFromRecords(records []map[string]any, fields string) []map[string]any {
	if fields == "*" || fields == "" {
		return records
	}
	fieldSet := make(map[string]bool)
	for _, f := range strings.Split(fields, ",") {
		f = strings.TrimSpace(f)
		if f != "" {
			fieldSet[f] = true
		}
	}
	// Always include id and resolved expansions
	fieldSet["id"] = true
	fieldSet["expand"] = true

	result := make([]map[string]any, len(records))
	for i, record := range records {
		filtered := make(map[string]any)
		for k, v := range record {
			if fieldSet[k] {
				filtered[k] = v
			}
		}
		result[i] = filtered
	}
	return result
}

func pickSingleRecordFields(record map[string]any, fields string) map[string]any {
	result := pickFieldsFromRecords([]map[string]any{record}, fields)
	return result[0]
}
