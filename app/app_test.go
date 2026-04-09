package app

import (
	"testing"

	"github.com/kusold/gotchi/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRequiresDatabaseURL(t *testing.T) {
	_, err := New(WithPort("3000"))
	if err == nil {
		t.Fatalf("expected error for missing database")
	}
}

func TestNewRequiresOIDCFieldsWhenEnabled(t *testing.T) {
	_, err := New(
		WithDatabase("postgres://example"),
		WithAuth(auth.Config{}),
	)
	if err == nil {
		t.Fatalf("expected error for missing OIDC fields")
	}
}

func TestNewSuccess(t *testing.T) {
	app, err := New(testOpts()...)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if app == nil {
		t.Fatalf("expected application instance")
	}
	if app.Router() == nil {
		t.Fatalf("expected router")
	}
}

func TestNewRespectsDatabaseTracingSetting(t *testing.T) {
	t.Run("tracing enabled", func(t *testing.T) {
		opts := testOptsWithDBConfig(dbConfigWithTracing(true))
		withTracing, err := New(opts...)
		require.NoError(t, err)
		assert.True(t, withTracing.config.dbConfig.EnableSlogTracing)
	})

	t.Run("tracing disabled", func(t *testing.T) {
		opts := testOptsWithDBConfig(dbConfigWithTracing(false))
		withoutTracing, err := New(opts...)
		require.NoError(t, err)
		assert.False(t, withoutTracing.config.dbConfig.EnableSlogTracing)
	})
}
