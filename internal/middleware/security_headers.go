package middleware

import "net/http"

// SecurityHeaders sets a small set of conservative response headers and
// strips the default Server fingerprint so the Go version is not
// advertised to scanners. Cache-Control and ETag are handled separately
// by CacheHeaders so this middleware stays method-agnostic.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Server", "")
		next.ServeHTTP(w, r)
	})
}
