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
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
)

func setupTracerProvider(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	spanRecorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	originalTP := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(originalTP) })
	return spanRecorder
}

func setupMeterProvider(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	originalMP := otel.GetMeterProvider()
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { otel.SetMeterProvider(originalMP) })
	return reader
}

func attrMap(set attribute.Set) map[attribute.Key]any {
	m := make(map[attribute.Key]any)
	iter := set.Iter()
	for iter.Next() {
		kv := iter.Attribute()
		m[kv.Key] = kv.Value.AsInterface()
	}
	return m
}

func TestOTELConfig_WithDefaults(t *testing.T) {
	t.Run("sets all defaults when empty", func(t *testing.T) {
		cfg := OTELConfig{}.WithDefaults()
		assert.Equal(t, "gotchi", cfg.ServiceName)
		assert.Equal(t, "localhost:4317", cfg.ExporterURL)
		assert.Equal(t, 1.0, cfg.SampleRate)
		assert.Equal(t, 5*time.Second, cfg.ShutdownTimeout)
		assert.True(t, *cfg.EnableTracing)
		assert.True(t, *cfg.EnableMetrics)
	})

	t.Run("preserves provided values", func(t *testing.T) {
		cfg := OTELConfig{
			ServiceName:     "my-service",
			ExporterURL:     "collector:4317",
			SampleRate:      0.5,
			ShutdownTimeout: 10 * time.Second,
		}.WithDefaults()
		assert.Equal(t, "my-service", cfg.ServiceName)
		assert.Equal(t, "collector:4317", cfg.ExporterURL)
		assert.Equal(t, 0.5, cfg.SampleRate)
		assert.Equal(t, 10*time.Second, cfg.ShutdownTimeout)
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

	t.Run("preserves insecure flag", func(t *testing.T) {
		cfg := OTELConfig{Insecure: true}.WithDefaults()
		assert.True(t, cfg.Insecure)
	})

	t.Run("defaults insecure to false", func(t *testing.T) {
		cfg := OTELConfig{}.WithDefaults()
		assert.False(t, cfg.Insecure)
	})

	t.Run("allows disabling tracing only", func(t *testing.T) {
		cfg := OTELConfig{Enabled: true, EnableTracing: boolPtr(false)}.WithDefaults()
		assert.False(t, cfg.TracingEnabled())
		assert.True(t, cfg.MetricsEnabled())
	})

	t.Run("allows disabling metrics only", func(t *testing.T) {
		cfg := OTELConfig{Enabled: true, EnableMetrics: boolPtr(false)}.WithDefaults()
		assert.True(t, cfg.TracingEnabled())
		assert.False(t, cfg.MetricsEnabled())
	})

	t.Run("TracingEnabled returns false when parent disabled", func(t *testing.T) {
		cfg := OTELConfig{Enabled: false}.WithDefaults()
		assert.False(t, cfg.TracingEnabled())
		assert.False(t, cfg.MetricsEnabled())
	})
}

func TestSetupOTEL_SetsGlobalProviders(t *testing.T) {
	cfg := OTELConfig{
		Enabled:     true,
		ExporterURL: "localhost:4317",
		Insecure:    true,
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

func TestSetupOTEL_CancelledContext(t *testing.T) {
	cfg := OTELConfig{
		Enabled:     true,
		ExporterURL: "localhost:4317",
		Insecure:    true,
	}

	shutdown, err := SetupOTEL(context.Background(), cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = shutdown(ctx)
	require.Error(t, err, "shutdown with cancelled context should return error when collector is unreachable")
}

func TestSetupOTEL_ShutdownReturnsShutdownErrors(t *testing.T) {
	cfg := OTELConfig{
		Enabled:     true,
		ExporterURL: "localhost:4317",
		Insecure:    true,
	}

	shutdown, err := SetupOTEL(context.Background(), cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	err = shutdown(ctx)
	require.Error(t, err, "shutdown with expired context should return error")
}

func TestSetupOTEL_UnreachableEndpoint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cfg := OTELConfig{
		Enabled:     true,
		ExporterURL: "192.0.2.1:4317",
		Insecure:    true,
	}

	shutdown, err := SetupOTEL(ctx, cfg)
	if err != nil {
		assert.Contains(t, err.Error(), "creating OTLP")
		return
	}
	require.NotNil(t, shutdown)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer shutdownCancel()
	shutdownErr := shutdown(shutdownCtx)
	assert.Error(t, shutdownErr, "shutdown with unreachable endpoint should return error")
}

func TestOTELTracingMiddleware_CreatesSpan(t *testing.T) {
	spanRecorder := setupTracerProvider(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := OTELTracingMiddleware("test-service")(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	spans := spanRecorder.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "GET /api/test", spans[0].Name())
	assert.Equal(t, "test-service", spans[0].InstrumentationScope().Name)
}

func TestOTELTracingMiddleware_RecordsStatusCode(t *testing.T) {
	spanRecorder := setupTracerProvider(t)

	tests := []struct {
		name         string
		statusCode   int
		expectStatus codes.Code
	}{
		{"200 OK", http.StatusOK, codes.Unset},
		{"404 Not Found", http.StatusNotFound, codes.Unset},
		{"500 Internal Server Error", http.StatusInternalServerError, codes.Error},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spanRecorder.Reset()

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			})

			middleware := OTELTracingMiddleware("test-service")(handler)
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := httptest.NewRecorder()
			middleware.ServeHTTP(rec, req)

			spans := spanRecorder.Ended()
			require.Len(t, spans, 1)
			assert.Equal(t, tt.expectStatus, spans[0].Status().Code)
		})
	}
}

func TestOTELTracingMiddleware_PropagatesContext(t *testing.T) {
	setupTracerProvider(t)

	var capturedCtx context.Context
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
	})

	middleware := OTELTracingMiddleware("test-service")(handler)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	assert.NotNil(t, capturedCtx)
}

