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
// If no route was matched (e.g. 404), it returns a static placeholder to
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

type OTELConfig struct {
	Enabled         bool
	EnableTracing   *bool
	EnableMetrics   *bool
	ServiceName     string
	ExporterURL     string
	SampleRate      *float64
	Insecure        bool
	ShutdownTimeout time.Duration
}

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

func (c OTELConfig) TracingEnabled() bool {
	return c.Enabled && c.EnableTracing != nil && *c.EnableTracing
}

func (c OTELConfig) MetricsEnabled() bool {
	return c.Enabled && c.EnableMetrics != nil && *c.EnableMetrics
}

func boolPtr(b bool) *bool          { return &b }
func float64Ptr(f float64) *float64 { return &f }

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

func getStatusRecorder(w http.ResponseWriter) *statusRecorder {
	if existing, ok := w.(*statusRecorder); ok {
		return existing
	}
	return &statusRecorder{ResponseWriter: w, status: http.StatusOK}
}
