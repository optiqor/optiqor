#!/usr/bin/env bash
# Dev bootstrap: docker compose up + run migrations + tail api.
set -euo pipefail

cd "$(dirname "$0")/.."

if [ ! -f .env ]; then
  echo "==> .env not found, copying from .env.example"
  cp .env.example .env
  echo "    Edit .env and fill in the blanks (Anthropic key, GitHub App, etc.)"
fi

# shellcheck disable=SC1091
source .env

echo "==> bringing up local stack"
docker compose up -d

echo "==> waiting for postgres to be healthy"
until docker compose exec -T postgres pg_isready -U optiqor -d optiqor >/dev/null 2>&1; do
  sleep 1
done

echo "==> running migrations"
make migrate || echo "    (no migrations yet — skipping)"

echo "==> dev stack ready."
echo "    api:           make run-api"
echo "    worker:        make run-worker"
echo "    temporal UI:   http://localhost:8233"
