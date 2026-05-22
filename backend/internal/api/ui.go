package api

import (
	"net/http"
	"path/filepath"
	"strings"
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

// serveStaticFiles returns an http.Handler that serves embedded static files
// (CSS, JS, fonts, images) with proper MIME types and caching headers.
func (s *Server) serveStaticFiles(fsys http.FileSystem) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Clean the path and remove leading slash for embed.FS compatibility
		path := strings.TrimPrefix(filepath.Clean(r.URL.Path), "/")

		f, err := fsys.Open(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()

		stat, err := f.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}

		// Set caching headers based on file type
		ext := filepath.Ext(path)
		switch ext {
		case ".css", ".js", ".woff", ".woff2", ".ttf", ".eot":
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		case ".png", ".jpg", ".jpeg", ".svg", ".ico", ".webp":
			w.Header().Set("Cache-Control", "public, max-age=86400")
		default:
			w.Header().Set("Cache-Control", "no-cache")
		}

		http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
	})
}
