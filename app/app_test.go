package app

import (
	"testing"

	"github.com/kusold/gotchi/auth"
	"github.com/kusold/gotchi/db"
)

func TestNewRequiresDatabaseURL(t *testing.T) {
	_, err := New(Config{Server: ServerConfig{Port: "3000"}})
	if err == nil {
		t.Fatalf("expected error for missing database URL")
	}
}

func TestNewRequiresOIDCFieldsWhenEnabled(t *testing.T) {
	_, err := New(Config{
		Server:   ServerConfig{Port: "3000"},
		Database: db.Config{DatabaseURL: "postgres://example"},
		Auth:     AuthConfig{OIDC: auth.Config{Enabled: true}},
	})
	if err == nil {
		t.Fatalf("expected error for missing OIDC fields")
	}
}

func TestNewSuccess(t *testing.T) {
	app, err := New(Config{
		Server:   ServerConfig{Port: "3000"},
		Database: db.Config{DatabaseURL: "postgres://example"},
	})
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
	withTracing, err := New(Config{
		Server:   ServerConfig{Port: "3000"},
		Database: db.Config{DatabaseURL: "postgres://example", EnableTracing: true},
	})
	if err != nil {
		t.Fatalf("unexpected error with tracing enabled: %v", err)
	}
	if !withTracing.cfg.Database.EnableTracing {
		t.Fatalf("expected database tracing to remain enabled")
	}

	withoutTracing, err := New(Config{
		Server:   ServerConfig{Port: "3000"},
		Database: db.Config{DatabaseURL: "postgres://example", EnableTracing: false},
	})
	if err != nil {
		t.Fatalf("unexpected error with tracing disabled: %v", err)
	}
	if withoutTracing.cfg.Database.EnableTracing {
		t.Fatalf("expected database tracing to remain disabled")
	}
}
