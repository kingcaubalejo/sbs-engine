package middleware

import (
	"net/http"
	"os"
	"strings"
)

// CORS implements the policy required by a public, unauthenticated API:
// reads (GET/HEAD/OPTIONS) are open to any origin so curl, mobile apps,
// and arbitrary frontends can consume the data; writes (POST/PUT/PATCH/
// DELETE) are restricted to an env-configured allowlist of trusted
// origins. Empty Origin (non-browser clients) bypasses the allowlist
// since the same-origin policy threat model does not apply.
//
// Allowlist source: comma-separated CORS_ALLOWED_ORIGINS env var.
func CORS(next http.Handler) http.Handler {
	allowed := parseOrigins(os.Getenv("CORS_ALLOWED_ORIGINS"))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		isWrite := r.Method == http.MethodPost ||
			r.Method == http.MethodPut ||
			r.Method == http.MethodPatch ||
			r.Method == http.MethodDelete

		if origin != "" && isWrite && !allowed[origin] {
			http.Error(w, "CORS origin not allowed", http.StatusForbidden)
			return
		}

		// Reflect allowed origin or fall back to "*" for opens reads.
		if origin != "" && (allowed[origin] || !isWrite) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		} else if !isWrite {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token, X-Request-ID")
		w.Header().Set("Access-Control-Allow-Credentials", "false")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func parseOrigins(raw string) map[string]bool {
	out := map[string]bool{}
	for _, o := range strings.Split(raw, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			out[o] = true
		}
	}
	return out
}
