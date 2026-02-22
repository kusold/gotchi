#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

DATABASE_URL="${DATABASE_URL:-}"
if [[ -z "$DATABASE_URL" ]]; then
  echo "DATABASE_URL is required. Use a disposable Postgres database for this check." >&2
  exit 1
fi

ARGS=(-database-url "$DATABASE_URL")

if [[ -n "${MIGRATION_REGRESSION_SCHEMA:-}" ]]; then
  ARGS+=(-schema "$MIGRATION_REGRESSION_SCHEMA")
fi

go run ./cmd/migration-regression "${ARGS[@]}"
