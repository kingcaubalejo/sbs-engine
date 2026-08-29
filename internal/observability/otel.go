// Package observability wires OpenTelemetry metrics and logs for the SBS
// engine. Provider setup is opt-in: if OTEL_EXPORTER_OTLP_ENDPOINT is not
// set, Init returns a no-op shutdown and the process runs without any
// exporter traffic. Callers can safely instrument code paths regardless.
package observability

import (
	"context"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	runtimeinstr "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// ShutdownFunc drains any pending telemetry and releases the exporter
// connection. It is safe to call multiple times; only the first call
// performs work.
type ShutdownFunc func(context.Context) error

// Init configures the global meter and log providers when
// OTEL_EXPORTER_OTLP_ENDPOINT is set. It returns a shutdown function that
// callers must invoke on process exit to flush pending batches. When no
// endpoint is configured Init logs a notice and returns a no-op shutdown
// so downstream code can call the metrics API unconditionally.
func Init(ctx context.Context) (ShutdownFunc, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		slog.Info("otel disabled: OTEL_EXPORTER_OTLP_ENDPOINT not set")
		return func(context.Context) error { return nil }, nil
	}

	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		semconv.ServiceName(serviceName()),
		semconv.ServiceVersion(serviceVersion()),
		semconv.DeploymentEnvironment(os.Getenv("APP_ENV")),
	))
	if err != nil {
		return nil, err
	}

	metricExp, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, err
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
	)
	otel.SetMeterProvider(mp)

	logExp, err := otlploghttp.New(ctx)
	if err != nil {
		_ = mp.Shutdown(ctx)
		return nil, err
	}
	lp := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
	)

	installSlogBridge(lp)

	if err := runtimeinstr.Start(runtimeinstr.WithMinimumReadMemStatsInterval(15 * time.Second)); err != nil {
		slog.Warn("otel runtime metrics failed to start", "error", err)
	}

	slog.Info("otel initialised",
		"endpoint", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		"service", serviceName())

	return func(shutdownCtx context.Context) error {
		if err := lp.Shutdown(shutdownCtx); err != nil {
			slog.Warn("otel log provider shutdown", "error", err)
		}
		return mp.Shutdown(shutdownCtx)
	}, nil
}

func serviceName() string {
	if v := os.Getenv("OTEL_SERVICE_NAME"); v != "" {
		return v
	}
	return "sbs-engine"
}

func serviceVersion() string {
	if v := os.Getenv("APP_VERSION"); v != "" {
		return v
	}
	return "dev"
}

// installSlogBridge fans slog output to both the existing JSON stdout
// handler and the OTLP log exporter, so operators keep their journalctl
// logs while the backend also receives structured events with trace
// context attached.
func installSlogBridge(lp *sdklog.LoggerProvider) {
	stdoutHandler := slog.Default().Handler()
	otelHandler := otelslog.NewHandler(serviceName(), otelslog.WithLoggerProvider(lp))
	slog.SetDefault(slog.New(&multiHandler{handlers: []slog.Handler{stdoutHandler, otelHandler}}))
}
