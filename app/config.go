package app

import (
	"fmt"
	"net/http"

	"github.com/kusold/gotchi/auth"
	"github.com/kusold/gotchi/db"
	"github.com/kusold/gotchi/observability"
	"github.com/kusold/gotchi/openapi"
	"github.com/kusold/gotchi/session"
)

type Config struct {
	Server     ServerConfig
	Database   db.Config
	Session    session.Config
	Auth       AuthConfig
	OpenAPI    openapi.Config
	Migrations MigrationConfig
	OTEL       observability.OTELConfig
	CORS       CORSConfig
}

type CORSConfig struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	AllowCredentials *bool
	MaxAge           int
}

const (
	DefaultCORSAllowedMethodGET     = "GET"
	DefaultCORSAllowedMethodPOST    = "POST"
	DefaultCORSAllowedMethodPUT     = "PUT"
	DefaultCORSAllowedMethodPATCH   = "PATCH"
	DefaultCORSAllowedMethodDELETE  = "DELETE"
	DefaultCORSAllowedMethodOPTIONS = "OPTIONS"

	DefaultCORSAllowedHeaderAccept        = "Accept"
	DefaultCORSAllowedHeaderAuthorization = "Authorization"
	DefaultCORSAllowedHeaderContentType   = "Content-Type"
	DefaultCORSAllowedHeaderXCSRFToken    = "X-CSRF-Token"
	DefaultCORSAllowedHeaderXRequestID    = "X-Request-ID"

	DefaultCORSExposedHeaderLink       = "Link"
	DefaultCORSExposedHeaderXRequestID = "X-Request-ID"

	DefaultCORSMaxAge = 300
)

var (
	DefaultCORSAllowedMethods = []string{
		DefaultCORSAllowedMethodGET,
		DefaultCORSAllowedMethodPOST,
		DefaultCORSAllowedMethodPUT,
		DefaultCORSAllowedMethodPATCH,
		DefaultCORSAllowedMethodDELETE,
		DefaultCORSAllowedMethodOPTIONS,
	}
	DefaultCORSAllowedHeaders = []string{
		DefaultCORSAllowedHeaderAccept,
		DefaultCORSAllowedHeaderAuthorization,
		DefaultCORSAllowedHeaderContentType,
		DefaultCORSAllowedHeaderXCSRFToken,
		DefaultCORSAllowedHeaderXRequestID,
	}
	DefaultCORSExposedHeaders = []string{
		DefaultCORSExposedHeaderLink,
		DefaultCORSExposedHeaderXRequestID,
	}
)

func boolPtr(b bool) *bool { return &b }

func (c CORSConfig) WithDefaults() CORSConfig {
	cfg := c
	if len(cfg.AllowedMethods) == 0 {
		cfg.AllowedMethods = DefaultCORSAllowedMethods
	}
	if len(cfg.AllowedHeaders) == 0 {
		cfg.AllowedHeaders = DefaultCORSAllowedHeaders
	}
	if len(cfg.ExposedHeaders) == 0 {
		cfg.ExposedHeaders = DefaultCORSExposedHeaders
	}
	if cfg.AllowCredentials == nil {
		cfg.AllowCredentials = boolPtr(true)
	}
	if cfg.MaxAge == 0 {
		cfg.MaxAge = DefaultCORSMaxAge
	}
	return cfg
}

func (c CORSConfig) Enabled() bool {
	return len(c.AllowedOrigins) > 0
}

type ServerConfig struct {
	Port string
}

type AuthConfig struct {
	OIDC          auth.Config
	IdentityStore auth.IdentityStore
	LoginHandler  http.HandlerFunc
}

type MigrationConfig struct {
	Sources    []db.MigrationSource
	EnableCore bool
	EnableAuth bool
}

func (c Config) withDefaults() Config {
	cfg := c
	if cfg.Server.Port == "" {
		cfg.Server.Port = "3000"
	}
	if cfg.Auth.OIDC.Enabled {
		cfg.Auth.OIDC = cfg.Auth.OIDC.WithDefaults()
	}
	if len(cfg.Migrations.Sources) == 0 {
		cfg.Migrations.Sources = []db.MigrationSource{}
	}
	if cfg.CORS.Enabled() {
		cfg.CORS = cfg.CORS.WithDefaults()
	}
	return cfg
}

func (c Config) validate() error {
	if c.Database.DatabaseURL == "" {
		return fmt.Errorf("database URL is required")
	}
	if c.Server.Port == "" {
		return fmt.Errorf("server port is required")
	}
	if c.Auth.OIDC.Enabled {
		if c.Auth.OIDC.IssuerURL == "" || c.Auth.OIDC.ClientID == "" || c.Auth.OIDC.ClientSecret == "" || c.Auth.OIDC.RedirectURL == "" {
			return fmt.Errorf("OIDC issuer/client credentials/redirect URL are required")
		}
	}
	return nil
}
