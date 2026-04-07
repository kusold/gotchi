package app

import (
	"testing"

	"github.com/kusold/gotchi/auth"
)

func TestNewRequiresDatabaseURL(t *testing.T) {
	_, err := New(Config{Server: ServerConfig{Port: "3000"}})
	if err == nil {
		t.Fatalf("expected error for missing database URL")
	}
}

func TestNewRequiresOIDCFieldsWhenEnabled(t *testing.T) {
	cfg := testConfig()
	cfg.Auth = AuthConfig{OIDC: auth.Config{Enabled: true}}
	_, err := New(cfg)
	if err == nil {
		t.Fatalf("expected error for missing OIDC fields")
	}
}

func TestNewSuccess(t *testing.T) {
	app, err := New(testConfig())
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
		cfg := testConfig()
		cfg.Database.EnableTracing = true
		withTracing, err := New(cfg)
		if err != nil {
			t.Fatalf("unexpected error with tracing enabled: %v", err)
		}
		if !withTracing.cfg.Database.EnableTracing {
			t.Fatalf("expected database tracing to remain enabled")
		}
	})

	t.Run("tracing disabled", func(t *testing.T) {
		cfg := testConfig()
		cfg.Database.EnableTracing = false
		withoutTracing, err := New(cfg)
		if err != nil {
			t.Fatalf("unexpected error with tracing disabled: %v", err)
		}
		if withoutTracing.cfg.Database.EnableTracing {
			t.Fatalf("expected database tracing to remain disabled")
		}
	})
}
