package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kusold/gotchi/internal/testoidc"
	"github.com/kusold/gotchi/session"
)

func setupOAuthHandler(t *testing.T, tenantName string) (*OIDCHandler, *testoidc.MockOIDCProvider, chi.Router) {
	t.Helper()

	session.RegisterGobTypes(SessionClaims{})
	sessionMgr := session.NewMemory(session.Config{})

	mockOIDC := testoidc.NewMockOIDCProvider("test-client-id")
	t.Cleanup(mockOIDC.Close)

	provider, err := oidc.NewProvider(context.Background(), mockOIDC.IssuerURL())
	require.NoError(t, err)

	cfg := Config{
		IssuerURL:         mockOIDC.IssuerURL(),
		ClientID:          "test-client-id",
		ClientSecret:      "test-client-secret",
		RedirectURL:       "http://localhost/oidc/callback",
		PostLoginRedirect: "/dashboard",
	}

	authenticator, err := NewOIDCAuthenticatorWithProvider(cfg, provider)
	require.NoError(t, err)

	store := newTestStore(t, tenantName)
	handler := NewOIDCHandlerWithAuthenticator(cfg, authenticator, sessionMgr, store)

	r := chi.NewRouter()
	r.Use(sessionMgr.LoadAndSave)
	handler.RegisterRoutes(r)

	return handler, mockOIDC, r
}

func doAuthorize(t *testing.T, router chi.Router) (string, []*http.Cookie) {
	t.Helper()

	req := httptest.NewRequest("GET", "/oidc/authorize", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusFound, rec.Code)

	location := rec.Header().Get("Location")
	parsedURL, err := url.Parse(location)
	require.NoError(t, err)

	state := parsedURL.Query().Get("state")
	require.NotEmpty(t, state)

	return state, rec.Result().Cookies()
}

func doCallback(t *testing.T, router chi.Router, code, state string, cookies []*http.Cookie, extraHeaders ...http.Header) *httptest.ResponseRecorder {
	t.Helper()

	callbackURL := "/oidc/callback?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)
	req := httptest.NewRequest("GET", callbackURL, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	for _, h := range extraHeaders {
		for k, vals := range h {
			for _, v := range vals {
				req.Header.Add(k, v)
			}
		}
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func addSecondTenant(t *testing.T, userID uuid.UUID, tenantName string) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	tenantID := uuid.New()
	_, err := testDB.Pool.Exec(ctx, "INSERT INTO tenants (tenant_id, name) VALUES ($1, $2)", tenantID, tenantName)
	require.NoError(t, err)
	_, err = testDB.Pool.Exec(ctx, "INSERT INTO tenant_memberships (user_id, tenant_id, role) VALUES ($1, $2, 'admin')", userID, tenantID)
	require.NoError(t, err)
	return tenantID
}

func TestOAuthFlow_SingleTenant(t *testing.T) {
	_, mockOIDC, router := setupOAuthHandler(t, "Single Tenant "+uuid.New().String()[:8])

	testUser := &testoidc.TestUser{
		Subject:           "user-single-1",
		Email:             "single@example.com",
		EmailVerified:     true,
		Name:              "Single Tenant User",
		PreferredUsername: "singleuser",
	}

	state, cookies := doAuthorize(t, router)
	code := mockOIDC.CreateAuthCode(testUser)

	rec := doCallback(t, router, code, state, cookies)

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/dashboard", rec.Header().Get("Location"))
}

func TestOAuthFlow_MultiTenant_RequiresSelection(t *testing.T) {
	ctx := context.Background()
	_, mockOIDC, router := setupOAuthHandler(t, "Multi Tenant A "+uuid.New().String()[:8])

	identity := Identity{
		Issuer:            mockOIDC.IssuerURL(),
		Subject:           "user-multi-1",
		Email:             "multi@example.com",
		EmailVerified:     true,
		PreferredUsername: "multiuser",
	}
	store := newTestStore(t, "Multi Tenant B "+uuid.New().String()[:8])
	userRef, err := store.ResolveOrProvisionUser(ctx, identity)
	require.NoError(t, err)
	addSecondTenant(t, userRef.UserID, "Second Org "+uuid.New().String()[:8])

	testUser := &testoidc.TestUser{
		Subject:           "user-multi-1",
		Email:             "multi@example.com",
		EmailVerified:     true,
		Name:              "Multi Tenant User",
		PreferredUsername: "multiuser",
	}

	state, cookies := doAuthorize(t, router)
	code := mockOIDC.CreateAuthCode(testUser)

	rec := doCallback(t, router, code, state, cookies, http.Header{
		"Accept": {"application/json"},
	})

	assert.Equal(t, http.StatusConflict, rec.Code)

	var body map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, true, body["tenant_selection_required"])
	assert.Equal(t, "tenant_selection_required", body["error"])
}

func TestTenantSelection_ListTenants(t *testing.T) {
	ctx := context.Background()
	_, mockOIDC, router := setupOAuthHandler(t, "List Tenants A "+uuid.New().String()[:8])

	identity := Identity{
		Issuer:            mockOIDC.IssuerURL(),
		Subject:           "user-list-1",
		Email:             "list@example.com",
		EmailVerified:     true,
		PreferredUsername: "listuser",
	}
	store := newTestStore(t, "List Tenants B "+uuid.New().String()[:8])
	userRef, err := store.ResolveOrProvisionUser(ctx, identity)
	require.NoError(t, err)
	addSecondTenant(t, userRef.UserID, "Second Org "+uuid.New().String()[:8])

	testUser := &testoidc.TestUser{
		Subject:           "user-list-1",
		Email:             "list@example.com",
		EmailVerified:     true,
		PreferredUsername: "listuser",
	}

	state, cookies := doAuthorize(t, router)
	code := mockOIDC.CreateAuthCode(testUser)
	rec := doCallback(t, router, code, state, cookies, http.Header{
		"Accept": {"application/json"},
	})
	require.Equal(t, http.StatusConflict, rec.Code)

	sessionCookies := rec.Result().Cookies()
	require.NotEmpty(t, sessionCookies)

	listReq := httptest.NewRequest("GET", "/tenants", nil)
	listReq.Header.Set("Accept", "application/json")
	for _, c := range sessionCookies {
		listReq.AddCookie(c)
	}
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)

	assert.Equal(t, http.StatusOK, listRec.Code)

	var tenants []map[string]any
	require.NoError(t, json.NewDecoder(listRec.Body).Decode(&tenants))
	assert.Len(t, tenants, 2)
}

