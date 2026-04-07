package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"
	"github.com/kusold/gotchi/auth"
	"github.com/kusold/gotchi/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigWithDefaults(t *testing.T) {
	t.Run("sets default port when empty", func(t *testing.T) {
		cfg := Config{
			Database: db.Config{DatabaseURL: "postgres://example"},
		}
		withDefaults := cfg.withDefaults()
		assert.Equal(t, "3000", withDefaults.Server.Port)
	})

	t.Run("preserves provided port", func(t *testing.T) {
		cfg := Config{
			Server:   ServerConfig{Port: "8080"},
			Database: db.Config{DatabaseURL: "postgres://example"},
		}
		withDefaults := cfg.withDefaults()
		assert.Equal(t, "8080", withDefaults.Server.Port)
	})

	t.Run("sets default OIDC config when enabled", func(t *testing.T) {
		cfg := Config{
			Database: db.Config{DatabaseURL: "postgres://example"},
			Auth:     AuthConfig{OIDC: auth.Config{Enabled: true}},
		}
		withDefaults := cfg.withDefaults()
		// WithDefaults should be called on the OIDC config
		assert.True(t, withDefaults.Auth.OIDC.Enabled)
	})

	t.Run("initializes empty migration sources when nil", func(t *testing.T) {
		cfg := Config{
			Database: db.Config{DatabaseURL: "postgres://example"},
		}
		withDefaults := cfg.withDefaults()
		assert.NotNil(t, withDefaults.Migrations.Sources)
		assert.Empty(t, withDefaults.Migrations.Sources)
	})

	t.Run("preserves provided migration sources", func(t *testing.T) {
		sources := []db.MigrationSource{{FS: fstest.MapFS{}, Dir: "migrations"}}
		cfg := Config{
			Database:   db.Config{DatabaseURL: "postgres://example"},
			Migrations: MigrationConfig{Sources: sources},
		}
		withDefaults := cfg.withDefaults()
		assert.Equal(t, sources, withDefaults.Migrations.Sources)
	})

	t.Run("preserves all provided values", func(t *testing.T) {
		cfg := Config{
			Server:     ServerConfig{Port: "9000"},
			Database:   db.Config{DatabaseURL: "postgres://example", EnableTracing: true},
			Migrations: MigrationConfig{EnableCore: true, EnableAuth: true},
		}
		withDefaults := cfg.withDefaults()
		assert.Equal(t, "9000", withDefaults.Server.Port)
		assert.Equal(t, "postgres://example", withDefaults.Database.DatabaseURL)
		assert.True(t, withDefaults.Database.EnableTracing)
		assert.True(t, withDefaults.Migrations.EnableCore)
		assert.True(t, withDefaults.Migrations.EnableAuth)
	})
}

func TestConfigValidate(t *testing.T) {
	t.Run("returns error when database URL is missing", func(t *testing.T) {
		cfg := Config{Server: ServerConfig{Port: "3000"}}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "database URL is required")
	})

	t.Run("returns error when port is missing", func(t *testing.T) {
		cfg := Config{Database: db.Config{DatabaseURL: "postgres://example"}}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "server port is required")
	})

	t.Run("returns error when OIDC enabled but issuer URL missing", func(t *testing.T) {
		cfg := Config{
			Server:   ServerConfig{Port: "3000"},
			Database: db.Config{DatabaseURL: "postgres://example"},
			Auth: AuthConfig{
				OIDC: auth.Config{
					Enabled:     true,
					ClientID:    "client",
					ClientSecret: "secret",
					RedirectURL: "http://localhost/callback",
				},
			},
		}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "OIDC issuer/client credentials/redirect URL are required")
	})

	t.Run("returns error when OIDC enabled but client ID missing", func(t *testing.T) {
		cfg := Config{
			Server:   ServerConfig{Port: "3000"},
			Database: db.Config{DatabaseURL: "postgres://example"},
			Auth: AuthConfig{
				OIDC: auth.Config{
					Enabled:      true,
					IssuerURL:    "http://issuer",
					ClientSecret: "secret",
					RedirectURL:  "http://localhost/callback",
				},
			},
		}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "OIDC issuer/client credentials/redirect URL are required")
	})

	t.Run("returns error when OIDC enabled but client secret missing", func(t *testing.T) {
		cfg := Config{
			Server:   ServerConfig{Port: "3000"},
			Database: db.Config{DatabaseURL: "postgres://example"},
			Auth: AuthConfig{
				OIDC: auth.Config{
					Enabled:     true,
					IssuerURL:   "http://issuer",
					ClientID:    "client",
					RedirectURL: "http://localhost/callback",
				},
			},
		}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "OIDC issuer/client credentials/redirect URL are required")
	})

	t.Run("returns error when OIDC enabled but redirect URL missing", func(t *testing.T) {
		cfg := Config{
			Server:   ServerConfig{Port: "3000"},
			Database: db.Config{DatabaseURL: "postgres://example"},
			Auth: AuthConfig{
				OIDC: auth.Config{
					Enabled:      true,
					IssuerURL:    "http://issuer",
					ClientID:     "client",
					ClientSecret: "secret",
				},
			},
		}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "OIDC issuer/client credentials/redirect URL are required")
	})

	t.Run("succeeds with valid config without OIDC", func(t *testing.T) {
		cfg := Config{
			Server:   ServerConfig{Port: "3000"},
			Database: db.Config{DatabaseURL: "postgres://example"},
		}
		err := cfg.validate()
		assert.NoError(t, err)
	})
}

func TestDefaultLoginHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	w := httptest.NewRecorder()

	defaultLoginHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/plain; charset=utf-8", w.Header().Get("Content-Type"))
	assert.Equal(t, "login endpoint", w.Body.String())
}

func TestModuleFunc(t *testing.T) {
	t.Run("Register calls the underlying function", func(t *testing.T) {
		called := false
		moduleFunc := ModuleFunc(func(r chi.Router, deps Dependencies) error {
			called = true
			return nil
		})

		err := moduleFunc.Register(nil, Dependencies{})

		assert.NoError(t, err)
		assert.True(t, called, "ModuleFunc should call the underlying function")
	})

	t.Run("Register returns error from underlying function", func(t *testing.T) {
		expectedErr := assert.AnError
		moduleFunc := ModuleFunc(func(r chi.Router, deps Dependencies) error {
			return expectedErr
		})

		err := moduleFunc.Register(nil, Dependencies{})

		assert.Equal(t, expectedErr, err)
	})
}
