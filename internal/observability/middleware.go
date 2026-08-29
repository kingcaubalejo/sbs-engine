package observability

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"sbs-engine/internal/middleware"
)

// instrumentsOnce lazily creates the request counter and duration
// histogram so the meter provider (which may be a no-op until Init runs)
// is only queried on first use.
var (
	instrumentsOnce sync.Once
	requestCounter  metric.Int64Counter
	requestDuration metric.Float64Histogram
)

func instruments() (metric.Int64Counter, metric.Float64Histogram) {
	instrumentsOnce.Do(func() {
		meter := otel.Meter("sbs-engine")
		requestCounter, _ = meter.Int64Counter(
			"sbs_requests_total",
			metric.WithDescription("Total HTTP requests handled, labelled by API version, route and status."),
		)
		requestDuration, _ = meter.Float64Histogram(
			"sbs_request_duration_seconds",
			metric.WithDescription("HTTP request duration in seconds."),
			metric.WithUnit("s"),
		)
	})
	return requestCounter, requestDuration
}

// statusRecorder mirrors the pattern in middleware/logging.go so we can
// observe the response status without depending on the access log
// running in the same chain.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// Instrument records per-request metrics tagged with the API version
// resolved by APIVersionMiddleware. It must therefore be installed
// inside APIVersionMiddleware in the chain so the context value is set.
func Instrument(next http.Handler) http.Handler {
	counter, hist := instruments()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}

		next.ServeHTTP(rec, r)

		attrs := metric.WithAttributes(
			attribute.String("version", middleware.GetAPIVersion(r)),
			attribute.String("route", r.URL.Path),
			attribute.String("method", r.Method),
			attribute.String("status", strconv.Itoa(rec.status)),
		)
		counter.Add(r.Context(), 1, attrs)
		hist.Record(r.Context(), time.Since(start).Seconds(), attrs)
	})
}
