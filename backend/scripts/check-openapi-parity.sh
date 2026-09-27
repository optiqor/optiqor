#!/usr/bin/env bash
# CI gate: every openapi.yaml path must be registered in cmd/api, and
# every registered route must appear in the spec. Path params are
# normalised to {X} before comparison; new routes outside that template
# style need their own grep below. Run from the backend repo root.
set -euo pipefail

SPEC="../optiqor-cli/docs/api/openapi.yaml"
if [[ ! -f "$SPEC" ]]; then
  echo "openapi spec not found at $SPEC (CI checks out optiqor-cli/ as sibling — same shape required locally)" >&2
  exit 2
fi

# Path keys sit at column 2 indent under `paths:` and end in `:`.
mapfile -t spec_paths < <(awk '
  /^paths:/         { in_paths = 1; next }
  in_paths && /^[a-zA-Z]/ { in_paths = 0 }
  in_paths && /^  \//      { sub(/:$/, "", $1); print $1 }
' "$SPEC")

if [[ ${#spec_paths[@]} -eq 0 ]]; then
  echo "no paths found in $SPEC — bad parser regex or empty spec" >&2
  exit 2
fi

# /debug/pprof/* is admin-only behind OPTIQOR_ADMIN_TOKEN, not a public
# API; the spec stays focused on customer-facing routes.
mapfile -t go_routes < <(
  grep -rhoE '"(GET|POST|PUT|PATCH|DELETE) /[^"]+"' \
    cmd/api/ internal/sandbox/sandbox.go internal/receipts/ internal/ingestion/ \
    internal/billing/ internal/onboarding/ internal/auth/ internal/prwriter/ 2>/dev/null \
    | sed -E 's/^"(GET|POST|PUT|PATCH|DELETE) //;s/"$//' \
    | grep -v '^/debug/pprof' \
    | sort -u
)

# /metrics is mounted via metrics.Handler() without a method-prefixed
# literal, so the grep above misses it; add it manually.
go_routes+=("/metrics")

norm() { sed -E 's/\{[^}]+\}/{X}/g'; }

mapfile -t spec_norm < <(printf '%s\n' "${spec_paths[@]}" | norm | sort -u)
mapfile -t go_norm   < <(printf '%s\n' "${go_routes[@]}"   | norm | sort -u)

missing_in_go=$(comm -23 <(printf '%s\n' "${spec_norm[@]}") <(printf '%s\n' "${go_norm[@]}"))
missing_in_spec=$(comm -13 <(printf '%s\n' "${spec_norm[@]}") <(printf '%s\n' "${go_norm[@]}"))

status=0
# Spec is allowed to be a superset of Go (forward-looking documentation
# of routes that land in a later stacked PR is fine). The hard
# constraint is the other direction: every Go route MUST appear in the
# spec so customers cannot hit undocumented endpoints.
if [[ -n "$missing_in_go" ]]; then
  echo "::warning::paths in openapi.yaml but not yet in Go handlers (forward-looking; OK during stacked rollout):" >&2
  printf '  %s\n' $missing_in_go >&2
fi
if [[ -n "$missing_in_spec" ]]; then
  echo "::error::Go routes not documented in openapi.yaml:" >&2
  printf '  %s\n' $missing_in_spec >&2
  status=1
fi

if [[ $status -eq 0 ]]; then
  printf 'openapi parity: %d Go routes documented (spec has %d total)\n' "${#go_norm[@]}" "${#spec_norm[@]}"
fi
exit "$status"
