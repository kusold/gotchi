package auth

import (
	"context"
	"encoding/json"
	"io"
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

// --- AllowPathsWithoutTenant tests ---

func TestRequireAuthenticated_AllowPathsWithoutTenant_ExactMatch(t *testing.T) {
	sessionMgr := setupSessionManager(t)
	userID := uuid.New()

	cookies := withSessionClaims(t, sessionMgr, SessionClaims{
		Authenticated:  true,
		UserID:         userID,
		ActiveTenantID: nil, // no tenant selected
	})

	var called bool
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	chain := sessionMgr.LoadAndSave(
		RequireAuthenticated(sessionMgr, MiddlewareConfig{
			Mode:                    ModeAPI,
			AllowPathsWithoutTenant: []string{"/allowed-path"},
		})(testHandler),
	)

	req := httptest.NewRequest("GET", "/allowed-path", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, called, "handler should be called for allowed path without tenant")
}

func TestRequireAuthenticated_AllowPathsWithoutTenant_WildcardMatch(t *testing.T) {
	sessionMgr := setupSessionManager(t)
	userID := uuid.New()

	cookies := withSessionClaims(t, sessionMgr, SessionClaims{
		Authenticated:  true,
		UserID:         userID,
		ActiveTenantID: nil,
	})

	var called bool
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	chain := sessionMgr.LoadAndSave(
		RequireAuthenticated(sessionMgr, MiddlewareConfig{
			Mode:                    ModeAPI,
			AllowPathsWithoutTenant: []string{"/api/public/*"},
		})(testHandler),
	)

	req := httptest.NewRequest("GET", "/api/public/health", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, called, "handler should be called for wildcard-matched path")
}

func TestRequireAuthenticated_AllowPathsWithoutTenant_NonMatchingPath(t *testing.T) {
	sessionMgr := setupSessionManager(t)
	userID := uuid.New()

	cookies := withSessionClaims(t, sessionMgr, SessionClaims{
		Authenticated:  true,
		UserID:         userID,
		ActiveTenantID: nil,
	})

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for non-matching path without tenant")
	})

	chain := sessionMgr.LoadAndSave(
		RequireAuthenticated(sessionMgr, MiddlewareConfig{
			Mode:                    ModeAPI,
			AllowPathsWithoutTenant: []string{"/allowed-path"},
		})(testHandler),
	)

	req := httptest.NewRequest("GET", "/not-allowed", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

// --- Legacy context key tests ---

type legacyTenantKey struct{}
type legacyClaimsKey struct{}

func TestRequireAuthenticated_LegacyTenantContextKey(t *testing.T) {
	sessionMgr := setupSessionManager(t)
	tenantID := uuid.New()
	userID := uuid.New()

	cookies := withSessionClaims(t, sessionMgr, SessionClaims{
		Authenticated:  true,
		UserID:         userID,
		ActiveTenantID: &tenantID,
	})

	var capturedCtx context.Context
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
	})

	chain := sessionMgr.LoadAndSave(
		RequireAuthenticated(sessionMgr, MiddlewareConfig{
			Mode:                   ModeAPI,
			LegacyTenantContextKey: legacyTenantKey{},
		})(testHandler),
	)

	req := httptest.NewRequest("GET", "/test", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	// Verify the legacy key has the tenant ID as a string
	legacyVal, ok := capturedCtx.Value(legacyTenantKey{}).(string)
	require.True(t, ok, "legacy tenant key should be set in context")
	assert.Equal(t, tenantID.String(), legacyVal)

	// Verify the standard tenantctx key is also set
	gotTenantID, ok := tenantctx.TenantID(capturedCtx)
	assert.True(t, ok)
	assert.Equal(t, tenantID, gotTenantID)
}

func TestRequireAuthenticated_LegacyClaimsContextKey(t *testing.T) {
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
		RequireAuthenticated(sessionMgr, MiddlewareConfig{
			Mode:                   ModeAPI,
			LegacyClaimsContextKey: legacyClaimsKey{},
		})(testHandler),
	)

	req := httptest.NewRequest("GET", "/test", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	legacyClaims, ok := capturedCtx.Value(legacyClaimsKey{}).(SessionClaims)
	require.True(t, ok, "legacy claims key should be set in context")
	assert.Equal(t, userID, legacyClaims.UserID)
	assert.Equal(t, "test-issuer", legacyClaims.Issuer)
}

// --- API response body format tests ---

func TestRequireAuthenticated_APIMode_UnauthorizedResponseBody(t *testing.T) {
	sessionMgr := setupSessionManager(t)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})

	chain := sessionMgr.LoadAndSave(
		RequireAuthenticated(sessionMgr, MiddlewareConfig{Mode: ModeAPI})(testHandler),
	)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	body, err := io.ReadAll(rec.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "unauthorized")
}

