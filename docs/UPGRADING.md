# Upgrade Guide

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
