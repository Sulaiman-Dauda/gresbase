package api

import (
	"net/http"
)

// serveUI serves the embedded admin dashboard.
// Uses the ui package which provides either the pre-built frontend
// (when compiled with embedded dist/) or the built-in fallback HTML UI.
func (s *Server) serveUI(w http.ResponseWriter, r *http.Request) {
	// Forward to the UI handler set during server construction
	if s.uiHandler != nil {
		s.uiHandler.ServeHTTP(w, r)
		return
	}
	// Absolute fallback (should never be reached)
	http.Error(w, "UI not available", http.StatusNotFound)
}
