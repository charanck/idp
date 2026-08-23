// Package observability wires up OpenTelemetry traces, metrics, and logs for
// the server, all exported via OTLP/gRPC to a collector.
//
// It is opt-in and controlled by the standard OTEL_EXPORTER_OTLP_ENDPOINT
// env var: if it's unset, Setup does nothing and leaves the otel SDK's own
// no-op global providers in place, so running the server (and every existing
// test) without a collector configured behaves exactly as it did before this
// package existed. This mirrors the fact that Django's settings.py has no
// OTEL wiring at all today - there is nothing to keep in parity with, so
// "off by default, on when a collector is configured" is the safest choice
// rather than making a collector a hard startup dependency the way Postgres
// and Redis are.
//
// Once enabled, per-request/query/command spans, RED-style metrics, and the
// server's existing log/slog output are all exported over OTLP/gRPC using
// the standard OTEL_EXPORTER_OTLP_* env vars (endpoint, headers, TLS, etc.)
// read directly by the exporter constructors - see
// https://opentelemetry.io/docs/languages/sdk-configuration/otlp-exporter/.
package observability

import (
	"context"
	"fmt"
	"log/slog"
	"os"

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
// span/metric/log, matching the Django project name so traces from either
// implementation are identifiable as the same logical service during the
// migration.
const ServiceName = "control-plane"

// Shutdown flushes and closes every OTEL provider that Setup started. It is
// always safe to call, even when OTEL was never enabled.
type Shutdown func(context.Context) error

// Enabled reports whether OTEL_EXPORTER_OTLP_ENDPOINT is set, i.e. whether
// Setup will actually start exporting rather than no-op.
func Enabled() bool {
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != ""
}

// Setup initializes the global TracerProvider, MeterProvider, and
// LoggerProvider and points them at an OTLP/gRPC collector, then bridges
// log/slog's default logger through the OTEL log pipeline (in addition to,
// not instead of, its existing stdout output - see newSlogHandler) so
// existing slog.Info/Warn/Error call sites throughout the codebase are
// exported as OTEL logs without every call site needing to change.
//
// If OTEL isn't enabled (see Enabled), Setup is a no-op and returns a no-op
// shutdown.
func Setup(ctx context.Context, version string) (Shutdown, error) {
	if !Enabled() {
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
		slog.NewJSONHandler(os.Stdout, nil),
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
