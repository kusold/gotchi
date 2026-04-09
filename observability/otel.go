package observability

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	"go.opentelemetry.io/otel/trace"
)

// routePattern extracts the matched route template from Chi's RouteContext.
// If no route was matched (e.g. 404), it returns an empty string to
// prevent high-cardinality attribute values from unmatched paths.
func routePattern(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		return ""
	}
	if pattern := rctx.RoutePattern(); pattern != "" && pattern != "/*" {
		return pattern
	}
	return ""
}

// OTELConfig configures the OpenTelemetry setup. A zero-value config is not
// valid; at minimum set Enabled to true. Use [OTELConfig.WithDefaults] to
// populate default values for ServiceName, ExporterURL, SampleRate, and
// ShutdownTimeout.
type OTELConfig struct {
	// Enabled controls whether OpenTelemetry is active. When false,
	// [SetupOTEL] is a no-op and tracing/metrics middleware passes requests
	// through without instrumentation.
	Enabled bool
	// EnableTracing controls whether traces are collected. Defaults to true
	// when nil. Use a pointer to distinguish "not set" from "explicitly false".
	EnableTracing *bool
	// EnableMetrics controls whether metrics are collected. Defaults to true
	// when nil. Use a pointer to distinguish "not set" from "explicitly false".
	EnableMetrics *bool
	// ServiceName identifies this service in traces and metrics. Defaults to
	// "gotchi" when empty.
	ServiceName string
	// ExporterURL is the OTLP gRPC endpoint (e.g. "localhost:4317"). Defaults
	// to "localhost:4317" when empty.
	ExporterURL string
	// SampleRate controls the trace sampling ratio (0.0–1.0). Defaults to 1.0
	// (all traces) when nil.
	SampleRate *float64
	// Insecure disables TLS for the OTLP gRPC connection. Useful for local
	// development with collectors like Jaeger or the OpenTelemetry Collector.
	Insecure bool
	// ShutdownTimeout is the maximum duration to wait for pending spans and
	// metrics to be exported during shutdown. Defaults to 5 seconds.
	ShutdownTimeout time.Duration
}

// WithDefaults returns a copy of the config with zero values replaced by
// sensible defaults: ServiceName="gotchi", ExporterURL="localhost:4317",
// SampleRate=1.0, ShutdownTimeout=5s, EnableTracing=true, EnableMetrics=true.
func (c OTELConfig) WithDefaults() OTELConfig {
	cfg := c
	if cfg.ServiceName == "" {
		cfg.ServiceName = "gotchi"
	}
	if cfg.ExporterURL == "" {
		cfg.ExporterURL = "localhost:4317"
	}
	if cfg.SampleRate == nil {
		cfg.SampleRate = float64Ptr(1.0)
	}
	if cfg.ShutdownTimeout == 0 {
		cfg.ShutdownTimeout = 5 * time.Second
	}
	if cfg.EnableTracing == nil {
		cfg.EnableTracing = boolPtr(true)
	}
	if cfg.EnableMetrics == nil {
		cfg.EnableMetrics = boolPtr(true)
	}
	return cfg
}

// TracingEnabled reports whether tracing is both globally enabled and
// explicitly enabled via EnableTracing.
func (c OTELConfig) TracingEnabled() bool {
	return c.Enabled && c.EnableTracing != nil && *c.EnableTracing
}

// MetricsEnabled reports whether metrics are both globally enabled and
// explicitly enabled via EnableMetrics.
func (c OTELConfig) MetricsEnabled() bool {
	return c.Enabled && c.EnableMetrics != nil && *c.EnableMetrics
}

func boolPtr(b bool) *bool          { return &b }
func float64Ptr(f float64) *float64 { return &f }

