#!/usr/bin/env bash
# Runs the api binary and the Next.js web frontend concurrently with
# prefixed, colour-tagged output and a single Ctrl+C teardown.
#
# Used by `make dev`. Lives in scripts/ so the Makefile stays small
# and the signal-handling stays POSIX-shell readable.
#
# Behaviour:
#   - Pre-flight: ensures go + pnpm are on PATH and the web/ deps are
#     installed (runs `pnpm install` on first invocation).
#   - Sources .env when present so OPTIQOR_* config reaches both
#     processes consistently.
#   - Streams both stdouts with a per-process tag (`[api]` cyan,
#     `[web]` amber) so logs are scannable.
#   - Ctrl+C / SIGTERM tears down both children; if either dies on its
#     own the other is killed too — `wait -n` is the trigger.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# ── colour palette (matches brand/tokens.json) ─────────────────────
if [[ -t 1 ]] && [[ "${NO_COLOR:-}" == "" ]]; then
  C_API=$'\033[38;5;43m'    # cyan
  C_WEB=$'\033[38;5;215m'   # amber
  C_DIM=$'\033[2m'
  C_BOLD=$'\033[1m'
  C_OFF=$'\033[0m'
else
  C_API=""; C_WEB=""; C_DIM=""; C_BOLD=""; C_OFF=""
fi

# ── pre-flight ─────────────────────────────────────────────────────
need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf "%s%s missing on PATH.%s %s\n" "$C_BOLD" "$1" "$C_OFF" "${2:-Install it and re-run.}"
    exit 1
  fi
}
need go    "https://go.dev/dl/"
need pnpm  "https://pnpm.io/installation (Node 18+ required)"

if [ ! -d web/node_modules ]; then
  printf "%s==>%s installing web deps (first run, one-time)\n" "$C_BOLD" "$C_OFF"
  (cd web && pnpm install)
fi

if [ -f .env ]; then
  printf "%s==>%s sourcing .env\n" "$C_BOLD" "$C_OFF"
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

# Sensible defaults so a bare `make dev` works without a populated .env.
export OPTIQOR_HTTP_ADDR="${OPTIQOR_HTTP_ADDR:-:8080}"
export OPTIQOR_ENV="${OPTIQOR_ENV:-dev}"

API_URL="http://localhost${OPTIQOR_HTTP_ADDR}"
WEB_URL="http://localhost:3000"

# Tell the Next.js dev server where to proxy /v1/*, /r/*, /v/* so the
# browser only ever talks to localhost:3000 (no CORS). The next.config
# rewrites read this. Do NOT export NEXT_PUBLIC_OPTIQOR_API here —
# that would force absolute URLs in the client and bypass the proxy.
export OPTIQOR_API_UPSTREAM="${OPTIQOR_API_UPSTREAM:-$API_URL}"

# ── prefix helper ──────────────────────────────────────────────────
# stdbuf keeps Go and Next.js stdout from buffering when piped.
prefix() {
  local tag="$1" color="$2"
  while IFS= read -r line; do
    printf '%s%-3s%s %s\n' "$color" "$tag" "$C_OFF" "$line"
  done
}

# ── signal handling ────────────────────────────────────────────────
# `set -m` (further down) launches each pipeline in its own process
# group. `kill -TERM -<pgid>` then takes down the whole tree —
# important for pnpm (spawns next as a grandchild) and `go run`
# (spawns the compiled exe). Without group-kill, those grandchildren
# leak past Ctrl+C and ports stay held.
#
# Two-phase teardown: SIGTERM → 1s grace → SIGKILL anything still
# alive. A final `pkill -P $$` catches anything that re-parented to
# us between phases.
PGIDS=()

# collect_descendants walks the process tree rooted at $1 and appends
# every pid found to the global DESCENDANTS array.
collect_descendants() {
  local pid="$1" kid
  for kid in $(ps -o pid= --ppid "$pid" 2>/dev/null); do
    DESCENDANTS+=("$kid")
    collect_descendants "$kid"
  done
}

shutdown() {
  printf "\n%s==>%s stopping (ctrl+c)\n" "$C_BOLD" "$C_OFF"
  # Snapshot every descendant pid BEFORE we start killing things.
  # Otherwise `go run` will exit on SIGTERM but leave its compiled
  # binary re-parented to init — outside our pgid reach.
  DESCENDANTS=()
  collect_descendants "$$"

  # phase 1: SIGTERM the pipeline pgids (catches the well-behaved
  # majority and gives them a chance to flush log lines).
  for pgid in "${PGIDS[@]}"; do
    kill -TERM -"$pgid" 2>/dev/null || true
  done
  sleep 1

  # phase 2: SIGKILL any pid from the pre-shutdown snapshot that is
  # still alive (orphans included).
  for pid in "${DESCENDANTS[@]}"; do
    kill -KILL "$pid" 2>/dev/null || true
  done
  wait 2>/dev/null || true
  exit 0
}
trap shutdown INT TERM

# ── banner ─────────────────────────────────────────────────────────
printf "\n"
printf "%s  ◐ optiqor dev%s\n" "$C_BOLD" "$C_OFF"
printf "%s     ───────────────────────────────────────────────%s\n" "$C_DIM" "$C_OFF"
printf "  %sapi%s   %s%s\n" "$C_API" "$C_OFF" "$API_URL" "$C_OFF"
printf "  %sweb%s   %s%s\n" "$C_WEB" "$C_OFF" "$WEB_URL" "$C_OFF"
printf "%s     ───────────────────────────────────────────────%s\n" "$C_DIM" "$C_OFF"
printf "%s     ctrl+c stops both. logs are tagged [api] / [web].%s\n\n" "$C_DIM" "$C_OFF"

# ── launch ─────────────────────────────────────────────────────────
# `set -m` (job control) makes each backgrounded pipeline its own
# process group; in that mode bash's `$!` is the pgid leader. We then
# `kill -TERM -<pgid>` in shutdown() to take down each tree atomically.
# Without this, pnpm survives a plain SIGTERM and Next.js leaks.
set -m

( go run ./cmd/api 2>&1 | prefix api "$C_API" ) &
PGIDS+=($!)

(
  cd web
  PNPM_SHOW_NOTIFICATION_BANNER=false \
    pnpm exec next dev --turbopack 2>&1 | prefix web "$C_WEB"
) &
PGIDS+=($!)

# wait -n exits when the FIRST background job finishes. We treat that
# as "one of them died, the other should not keep running solo."
wait -n || true
shutdown