func TestOTELMetricsMiddleware_DurationHistogramAttributes(t *testing.T) {
	setupTracerProvider(t)
	reader := setupMeterProvider(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Millisecond)
		w.WriteHeader(http.StatusCreated)
	})

	middleware := OTELMetricsMiddleware("test-service")(handler)
	req := httptest.NewRequest(http.MethodPost, "/api/users", nil)
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	var rm metricdata.ResourceMetrics
	err := reader.Collect(context.Background(), &rm)
	require.NoError(t, err)

	require.Len(t, rm.ScopeMetrics, 1, "expected one scope")

	var histogramFound bool
	for _, m := range rm.ScopeMetrics[0].Metrics {
		if m.Name == "http.server.request.duration" {
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			require.Len(t, histogram.DataPoints, 1)

			dp := histogram.DataPoints[0]
			assert.GreaterOrEqual(t, dp.Sum, float64(0), "duration should be >= 0")
			assert.Equal(t, uint64(1), dp.Count)

			attrs := attrMap(dp.Attributes)
			assert.Equal(t, "POST", attrs[semconv.HTTPRequestMethodKey])
			assert.Equal(t, "/api/users", attrs[semconv.URLPathKey])
			assert.Equal(t, int64(201), attrs[semconv.HTTPResponseStatusCodeKey])
			histogramFound = true
			break
		}
	}
	assert.True(t, histogramFound, "expected http.server.request.duration histogram")
}

