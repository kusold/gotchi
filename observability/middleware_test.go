package observability

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kusold/gotchi/auth"
	"github.com/kusold/gotchi/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSessionManager(t *testing.T) *session.Manager {
	t.Helper()
	session.RegisterGobTypes(auth.SessionClaims{})
	mgr := session.NewMemory(session.Config{})
	return mgr
}

func setupTestLogger(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	original := slog.Default()
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	slog.SetDefault(logger)
	t.Cleanup(func() {
		slog.SetDefault(original)
	})
	return &buf
}

func extractSessionToken(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == "session" {
			return c.Value
		}
	}
	return ""
}

func setupSessionWithClaims(t *testing.T, sessionMgr *session.Manager, key string, value any) string {
	t.Helper()
	setUpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionMgr.Put(r.Context(), key, value)
		w.WriteHeader(http.StatusOK)
	})
	wrappedSetup := sessionMgr.LoadAndSave(setUpHandler)
	setupReq := httptest.NewRequest(http.MethodGet, "/setup", nil)
	setupRec := httptest.NewRecorder()
	wrappedSetup.ServeHTTP(setupRec, setupReq)
	token := extractSessionToken(t, setupRec)
	require.NotEmpty(t, token, "should have session token")
	return token
}

func TestRequestID_Get(t *testing.T) {
	tests := []struct {
		name      string
		setupCtx  func() context.Context
		wantID    string
		wantFound bool
	}{
		{
			name: "returns ID when present",
			setupCtx: func() context.Context {
				return WithRequestID(context.Background(), "test-request-id")
			},
			wantID:    "test-request-id",
			wantFound: true,
		},
		{
			name: "returns false when not set",
			setupCtx: func() context.Context {
				return context.Background()
			},
			wantID:    "",
			wantFound: false,
		},
		{
			name: "returns false when empty string",
			setupCtx: func() context.Context {
				return WithRequestID(context.Background(), "")
			},
			wantID:    "",
			wantFound: false,
		},
		{
			name: "returns false when wrong type in context",
			setupCtx: func() context.Context {
				return context.WithValue(context.Background(), requestIDKey, 123)
			},
			wantID:    "",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()
			gotID, gotFound := RequestID(ctx)
			assert.Equal(t, tt.wantID, gotID)
			assert.Equal(t, tt.wantFound, gotFound)
		})
	}
}

func TestWithRequestID(t *testing.T) {
	ctx := context.Background()
	newCtx := WithRequestID(ctx, "my-request-id")

	_, found := RequestID(ctx)
	assert.False(t, found)

	id, found := RequestID(newCtx)
	assert.True(t, found)
	assert.Equal(t, "my-request-id", id)
}

func TestCorrelationAndAudit_GeneratesUUID(t *testing.T) {
	logBuf := setupTestLogger(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := CorrelationAndAudit(nil, "")(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	requestID := rec.Header().Get("X-Request-ID")
	assert.NotEmpty(t, requestID)
	_, err := uuid.Parse(requestID)
	assert.NoError(t, err, "generated request ID should be a valid UUID")

	assert.Contains(t, logBuf.String(), "request_id="+requestID)
}

func TestCorrelationAndAudit_UsesProvidedRequestID(t *testing.T) {
	logBuf := setupTestLogger(t)

	providedID := "custom-request-id-123"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := CorrelationAndAudit(nil, "")(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Request-ID", providedID)
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	assert.Equal(t, providedID, rec.Header().Get("X-Request-ID"))
	assert.Contains(t, logBuf.String(), "request_id="+providedID)
}

func TestCorrelationAndAudit_SetsResponseHeader(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := CorrelationAndAudit(nil, "")(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	assert.NotEmpty(t, rec.Header().Get("X-Request-ID"))
}

func TestCorrelationAndAudit_RecordsStatusCodes(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		expectStatus string
	}{
		{
			name:         "200 OK",
			statusCode:   http.StatusOK,
			expectStatus: "200",
		},
		{
			name:         "404 Not Found",
			statusCode:   http.StatusNotFound,
			expectStatus: "404",
		},
		{
			name:         "500 Internal Server Error",
			statusCode:   http.StatusInternalServerError,
			expectStatus: "500",
		},
		{
			name:         "201 Created",
			statusCode:   http.StatusCreated,
			expectStatus: "201",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logBuf := setupTestLogger(t)

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			})

			middleware := CorrelationAndAudit(nil, "")(handler)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := httptest.NewRecorder()
			middleware.ServeHTTP(rec, req)

			assert.Equal(t, tt.statusCode, rec.Code)
			assert.Contains(t, logBuf.String(), "status="+tt.expectStatus)
		})
	}
}

