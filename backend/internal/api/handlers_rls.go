package api

import (
	"net/http"

	"github.com/gresbase/gresbase/internal/collection"
)

// ---------------------------------------------------------------------------
// Row-Level Security — collection rules compiled to Postgres policies.
// The API layer stays the primary enforcement point; these policies add
// defense in depth for direct database connections via a restricted role.
// ---------------------------------------------------------------------------

func (h *Handlers) rlsCollections(r *http.Request) ([]*collection.Collection, error) {
	tenantID, _ := r.Context().Value(contextKeyTenantID).(string)
	if tenantID == "" {
		tenantID = "default"
	}
	return h.app.Collections().ListCollections(r.Context(), tenantID)
}

func (h *Handlers) rlsOptions(r *http.Request) collection.RLSOptions {
	role := r.URL.Query().Get("role")
	if role == "" && h.app.Config() != nil {
		role = h.app.Config().RLSRole
	}
	return collection.RLSOptions{
		Role:                 role,
		ForceLockUnsupported: r.URL.Query().Get("force_lock") == "true",
	}
}

// RLSScript returns the generated RLS SQL without applying it, so policies
// can be reviewed or applied out-of-band.
func (h *Handlers) RLSScript(w http.ResponseWriter, r *http.Request) {
	colls, err := h.rlsCollections(r)
	if err != nil {
		writeError(w, 500, "Failed to load collections: "+err.Error())
		return
	}
	script, warnings, err := collection.RLSScript(colls, h.rlsOptions(r))
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeOK(w, map[string]any{"script": script, "warnings": warnings})
}

// RLSApply enables RLS and creates policies for all collections.
func (h *Handlers) RLSApply(w http.ResponseWriter, r *http.Request) {
	colls, err := h.rlsCollections(r)
	if err != nil {
		writeError(w, 500, "Failed to load collections: "+err.Error())
		return
	}
	warnings, err := h.app.Collections().ApplyRLS(r.Context(), colls, h.rlsOptions(r))
	if err != nil {
		writeError(w, 400, "RLS apply failed: "+err.Error())
		return
	}
	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "rls.apply", "", "", map[string]any{"collections": len(colls)}, r)
	writeOK(w, map[string]any{"applied": true, "warnings": warnings})
}

// RLSRemove drops generated policies and disables RLS.
func (h *Handlers) RLSRemove(w http.ResponseWriter, r *http.Request) {
	colls, err := h.rlsCollections(r)
	if err != nil {
		writeError(w, 500, "Failed to load collections: "+err.Error())
		return
	}
	if err := h.app.Collections().RemoveRLS(r.Context(), colls); err != nil {
		writeError(w, 400, "RLS remove failed: "+err.Error())
		return
	}
	adminID, _ := r.Context().Value(contextKeyAdminID).(string)
	h.app.Auth().RecordAudit(r.Context(), adminID, "rls.remove", "", "", nil, r)
	writeOK(w, map[string]any{"removed": true})
}
