// Package scaffold generates boilerplate files for new gotchi applications.
//
// It produces a ready-to-run project structure including a Go module file,
// a main entry point wired to the gotchi [app] and [db] packages, an example
// module with a health-check endpoint, an embedded SQL migration, a Docker
// Compose configuration, an environment variable template, a README, and a
// GitHub Actions CI workflow.
//
// Typical usage is through the CLI command "gotchi init", which calls
// [Generate] to build the file map and [Write] to persist files to disk:
//
//	files, err := scaffold.Generate(scaffold.Options{
//	    AppName:    "my-app",
//	    ModulePath: "github.com/example/my-app",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	if err := scaffold.Write("/path/to/project", files); err != nil {
//	    log.Fatal(err)
//	}
package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Options holds the parameters that configure scaffold generation.
//
// AppName is used in the README title, Docker Compose database name, and
// the environment variable template. ModulePath is the Go module path
// written to go.mod and used in import statements throughout the generated
// source files. Both fields are required.
type Options struct {
	// AppName is the human-readable application name (e.g. "my-app").
	AppName string
	// ModulePath is the Go module import path (e.g. "github.com/example/my-app").
	ModulePath string
}

// Generate creates a map of file paths to their contents for a new gotchi
// application. The returned map keys are slash-separated relative paths
// (e.g. "cmd/server/main.go") and the values are the complete file contents
// as strings.
//
// Both opts.AppName and opts.ModulePath must be non-empty. If either is
// blank or contains only whitespace, Generate returns an error.
//
// The generated files include:
//   - go.mod: Go module file declaring a dependency on github.com/kusold/gotchi.
//   - cmd/server/main.go: Application entry point that wires config, database,
//     migrations, and a single module.
//   - internal/module/module.go: Example module with a /healthz endpoint.
//   - migrations/migrations.go: Embedded migration filesystem declaration.
//   - migrations/20260101000000_example.sql: Example goose migration.
//   - .env.sample: Template for required environment variables.
//   - docker-compose.yaml: PostgreSQL service for local development.
//   - README.md: Quick-start guide referencing the AppName.
//   - .github/workflows/test.yaml: CI workflow that runs "go test ./...".
func Generate(opts Options) (map[string]string, error) {
	if strings.TrimSpace(opts.AppName) == "" {
		return nil, fmt.Errorf("app name is required")
	}
	if strings.TrimSpace(opts.ModulePath) == "" {
		return nil, fmt.Errorf("module path is required")
	}

	appName := opts.AppName
	module := opts.ModulePath

	files := map[string]string{
		"go.mod": fmt.Sprintf(`module %s

go 1.25.1

require github.com/kusold/gotchi v0.1.0
`, module),
		"cmd/server/main.go": fmt.Sprintf(`package main

import (
	"context"
	"log"
	"os"

	"github.com/kusold/gotchi/app"
	"github.com/kusold/gotchi/db"
	"%s/internal/module"
	"%s/migrations"
)

func main() {
	application, err := app.New(
		app.WithDatabase(getenv("DATABASE_URL", "")),
		app.WithPort(getenv("PORT", "3000")),
		app.WithCoreMigrations(),
		app.WithAuthMigrations(),
		app.WithMigrations(db.MigrationSource{FS: migrations.Migrations, Dir: "."}),
		app.WithModule(module.New()),
	)
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
`, module, module),
		"internal/module/module.go": `package module

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/kusold/gotchi/app"
)

type Module struct{}

func New() Module { return Module{} }

func (m Module) Register(r chi.Router, deps app.Dependencies) error {
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return nil
}
`,
		"migrations/migrations.go": `package migrations

import "embed"

//go:embed *.sql
var Migrations embed.FS
`,
		"migrations/20260101000000_example.sql": `-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS example_records (
	id UUID PRIMARY KEY,
	name TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS example_records;
-- +goose StatementEnd
`,
		".env.sample": `DATABASE_URL=postgres://pguser:pgpass@localhost:5432/` + appName + `
PORT=3000

OIDC_ISSUER_URL=
OIDC_CLIENT_ID=
OIDC_CLIENT_SECRET=
OIDC_REDIRECT_URL=http://localhost:3000/auth/oidc/callback
OIDC_TENANT_PICKER_PATH=/auth/tenants
OIDC_POST_LOGIN_REDIRECT=/
`,
		"docker-compose.yaml": `services:
  db:
    image: postgres:18
    environment:
      - POSTGRES_USER=pguser
      - POSTGRES_PASSWORD=pgpass
      - POSTGRES_DB=` + appName + `
    ports:
      - "5432:5432"
`,
		"README.md": fmt.Sprintf(`# %s

Generated by `+"`gotchi init`"+`.

## Run

1. Start postgres: `+"`docker compose up -d db`"+`
2. Set env vars from `+"`.env.sample`"+`
3. Run server: `+"`go run ./cmd/server`"+`
`, appName),
		".github/workflows/test.yaml": `name: Tests
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.25.x"
      - run: go test ./...
`,
	}

	return files, nil
}

// Write persists a map of files to the filesystem. For each entry in files,
// it creates any missing parent directories under root and writes the content
// with permissions 0o644. If a file already exists at the target path it is
// overwritten.
//
// root must be an existing directory (or an empty string to write relative
// to the working directory). An error is returned if directory creation or
// any file write fails; files written before the failure are not rolled back.
func Write(root string, files map[string]string) error {
	for path, content := range files {
		target := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// FileList returns the keys of the files map as a sorted slice of paths.
// It is useful for displaying or logging the set of files that will be
// generated.
func FileList(files map[string]string) []string {
	list := make([]string, 0, len(files))
	for path := range files {
		list = append(list, path)
	}
	sort.Strings(list)
	return list
}
