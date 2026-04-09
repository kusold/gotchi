package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/kusold/gotchi/session"
)

// OIDCHandler manages the OpenID Connect authentication flow including
// authorization redirects, token exchange, user provisioning, and tenant
// selection. Create one with [NewOIDCHandler] and register its routes with
// [OIDCHandler.RegisterRoutes].
type OIDCHandler struct {
	cfg     Config
	oidc    *OIDCAuthenticator
	session *session.Manager
	store   IdentityStore
}

// NewOIDCHandler creates a new OIDCHandler with the given configuration,
// session manager, and identity store. It initializes the OIDC authenticator
// by discovering the provider's endpoints from the IssuerURL.
//
// Returns an error if the identity store is nil or if the OIDC provider
// cannot be reached.
func NewOIDCHandler(cfg Config, sessionManager *session.Manager, store IdentityStore) (*OIDCHandler, error) {
	conf := cfg.withDefaults()
	if store == nil {
		return nil, fmt.Errorf("identity store is required")
	}
	authenticator, err := NewOIDCAuthenticator(conf)
	if err != nil {
		return nil, err
	}
	return NewOIDCHandlerWithAuthenticator(conf, authenticator, sessionManager, store), nil
}

// NewOIDCHandlerWithAuthenticator creates a new OIDCHandler with a
// pre-configured [OIDCAuthenticator]. This is useful for testing or when
// the authenticator needs custom configuration.
func NewOIDCHandlerWithAuthenticator(cfg Config, authenticator *OIDCAuthenticator, sessionManager *session.Manager, store IdentityStore) *OIDCHandler {
	return &OIDCHandler{
		cfg:     cfg.withDefaults(),
		oidc:    authenticator,
		session: sessionManager,
		store:   store,
	}
}

// RegisterRoutes registers the OIDC authentication routes on the given Chi
// router:
//   - GET [Config.AuthorizePath] — initiates the OIDC authorize redirect
//   - GET [Config.CallbackPath] — handles the OIDC callback and token exchange
//   - GET [Config.TenantsPath] — lists the user's tenant memberships (JSON)
//   - POST [Config.TenantSelectPath] — selects an active tenant (JSON body)
func (h *OIDCHandler) RegisterRoutes(r chi.Router) {
	r.Get(h.cfg.AuthorizePath, h.AuthorizeHandler)
	r.Get(h.cfg.CallbackPath, h.CallbackHandler)
	r.Get(h.cfg.TenantsPath, h.ListTenantsHandler)
	r.Post(h.cfg.TenantSelectPath, h.SelectTenantHandler)
}

// AuthorizeHandler initiates the OIDC authorization code flow by generating
// a state parameter, storing it in a cookie, and redirecting to the identity
// provider's authorization endpoint.
func (h *OIDCHandler) AuthorizeHandler(w http.ResponseWriter, r *http.Request) {
	state, err := generateState()
	if err != nil {
		http.Error(w, "failed to generate state", http.StatusInternalServerError)
		return
	}

	setStateCookie(w, h.cfg.StateCookieName, state, *h.cfg.CookieSecure)
	http.Redirect(w, r, h.oidc.AuthCodeURL(state), http.StatusFound)
}

