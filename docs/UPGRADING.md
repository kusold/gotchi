# Upgrade Guide

## v0.2.0 - Functional Options API

The `app.Config` struct and related wrapper types (`ServerConfig`, `AuthConfig`, `MigrationConfig`, `CORSConfig`) have been replaced with a functional options pattern.

### Before

```go
cfg := app.Config{
    Server:   app.ServerConfig{Port: "3000"},
    Database: db.Config{DatabaseURL: os.Getenv("DATABASE_URL")},
    Auth:     app.AuthConfig{OIDC: auth.Config{Enabled: true, ...}},
    CORS:     app.CORSConfig{AllowedOrigins: []string{"https://example.com"}},
    Migrations: app.MigrationConfig{
        EnableCore: true,
        EnableAuth: true,
        Sources:    []db.MigrationSource{{FS: migrations.Migrations, Dir: "."}},
    },
}
application, err := app.New(cfg, module.New())
```

### After

```go
application, err := app.New(
    app.WithDatabase(os.Getenv("DATABASE_URL")),
    app.WithPort("3000"),
    app.WithAuth(auth.Config{...}),
    app.WithCORS("https://example.com"),
    app.WithCoreMigrations(),
    app.WithAuthMigrations(),
    app.WithMigrations(db.MigrationSource{FS: migrations.Migrations, Dir: "."}),
    app.WithModule(module.New()),
)
```

### Key changes

- `app.New` now accepts `...Option` instead of `Config` + `...Module`.
- Modules are registered via `app.WithModule(...)`.
- Auth is optional and opt-in via `app.WithAuth(cfg)`. Sessions are auto-enabled when auth is configured.
- CORS, OTEL, and OpenAPI are optional and opt-in via their respective `With*` functions.
- Default middleware (RealIP, Logger, Recoverer) is still applied unless `app.WithNoDefaultMiddleware()` is used.
- Custom middleware can be added with `app.WithMiddleware(...)`.
- For full database config control, use `app.WithDatabaseConfig(db.Config{...})` instead of `app.WithDatabase(url)`.

## Freezer-catalog to gotchi v0.1.0
1. Update imports to `github.com/kusold/gotchi/...`.
2. Replace custom runtime composition with `gotchi/app.New(...)`.
3. Register application routes as an `app.Module` implementation.
4. Remove local DB/session/auth/openapi wrapper layers and use gotchi primitives directly.
5. Keep existing app migrations enabled; disable gotchi baseline profiles when your app already owns those tables/functions.

## Breaking Changes
- Session and auth middleware context should use gotchi typed helpers (`auth.SessionClaimsFromContext`, `tenantctx.TenantID`) instead of string context keys.
- Legacy wrapper layers in freezer-catalog were removed (`database_config`, session manager wrapper, middleware wrappers, old server composition path).

## Migration Notes
- Multi-tenant behavior remains strict: users must belong to at least one tenant.
- Single-membership users auto-select the active tenant at login.
- Multi-membership users must select an active tenant through `/auth/tenant/select`.
