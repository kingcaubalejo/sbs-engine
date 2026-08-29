package middleware

import (
	"context"
	"net/http"

	"sbs-engine/internal/response"
)

// apiVersionKey is the context key under which APIVersionMiddleware stores
// the resolved API version. Kept unexported so callers must go through
// GetAPIVersion.
type apiVersionKey struct{}

func APIVersionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		version := r.Header.Get("X-API-VERSION")

		// No header = legacy client
		if version == "" {
			version = "1"
		}

		switch version {
		case "1", "2":
			// supported
		default:
			response.Error(
				w,
				http.StatusBadRequest,
				"Unsupported API version",
			)
			return
		}

		ctx := context.WithValue(
			r.Context(),
			apiVersionKey{},
			version,
		)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetAPIVersion returns the API version resolved by APIVersionMiddleware,
// defaulting to "1" when the middleware did not run.
func GetAPIVersion(r *http.Request) string {
	version, ok := r.Context().Value(apiVersionKey{}).(string)
	if !ok {
		return "1"
	}
	return version
}
