package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWithModules(t *testing.T) {
	t.Run("creates application with no modules", func(t *testing.T) {
		app, err := New(testOpts()...)
		require.NoError(t, err)
		require.NotNil(t, app)
		assert.Empty(t, app.config.modules)
	})

	t.Run("creates application with one module", func(t *testing.T) {
		opts := append(testOpts(), WithModule(noopModule()))
		app, err := New(opts...)
		require.NoError(t, err)
		require.NotNil(t, app)
		assert.Len(t, app.config.modules, 1)
	})

	t.Run("creates application with multiple modules", func(t *testing.T) {
		opts := append(testOpts(), WithModule(noopModule(), noopModule()))
		app, err := New(opts...)
		require.NoError(t, err)
		require.NotNil(t, app)
		assert.Len(t, app.config.modules, 2)
	})
}

func TestApplicationRouter(t *testing.T) {
	t.Run("returns a chi mux", func(t *testing.T) {
		app, err := New(testOpts()...)
		require.NoError(t, err)
		router := app.Router()
		assert.NotNil(t, router)
		assert.IsType(t, &chi.Mux{}, router)
	})

	t.Run("returns the same instance on subsequent calls", func(t *testing.T) {
		app, err := New(testOpts()...)
		require.NoError(t, err)
		router := app.Router()
		router2 := app.Router()
		assert.Equal(t, router, router2)
	})
}

func TestApplicationDependencies(t *testing.T) {
	app, err := New(testOpts()...)
	require.NoError(t, err)
	deps := app.Dependencies()
	assert.NotNil(t, deps)
	assert.Nil(t, deps.DB)
	assert.Nil(t, deps.Pool)
	assert.Nil(t, deps.Session)
	assert.Nil(t, deps.Auth)
}

func TestApplicationClose(t *testing.T) {
	t.Run("close works without Connect", func(t *testing.T) {
		app, err := New(testOpts()...)
		require.NoError(t, err)
		err = app.Close()
		assert.NoError(t, err)
	})

	t.Run("close can be called multiple times", func(t *testing.T) {
		app, err := New(testOpts()...)
		require.NoError(t, err)
		err = app.Close()
		assert.NoError(t, err)
		err = app.Close()
		assert.NoError(t, err)
	})
}

func TestRealClock(t *testing.T) {
	t.Run("returns non-zero time", func(t *testing.T) {
		clock := realClock{}
		now := clock.Now()
		assert.False(t, now.IsZero())
	})
}

func TestApplicationInitialState(t *testing.T) {
	opts := testOptsWithDBConfig(dbConfigWithTracing(true))
	app, err := New(opts...)
	require.NoError(t, err)
	assert.NotNil(t, app.router)
	assert.NotNil(t, app.db)
	assert.Equal(t, "3000", app.config.port)
	assert.Equal(t, "postgres://example", app.config.dbConfig.DatabaseURL)
	assert.True(t, app.config.dbConfig.EnableSlogTracing)
}

func TestNewAppliesDefaults(t *testing.T) {
	t.Run("applies default port when not provided", func(t *testing.T) {
		app, err := New(WithDatabase("postgres://example"))
		require.NoError(t, err)
		assert.Equal(t, "3000", app.config.port)
	})

	t.Run("initializes empty migration sources", func(t *testing.T) {
		app, err := New(testOpts()...)
		require.NoError(t, err)
		assert.Empty(t, app.config.migrationSources)
	})
}

func TestCORSMiddleware(t *testing.T) {
	t.Run("sets CORS headers when origins configured", func(t *testing.T) {
		opts := append(testOpts(), WithCORS("https://example.com"), WithNoDefaultMiddleware())
		app, err := New(opts...)
		require.NoError(t, err)

		// Apply middleware to the router (no session manager needed for CORS)
		app.setupMiddleware(nil)

		app.router.Get("/test", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodOptions, "/test", nil)
		req.Header.Set("Origin", "https://example.com")
		req.Header.Set("Access-Control-Request-Method", "GET")
		w := httptest.NewRecorder()

		app.router.ServeHTTP(w, req)

		assert.Equal(t, "https://example.com", w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("rejects disallowed origin", func(t *testing.T) {
		opts := append(testOpts(), WithCORS("https://example.com"), WithNoDefaultMiddleware())
		app, err := New(opts...)
		require.NoError(t, err)

		app.setupMiddleware(nil)

		app.router.Get("/test", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodOptions, "/test", nil)
		req.Header.Set("Origin", "https://evil.com")
		req.Header.Set("Access-Control-Request-Method", "GET")
		w := httptest.NewRecorder()

		app.router.ServeHTTP(w, req)

		assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("no CORS headers when not configured", func(t *testing.T) {
		opts := append(testOpts(), WithNoDefaultMiddleware())
		app, err := New(opts...)
		require.NoError(t, err)

		app.setupMiddleware(nil)

		app.router.Get("/test", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodOptions, "/test", nil)
		req.Header.Set("Origin", "https://example.com")
		req.Header.Set("Access-Control-Request-Method", "GET")
		w := httptest.NewRecorder()

		app.router.ServeHTTP(w, req)

		assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("WithCORSConfig applies custom settings", func(t *testing.T) {
		cfg := CORSConfig{
			AllowedOrigins:   []string{"https://custom.com"},
			AllowedMethods:   []string{"GET"},
			AllowedHeaders:   []string{"X-Custom"},
			AllowCredentials: false,
			MaxAge:           600,
		}
		opts := append(testOpts(), WithCORSConfig(cfg), WithNoDefaultMiddleware())
		app, err := New(opts...)
		require.NoError(t, err)

		app.setupMiddleware(nil)

		app.router.Get("/test", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodOptions, "/test", nil)
		req.Header.Set("Origin", "https://custom.com")
		req.Header.Set("Access-Control-Request-Method", "GET")
		w := httptest.NewRecorder()

		app.router.ServeHTTP(w, req)

		assert.Equal(t, "https://custom.com", w.Header().Get("Access-Control-Allow-Origin"))
		assert.Equal(t, "GET", w.Header().Get("Access-Control-Allow-Methods"))
		assert.Equal(t, "600", w.Header().Get("Access-Control-Max-Age"))
	})
}
