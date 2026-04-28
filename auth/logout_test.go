package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
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

// mockIdentityStore is a minimal mock for testing OIDC handlers that don't hit the store.
type mockIdentityStore struct {
	IdentityStore
}
