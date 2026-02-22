# Semver Policy

- `MAJOR`: incompatible public API behavior changes in bootstrap/auth/migrations.
- `MINOR`: backward-compatible features (new middleware, optional config, new module capabilities).
- `PATCH`: bug fixes and internal improvements with no intended public API change.

Release requirements:
1. Update `CHANGELOG.md`.
2. Add upgrade notes in `docs/UPGRADING.md` when behavior or config expectations change.
3. Validate both consumers (`freezer-catalog` and `examples/reference-app`) against the target version.
