package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/gresbase/gresbase/internal/collection"
	"github.com/gresbase/gresbase/internal/filter"
)

// vectorSearchRequest is the body for a similarity search.
type vectorSearchRequest struct {
	Field    string `json:"field"`
	Vector   []any  `json:"vector"`
	Limit    int    `json:"limit"`
	Distance string `json:"distance"`
}

// VectorSearch runs a pgvector similarity search over a collection, honoring
// the collection list rule. POST /api/v1/records/{collection}/search-vector
func (h *Handlers) VectorSearch(w http.ResponseWriter, r *http.Request) {
	collName := chi.URLParam(r, "collection")
	coll, err := h.app.Collections().GetCollectionByName(r.Context(), collName)
	if err != nil {
		writeError(w, 404, "Collection not found: "+collName)
		return
	}
	if !requireAPIKeyPermission(w, r, "records.read") {
		return
	}

	// Same locked-by-default list-rule gate as RecordsList.
	ruleFilter, allowed := h.evaluateRuleWhere(r, coll.ListRule, nil)
	if !allowed {
		writeError(w, 403, "Access denied")
		return
	}

	var req vectorSearchRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if req.Field == "" {
		writeError(w, 400, "field is required")
		return
	}
	if len(req.Vector) == 0 {
		writeError(w, 400, "vector is required")
		return
	}

	opts := collection.VectorSearchOptions{
		Field:    req.Field,
		Vector:   req.Vector,
		Limit:    req.Limit,
		Distance: req.Distance,
	}

	// Compile the resolved list-rule filter into a WHERE fragment so the
	// similarity search never returns rows the caller may not see.
	if ruleFilter != "" {
		expr, perr := filter.ParseFilter(ruleFilter)
		if perr != nil {
			writeError(w, 403, "Access denied")
			return
		}
		whereSQL, args, ferr := filter.FilterToSQL(expr, nil)
		if ferr != nil {
			writeError(w, 403, "Access denied")
			return
		}
		opts.Where = whereSQL
		opts.WhereArgs = args
	}

	records, err := h.app.Collections().VectorSearch(r.Context(), coll, opts)
	if err != nil {
		writeError(w, 400, "Vector search failed: "+err.Error())
		return
	}
	if records == nil {
		records = []collection.Record{}
	}

	writeOK(w, map[string]any{
		"items":      records,
		"totalItems": len(records),
	})
}

// Features advertises optional server capabilities so the dashboard can show
// only what this deployment actually supports. GET /api/v1/features
func (h *Handlers) Features(w http.ResponseWriter, r *http.Request) {
	vector := false
	if h.app.DB() != nil {
		vector = h.app.DB().HasVectorSupport(r.Context())
	}
	writeOK(w, map[string]any{
		"vector":        vector,
		"realtime":      true,
		"oauth":         h.app.OAuth() != nil,
		"tls":           h.app.Config() != nil && h.app.Config().EnableTLS,
		"embeddedDB":    h.app.DB() != nil && h.app.DB().IsEmbedded(),
		"typedSDK":      true,
		"ruleSimulator": true,
	})
}
