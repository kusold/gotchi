# Changelog

## v0.1.0 - 2026-02-22
- Introduced `github.com/kusold/gotchi/app` with opinionated application bootstrap (`app.New`, `Application.Run`, module registration).
- Added default Postgres identity store for OIDC provisioning and tenant membership management.
- Added tenant listing/selection OIDC endpoints and membership enforcement (user must belong to at least one tenant).
- Added standardized correlation and audit middleware with `request_id`, `user_id`, and `tenant_id` logging context.
- Added scaffold CLI (`gotchi init`, `gotchi add feature`, `gotchi doctor`) and generated reference app under `examples/reference-app`.
- Added core/auth baseline migrations and migration profile support in app bootstrap.
