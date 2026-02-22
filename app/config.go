package app

import (
	"fmt"
	"net/http"

	"github.com/kusold/gotchi/auth"
	"github.com/kusold/gotchi/db"
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
