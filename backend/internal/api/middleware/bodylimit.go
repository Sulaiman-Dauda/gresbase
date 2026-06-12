package middleware

import (
	"net/http"
	"strings"
)

// MaxBodyBytes caps the size of request bodies via http.MaxBytesReader for all
// routes except those whose path matches one of the exemptPrefixes (TUS and
// file-upload routes stream large bodies and must not be capped here). A limit
// of <= 0 disables the cap entirely.
func MaxBodyBytes(limit int64, exemptPrefixes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limit <= 0 {
				next.ServeHTTP(w, r)
				return
			}
			path := r.URL.Path
			for _, p := range exemptPrefixes {
				if p != "" && strings.HasPrefix(path, p) {
					next.ServeHTTP(w, r)
					return
				}
			}
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}