// SetupOTEL initializes the OpenTelemetry tracer provider and meter provider,
// configured to export to an OTLP gRPC endpoint. It returns a shutdown function
// that must be called on application exit to flush pending telemetry.
//
// The function configures:
//   - A TracerProvider with the given sampling rate and OTLP gRPC exporter.
//   - A MeterProvider with a periodic reader exporting to the same endpoint.
//   - A composite TextMapPropagator for trace context and baggage propagation.
//
// Call the returned shutdown function in a defer to ensure telemetry is flushed:
//
//	shutdown, err := observability.SetupOTEL(ctx, cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer shutdown(context.Background())
func SetupOTEL(ctx context.Context, cfg OTELConfig) (func(context.Context) error, error) {
	cfg = cfg.WithDefaults()

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("creating OTEL resource: %w", err)
	}

	traceOpts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.ExporterURL)}
	if cfg.Insecure {
		traceOpts = append(traceOpts, otlptracegrpc.WithInsecure())
	}
	traceExporter, err := otlptracegrpc.New(ctx, traceOpts...)
	if err != nil {
		return nil, fmt.Errorf("creating OTLP trace exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(*cfg.SampleRate)),
		sdktrace.WithBatcher(traceExporter),
	)
	otel.SetTracerProvider(tp)

	metricOpts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(cfg.ExporterURL)}
	if cfg.Insecure {
		metricOpts = append(metricOpts, otlpmetricgrpc.WithInsecure())
	}
	metricExporter, err := otlpmetricgrpc.New(ctx, metricOpts...)
	if err != nil {
		return nil, fmt.Errorf("creating OTLP metric exporter: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
	)
	otel.SetMeterProvider(mp)

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return func(ctx context.Context) error {
		return errors.Join(
			tp.Shutdown(ctx),
			mp.Shutdown(ctx),
		)
	}, nil
}

// OTELTracingMiddleware returns HTTP middleware that creates an OpenTelemetry span
// for each request. After the handler completes, it updates the span name with
// the matched Chi route pattern (e.g. "GET /users/{id}") and records the HTTP
// status code. Server errors (5xx) are marked as error spans.
//
// Place this middleware after [CorrelationAndAudit] but before route-specific
// middleware to capture the full request lifecycle.
func OTELTracingMiddleware(serviceName string) func(http.Handler) http.Handler {
	tracer := otel.Tracer(serviceName)
	propagator := otel.GetTextMapPropagator()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			// Start span with a generic name; Chi hasn't matched the route yet.
			ctx, span := tracer.Start(ctx, r.Method,
				trace.WithAttributes(
					semconv.HTTPRequestMethodKey.String(r.Method),
					semconv.URLPathKey.String(r.URL.Path),
					semconv.ServerAddressKey.String(r.Host),
				),
				trace.WithSpanKind(trace.SpanKindServer),
			)
			defer span.End()

			rw := getStatusRecorder(w)
			r = r.WithContext(ctx)

			next.ServeHTTP(rw, r)

			// After routing, Chi's RouteContext is populated with the
			// matched route template (e.g. "/users/{id}").
			if route := routePattern(r); route != "" {
				span.SetName(r.Method + " " + route)
				span.SetAttributes(semconv.HTTPRouteKey.String(route))
			}

			statusCode := rw.status
			span.SetAttributes(semconv.HTTPResponseStatusCodeKey.Int(statusCode))
			if statusCode >= 500 {
				span.SetStatus(codes.Error, "server error")
			}
		})
	}
}

// OTELMetricsMiddleware returns HTTP middleware that records request duration
// (histogram in milliseconds) and request count (counter) as OpenTelemetry
// metrics. Metrics are labeled with HTTP method, status code, and (when
// available) the matched Chi route pattern for low-cardinality grouping.
func OTELMetricsMiddleware(serviceName string) func(http.Handler) http.Handler {
	meter := otel.Meter(serviceName)
	duration, err := meter.Float64Histogram(
		"http.server.request.duration",
		metric.WithDescription("Duration of HTTP server requests in milliseconds"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		slog.Warn("failed to create HTTP duration histogram, metrics will not be recorded", "err", err)
	}
	requestCount, err := meter.Int64Counter(
		"http.server.request.count",
		metric.WithDescription("Count of HTTP server requests"),
	)
	if err != nil {
		slog.Warn("failed to create HTTP request counter, metrics will not be recorded", "err", err)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := getStatusRecorder(w)
			start := time.Now()

			next.ServeHTTP(rw, r)

			statusCode := rw.status
			attrs := []attribute.KeyValue{
				semconv.HTTPRequestMethodKey.String(r.Method),
				semconv.HTTPResponseStatusCodeKey.Int(statusCode),
			}
			if route := routePattern(r); route != "" {
				attrs = append(attrs, semconv.HTTPRouteKey.String(route))
			}
			duration.Record(r.Context(), float64(time.Since(start).Milliseconds()), metric.WithAttributes(attrs...))
			requestCount.Add(r.Context(), 1, metric.WithAttributes(attrs...))
		})
	}
}

// getStatusRecorder returns an existing statusRecorder wrapping w, or creates
// a new one. This avoids double-wrapping when both tracing and metrics
// middleware are applied.
func getStatusRecorder(w http.ResponseWriter) *statusRecorder {
	if existing, ok := w.(*statusRecorder); ok {
		return existing
	}
	return &statusRecorder{ResponseWriter: w, status: http.StatusOK}
}
