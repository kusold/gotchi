package app

import (
	"github.com/go-chi/chi/v5"
	"github.com/kusold/gotchi/db"
)

func testOpts() []Option {
	return []Option{
		WithDatabase("postgres://example"),
		WithPort("3000"),
	}
}

func testOptsWithDBConfig(cfg db.Config) []Option {
	return []Option{
		WithDatabaseConfig(cfg),
		WithPort("3000"),
	}
}

func dbConfigWithTracing(enabled bool) db.Config {
	return db.Config{
		DatabaseURL:       "postgres://example",
		EnableSlogTracing: enabled,
	}
}

func noopModule() Module {
	return ModuleFunc(func(r chi.Router, deps Dependencies) error { return nil })
}
