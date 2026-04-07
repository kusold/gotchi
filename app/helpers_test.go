package app

import (
	"github.com/go-chi/chi/v5"
	"github.com/kusold/gotchi/db"
)

func testConfig() Config {
	return Config{
		Server:   ServerConfig{Port: "3000"},
		Database: db.Config{DatabaseURL: "postgres://example"},
	}
}

func noopModule() Module {
	return ModuleFunc(func(r chi.Router, deps Dependencies) error { return nil })
}
