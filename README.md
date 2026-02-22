# gotchi

`gotchi` is a reusable backend core for Go services with an opinionated stack:
- `chi`
- `pgx` + `goose`
- `scs` sessions (`pgxstore`)
- OIDC auth with multi-tenant membership enforcement
- Postgres RLS tenancy helpers
- OpenAPI validation + oapi-codegen mounting

## Module

```go
module github.com/kusold/gotchi
```

## Bootstrap

Use `app.New(...)` to construct and run an application with minimal wiring.

## CLI

- `gotchi init <app-name>`
- `gotchi add feature <name>`
- `gotchi doctor`

## Migration regression check

Run this against a disposable Postgres database:

```bash
DATABASE_URL='postgres://postgres:postgres@localhost:5432/gotchi_regression?sslmode=disable' ./scripts/migration-regression.sh
```

## Git hooks

This repo uses [`prek`](https://prek.j178.dev/) for commit hooks and enforces Conventional Commit messages via a `commit-msg` hook.

Install and set up hooks:

```bash
brew install prek
prek install --install-hooks --hook-type commit-msg
```

## Nix dev shell

This repo includes a cross-system `flake-parts` dev shell with Go tooling and `prek`.

Enable automatic shell loading with `direnv`:

```bash
direnv allow
```

Or enter it manually:

```bash
nix develop
```

## Consumers in this workspace

- `/Users/mike/code/repos/kusold/freezer-catalog`
- `/Users/mike/code/repos/kusold/gotchi/examples/reference-app`
