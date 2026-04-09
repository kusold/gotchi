package auth

import (
	"context"
	"fmt"
	"net/url"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// UserInfoClaims maps the standard OIDC UserInfo claims to Go fields. Not all
// fields will be populated by every identity provider.
type UserInfoClaims struct {
	Sub               string   `json:"sub"`                // Subject identifier.
	Acr               string   `json:"acr"`                // Authentication context class reference.
	Amr               []string `json:"amr"`                // Authentication methods references.
	Aud               string   `json:"aud"`                // Audience(s) of the token.
	Iss               string   `json:"iss"`                // Issuer of the token.
	Sid               string   `json:"sid"`                // Session ID.
	AuthTime          float64  `json:"auth_time"`          // Time of authentication (epoch seconds).
	Email             string   `json:"email"`              // User email address.
	EmailVerified     bool     `json:"email_verified"`     // Whether email has been verified.
	Exp               float64  `json:"exp"`                // Token expiration time (epoch seconds).
	GivenName         string   `json:"given_name"`         // Given name (first name).
	Groups            []string `json:"groups"`             // Group memberships.
	Iat               float64  `json:"iat"`                // Issued at time (epoch seconds).
	Name              string   `json:"name"`               // Full display name.
	Nickname          string   `json:"nickname"`           // Nickname.
	PreferredUsername string   `json:"preferred_username"` // Preferred username.
}

// OIDCProvider is the interface for OIDC provider operations. The standard
// implementation uses [go-oidc] to discover endpoints from the issuer URL.
// Implement this interface for testing with a mock provider.
type OIDCProvider interface {
	// Endpoint returns the OAuth2 authorization and token endpoints.
	Endpoint() oauth2.Endpoint
	// Verifier returns an IDTokenVerifier configured with the given OIDC config.
	Verifier(*oidc.Config) *oidc.IDTokenVerifier
	// UserInfo retrieves user claims from the provider using the given token source.
	UserInfo(context.Context, oauth2.TokenSource) (*oidc.UserInfo, error)
}

// OIDCAuthenticator handles low-level OIDC operations: OAuth2 token exchange,
// ID token verification, and UserInfo retrieval. It is used internally by
// [OIDCHandler] but can also be used directly for custom authentication flows.
type OIDCAuthenticator struct {
	oauth2Config oauth2.Config
	verifier     *oidc.IDTokenVerifier
	provider     OIDCProvider
	issuerURL    string
}

// NewOIDCAuthenticator creates a new OIDCAuthenticator by discovering the
// provider's endpoints from the configured IssuerURL. Returns an error if
// required config fields are missing or the provider cannot be reached.
func NewOIDCAuthenticator(cfg Config) (*OIDCAuthenticator, error) {
	parsedCfg := cfg.withDefaults()
	if parsedCfg.IssuerURL == "" || parsedCfg.ClientID == "" || parsedCfg.ClientSecret == "" || parsedCfg.RedirectURL == "" {
		return nil, fmt.Errorf("issuer URL, client ID, client secret, and redirect URL are required")
	}

	issuer, err := url.Parse(parsedCfg.IssuerURL)
	if err != nil {
		return nil, err
	}

	provider, err := oidc.NewProvider(context.Background(), issuer.String())
	if err != nil {
		return nil, err
	}

	return NewOIDCAuthenticatorWithProvider(parsedCfg, provider)
}

// NewOIDCAuthenticatorWithProvider creates a new OIDCAuthenticator with a
// pre-configured [OIDCProvider]. This is useful for testing with mock providers.
func NewOIDCAuthenticatorWithProvider(cfg Config, provider OIDCProvider) (*OIDCAuthenticator, error) {
	if provider == nil {
		return nil, fmt.Errorf("oidc provider is required")
	}

	oauth2Config := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	return &OIDCAuthenticator{
		oauth2Config: oauth2Config,
		verifier:     provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		provider:     provider,
		issuerURL:    cfg.IssuerURL,
	}, nil
}

// GetIssuer returns the OIDC issuer URL.
func (o *OIDCAuthenticator) GetIssuer() string {
	return o.issuerURL
}

// Exchange exchanges an OIDC authorization code for OAuth2 tokens.
func (o *OIDCAuthenticator) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return o.oauth2Config.Exchange(ctx, code)
}

// GetUserInfo retrieves user info from the OIDC provider using the given
// access token.
func (o *OIDCAuthenticator) GetUserInfo(ctx context.Context, token *oauth2.Token) (*oidc.UserInfo, error) {
	return o.provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
}

// VerifyIDToken extracts and verifies the ID token from the OAuth2 token
// response. It validates the token signature and claims against the configured
// client ID.
func (o *OIDCAuthenticator) VerifyIDToken(ctx context.Context, token *oauth2.Token) (*oidc.IDToken, error) {
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("id_token missing from oauth2 token")
	}
	return o.verifier.Verify(ctx, rawIDToken)
}

// AuthCodeURL returns the OIDC authorization URL with the given state parameter.
func (o *OIDCAuthenticator) AuthCodeURL(state string) string {
	return o.oauth2Config.AuthCodeURL(state)
}
