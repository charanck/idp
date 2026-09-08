// Package observability wires up OpenTelemetry traces, metrics, and logs for
// the server, all exported via OTLP/gRPC to a collector.
//
// OTLP export is opt-in and controlled by the standard
// OTEL_EXPORTER_OTLP_ENDPOINT env var: if it's unset, Setup skips the OTLP
// SDK providers and leaves the otel SDK's own no-op global providers in
// place, so running the server (and every existing test) without a
// collector configured behaves exactly as it did before this package
// existed. "Off by default, on when a collector is configured" is the
// safest choice rather than making a collector a hard startup dependency
// the way Postgres and Redis are.
//
// Structured JSON logging to stdout, however, is always on (this is a dev
// tool - operators shouldn't need a collector configured just to get
// leveled, greppable logs). Its minimum level is controlled by LOG_LEVEL
// (debug/info/warn/error, default debug), independently of whether OTLP
// export is enabled.
//
// Once OTLP export is enabled, per-request/query/command spans, RED-style
// metrics, and the server's existing log/slog output (still honoring
// LOG_LEVEL) are additionally exported over OTLP/gRPC using the standard
// OTEL_EXPORTER_OTLP_* env vars (endpoint, headers, TLS, etc.) read directly
// by the exporter constructors - see
// https://opentelemetry.io/docs/languages/sdk-configuration/otlp-exporter/.
//
// IMPORTANT: both OTEL_EXPORTER_OTLP_ENDPOINT and LOG_LEVEL are read via
// os.Getenv at Setup() time, so whatever calls Setup must ensure .env (if
// used) has already been loaded into the process environment first - see
// appconfig.LoadDotEnv's doc comment.
package observability

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	logglobal "go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// ServiceName is the service.name resource attribute reported on every
// span/metric/log.
const ServiceName = "control-plane"

// Shutdown flushes and closes every OTEL provider that Setup started. It is
// always safe to call, even when OTEL was never enabled.
type Shutdown func(context.Context) error

// Enabled reports whether OTEL_EXPORTER_OTLP_ENDPOINT is set, i.e. whether
// Setup will actually start exporting rather than no-op.
func Enabled() bool {
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != ""
}

// logLevel parses LOG_LEVEL (debug/info/warn/error, case-insensitive).
// Defaults to Debug: this is a dev tool, and verbose-by-default logging is
// far more useful for debugging than the Info default Go's slog otherwise
// picks - operators who want quieter production logs can set
// LOG_LEVEL=info (or warn) explicitly.
func logLevel() slog.Level {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LOG_LEVEL"))) {
	case "debug", "":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelDebug
	}
}

// Setup always installs a JSON stdout slog handler honoring LOG_LEVEL as the
// global default logger. If OTEL is enabled (see Enabled), it additionally
// initializes the global TracerProvider, MeterProvider, and LoggerProvider,
// points them at an OTLP/gRPC collector, and fans every log record out to
// the OTEL log pipeline too (in addition to, not instead of, stdout - see
// newFanoutHandler) so existing slog.Debug/Info/Warn/Error call sites
// throughout the codebase are exported as OTEL logs without every call site
// needing to change.
//
// If OTEL isn't enabled, Setup returns a no-op shutdown once the stdout
// handler is installed.
func Setup(ctx context.Context, version string) (Shutdown, error) {
	stdout := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel()})

	if !Enabled() {
		slog.SetDefault(slog.New(stdout))
		return func(context.Context) error { return nil }, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(ServiceName),
			semconv.ServiceVersion(version),
		),
		resource.WithFromEnv(),
		resource.WithHost(),
		resource.WithProcess(),
	)
	if err != nil {
		return nil, fmt.Errorf("build otel resource: %w", err)
	}

	traceExporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create otlp trace exporter: %w", err)
	}
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tracerProvider)

	metricExporter, err := otlpmetricgrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create otlp metric exporter: %w", err)
	}
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(meterProvider)

	logExporter, err := otlploggrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create otlp log exporter: %w", err)
	}
	loggerProvider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
		sdklog.WithResource(res),
	)
	logglobal.SetLoggerProvider(loggerProvider)

	slog.SetDefault(slog.New(newFanoutHandler(
		stdout,
		otelslog.NewHandler(ServiceName, otelslog.WithLoggerProvider(loggerProvider)),
	)))

	return func(shutdownCtx context.Context) error {
		var errs []error
		if err := tracerProvider.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("shutdown tracer provider: %w", err))
		}
		if err := meterProvider.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("shutdown meter provider: %w", err))
		}
		if err := loggerProvider.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("shutdown logger provider: %w", err))
		}
		if len(errs) > 0 {
			return fmt.Errorf("otel shutdown errors: %v", errs)
		}
		return nil
	}, nil
}
