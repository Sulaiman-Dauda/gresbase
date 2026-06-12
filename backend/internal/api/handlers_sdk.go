package api

import (
	"net/http"

	"github.com/gresbase/gresbase/internal/sdkgen"
)

// TypesTS generates a typed TypeScript SDK from the live collection schema and
// serves it as a downloadable module (gresbase.ts).
func (h *Handlers) TypesTS(w http.ResponseWriter, r *http.Request) {
	colls, err := h.app.Collections().ListCollections(r.Context(), "default")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list collections")
		return
	}

	module := sdkgen.Generate(colls, getBaseURL(r))

	w.Header().Set("Content-Type", "application/typescript; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="gresbase.ts"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(module))
}
