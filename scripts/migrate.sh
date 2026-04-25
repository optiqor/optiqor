#!/usr/bin/env bash
# Wrapper around goose so the same command works in CI and locally.
set -euo pipefail

cd "$(dirname "$0")/.."

# shellcheck disable=SC1091
[ -f .env ] && source .env

: "${COSTIFY_POSTGRES_DSN:?COSTIFY_POSTGRES_DSN must be set}"

cmd="${1:-up}"
shift || true

exec goose -dir ./migrations postgres "$COSTIFY_POSTGRES_DSN" "$cmd" "$@"
