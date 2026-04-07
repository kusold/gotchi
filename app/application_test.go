package app

import (
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWithModules(t *testing.T) {
	t.Run("creates application with no modules", func(t *testing.T) {
		app, err := New(testConfig())
		require.NoError(t, err)
		require.NotNil(t, app)
		assert.Empty(t, app.modules)
	})

	t.Run("creates application with one module", func(t *testing.T) {
		app, err := New(testConfig(), noopModule())
		require.NoError(t, err)
		require.NotNil(t, app)
		assert.Len(t, app.modules, 1)
	})

	t.Run("creates application with multiple modules", func(t *testing.T) {
		app, err := New(testConfig(), noopModule(), noopModule())
		require.NoError(t, err)
		require.NotNil(t, app)
		assert.Len(t, app.modules, 2)
	})
}

func TestApplicationRouter(t *testing.T) {
	t.Run("returns a chi mux", func(t *testing.T) {
		app, err := New(testConfig())
		require.NoError(t, err)
		router := app.Router()
		assert.NotNil(t, router)
		assert.IsType(t, &chi.Mux{}, router)
	})

	t.Run("returns the same instance on subsequent calls", func(t *testing.T) {
		app, err := New(testConfig())
		require.NoError(t, err)
		router := app.Router()
		router2 := app.Router()
		assert.Equal(t, router, router2)
	})
}

func TestApplicationDependencies(t *testing.T) {
	app, err := New(testConfig())
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
		app, err := New(testConfig())
		require.NoError(t, err)
		err = app.Close()
		assert.NoError(t, err)
	})

	t.Run("close can be called multiple times", func(t *testing.T) {
		app, err := New(testConfig())
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
	cfg := testConfig()
	cfg.Database.EnableTracing = true
	app, err := New(cfg)
	require.NoError(t, err)
	assert.NotNil(t, app.router)
	assert.NotNil(t, app.db)
	assert.Equal(t, "3000", app.cfg.Server.Port)
	assert.Equal(t, "postgres://example", app.cfg.Database.DatabaseURL)
	assert.True(t, app.cfg.Database.EnableTracing)
}

func TestNewAppliesDefaults(t *testing.T) {
	t.Run("applies default port when not provided", func(t *testing.T) {
		cfg := testConfig()
		cfg.Server = ServerConfig{}
		app, err := New(cfg)
		require.NoError(t, err)
		assert.Equal(t, "3000", app.cfg.Server.Port)
	})

	t.Run("initializes empty migration sources", func(t *testing.T) {
		cfg := testConfig()
		cfg.Migrations = MigrationConfig{}
		app, err := New(cfg)
		require.NoError(t, err)
		assert.NotNil(t, app.cfg.Migrations.Sources)
		assert.Empty(t, app.cfg.Migrations.Sources)
	})
}
