package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/alexedwards/scs/v2/memstore"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kusold/gotchi/session"
)

func TestLogoutHandler_DestroysSession(t *testing.T) {
	sessionMgr := setupSessionManager(t)
	tenantID := uuid.New()

	authenticator := &OIDCAuthenticator{
		issuerURL:          "https://example.com",
		endSessionEndpoint: "",
	}

	handler := NewOIDCHandlerWithAuthenticator(
		Config{PostLogoutRedirect: "/goodbye"},
		authenticator,
		sessionMgr,
		&mockIdentityStore{},
	)

	cookies := withSessionClaims(t, sessionMgr, SessionClaims{
		Authenticated:  true,
		UserID:         uuid.New(),
		ActiveTenantID: &tenantID,
	})

	chain := sessionMgr.LoadAndSave(http.HandlerFunc(handler.LogoutHandler))

	req := httptest.NewRequest("POST", "/oidc/logout", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/goodbye", rec.Header().Get("Location"))
}

func TestLogoutHandler_RedirectsToProviderEndSession(t *testing.T) {
	sessionMgr := setupSessionManager(t)
	tenantID := uuid.New()

	authenticator := &OIDCAuthenticator{
		issuerURL:          "https://example.com",
		endSessionEndpoint: "https://example.com/session/end",
	}

	handler := NewOIDCHandlerWithAuthenticator(
		Config{PostLogoutRedirect: "/goodbye"},
		authenticator,
		sessionMgr,
		&mockIdentityStore{},
	)

	cookies := withSessionClaims(t, sessionMgr, SessionClaims{
		Authenticated:  true,
		UserID:         uuid.New(),
		ActiveTenantID: &tenantID,
	})

	chain := sessionMgr.LoadAndSave(http.HandlerFunc(handler.LogoutHandler))

	req := httptest.NewRequest("POST", "/oidc/logout", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	location := rec.Header().Get("Location")
	assert.Contains(t, location, "https://example.com/session/end")
	assert.Contains(t, location, "post_logout_redirect_uri=%2Fgoodbye")
}

func TestLogoutHandler_NoSessionStillSucceeds(t *testing.T) {
	sessionMgr := setupSessionManager(t)

	authenticator := &OIDCAuthenticator{
		issuerURL:          "https://example.com",
		endSessionEndpoint: "",
	}

	handler := NewOIDCHandlerWithAuthenticator(
		Config{PostLogoutRedirect: "/"},
		authenticator,
		sessionMgr,
		&mockIdentityStore{},
	)

	chain := sessionMgr.LoadAndSave(http.HandlerFunc(handler.LogoutHandler))

	req := httptest.NewRequest("POST", "/oidc/logout", nil)
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/", rec.Header().Get("Location"))
}

func TestOIDCAuthenticator_EndSessionURL(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		want     string
	}{
		{"with endpoint", "https://id.example.com/end_session", "https://id.example.com/end_session"},
		{"no endpoint", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := &OIDCAuthenticator{
				issuerURL:          "https://example.com",
				endSessionEndpoint: tt.endpoint,
			}
			assert.Equal(t, tt.want, auth.EndSessionURL())
		})
	}
}

func TestLogoutHandler_SessionDestroyError(t *testing.T) {
	inner := memstore.NewWithCleanupInterval(5 * time.Minute)
	failingStore := &failingDeleteStore{inner: inner}
	sessionMgr := session.New(session.Config{}, failingStore)
	session.RegisterGobTypes(SessionClaims{})

	handler := NewOIDCHandlerWithAuthenticator(
		Config{PostLogoutRedirect: "/goodbye"},
		&OIDCAuthenticator{issuerURL: "https://example.com"},
		sessionMgr,
		&mockIdentityStore{},
	)

	tenantID := uuid.New()
	cookies := withSessionClaims(t, sessionMgr, SessionClaims{
		Authenticated:  true,
		UserID:         uuid.New(),
		ActiveTenantID: &tenantID,
	})

	chain := sessionMgr.LoadAndSave(http.HandlerFunc(handler.LogoutHandler))

	req := httptest.NewRequest("POST", "/oidc/logout", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "failed to destroy session")
}

func TestLogoutHandler_MalformedEndSessionEndpoint(t *testing.T) {
	sessionMgr := setupSessionManager(t)

	authenticator := &OIDCAuthenticator{
		issuerURL:          "https://example.com",
		endSessionEndpoint: "https://example.com/\x00bad",
	}

	handler := NewOIDCHandlerWithAuthenticator(
		Config{PostLogoutRedirect: "/goodbye"},
		authenticator,
		sessionMgr,
		&mockIdentityStore{},
	)

	chain := sessionMgr.LoadAndSave(http.HandlerFunc(handler.LogoutHandler))

	req := httptest.NewRequest("POST", "/oidc/logout", nil)
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/goodbye", rec.Header().Get("Location"))
}

func TestLogoutHandler_PassesIDTokenHint(t *testing.T) {
	sessionMgr := setupSessionManager(t)

	authenticator := &OIDCAuthenticator{
		issuerURL:          "https://example.com",
		endSessionEndpoint: "https://example.com/session/end",
	}

	handler := NewOIDCHandlerWithAuthenticator(
		Config{PostLogoutRedirect: "/goodbye"},
		authenticator,
		sessionMgr,
		&mockIdentityStore{},
	)

	tenantID := uuid.New()
	cookies := withSessionClaimsAndIDToken(t, sessionMgr, SessionClaims{
		Authenticated:  true,
		UserID:         uuid.New(),
		ActiveTenantID: &tenantID,
	}, "mock-raw-id-token")

	chain := sessionMgr.LoadAndSave(http.HandlerFunc(handler.LogoutHandler))

	req := httptest.NewRequest("POST", "/oidc/logout", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	location := rec.Header().Get("Location")
	assert.Contains(t, location, "https://example.com/session/end")
	assert.Contains(t, location, "id_token_hint=mock-raw-id-token")
	assert.Contains(t, location, "post_logout_redirect_uri=%2Fgoodbye")
}

type failingDeleteStore struct {
	inner scs.Store
}

func (s *failingDeleteStore) Delete(_ string) error {
	return fmt.Errorf("forced delete failure")
}

func (s *failingDeleteStore) Find(token string) ([]byte, bool, error) {
	return s.inner.Find(token)
}

func (s *failingDeleteStore) Commit(token string, b []byte, expiry time.Time) error {
	return s.inner.Commit(token, b, expiry)
}

func withSessionClaimsAndIDToken(t *testing.T, sessionMgr *session.Manager, claims SessionClaims, rawIDToken string) []*http.Cookie {
	t.Helper()

	setHandler := sessionMgr.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionMgr.Put(r.Context(), DefaultSessionKey, claims)
		sessionMgr.Put(r.Context(), idTokenKey, rawIDToken)
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

// mockIdentityStore is a minimal mock for testing OIDC handlers that don't hit the store.
type mockIdentityStore struct {
	IdentityStore
}
