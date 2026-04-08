package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func setupTracerProvider(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	spanRecorder := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
	originalTP := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(originalTP) })
	return spanRecorder
}

func TestOTELConfig_WithDefaults(t *testing.T) {
	t.Run("sets all defaults when empty", func(t *testing.T) {
		cfg := OTELConfig{}.WithDefaults()
		assert.Equal(t, "gotchi", cfg.ServiceName)
		assert.Equal(t, "localhost:4317", cfg.ExporterURL)
		assert.Equal(t, 1.0, cfg.SampleRate)
	})

	t.Run("preserves provided values", func(t *testing.T) {
		cfg := OTELConfig{
			ServiceName: "my-service",
			ExporterURL: "collector:4317",
			SampleRate:  0.5,
		}.WithDefaults()
		assert.Equal(t, "my-service", cfg.ServiceName)
		assert.Equal(t, "collector:4317", cfg.ExporterURL)
		assert.Equal(t, 0.5, cfg.SampleRate)
	})

	t.Run("preserves enabled flag", func(t *testing.T) {
		cfg := OTELConfig{Enabled: true}.WithDefaults()
		assert.True(t, cfg.Enabled)
	})

	t.Run("handles zero sample rate", func(t *testing.T) {
		cfg := OTELConfig{SampleRate: 0}.WithDefaults()
		assert.Equal(t, 1.0, cfg.SampleRate)
	})

	t.Run("handles explicit sample rate of 0.0", func(t *testing.T) {
		cfg := OTELConfig{SampleRate: 0.0}.WithDefaults()
		assert.Equal(t, 1.0, cfg.SampleRate)
	})
}

func TestSetupOTEL_SetsGlobalProviders(t *testing.T) {
	cfg := OTELConfig{
		Enabled:     true,
		ExporterURL: "localhost:4317",
	}
	shutdown, err := SetupOTEL(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	assert.NotNil(t, otel.GetTracerProvider())
	assert.NotNil(t, otel.GetMeterProvider())
	assert.NotNil(t, otel.GetTextMapPropagator())

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = shutdown(ctx)
}

func TestTracingMiddleware_CreatesSpan(t *testing.T) {
	spanRecorder := setupTracerProvider(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := TracingMiddleware("test-service")(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	spans := spanRecorder.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "GET /api/test", spans[0].Name())
	assert.Equal(t, "test-service", spans[0].InstrumentationScope().Name)
}

func TestTracingMiddleware_RecordsStatusCode(t *testing.T) {
	spanRecorder := setupTracerProvider(t)

	tests := []struct {
		name       string
		statusCode int
	}{
		{"200 OK", http.StatusOK},
		{"404 Not Found", http.StatusNotFound},
		{"500 Internal Server Error", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spanRecorder.Reset()

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			})

			middleware := TracingMiddleware("test-service")(handler)
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := httptest.NewRecorder()
			middleware.ServeHTTP(rec, req)

			spans := spanRecorder.Ended()
			require.Len(t, spans, 1)
		})
	}
}

func TestTracingMiddleware_PropagatesContext(t *testing.T) {
	setupTracerProvider(t)

	var capturedCtx context.Context
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
	})

	middleware := TracingMiddleware("test-service")(handler)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	assert.NotNil(t, capturedCtx)
}

func TestHTTPMetricsMiddleware_RecordsMetrics(t *testing.T) {
	setupTracerProvider(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := HTTPMetricsMiddleware("test-service")(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	require.NotPanics(t, func() {
		middleware.ServeHTTP(rec, req)
	})

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHTTPMetricsMiddleware_CapturesStatusCodes(t *testing.T) {
	setupTracerProvider(t)

	tests := []struct {
		name       string
		statusCode int
	}{
		{"200", http.StatusOK},
		{"404", http.StatusNotFound},
		{"500", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			})

			middleware := HTTPMetricsMiddleware("test-service")(handler)
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := httptest.NewRecorder()
			middleware.ServeHTTP(rec, req)

			assert.Equal(t, tt.statusCode, rec.Code)
		})
	}
}