func TestRequireAuthenticated_APIMode_TenantRequiredResponseBody(t *testing.T) {
	sessionMgr := setupSessionManager(t)
	userID := uuid.New()

	cookies := withSessionClaims(t, sessionMgr, SessionClaims{
		Authenticated:  true,
		UserID:         userID,
		ActiveTenantID: nil,
	})

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
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
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var respBody map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&respBody))
	assert.Equal(t, "tenant_selection_required", respBody["error"])
	assert.Equal(t, true, respBody["tenant_selection_required"])
}

// --- isTenantOptionalPath unit tests ---

func TestIsTenantOptionalPath_ExactMatch(t *testing.T) {
	assert.True(t, isTenantOptionalPath("/auth/tenants", []string{"/auth/tenants"}))
}

func TestIsTenantOptionalPath_ExactMatch_MultiplePaths(t *testing.T) {
	assert.True(t, isTenantOptionalPath("/auth/tenant/select", []string{"/auth/tenants", "/auth/tenant/select"}))
}

func TestIsTenantOptionalPath_ExactMatch_NoMatch(t *testing.T) {
	assert.False(t, isTenantOptionalPath("/dashboard", []string{"/auth/tenants", "/auth/tenant/select"}))
}

func TestIsTenantOptionalPath_PrefixMatch(t *testing.T) {
	assert.True(t, isTenantOptionalPath("/api/public/health", []string{"/api/public/*"}))
}

func TestIsTenantOptionalPath_PrefixMatch_DeepPath(t *testing.T) {
	assert.True(t, isTenantOptionalPath("/api/public/v1/status", []string{"/api/public/*"}))
}

func TestIsTenantOptionalPath_PrefixMatch_NoMatch(t *testing.T) {
	assert.False(t, isTenantOptionalPath("/api/private/data", []string{"/api/public/*"}))
}

func TestIsTenantOptionalPath_EmptyAllowedPath(t *testing.T) {
	assert.False(t, isTenantOptionalPath("/anything", []string{""}))
}

func TestIsTenantOptionalPath_MixedAllowedPaths(t *testing.T) {
	allowPaths := []string{"", "/exact", "/prefix/*"}
	assert.False(t, isTenantOptionalPath("/anything", allowPaths), "empty string should be skipped")
	assert.True(t, isTenantOptionalPath("/exact", allowPaths), "exact match should work")
	assert.True(t, isTenantOptionalPath("/prefix/sub", allowPaths), "prefix match should work")
	assert.False(t, isTenantOptionalPath("/prefix", allowPaths), "prefix without trailing slash should not match wildcard /prefix/*")
}

func TestIsTenantOptionalPath_EmptyAllowPaths(t *testing.T) {
	assert.False(t, isTenantOptionalPath("/any", []string{}))
}

func TestIsTenantOptionalPath_WildcardMatchesExactPrefix(t *testing.T) {
	assert.True(t, isTenantOptionalPath("/api/public", []string{"/api/public/*"}))
}

// --- MiddlewareConfig.withDefaults tests ---

func TestMiddlewareConfig_WithDefaults(t *testing.T) {
	cfg := MiddlewareConfig{}.withDefaults()
	assert.Equal(t, DefaultSessionKey, cfg.SessionKey)
	assert.Equal(t, DefaultLoginPath, cfg.LoginPath)
	assert.Equal(t, DefaultTenantPickerPath, cfg.TenantPickerPath)
	assert.Equal(t, ModeAPI, cfg.Mode)
	assert.Equal(t, []string{DefaultTenantPickerPath, "/auth/tenant/select"}, cfg.AllowPathsWithoutTenant)
}

func TestMiddlewareConfig_WithDefaults_PreservesExplicitValues(t *testing.T) {
	customPath := "/custom-login"
	cfg := MiddlewareConfig{LoginPath: customPath, Mode: ModeUI}.withDefaults()
	assert.Equal(t, customPath, cfg.LoginPath)
	assert.Equal(t, ModeUI, cfg.Mode)
}

// --- Corrupted session data test ---

func TestRequireAuthenticated_CorruptedSessionData(t *testing.T) {
	sessionMgr := setupSessionManager(t)

	// Write a non-SessionClaims value into the session under the auth key
	setHandler := sessionMgr.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionMgr.Put(r.Context(), DefaultSessionKey, "not-a-claims-struct")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/setup", nil)
	rec := httptest.NewRecorder()
	setHandler.ServeHTTP(rec, req)
	cookies := rec.Result().Cookies()
	require.NotEmpty(t, cookies)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called with corrupted session")
	})

	chain := sessionMgr.LoadAndSave(
		RequireAuthenticated(sessionMgr, MiddlewareConfig{Mode: ModeAPI})(testHandler),
	)

	req = httptest.NewRequest("GET", "/test", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec = httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code, "corrupted session should be treated as unauthenticated")
}