func TestCorrelationAndAudit_LogsCorrectFields(t *testing.T) {
	logBuf := setupTestLogger(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("response"))
	})

	middleware := CorrelationAndAudit(nil, "")(handler)

	req := httptest.NewRequest(http.MethodPost, "/api/users/123", nil)
	req.Header.Set("X-Request-ID", "req-123")
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	logOutput := logBuf.String()

	assert.Contains(t, logOutput, "request_id=req-123")
	assert.Contains(t, logOutput, "method=POST")
	assert.Contains(t, logOutput, "path=/api/users/123")
	assert.Contains(t, logOutput, "status=200")
	assert.Contains(t, logOutput, "duration_ms=")
	assert.Contains(t, logOutput, "msg=request")
}

func TestCorrelationAndAudit_NilSessionManager(t *testing.T) {
	logBuf := setupTestLogger(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := CorrelationAndAudit(nil, "")(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() {
		middleware.ServeHTTP(rec, req)
	})

	assert.NotContains(t, logBuf.String(), "user_id=")
	assert.NotContains(t, logBuf.String(), "tenant_id=")
}

func TestCorrelationAndAudit_WithSessionClaims(t *testing.T) {
	setupTestLogger(t)

	sessionMgr := newTestSessionManager(t)
	userID := uuid.New()
	tenantID := uuid.New()
	claims := auth.SessionClaims{
		Authenticated:  true,
		UserID:         userID,
		Issuer:         "test-issuer",
		Subject:        "test-subject",
		ActiveTenantID: &tenantID,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := sessionMgr.LoadAndSave(CorrelationAndAudit(sessionMgr, "session")(handler))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	sessionToken := extractSessionToken(t, rec)

	if sessionToken != "" {
		logBuf2 := setupTestLogger(t)

		ctx, err := sessionMgr.Load(context.Background(), sessionToken)
		require.NoError(t, err)
		sessionMgr.Put(ctx, "session", claims)

		handler2 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotClaims, ok := sessionMgr.Get(r.Context(), "session").(auth.SessionClaims)
			assert.True(t, ok)
			assert.Equal(t, userID, gotClaims.UserID)
			w.WriteHeader(http.StatusOK)
		})

		middleware2 := sessionMgr.LoadAndSave(CorrelationAndAudit(sessionMgr, "session")(handler2))
		req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
		req2.AddCookie(&http.Cookie{Name: "session", Value: sessionToken})
		rec2 := httptest.NewRecorder()
		middleware2.ServeHTTP(rec2, req2)

		_ = logBuf2.String()
	}
}

func TestCorrelationAndAudit_SessionClaimsWithNilTenantID(t *testing.T) {
	userID := uuid.New()
	claims := auth.SessionClaims{
		Authenticated:  true,
		UserID:         userID,
		ActiveTenantID: nil,
	}

	sessionMgr := newTestSessionManager(t)
	sessionToken := setupSessionWithClaims(t, sessionMgr, "session", claims)
	logOutput := makeRequestWithClaims(t, sessionMgr, sessionToken, claims)

	assert.Contains(t, logOutput, "user_id="+userID.String())
	assert.NotContains(t, logOutput, "tenant_id=")
}

func TestCorrelationAndAudit_SessionClaimsNilUserID(t *testing.T) {
	claims := auth.SessionClaims{
		Authenticated:  true,
		UserID:         uuid.Nil,
		ActiveTenantID: nil,
	}

	sessionMgr := newTestSessionManager(t)
	sessionToken := setupSessionWithClaims(t, sessionMgr, "session", claims)
	logOutput := makeRequestWithClaims(t, sessionMgr, sessionToken, claims)

	assert.NotContains(t, logOutput, "user_id=")
}

func TestCorrelationAndAudit_SessionClaimsWrongType(t *testing.T) {
	sessionMgr := newTestSessionManager(t)
	sessionToken := setupSessionWithClaims(t, sessionMgr, "session", "not-session-claims")
	logBuf := setupTestLogger(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := sessionMgr.LoadAndSave(CorrelationAndAudit(sessionMgr, "session")(handler))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: sessionToken})
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() {
		middleware.ServeHTTP(rec, req)
	})

	assert.NotContains(t, logBuf.String(), "user_id=")
	assert.NotContains(t, logBuf.String(), "tenant_id=")
}

