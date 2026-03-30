package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kusold/gotchi/session"
	"github.com/kusold/gotchi/tenantctx"
)

func setupSessionManager(t *testing.T) *session.Manager {
	t.Helper()
	session.RegisterGobTypes(SessionClaims{})
	return session.NewMemory(session.Config{})
}

func withSessionClaims(t *testing.T, sessionMgr *session.Manager, claims SessionClaims) []*http.Cookie {
	t.Helper()

	setHandler := sessionMgr.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionMgr.Put(r.Context(), DefaultSessionKey, claims)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	setHandler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	cookies := rec.Result().Cookies()
	require.NotEmpty(t, cookies, "should have a session cookie")
	return cookies
}

func TestRequireAuthenticated_SetsTenantContext(t *testing.T) {
	sessionMgr := setupSessionManager(t)
	tenantID := uuid.New()
	userID := uuid.New()

	cookies := withSessionClaims(t, sessionMgr, SessionClaims{
		Authenticated:  true,
		UserID:         userID,
		Issuer:         "test-issuer",
		Subject:        "test-subject",
		ActiveTenantID: &tenantID,
	})

	var capturedCtx context.Context
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
	})

	chain := sessionMgr.LoadAndSave(
		RequireAuthenticated(sessionMgr, MiddlewareConfig{Mode: ModeAPI})(testHandler),
	)

	req := httptest.NewRequest("GET", "/test", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	gotTenantID, ok := tenantctx.TenantID(capturedCtx)
	assert.True(t, ok, "tenant ID should be set in context")
	assert.Equal(t, tenantID, gotTenantID)

	claims, ok := SessionClaimsFromContext(capturedCtx)
	assert.True(t, ok, "claims should be set in context")
	assert.True(t, claims.Authenticated)
	assert.Equal(t, userID, claims.UserID)
}

func TestRequireAuthenticated_RejectsUnauthenticated(t *testing.T) {
	sessionMgr := setupSessionManager(t)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for unauthenticated request")
	})

	chain := sessionMgr.LoadAndSave(
		RequireAuthenticated(sessionMgr, MiddlewareConfig{Mode: ModeAPI})(testHandler),
	)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireAuthenticated_RequiresTenantSelection(t *testing.T) {
	sessionMgr := setupSessionManager(t)
	userID := uuid.New()

	cookies := withSessionClaims(t, sessionMgr, SessionClaims{
		Authenticated:  true,
		UserID:         userID,
		Issuer:         "test-issuer",
		Subject:        "test-subject",
		ActiveTenantID: nil,
	})

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called when tenant not selected")
	})

	chain := sessionMgr.LoadAndSave(
		RequireAuthenticated(sessionMgr, MiddlewareConfig{Mode: ModeAPI})(testHandler),
	)

	req := httptest.NewRequest("GET", "/test", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestRequireAuthenticated_UIMode_RedirectsUnauthenticated(t *testing.T) {
	sessionMgr := setupSessionManager(t)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})

	chain := sessionMgr.LoadAndSave(
		RequireAuthenticated(sessionMgr, MiddlewareConfig{Mode: ModeUI})(testHandler),
	)

	req := httptest.NewRequest("GET", "/protected", nil)
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Contains(t, rec.Header().Get("Location"), "/auth/login")
}

func TestRequireAuthenticated_UIMode_RedirectsToTenantPicker(t *testing.T) {
	sessionMgr := setupSessionManager(t)
	userID := uuid.New()

	cookies := withSessionClaims(t, sessionMgr, SessionClaims{
		Authenticated:  true,
		UserID:         userID,
		ActiveTenantID: nil,
	})

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called when tenant not selected")
	})

	chain := sessionMgr.LoadAndSave(
		RequireAuthenticated(sessionMgr, MiddlewareConfig{Mode: ModeUI})(testHandler),
	)

	req := httptest.NewRequest("GET", "/protected", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Contains(t, rec.Header().Get("Location"), "/auth/tenants")
}
