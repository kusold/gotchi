package main

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/kusold/gotchi/app"
	"github.com/kusold/gotchi/db"
	"github.com/kusold/reference-app/internal/module"
	"github.com/kusold/reference-app/migrations"
)

func main() {
	cfg := app.Config{
		Server: app.ServerConfig{Port: getenv("PORT", "3000")},
		Database: db.Config{
			DatabaseURL: getenv("DATABASE_URL", ""),
		},
		Auth: app.AuthConfig{},
		CORS: app.CORSConfig{
			AllowedOrigins: parseCSV(getenv("CORS_ALLOWED_ORIGINS", "")),
		},
		Migrations: app.MigrationConfig{
			EnableCore: true,
			EnableAuth: true,
			Sources:    []db.MigrationSource{{FS: migrations.Migrations, Dir: "."}},
		},
	}

	application, err := app.New(cfg, module.New())
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(application.Run(context.Background()))
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func parseCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts
}