func TestCorrelationAndAudit_DefaultSessionKey(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()
	claims := auth.SessionClaims{
		Authenticated:  true,
		UserID:         userID,
		ActiveTenantID: &tenantID,
	}

	sessionMgr := newTestSessionManager(t)
	sessionToken := setupSessionWithClaims(t, sessionMgr, auth.DefaultSessionKey, claims)
	logBuf := setupTestLogger(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := sessionMgr.LoadAndSave(CorrelationAndAudit(sessionMgr, "")(handler))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: sessionToken})
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	assert.Contains(t, logBuf.String(), "user_id="+userID.String())
	assert.Contains(t, logBuf.String(), "tenant_id="+tenantID.String())
}

func TestCorrelationAndAudit_CustomSessionKey(t *testing.T) {
	userID := uuid.New()
	claims := auth.SessionClaims{
		Authenticated: true,
		UserID:        userID,
	}

	sessionMgr := newTestSessionManager(t)
	sessionToken := setupSessionWithClaims(t, sessionMgr, "custom-key", claims)
	logBuf := setupTestLogger(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := sessionMgr.LoadAndSave(CorrelationAndAudit(sessionMgr, "custom-key")(handler))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: sessionToken})
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	assert.Contains(t, logBuf.String(), "user_id="+userID.String())
}

func TestCorrelationAndAudit_PassesContextToHandler(t *testing.T) {
	var capturedCtx context.Context

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
	})

	middleware := CorrelationAndAudit(nil, "")(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Request-ID", "test-id-123")
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	id, found := RequestID(capturedCtx)
	require.True(t, found, "handler context should have request ID")
	assert.Equal(t, "test-id-123", id)
}

func TestCorrelationAndAudit_DefaultStatusOnNoWriteHeader(t *testing.T) {
	logBuf := setupTestLogger(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello"))
	})

	middleware := CorrelationAndAudit(nil, "")(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	assert.Contains(t, logBuf.String(), "status=200")
}

func makeRequestWithClaims(t *testing.T, sessionMgr *session.Manager, sessionToken string, claims auth.SessionClaims) string {
	t.Helper()

	logBuf := setupTestLogger(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := sessionMgr.LoadAndSave(CorrelationAndAudit(sessionMgr, "session")(handler))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: sessionToken})
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	return logBuf.String()
}

func TestStatusRecorder_WriteHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}

	assert.Equal(t, http.StatusOK, sr.status)

	sr.WriteHeader(http.StatusNotFound)
	assert.Equal(t, http.StatusNotFound, sr.status)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestStatusRecorder_MultipleWriteHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}

	sr.WriteHeader(http.StatusInternalServerError)
	assert.Equal(t, http.StatusInternalServerError, sr.status)

	sr.WriteHeader(http.StatusBadRequest)
	assert.Equal(t, http.StatusBadRequest, sr.status)
}

func TestStatusRecorder_DefaultStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}

	assert.Equal(t, http.StatusOK, sr.status)
}

func TestDurationLogged(t *testing.T) {
	logBuf := setupTestLogger(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := CorrelationAndAudit(nil, "")(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "duration_ms=")

	lines := strings.Split(logOutput, "\n")
	for _, line := range lines {
		if strings.Contains(line, "duration_ms=") {
			parts := strings.Split(line, " ")
			for _, part := range parts {
				if strings.HasPrefix(part, "duration_ms=") {
					assert.True(t, len(part) > len("duration_ms="), "duration_ms should have a value")
				}
			}
		}
	}
}

func TestCorrelationAndAudit_WithBothUserIDAndTenantID(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()
	claims := auth.SessionClaims{
		Authenticated:  true,
		UserID:         userID,
		ActiveTenantID: &tenantID,
	}

	sessionMgr := newTestSessionManager(t)
	sessionToken := setupSessionWithClaims(t, sessionMgr, "session", claims)
	logOutput := makeRequestWithClaims(t, sessionMgr, sessionToken, claims)

	assert.Contains(t, logOutput, "user_id="+userID.String())
	assert.Contains(t, logOutput, "tenant_id="+tenantID.String())
}
