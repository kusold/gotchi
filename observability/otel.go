package observability

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

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

type OTELConfig struct {
	Enabled     bool
	ServiceName string
	ExporterURL string
	SampleRate  float64
	Insecure    bool
}

func (c OTELConfig) WithDefaults() OTELConfig {
	cfg := c
	if cfg.ServiceName == "" {
		cfg.ServiceName = "gotchi"
	}
	if cfg.ExporterURL == "" {
		cfg.ExporterURL = "localhost:4317"
	}
	if cfg.SampleRate == 0 {
		cfg.SampleRate = 1.0
	}
	return cfg
}

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
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.SampleRate)),
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

func TracingMiddleware(serviceName string) func(http.Handler) http.Handler {
	tracer := otel.Tracer(serviceName)
	propagator := otel.GetTextMapPropagator()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			spanName := r.Method + " " + r.URL.Path
			ctx, span := tracer.Start(ctx, spanName,
				trace.WithAttributes(
					semconv.HTTPRequestMethodKey.String(r.Method),
					semconv.URLPathKey.String(r.URL.Path),
					semconv.ServerAddressKey.String(r.Host),
				),
				trace.WithSpanKind(trace.SpanKindServer),
			)
			defer span.End()

			rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			r = r.WithContext(ctx)

			next.ServeHTTP(rw, r)

			span.SetAttributes(semconv.HTTPResponseStatusCodeKey.Int(rw.status))
			if rw.status >= 500 {
				span.SetStatus(codes.Error, "server error")
			}
		})
	}
}

func HTTPMetricsMiddleware(serviceName string) func(http.Handler) http.Handler {
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
			start := time.Now()
			rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rw, r)

			attrs := []attribute.KeyValue{
				semconv.HTTPRequestMethodKey.String(r.Method),
				semconv.URLPathKey.String(r.URL.Path),
				semconv.HTTPResponseStatusCodeKey.Int(rw.status),
			}
			duration.Record(r.Context(), float64(time.Since(start).Milliseconds()), metric.WithAttributes(attrs...))
			requestCount.Add(r.Context(), 1, metric.WithAttributes(attrs...))
		})
	}
}
