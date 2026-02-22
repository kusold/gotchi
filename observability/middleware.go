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

type requestIDContextKey struct{}

var requestIDKey = requestIDContextKey{}

func RequestID(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(requestIDKey).(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

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

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(statusCode int) {
	sr.status = statusCode
	sr.ResponseWriter.WriteHeader(statusCode)
}
