package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/kusold/gotchi/auth"
	"github.com/kusold/gotchi/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuilderDefaults(t *testing.T) {
	t.Run("sets default port when not provided", func(t *testing.T) {
		app, err := New(WithDatabase("postgres://example"))
		require.NoError(t, err)
		assert.Equal(t, "3000", app.config.port)
	})

	t.Run("preserves provided port", func(t *testing.T) {
		app, err := New(
			WithDatabase("postgres://example"),
			WithPort("8080"),
		)
		require.NoError(t, err)
		assert.Equal(t, "8080", app.config.port)
	})

	t.Run("sets default clock and logger", func(t *testing.T) {
		app, err := New(testOpts()...)
		require.NoError(t, err)
		assert.NotNil(t, app.config.clock)
		assert.NotNil(t, app.config.logger)
	})
}

func TestBuilderValidation(t *testing.T) {
	t.Run("returns error when database is missing", func(t *testing.T) {
		_, err := New(WithPort("3000"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "database is required")
	})

	t.Run("returns error when database URL is empty", func(t *testing.T) {
		_, err := New(WithDatabaseConfig(db.Config{}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "database URL is required")
	})

	t.Run("returns error when OIDC enabled with missing fields", func(t *testing.T) {
		tests := []struct {
			name string
			oidc auth.Config
		}{
			{
				"missing issuer URL",
				auth.Config{ClientID: "client", ClientSecret: "secret", RedirectURL: "http://localhost/callback"},
			},
			{
				"missing client ID",
				auth.Config{IssuerURL: "http://issuer", ClientSecret: "secret", RedirectURL: "http://localhost/callback"},
			},
			{
				"missing client secret",
				auth.Config{IssuerURL: "http://issuer", ClientID: "client", RedirectURL: "http://localhost/callback"},
			},
			{
				"missing redirect URL",
				auth.Config{IssuerURL: "http://issuer", ClientID: "client", ClientSecret: "secret"},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := New(
					WithDatabase("postgres://example"),
					WithAuth(tt.oidc),
				)
				require.Error(t, err)
				assert.Contains(t, err.Error(), "OIDC issuer/client credentials/redirect URL are required")
			})
		}
	})

	t.Run("succeeds without auth", func(t *testing.T) {
		_, err := New(testOpts()...)
		assert.NoError(t, err)
	})
}

func TestWithDatabaseConfig(t *testing.T) {
	t.Run("preserves all database config fields", func(t *testing.T) {
		cfg := db.Config{
			DatabaseURL:       "postgres://example",
			EnableSlogTracing: true,
		}
		app, err := New(testOptsWithDBConfig(cfg)...)
		require.NoError(t, err)
		assert.Equal(t, "postgres://example", app.config.dbConfig.DatabaseURL)
		assert.True(t, app.config.dbConfig.EnableSlogTracing)
	})
}

func TestWithMigrations(t *testing.T) {
	t.Run("adds custom migration sources", func(t *testing.T) {
		source := db.MigrationSource{FS: fstest.MapFS{}, Dir: "migrations"}
		opts := append(testOpts(), WithMigrations(source))
		app, err := New(opts...)
		require.NoError(t, err)
		assert.Len(t, app.config.migrationSources, 1)
		assert.Equal(t, source, app.config.migrationSources[0])
	})

	t.Run("enables core migrations", func(t *testing.T) {
		opts := append(testOpts(), WithCoreMigrations())
		app, err := New(opts...)
		require.NoError(t, err)
		assert.True(t, app.config.enableCoreMigrations)
	})

	t.Run("enables auth migrations", func(t *testing.T) {
		opts := append(testOpts(), WithAuthMigrations())
		app, err := New(opts...)
		require.NoError(t, err)
		assert.True(t, app.config.enableAuthMigrations)
	})
}

func TestWithCORS(t *testing.T) {
	t.Run("stores allowed origins with defaults", func(t *testing.T) {
		opts := append(testOpts(), WithCORS("https://example.com", "https://other.com"))
		app, err := New(opts...)
		require.NoError(t, err)
		require.NotNil(t, app.config.corsConfig)
		assert.Equal(t, []string{"https://example.com", "https://other.com"}, app.config.corsConfig.AllowedOrigins)
		assert.True(t, app.config.corsConfig.AllowCredentials)
		assert.Equal(t, 300, app.config.corsConfig.MaxAge)
	})

	t.Run("no CORS when not configured", func(t *testing.T) {
		app, err := New(testOpts()...)
		require.NoError(t, err)
		assert.Nil(t, app.config.corsConfig)
	})

	t.Run("rejects wildcard origin because credentials default to true", func(t *testing.T) {
		opts := append(testOpts(), WithCORS("*"))
		_, err := New(opts...)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "wildcard origin (*) is incompatible with AllowCredentials: true")
	})
}

