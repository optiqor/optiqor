#!/usr/bin/env bash
# Seed local dev data. Idempotent.
set -euo pipefail

cd "$(dirname "$0")/.."

# shellcheck disable=SC1091
[ -f .env ] && source .env

echo "==> seed: nothing to seed yet (Phase 1 will add tenants/users)"
