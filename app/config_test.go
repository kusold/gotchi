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
		cfg := testConfig()
		cfg.Server.Port = "8080"
		withDefaults := cfg.withDefaults()
		assert.Equal(t, "8080", withDefaults.Server.Port)
	})

	t.Run("sets default OIDC config when enabled", func(t *testing.T) {
		cfg := testConfig()
		cfg.Auth = AuthConfig{OIDC: auth.Config{Enabled: true}}
		withDefaults := cfg.withDefaults()
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
		cfg := testConfig()
		cfg.Migrations = MigrationConfig{Sources: sources}
		withDefaults := cfg.withDefaults()
		assert.Equal(t, sources, withDefaults.Migrations.Sources)
	})

	t.Run("preserves all provided values", func(t *testing.T) {
		cfg := testConfig()
		cfg.Server.Port = "9000"
		cfg.Database.EnableSlogTracing = true
		cfg.Migrations = MigrationConfig{EnableCore: true, EnableAuth: true}
		cfg.CORS = CORSConfig{AllowedOrigins: []string{"https://example.com"}}
		withDefaults := cfg.withDefaults()
		assert.Equal(t, "9000", withDefaults.Server.Port)
		assert.Equal(t, "postgres://example", withDefaults.Database.DatabaseURL)
		assert.True(t, withDefaults.Database.EnableSlogTracing)
		assert.True(t, withDefaults.Migrations.EnableCore)
		assert.True(t, withDefaults.Migrations.EnableAuth)
		assert.Equal(t, []string{"https://example.com"}, withDefaults.CORS.AllowedOrigins)
	})

	t.Run("preserves empty CORS config when not provided", func(t *testing.T) {
		cfg := testConfig()
		withDefaults := cfg.withDefaults()
		assert.Nil(t, withDefaults.CORS.AllowedOrigins)
	})
}

func TestCORSConfigWithDefaults(t *testing.T) {
	t.Run("fills all defaults when only origins provided", func(t *testing.T) {
		cfg := CORSConfig{AllowedOrigins: []string{"https://example.com"}}
		withDefaults := cfg.WithDefaults()
		assert.Equal(t, []string{"https://example.com"}, withDefaults.AllowedOrigins)
		assert.Equal(t, DefaultCORSAllowedMethods, withDefaults.AllowedMethods)
		assert.Equal(t, DefaultCORSAllowedHeaders, withDefaults.AllowedHeaders)
		assert.Equal(t, DefaultCORSExposedHeaders, withDefaults.ExposedHeaders)
		assert.True(t, *withDefaults.AllowCredentials)
		assert.Equal(t, DefaultCORSMaxAge, withDefaults.MaxAge)
	})

	t.Run("preserves custom AllowedMethods", func(t *testing.T) {
		cfg := CORSConfig{
			AllowedOrigins: []string{"https://example.com"},
			AllowedMethods: []string{"GET", "POST"},
		}
		withDefaults := cfg.WithDefaults()
		assert.Equal(t, []string{"GET", "POST"}, withDefaults.AllowedMethods)
	})

	t.Run("preserves custom AllowedHeaders", func(t *testing.T) {
		cfg := CORSConfig{
			AllowedOrigins: []string{"https://example.com"},
			AllowedHeaders: []string{"Content-Type"},
		}
		withDefaults := cfg.WithDefaults()
		assert.Equal(t, []string{"Content-Type"}, withDefaults.AllowedHeaders)
	})

	t.Run("preserves custom ExposedHeaders", func(t *testing.T) {
		cfg := CORSConfig{
			AllowedOrigins: []string{"https://example.com"},
			ExposedHeaders: []string{"X-Custom"},
		}
		withDefaults := cfg.WithDefaults()
		assert.Equal(t, []string{"X-Custom"}, withDefaults.ExposedHeaders)
	})

	t.Run("preserves AllowCredentials false", func(t *testing.T) {
		cfg := CORSConfig{
			AllowedOrigins:   []string{"https://example.com"},
			AllowCredentials: boolPtr(false),
		}
		withDefaults := cfg.WithDefaults()
		assert.False(t, *withDefaults.AllowCredentials)
	})

	t.Run("preserves custom MaxAge", func(t *testing.T) {
		cfg := CORSConfig{
			AllowedOrigins: []string{"https://example.com"},
			MaxAge:         600,
		}
		withDefaults := cfg.WithDefaults()
		assert.Equal(t, 600, withDefaults.MaxAge)
	})

	t.Run("Enabled returns true when origins set", func(t *testing.T) {
		cfg := CORSConfig{AllowedOrigins: []string{"https://example.com"}}
		assert.True(t, cfg.Enabled())
	})

	t.Run("Enabled returns false when origins empty", func(t *testing.T) {
		cfg := CORSConfig{}
		assert.False(t, cfg.Enabled())
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

	t.Run("returns error when OIDC enabled with missing fields", func(t *testing.T) {
		tests := []struct {
			name string
			oidc auth.Config
		}{
			{
				"missing issuer URL",
				auth.Config{Enabled: true, ClientID: "client", ClientSecret: "secret", RedirectURL: "http://localhost/callback"},
			},
			{
				"missing client ID",
				auth.Config{Enabled: true, IssuerURL: "http://issuer", ClientSecret: "secret", RedirectURL: "http://localhost/callback"},
			},
			{
				"missing client secret",
				auth.Config{Enabled: true, IssuerURL: "http://issuer", ClientID: "client", RedirectURL: "http://localhost/callback"},
			},
			{
				"missing redirect URL",
				auth.Config{Enabled: true, IssuerURL: "http://issuer", ClientID: "client", ClientSecret: "secret"},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				cfg := testConfig()
				cfg.Auth = AuthConfig{OIDC: tt.oidc}
				err := cfg.validate()
				require.Error(t, err)
				assert.Contains(t, err.Error(), "OIDC issuer/client credentials/redirect URL are required")
			})
		}
	})

	t.Run("succeeds with valid config without OIDC", func(t *testing.T) {
		cfg := testConfig()
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
		assert.True(t, called)
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
