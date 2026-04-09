// Package observability provides HTTP middleware for request correlation, audit
// logging, and OpenTelemetry (OTEL) tracing and metrics in the gotchi framework.
//
// The package is organized around two main concerns:
//
//   - Request correlation and audit logging via [CorrelationAndAudit], which
//     assigns (or propagates) a unique request ID, enriches the structured log
//     with method, path, status code, duration, and (when a session is present)
//     user and tenant identifiers.
//   - OpenTelemetry integration via [SetupOTEL], [OTELTracingMiddleware], and
//     [OTELMetricsMiddleware], which configure an OTLP exporter and inject
//     per-request spans and metrics that use Chi route patterns (e.g.
//     "/users/{id}") as low-cardinality attributes.
//
// # Quick Start
//
// Set up OpenTelemetry early in your application startup:
//
//	shutdown, err := observability.SetupOTEL(ctx, observability.OTELConfig{
//	    Enabled:     true,
//	    ServiceName: "my-service",
//	    ExporterURL: "localhost:4317",
//	    Insecure:    true,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer shutdown(context.Background())
//
// Then apply the middleware stack to a Chi router:
//
//	r := chi.NewRouter()
//	r.Use(observability.CorrelationAndAudit(sessionMgr, "session"))
//	r.Use(observability.OTELTracingMiddleware("my-service"))
//	r.Use(observability.OTELMetricsMiddleware("my-service"))
//
// # Request IDs
//
// Every request passing through [CorrelationAndAudit] receives a UUID-based
// request ID (propagated from the X-Request-ID header when present, or
// generated automatically). The ID is stored in the request context and can be
// retrieved with [RequestID].
package observability

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/kusold/gotchi/auth"
	"github.com/kusold/gotchi/session"
)

// requestIDContextKey is an unexported type used as the context key for the
// request ID value. Using an unexported type prevents collisions with keys
// defined in other packages.
type requestIDContextKey struct{}

// requestIDKey is the concrete context key instance for storing and retrieving
// the request ID.
var requestIDKey = requestIDContextKey{}

// RequestID retrieves the request ID from the given context. It returns the ID
// string and true if a non-empty request ID is present, or ("", false) if the
// context carries no request ID, an empty one, or a value of the wrong type.
func RequestID(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(requestIDKey).(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// WithRequestID returns a new context derived from ctx that carries the given
// requestID. It is used internally by [CorrelationAndAudit] but is exported so
// callers can propagate the ID in downstream goroutines or tests.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// CorrelationAndAudit returns an HTTP middleware that assigns each request a
// correlation ID, records a structured audit log line, and propagates the ID
// through the X-Request-ID header and request context.
//
// If the incoming request already carries an X-Request-ID header its value is
// reused; otherwise a new UUID v4 is generated. The ID is written to the
// response header so callers can correlate logs with the downstream response.
//
// The audit log entry (written at slog.InfoLevel) includes the following
// structured fields:
//
//	request_id   – the correlation ID
//	method       – HTTP method
//	path         – request URL path
//	status       – response status code
//	duration_ms  – request duration in milliseconds
//	user_id      – (optional) from session claims
//	tenant_id    – (optional) from session claims
//
// When sessionManager is non-nil the middleware reads [auth.SessionClaims]
// from the session store using sessionKey. If sessionKey is empty it defaults
// to [auth.DefaultSessionKey].
func CorrelationAndAudit(sessionManager *session.Manager, sessionKey string) func(http.Handler) http.Handler {
	key := sessionKey
	if key == "" {
		key = auth.DefaultSessionKey
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			requestID := r.Header.Get("X-Request-ID")
			if requestID == "" {
				requestID = uuid.NewString()
			}

			ctx := WithRequestID(r.Context(), requestID)
			r = r.WithContext(ctx)
			w.Header().Set("X-Request-ID", requestID)

			rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)

			fields := []any{
				"request_id", requestID,
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.status,
				"duration_ms", time.Since(start).Milliseconds(),
			}

			if sessionManager != nil {
				if claims, ok := sessionManager.Get(r.Context(), key).(auth.SessionClaims); ok {
					if claims.UserID != uuid.Nil {
						fields = append(fields, "user_id", claims.UserID.String())
					}
					if claims.ActiveTenantID != nil {
						fields = append(fields, "tenant_id", claims.ActiveTenantID.String())
					}
				}
			}

			slog.Info("request", fields...)
		})
	}
}

// statusRecorder wraps an http.ResponseWriter to capture the status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader intercepts the status code before delegating to the underlying
// ResponseWriter.
func (sr *statusRecorder) WriteHeader(statusCode int) {
	sr.status = statusCode
	sr.ResponseWriter.WriteHeader(statusCode)
}
