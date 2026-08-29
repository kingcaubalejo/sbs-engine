package middleware

import "net/http"

// BodyLimit caps the request body for write methods (POST/PUT/PATCH).
// MaxBytesReader closes the connection if the limit is exceeded, so a
// single large-payload request cannot tie up server resources.
func BodyLimit(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch:
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}