// CallbackHandler handles the OIDC callback after user authentication. It
// verifies the state parameter, exchanges the authorization code for tokens,
// resolves or provisions the local user, and stores session claims. If the
// user has multiple memberships, they are redirected to the tenant picker.
func (h *OIDCHandler) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie(h.cfg.StateCookieName)
	if err != nil {
		http.Error(w, "state not found", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("state") != stateCookie.Value {
		http.Error(w, "state did not match", http.StatusBadRequest)
		return
	}
	clearStateCookie(w, h.cfg.StateCookieName, *h.cfg.CookieSecure)

	code := r.URL.Query().Get("code")
	token, err := h.oidc.Exchange(r.Context(), code)
	if err != nil {
		http.Error(w, "failed to exchange token", http.StatusInternalServerError)
		return
	}

	idToken, err := h.oidc.VerifyIDToken(r.Context(), token)
	if err != nil {
		http.Error(w, "failed to verify id token", http.StatusUnauthorized)
		return
	}

	var idTokenClaims struct {
		Sub string `json:"sub"`
	}
	if err := idToken.Claims(&idTokenClaims); err != nil {
		http.Error(w, "failed to parse id token claims", http.StatusUnauthorized)
		return
	}

	userInfo, err := h.oidc.GetUserInfo(r.Context(), token)
	if err != nil {
		http.Error(w, "failed to get user info", http.StatusInternalServerError)
		return
	}

	var claims UserInfoClaims
	if err := userInfo.Claims(&claims); err != nil {
		http.Error(w, "failed to parse user claims", http.StatusInternalServerError)
		return
	}

	subject, err := resolveSubjectFromClaims(idTokenClaims.Sub, claims.Sub)
	if err != nil {
		http.Error(w, "invalid subject claims", http.StatusUnauthorized)
		return
	}

	identity := Identity{
		Issuer:            h.oidc.GetIssuer(),
		Subject:           subject,
		Email:             claims.Email,
		EmailVerified:     claims.EmailVerified,
		Username:          claims.Nickname,
		Name:              claims.Name,
		PreferredUsername: claims.PreferredUsername,
	}

	userRef, err := h.store.ResolveOrProvisionUser(r.Context(), identity)
	if err != nil {
		http.Error(w, "failed to resolve user", http.StatusInternalServerError)
		return
	}

	memberships, err := h.store.ListMemberships(r.Context(), userRef.UserID)
	if err != nil {
		http.Error(w, "failed to list memberships", http.StatusInternalServerError)
		return
	}
	if len(memberships) == 0 {
		http.Error(w, "user has no tenant memberships", http.StatusForbidden)
		return
	}

	claimsSession := SessionClaims{
		Authenticated: true,
		UserID:        userRef.UserID,
		Issuer:        userRef.Issuer,
		Subject:       userRef.Subject,
	}

	if len(memberships) == 1 {
		claimsSession.ActiveTenantID = &memberships[0].TenantID
	}

	h.session.Put(r.Context(), h.cfg.SessionKey, claimsSession)

	if claimsSession.ActiveTenantID == nil {
		h.handleTenantSelectionRequired(w, r, memberships)
		return
	}

	http.Redirect(w, r, h.cfg.PostLoginRedirect, http.StatusSeeOther)
}

// ListTenantsHandler returns the authenticated user's tenant memberships as
// a JSON array. Each entry includes the tenant ID, name, role, and whether
// it is the currently active tenant.
func (h *OIDCHandler) ListTenantsHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.getSessionClaims(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	memberships, err := h.store.ListMemberships(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, "failed to list memberships", http.StatusInternalServerError)
		return
	}

	type tenantResp struct {
		TenantID string `json:"tenant_id"`
		Name     string `json:"name"`
		Role     Role   `json:"role"`
		IsActive bool   `json:"is_active"`
	}

	response := make([]tenantResp, 0, len(memberships))
	for _, membership := range memberships {
		name := membership.TenantName
		if name == "" {
			display, err := h.store.GetTenantDisplay(r.Context(), membership.TenantID)
			if err == nil {
				name = display.Name
			}
		}
		isActive := claims.ActiveTenantID != nil && *claims.ActiveTenantID == membership.TenantID
		response = append(response, tenantResp{
			TenantID: membership.TenantID.String(),
			Name:     name,
			Role:     membership.Role,
			IsActive: isActive,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// SelectTenantHandler accepts a JSON body with a "tenant_id" field and sets
// it as the active tenant in the session. The user must be a member of the
// specified tenant.
func (h *OIDCHandler) SelectTenantHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.getSessionClaims(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var payload struct {
		TenantID string `json:"tenant_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	tenantID, err := uuid.Parse(payload.TenantID)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}

	memberships, err := h.store.ListMemberships(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, "failed to list memberships", http.StatusInternalServerError)
		return
	}

	allowed := false
	for _, membership := range memberships {
		if membership.TenantID == tenantID {
			allowed = true
			break
		}
	}
	if !allowed {
		http.Error(w, "tenant not allowed", http.StatusForbidden)
		return
	}

	claims.ActiveTenantID = &tenantID
	h.session.Put(r.Context(), h.cfg.SessionKey, claims)
	w.WriteHeader(http.StatusNoContent)
}

func (h *OIDCHandler) handleTenantSelectionRequired(w http.ResponseWriter, r *http.Request, memberships []Membership) {
	if acceptsHTML(r) {
		http.Redirect(w, r, h.cfg.TenantPickerPath, http.StatusSeeOther)
		return
	}

	payload := map[string]any{
		"error":                     "tenant_selection_required",
		"tenant_selection_required": true,
		"tenant_count":              len(memberships),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	json.NewEncoder(w).Encode(payload)
}

func (h *OIDCHandler) getSessionClaims(r *http.Request) (SessionClaims, bool) {
	claims, ok := h.session.Get(r.Context(), h.cfg.SessionKey).(SessionClaims)
	if !ok || !claims.Authenticated {
		return SessionClaims{}, false
	}
	return claims, true
}

func resolveSubjectFromClaims(idTokenSubject, userInfoSubject string) (string, error) {
	idTokenSub := strings.TrimSpace(idTokenSubject)
	if idTokenSub == "" {
		return "", fmt.Errorf("id token subject is missing")
	}

	userInfoSub := strings.TrimSpace(userInfoSubject)
	if userInfoSub == "" {
		return idTokenSub, nil
	}

	if userInfoSub != idTokenSub {
		return "", fmt.Errorf("id token and userinfo subjects do not match")
	}
	return idTokenSub, nil
}

func acceptsHTML(r *http.Request) bool {
	if r.Header.Get("HX-Request") == "true" {
		return true
	}
	accept := r.Header.Get("Accept")
	if accept == "" {
		return true
	}
	parts := strings.Split(accept, ",")
	for _, part := range parts {
		mediaType := strings.TrimSpace(strings.Split(part, ";")[0])
		if mediaType == "text/html" || mediaType == "*/*" {
			return true
		}
	}
	return false
}

func generateState() (string, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func setStateCookie(w http.ResponseWriter, name, value string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   DefaultCookieMaxAgeSecond,
		Secure:   secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearStateCookie(w http.ResponseWriter, name string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Secure:   secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
	})
}
