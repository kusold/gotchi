package app

import (
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/kusold/gotchi/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWithModules(t *testing.T) {
	t.Run("creates application with no modules", func(t *testing.T) {
		app, err := New(Config{
			Server:   ServerConfig{Port: "3000"},
			Database: db.Config{DatabaseURL: "postgres://example"},
		})
		require.NoError(t, err)
		require.NotNil(t, app)
		assert.Empty(t, app.modules)
	})

	t.Run("creates application with one module", func(t *testing.T) {
		module := ModuleFunc(func(r chi.Router, deps Dependencies) error {
			return nil
		})
		app, err := New(Config{
			Server:   ServerConfig{Port: "3000"},
			Database: db.Config{DatabaseURL: "postgres://example"},
		}, module)
		require.NoError(t, err)
		require.NotNil(t, app)
		assert.Len(t, app.modules, 1)
	})

	t.Run("creates application with multiple modules", func(t *testing.T) {
		module1 := ModuleFunc(func(r chi.Router, deps Dependencies) error {
			return nil
		})
		module2 := ModuleFunc(func(r chi.Router, deps Dependencies) error {
			return nil
		})
		app, err := New(Config{
			Server:   ServerConfig{Port: "3000"},
			Database: db.Config{DatabaseURL: "postgres://example"},
		}, module1, module2)
		require.NoError(t, err)
		require.NotNil(t, app)
		assert.Len(t, app.modules, 2)
	})
}

func TestApplicationRouter(t *testing.T) {
	app, err := New(Config{
		Server:   ServerConfig{Port: "3000"},
		Database: db.Config{DatabaseURL: "postgres://example"},
	})
	require.NoError(t, err)

	router := app.Router()
	assert.NotNil(t, router)
	assert.IsType(t, &chi.Mux{}, router)

	// Router should return the same instance
	router2 := app.Router()
	assert.Equal(t, router, router2, "Router() should return the same instance")
}

func TestApplicationDependencies(t *testing.T) {
	app, err := New(Config{
		Server:   ServerConfig{Port: "3000"},
		Database: db.Config{DatabaseURL: "postgres://example"},
	})
	require.NoError(t, err)

	deps := app.Dependencies()
	// Dependencies are empty until Run() is called
	assert.NotNil(t, deps)
	assert.Nil(t, deps.DB)
	assert.Nil(t, deps.Pool)
	assert.Nil(t, deps.Session)
	assert.Nil(t, deps.Auth)
}

func TestApplicationClose(t *testing.T) {
	t.Run("close works without Connect", func(t *testing.T) {
		app, err := New(Config{
			Server:   ServerConfig{Port: "3000"},
			Database: db.Config{DatabaseURL: "postgres://example"},
		})
		require.NoError(t, err)

		// Close should not panic even if Connect was never called
		err = app.Close()
		assert.NoError(t, err)
	})

	t.Run("close can be called multiple times", func(t *testing.T) {
		app, err := New(Config{
			Server:   ServerConfig{Port: "3000"},
			Database: db.Config{DatabaseURL: "postgres://example"},
		})
		require.NoError(t, err)

		// Should be safe to call Close multiple times
		err = app.Close()
		assert.NoError(t, err)
		err = app.Close()
		assert.NoError(t, err)
	})
}

func TestRealClock(t *testing.T) {
	clock := realClock{}
	now := clock.Now()
	assert.False(t, now.IsZero(), "Clock.Now() should return a non-zero time")
}

func TestApplicationInitialState(t *testing.T) {
	cfg := Config{
		Server:   ServerConfig{Port: "3000"},
		Database: db.Config{DatabaseURL: "postgres://example", EnableTracing: true},
	}
	app, err := New(cfg)
	require.NoError(t, err)

	// Verify initial state
	assert.NotNil(t, app.router)
	assert.NotNil(t, app.db)
	assert.Equal(t, "3000", app.cfg.Server.Port)
	assert.Equal(t, "postgres://example", app.cfg.Database.DatabaseURL)
	assert.True(t, app.cfg.Database.EnableTracing)
}

func TestNewAppliesDefaults(t *testing.T) {
	t.Run("applies default port when not provided", func(t *testing.T) {
		app, err := New(Config{
			Database: db.Config{DatabaseURL: "postgres://example"},
		})
		require.NoError(t, err)
		assert.Equal(t, "3000", app.cfg.Server.Port)
	})
}