func TestWithCORSConfig(t *testing.T) {
	t.Run("allows full CORS control", func(t *testing.T) {
		cfg := CORSConfig{
			Options: cors.Options{
				AllowedOrigins:   []string{"https://custom.com"},
				AllowedMethods:   []string{"GET"},
				AllowedHeaders:   []string{"X-Custom"},
				AllowCredentials: false,
				MaxAge:           600,
			},
		}
		opts := append(testOpts(), WithCORSConfig(cfg))
		app, err := New(opts...)
		require.NoError(t, err)
		require.NotNil(t, app.config.corsConfig)
		assert.Equal(t, []string{"GET"}, app.config.corsConfig.AllowedMethods)
		assert.Equal(t, []string{"X-Custom"}, app.config.corsConfig.AllowedHeaders)
		assert.False(t, app.config.corsConfig.AllowCredentials)
		assert.Equal(t, 600, app.config.corsConfig.MaxAge)
	})

	t.Run("rejects wildcard origin with credentials", func(t *testing.T) {
		cfg := CORSConfig{
			Options: cors.Options{
				AllowedOrigins:   []string{"*"},
				AllowCredentials: true,
			},
		}
		opts := append(testOpts(), WithCORSConfig(cfg))
		_, err := New(opts...)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "wildcard origin (*) is incompatible with AllowCredentials: true")
	})

	t.Run("allows wildcard origin without credentials", func(t *testing.T) {
		cfg := CORSConfig{
			Options: cors.Options{
				AllowedOrigins:   []string{"*"},
				AllowCredentials: false,
			},
		}
		opts := append(testOpts(), WithCORSConfig(cfg))
		app, err := New(opts...)
		require.NoError(t, err)
		require.NotNil(t, app.config.corsConfig)
	})

	t.Run("rejects empty origins", func(t *testing.T) {
		cfg := CORSConfig{
			Options: cors.Options{
				AllowedOrigins: []string{},
			},
		}
		opts := append(testOpts(), WithCORSConfig(cfg))
		_, err := New(opts...)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least one allowed origin")
	})
}

func TestWithAuth(t *testing.T) {
	t.Run("auto-enables sessions when auth is configured", func(t *testing.T) {
		oidcCfg := auth.Config{
			IssuerURL:    "http://issuer",
			ClientID:     "client",
			ClientSecret: "secret",
			RedirectURL:  "http://localhost/callback",
		}
		opts := append(testOpts(), WithAuth(oidcCfg))
		app, err := New(opts...)
		require.NoError(t, err)
		assert.NotNil(t, app.config.authConfig)
		assert.NotNil(t, app.config.sessionConfig, "sessions should be auto-enabled")
	})

	t.Run("does not mutate the Enabled field on the stored config", func(t *testing.T) {
		oidcCfg := auth.Config{
			IssuerURL:    "http://issuer",
			ClientID:     "client",
			ClientSecret: "secret",
			RedirectURL:  "http://localhost/callback",
			Enabled:      false,
		}
		opts := append(testOpts(), WithAuth(oidcCfg))
		app, err := New(opts...)
		require.NoError(t, err)
		// WithAuth does not set Enabled; Run() sets it at startup time.
		assert.False(t, app.config.authConfig.Enabled)
	})
}

func TestWithOTEL(t *testing.T) {
	t.Run("does not mutate the Enabled field on the stored config", func(t *testing.T) {
		opts := append(testOpts(), WithOTEL(observabilityConfig()))
		app, err := New(opts...)
		require.NoError(t, err)
		// WithOTEL does not set Enabled; Run() sets it at startup time.
		assert.False(t, app.config.otelConfig.Enabled)
	})
}

func TestWithNoDefaultMiddleware(t *testing.T) {
	opts := append(testOpts(), WithNoDefaultMiddleware())
	app, err := New(opts...)
	require.NoError(t, err)
	assert.True(t, app.config.disableDefaultMiddleware)
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
