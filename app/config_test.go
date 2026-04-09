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

func TestBuilderDefaults(t *testing.T) {
	t.Run("sets default port when not provided", func(t *testing.T) {
		app, err := New(WithDatabase("postgres://example"))
		require.NoError(t, err)
		assert.Equal(t, "3000", app.port)
	})

	t.Run("preserves provided port", func(t *testing.T) {
		app, err := New(
			WithDatabase("postgres://example"),
			WithPort("8080"),
		)
		require.NoError(t, err)
		assert.Equal(t, "8080", app.port)
	})

	t.Run("sets default clock and logger", func(t *testing.T) {
		app, err := New(testOpts()...)
		require.NoError(t, err)
		assert.NotNil(t, app.clock)
		assert.NotNil(t, app.logger)
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
		assert.Equal(t, "postgres://example", app.dbConfig.DatabaseURL)
		assert.True(t, app.dbConfig.EnableSlogTracing)
	})
}

func TestWithMigrations(t *testing.T) {
	t.Run("adds custom migration sources", func(t *testing.T) {
		source := db.MigrationSource{FS: fstest.MapFS{}, Dir: "migrations"}
		opts := append(testOpts(), WithMigrations(source))
		app, err := New(opts...)
		require.NoError(t, err)
		assert.Len(t, app.migrationSources, 1)
		assert.Equal(t, source, app.migrationSources[0])
	})

	t.Run("enables core migrations", func(t *testing.T) {
		opts := append(testOpts(), WithCoreMigrations())
		app, err := New(opts...)
		require.NoError(t, err)
		assert.True(t, app.enableCoreMigrations)
	})

	t.Run("enables auth migrations", func(t *testing.T) {
		opts := append(testOpts(), WithAuthMigrations())
		app, err := New(opts...)
		require.NoError(t, err)
		assert.True(t, app.enableAuthMigrations)
	})
}

func TestWithCORS(t *testing.T) {
	t.Run("stores allowed origins", func(t *testing.T) {
		opts := append(testOpts(), WithCORS("https://example.com", "https://other.com"))
		app, err := New(opts...)
		require.NoError(t, err)
		assert.Equal(t, []string{"https://example.com", "https://other.com"}, app.corsOrigins)
	})

	t.Run("no CORS when not configured", func(t *testing.T) {
		app, err := New(testOpts()...)
		require.NoError(t, err)
		assert.Nil(t, app.corsOrigins)
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
		assert.NotNil(t, app.authConfig)
		assert.True(t, app.authConfig.Enabled)
		assert.NotNil(t, app.sessionConfig, "sessions should be auto-enabled")
	})
}

func TestWithNoDefaultMiddleware(t *testing.T) {
	opts := append(testOpts(), WithNoDefaultMiddleware())
	app, err := New(opts...)
	require.NoError(t, err)
	assert.True(t, app.disableDefaultMiddleware)
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
