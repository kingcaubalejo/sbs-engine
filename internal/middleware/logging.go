package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// statusRecorder wraps http.ResponseWriter to capture the status code and
// number of bytes written for the access log. It only intercepts WriteHeader
// and Write; all other behaviour is delegated to the underlying writer.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// AccessLog emits one structured log line per request with method, path,
// status, duration, bytes, request_id, and client IP. Request ID is read
// from the context populated by RequestID, so this middleware must be
// installed inside RequestID.
func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		slog.Info("request",
			"request_id", RequestIDFrom(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"bytes", rec.bytes,
			"ip", clientIP(r),
			"user_agent", r.UserAgent(),
		)
	})
}
