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

	"github.com/kusold/gotchi/internal/db"
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

func uniqueSuffix() string {
	return uuid.New().String()[:8] // 8 hex chars provides ~4 billion unique values
}

func addSecondTenant(t *testing.T, store *PostgresIdentityStore, userID uuid.UUID, tenantName string) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	tenantID := uuid.New()
	err := store.queries.InsertTenant(ctx, db.InsertTenantParams{
		TenantID: tenantID,
		Name:     tenantName,
	})
	require.NoError(t, err)
	_, err = store.createMembership(ctx, userID, tenantID, RoleAdmin)
	require.NoError(t, err)
	return tenantID
}

func provisionUserWithSecondTenant(t *testing.T, issuer string) (*testoidc.TestUser, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	suffix := uniqueSuffix()

	identity := Identity{
		Issuer:            issuer,
		Subject:           "user-mt-" + suffix,
		Email:             suffix + "@example.com",
		EmailVerified:     true,
		PreferredUsername: "mt-" + suffix,
	}
	store := newTestStore(t, "Secondary "+suffix)
	userRef, err := store.ResolveOrProvisionUser(ctx, identity)
	require.NoError(t, err)
	secondTenantID := addSecondTenant(t, store, userRef.UserID, "Second Org "+suffix)

	testUser := &testoidc.TestUser{
		Subject:           identity.Subject,
		Email:             identity.Email,
		EmailVerified:     identity.EmailVerified,
		PreferredUsername: identity.PreferredUsername,
	}

	return testUser, secondTenantID
}

func doAuthorizeAndCallback(t *testing.T, router chi.Router, mockOIDC *testoidc.MockOIDCProvider, testUser *testoidc.TestUser, extraHeaders ...http.Header) *httptest.ResponseRecorder {
	t.Helper()

	state, cookies := doAuthorize(t, router)
	code := mockOIDC.CreateAuthCode(testUser)
	rec := doCallback(t, router, code, state, cookies, extraHeaders...)

	return rec
}

func doSelectTenant(t *testing.T, router chi.Router, sessionCookies []*http.Cookie, tenantID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest("POST", "/tenant/select", strings.NewReader(
		`{"tenant_id":"`+tenantID.String()+`"}`,
	))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range sessionCookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestOAuthFlow_SingleTenant(t *testing.T) {
	_, mockOIDC, router := setupOAuthHandler(t, "Single Tenant "+uniqueSuffix())

	testUser := testoidc.NewTestUser("single").WithName("Single Tenant User").Build()

	state, cookies := doAuthorize(t, router)
	code := mockOIDC.CreateAuthCode(testUser)

	rec := doCallback(t, router, code, state, cookies)

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/dashboard", rec.Header().Get("Location"))
}

func TestOAuthFlow_MultiTenant_RequiresSelection(t *testing.T) {
	_, mockOIDC, router := setupOAuthHandler(t, "Multi Tenant "+uniqueSuffix())
	testUser, _ := provisionUserWithSecondTenant(t, mockOIDC.IssuerURL())

	rec := doAuthorizeAndCallback(t, router, mockOIDC, testUser, http.Header{
		"Accept": {"application/json"},
	})

	assert.Equal(t, http.StatusConflict, rec.Code)

	var body map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, true, body["tenant_selection_required"])
	assert.Equal(t, "tenant_selection_required", body["error"])
}

func TestTenantSelection_ListTenants(t *testing.T) {
	_, mockOIDC, router := setupOAuthHandler(t, "List Tenants "+uniqueSuffix())
	testUser, _ := provisionUserWithSecondTenant(t, mockOIDC.IssuerURL())

	rec := doAuthorizeAndCallback(t, router, mockOIDC, testUser, http.Header{
		"Accept": {"application/json"},
	})
	sessionCookies := rec.Result().Cookies()

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
	_, mockOIDC, router := setupOAuthHandler(t, "Select Tenant "+uniqueSuffix())
	testUser, secondTenantID := provisionUserWithSecondTenant(t, mockOIDC.IssuerURL())

	rec := doAuthorizeAndCallback(t, router, mockOIDC, testUser, http.Header{
		"Accept": {"application/json"},
	})
	sessionCookies := rec.Result().Cookies()

	rec = doSelectTenant(t, router, sessionCookies, secondTenantID)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestTenantSelection_UnauthorizedTenant(t *testing.T) {
	suffix := uniqueSuffix()
	_, mockOIDC, router := setupOAuthHandler(t, "Unauthorized Tenant "+suffix)

	testUser := testoidc.NewTestUser("unauth").Build()

	rec := doAuthorizeAndCallback(t, router, mockOIDC, testUser)
	sessionCookies := rec.Result().Cookies()
	require.NotEmpty(t, sessionCookies)

	rec = doSelectTenant(t, router, sessionCookies, uuid.New())
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestOAuthCallback_InvalidState(t *testing.T) {
	_, _, router := setupOAuthHandler(t, "Invalid State "+uniqueSuffix())

	_, cookies := doAuthorize(t, router)

	rec := doCallback(t, router, "some-code", "wrong-state-value", cookies)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "state did not match")
}

func TestOAuthCallback_MissingStateCookie(t *testing.T) {
	_, _, router := setupOAuthHandler(t, "Missing Cookie "+uniqueSuffix())

	rec := doCallback(t, router, "some-code", "some-state", nil)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "state not found")
}

func TestOAuthCallback_InvalidCode(t *testing.T) {
	_, _, router := setupOAuthHandler(t, "Invalid Code "+uniqueSuffix())

	state, cookies := doAuthorize(t, router)

	rec := doCallback(t, router, "totally-invalid-code", state, cookies)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "failed to exchange token")
}

func TestOAuthCallback_StateCookieClearedOnSuccess(t *testing.T) {
	_, mockOIDC, router := setupOAuthHandler(t, "State Cleared "+uniqueSuffix())

	testUser := testoidc.NewTestUser("clear").Build()

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
