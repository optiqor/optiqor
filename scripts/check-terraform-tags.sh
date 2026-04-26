#!/usr/bin/env bash
# CI guard: every env Terraform file must declare default_tags with
# Project, Environment, and Tenant keys, so AWS resources are
# unambiguously attributed to Sevro and cost-attributable per tenant.
#
# Modules themselves do NOT declare provider blocks; they inherit
# default_tags from the env they are instantiated by. We therefore
# only check files under infra/terraform/envs/.
set -euo pipefail

REQUIRED_KEYS=("Project" "Environment" "Tenant" "ManagedBy")
ENV_DIRS=(infra/terraform/envs/dev infra/terraform/envs/staging infra/terraform/envs/prod)
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

fail() {
  printf "tag-check: FAIL %s\n" "$1" >&2
  exit 1
}

for env in "${ENV_DIRS[@]}"; do
  dir="$ROOT/$env"
  if [ ! -d "$dir" ]; then
    fail "missing env dir: $env"
  fi
  main="$dir/main.tf"
  if [ ! -f "$main" ]; then
    fail "missing main.tf: $env/main.tf"
  fi

  if ! grep -q 'default_tags' "$main"; then
    fail "$env/main.tf has no default_tags block"
  fi

  for key in "${REQUIRED_KEYS[@]}"; do
    if ! grep -qE "^\s+${key}\s*=" "$main"; then
      fail "$env/main.tf: default_tags missing required key '${key}'"
    fi
  done
done

echo "tag-check: ok (${#ENV_DIRS[@]} envs · ${REQUIRED_KEYS[*]})"
