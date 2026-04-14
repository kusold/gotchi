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
	var opts []app.Option
	opts = append(opts,
		app.WithDatabase(getenv("DATABASE_URL", "")),
		app.WithPort(getenv("PORT", "3000")),
		app.WithCoreMigrations(),
		app.WithAuthMigrations(),
		app.WithMigrations(db.MigrationSource{FS: migrations.Migrations, Dir: "."}),
		app.WithModule(module.New()),
	)

	if origins := parseCSV(getenv("CORS_ALLOWED_ORIGINS", "")); len(origins) > 0 {
		opts = append(opts, app.WithCORS(origins[0], origins[1:]...))
	}

	application, err := app.New(opts...)
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
