# Changelog

## v0.2.0 - 2026-04-09
- **BREAKING**: Replaced `app.Config` struct with functional options pattern (`app.New(opts ...Option)`).
- Removed `Config`, `ServerConfig`, `AuthConfig`, `MigrationConfig`, and `CORSConfig` wrapper types.
- Added `With*` option functions: `WithDatabase`, `WithPort`, `WithAuth`, `WithSessions`, `WithOTEL`, `WithCORS`, `WithOpenAPI`, `WithCoreMigrations`, `WithAuthMigrations`, `WithMigrations`, `WithMiddleware`, `WithNoDefaultMiddleware`, `WithModule`, `WithClock`, `WithLogger`, and more.
- Features (auth, OTEL, CORS) are now explicitly opt-in rather than always-on.
- Sessions are auto-enabled when `WithAuth` is used.
- Custom middleware can be added via `WithMiddleware`; default middleware can be disabled via `WithNoDefaultMiddleware`.

## v0.1.0 - 2026-02-22
- Introduced `github.com/kusold/gotchi/app` with opinionated application bootstrap (`app.New`, `Application.Run`, module registration).
- Added default Postgres identity store for OIDC provisioning and tenant membership management.
- Added tenant listing/selection OIDC endpoints and membership enforcement (user must belong to at least one tenant).
- Added standardized correlation and audit middleware with `request_id`, `user_id`, and `tenant_id` logging context.
- Added scaffold CLI (`gotchi init`, `gotchi add feature`, `gotchi doctor`) and generated reference app under `examples/reference-app`.
- Added core/auth baseline migrations and migration profile support in app bootstrap.
