package middleware

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"sbs-engine/internal/response"
)

// RequireAPIKey enforces a static admin API key on non-GET/HEAD/OPTIONS
// requests. Reads, preflight, and HEAD probes flow through untouched —
// this middleware exists only to gate mutations.
//
// The key is read from ADMIN_API_KEY when the middleware is constructed
// and compared in constant time so an attacker cannot use timing
// signals to recover it byte-by-byte. Clients send it as either:
//
//	Authorization: Bearer <key>
//	X-API-Key: <key>
//
// Bearer is preferred because Swagger UI's "Authorize" button writes
// it natively. If ADMIN_API_KEY is unset the middleware fails closed:
// every write returns 503, so a misconfigured production deploy cannot
// silently accept anonymous mutations.
func RequireAPIKey(next http.Handler) http.Handler {
	expected := []byte(os.Getenv("ADMIN_API_KEY"))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}

		if len(expected) == 0 {
			response.Error(w, http.StatusServiceUnavailable, "admin API key is not configured")
			return
		}

		provided := extractAPIKey(r)
		if provided == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="sbs-engine"`)
			response.Error(w, http.StatusUnauthorized, "missing API key")
			return
		}

		if subtle.ConstantTimeCompare([]byte(provided), expected) != 1 {
			response.Error(w, http.StatusUnauthorized, "invalid API key")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func extractAPIKey(r *http.Request) string {
	const bearerPrefix = "Bearer "
	if h := r.Header.Get("Authorization"); h != "" {
		if len(h) > len(bearerPrefix) && strings.EqualFold(h[:len(bearerPrefix)], bearerPrefix) {
			return strings.TrimSpace(h[len(bearerPrefix):])
		}
	}
	return strings.TrimSpace(r.Header.Get("X-API-Key"))
}
