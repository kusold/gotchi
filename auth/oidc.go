package auth

import (
	"context"
	"fmt"
	"net/url"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type UserInfoClaims struct {
	Sub               string   `json:"sub"`
	Acr               string   `json:"acr"`
	Amr               []string `json:"amr"`
	Aud               string   `json:"aud"`
	Iss               string   `json:"iss"`
	Sid               string   `json:"sid"`
	AuthTime          float64  `json:"auth_time"`
	Email             string   `json:"email"`
	EmailVerified     bool     `json:"email_verified"`
	Exp               float64  `json:"exp"`
	GivenName         string   `json:"given_name"`
	Groups            []string `json:"groups"`
	Iat               float64  `json:"iat"`
	Name              string   `json:"name"`
	Nickname          string   `json:"nickname"`
	PreferredUsername string   `json:"preferred_username"`
}

type OIDCProvider interface {
	Endpoint() oauth2.Endpoint
	Verifier(*oidc.Config) *oidc.IDTokenVerifier
	UserInfo(context.Context, oauth2.TokenSource) (*oidc.UserInfo, error)
}

type OIDCAuthenticator struct {
	oauth2Config oauth2.Config
	verifier     *oidc.IDTokenVerifier
	provider     OIDCProvider
	issuerURL    string
}

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

func (o *OIDCAuthenticator) GetIssuer() string {
	return o.issuerURL
}

func (o *OIDCAuthenticator) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return o.oauth2Config.Exchange(ctx, code)
}

func (o *OIDCAuthenticator) GetUserInfo(ctx context.Context, token *oauth2.Token) (*oidc.UserInfo, error) {
	return o.provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
}

func (o *OIDCAuthenticator) VerifyIDToken(ctx context.Context, token *oauth2.Token) (*oidc.IDToken, error) {
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("id_token missing from oauth2 token")
	}
	return o.verifier.Verify(ctx, rawIDToken)
}

func (o *OIDCAuthenticator) AuthCodeURL(state string) string {
	return o.oauth2Config.AuthCodeURL(state)
}