func TestTenantSelection_Success(t *testing.T) {
	ctx := context.Background()
	_, mockOIDC, router := setupOAuthHandler(t, "Select Tenant A "+uuid.New().String()[:8])

	identity := Identity{
		Issuer:            mockOIDC.IssuerURL(),
		Subject:           "user-select-1",
		Email:             "select@example.com",
		EmailVerified:     true,
		PreferredUsername: "selectuser",
	}
	store := newTestStore(t, "Select Tenant B "+uuid.New().String()[:8])
	userRef, err := store.ResolveOrProvisionUser(ctx, identity)
	require.NoError(t, err)
	secondTenantID := addSecondTenant(t, userRef.UserID, "Target Org "+uuid.New().String()[:8])

	testUser := &testoidc.TestUser{
		Subject:           "user-select-1",
		Email:             "select@example.com",
		EmailVerified:     true,
		PreferredUsername: "selectuser",
	}

	state, cookies := doAuthorize(t, router)
	code := mockOIDC.CreateAuthCode(testUser)
	rec := doCallback(t, router, code, state, cookies, http.Header{
		"Accept": {"application/json"},
	})
	require.Equal(t, http.StatusConflict, rec.Code)
	sessionCookies := rec.Result().Cookies()
	require.NotEmpty(t, sessionCookies)

	selectReq := httptest.NewRequest("POST", "/tenant/select", strings.NewReader(
		`{"tenant_id":"`+secondTenantID.String()+`"}`,
	))
	selectReq.Header.Set("Content-Type", "application/json")
	for _, c := range sessionCookies {
		selectReq.AddCookie(c)
	}
	selectRec := httptest.NewRecorder()
	router.ServeHTTP(selectRec, selectReq)

	assert.Equal(t, http.StatusNoContent, selectRec.Code)
}

func TestTenantSelection_UnauthorizedTenant(t *testing.T) {
	_, mockOIDC, router := setupOAuthHandler(t, "Unauthorized Tenant "+uuid.New().String()[:8])

	testUser := &testoidc.TestUser{
		Subject:           "user-unauth-1",
		Email:             "unauth@example.com",
		EmailVerified:     true,
		PreferredUsername: "unauthuser",
	}

	state, cookies := doAuthorize(t, router)
	code := mockOIDC.CreateAuthCode(testUser)
	rec := doCallback(t, router, code, state, cookies)
	require.Equal(t, http.StatusSeeOther, rec.Code)

	sessionCookies := rec.Result().Cookies()
	require.NotEmpty(t, sessionCookies)

	fakeTenantID := uuid.New()
	selectReq := httptest.NewRequest("POST", "/tenant/select", strings.NewReader(
		`{"tenant_id":"`+fakeTenantID.String()+`"}`,
	))
	selectReq.Header.Set("Content-Type", "application/json")
	for _, c := range sessionCookies {
		selectReq.AddCookie(c)
	}
	selectRec := httptest.NewRecorder()
	router.ServeHTTP(selectRec, selectReq)

	assert.Equal(t, http.StatusForbidden, selectRec.Code)
}

func TestOAuthCallback_InvalidState(t *testing.T) {
	_, _, router := setupOAuthHandler(t, "Invalid State "+uuid.New().String()[:8])

	_, cookies := doAuthorize(t, router)

	rec := doCallback(t, router, "some-code", "wrong-state-value", cookies)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "state did not match")
}

func TestOAuthCallback_MissingStateCookie(t *testing.T) {
	_, _, router := setupOAuthHandler(t, "Missing Cookie "+uuid.New().String()[:8])

	rec := doCallback(t, router, "some-code", "some-state", nil)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "state not found")
}

func TestOAuthCallback_InvalidCode(t *testing.T) {
	_, _, router := setupOAuthHandler(t, "Invalid Code "+uuid.New().String()[:8])

	state, cookies := doAuthorize(t, router)

	rec := doCallback(t, router, "totally-invalid-code", state, cookies)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "failed to exchange token")
}

func TestOAuthCallback_StateCookieClearedOnSuccess(t *testing.T) {
	_, mockOIDC, router := setupOAuthHandler(t, "State Cleared "+uuid.New().String()[:8])

	testUser := &testoidc.TestUser{
		Subject:           "user-clear-1",
		Email:             "clear@example.com",
		EmailVerified:     true,
		PreferredUsername: "clearuser",
	}

	state, cookies := doAuthorize(t, router)
	code := mockOIDC.CreateAuthCode(testUser)

	rec := doCallback(t, router, code, state, cookies)
	require.Equal(t, http.StatusSeeOther, rec.Code)

	var stateCookieCleared bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == DefaultStateCookieName {
			stateCookieCleared = true
			assert.LessOrEqual(t, c.MaxAge, 0, "state cookie should be cleared")
		}
	}
	assert.True(t, stateCookieCleared, "state cookie should be present but cleared")
}