func TestOTELMetricsMiddleware_RequestCounterIncrements(t *testing.T) {
	setupTracerProvider(t)
	reader := setupMeterProvider(t)

	callCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	})

	middleware := OTELMetricsMiddleware("test-service")(handler)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		rec := httptest.NewRecorder()
		middleware.ServeHTTP(rec, req)
	}
	assert.Equal(t, 3, callCount)

	var rm metricdata.ResourceMetrics
	err := reader.Collect(context.Background(), &rm)
	require.NoError(t, err)

	require.Len(t, rm.ScopeMetrics, 1, "expected one scope")

	var counterFound bool
	for _, m := range rm.ScopeMetrics[0].Metrics {
		if m.Name == "http.server.request.count" {
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			require.Len(t, sum.DataPoints, 1)

			dp := sum.DataPoints[0]
			assert.Equal(t, int64(3), dp.Value, "counter should have incremented 3 times")

			attrs := attrMap(dp.Attributes)
			assert.Equal(t, "GET", attrs[semconv.HTTPRequestMethodKey])
			assert.Equal(t, "/api/test", attrs[semconv.URLPathKey])
			assert.Equal(t, int64(200), attrs[semconv.HTTPResponseStatusCodeKey])
			counterFound = true
			break
		}
	}
	assert.True(t, counterFound, "expected http.server.request.count counter")
}

func TestOTELMetricsMiddleware_DistinctAttributesPerStatus(t *testing.T) {
	setupTracerProvider(t)
	reader := setupMeterProvider(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		statusStr := r.URL.Query().Get("status")
		if statusStr == "500" {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	})

	middleware := OTELMetricsMiddleware("test-service")(handler)

	for _, status := range []string{"200", "500", "200"} {
		req := httptest.NewRequest(http.MethodGet, "/test?status="+status, nil)
		rec := httptest.NewRecorder()
		middleware.ServeHTTP(rec, req)
	}

	var rm metricdata.ResourceMetrics
	err := reader.Collect(context.Background(), &rm)
	require.NoError(t, err)

	require.Len(t, rm.ScopeMetrics, 1, "expected one scope")

	for _, m := range rm.ScopeMetrics[0].Metrics {
		if m.Name == "http.server.request.count" {
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)

			statusCounts := map[int]int64{}
			for _, dp := range sum.DataPoints {
				attrs := attrMap(dp.Attributes)
				statusCode := attrs[semconv.HTTPResponseStatusCodeKey].(int64)
				statusCounts[int(statusCode)] = dp.Value
			}
			assert.Equal(t, int64(2), statusCounts[200], "200 count should be 2")
			assert.Equal(t, int64(1), statusCounts[500], "500 count should be 1")
			break
		}
	}
}

func TestOTELMiddleware_CombinesTracingAndMetrics(t *testing.T) {
	spanRecorder := setupTracerProvider(t)
	reader := setupMeterProvider(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := OTELMiddleware("test-service")(handler)
	req := httptest.NewRequest(http.MethodGet, "/combined", nil)
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	spans := spanRecorder.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "GET /combined", spans[0].Name())

	var rm metricdata.ResourceMetrics
	err := reader.Collect(context.Background(), &rm)
	require.NoError(t, err)
	require.Len(t, rm.ScopeMetrics, 1)

	var foundCounter, foundHistogram bool
	for _, m := range rm.ScopeMetrics[0].Metrics {
		if m.Name == "http.server.request.count" {
			foundCounter = true
		}
		if m.Name == "http.server.request.duration" {
			foundHistogram = true
		}
	}
	assert.True(t, foundCounter, "combined middleware should record counter")
	assert.True(t, foundHistogram, "combined middleware should record histogram")
}

func TestOTELMiddleware_CapturesStatusCodes(t *testing.T) {
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

			middleware := OTELMiddleware("test-service")(handler)
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := httptest.NewRecorder()
			middleware.ServeHTTP(rec, req)

			assert.Equal(t, tt.statusCode, rec.Code)
		})
	}
}

func TestGetStatusRecorder_ReusesExisting(t *testing.T) {
	rec := httptest.NewRecorder()
	existing := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}

	result := getStatusRecorder(existing)
	assert.Equal(t, existing, result, "should reuse existing statusRecorder")
}

func TestGetStatusRecorder_WrapsNew(t *testing.T) {
	rec := httptest.NewRecorder()
	result := getStatusRecorder(rec)

	assert.IsType(t, &statusRecorder{}, result)
	assert.Equal(t, http.StatusOK, result.status)
}
