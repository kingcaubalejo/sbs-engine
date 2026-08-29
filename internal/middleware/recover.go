package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"sbs-engine/internal/response"
)

// Recover catches panics from any inner handler or middleware and converts
// them into a 500 response so a single panic — for example from the MongoDB
// driver — cannot crash the process. Must be the outermost middleware.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered",
					"request_id", RequestIDFrom(r.Context()),
					"method", r.Method,
					"path", r.URL.Path,
					"panic", rec,
					"stack", string(debug.Stack()),
				)
				response.Error(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
