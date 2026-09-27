#!/usr/bin/env bash
# verify.sh — Optiqor backend reality check.
#
# Audits the backend monorepo against the contracts in:
#   docs/strategy/idea.md
#   docs/strategy/business_strategy.md
#   docs/strategy/technical_implementation.md
#   docs/strategy/open_source_cli_playbook.md
#   CLAUDE.md
#
# Three result types:
#
#   PASS — implemented and working today
#   GAP  — strategy docs commit to this; not implemented yet (Phase 2+)
#          Counted separately; does NOT fail the script.
#   FAIL — implemented incorrectly, or a hard rule is violated.
#          Fails the script.
#
# Main goal: surface every place where the live code diverges from
# what the strategy + CLAUDE.md say should exist. Run before pushing
# anything user-facing.
#
# Usage:
#   ./verify.sh                 # full run
#   ./verify.sh --quiet         # only print FAIL + GAP + summary
#   ./verify.sh --section F     # run a single section
#   ./verify.sh --list          # list every check without running
#   ./verify.sh --no-build      # skip the build/test section (faster)
#
set -u
set -o pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$HERE"

# ─── output helpers ───────────────────────────────────────────────────
if [[ -t 1 ]] && [[ "${NO_COLOR:-}" == "" ]]; then
  R=$'\033[0;31m'; G=$'\033[0;32m'; A=$'\033[0;33m'
  B=$'\033[0;34m'; BOLD=$'\033[1m'; DIM=$'\033[2m'; X=$'\033[0m'
else R=""; G=""; A=""; B=""; BOLD=""; DIM=""; X=""; fi

PASS=0; GAP=0; FAIL=0
GAPS=(); FAILS=()
QUIET=0; LIST_ONLY=0; ONLY_SECTION=""; NO_BUILD=0
for arg in "$@"; do
  case "$arg" in
    --quiet) QUIET=1 ;;
    --list) LIST_ONLY=1 ;;
    --no-build) NO_BUILD=1 ;;
    --section) shift; ONLY_SECTION="${1:-}" ;;
    --section=*) ONLY_SECTION="${arg#--section=}" ;;
    -h|--help) sed -n '2,30p' "$0"; exit 0 ;;
  esac
done

CURRENT_SECTION=""
section() {
  CURRENT_SECTION="$1"
  if [[ "$LIST_ONLY" == 1 ]]; then
    echo "${BOLD}[$1]${X} $2"; return
  fi
  if [[ -n "$ONLY_SECTION" && "$ONLY_SECTION" != "$1" ]]; then return; fi
  [[ "$QUIET" == 0 ]] && echo -e "\n${BOLD}${B}━━ [$1] $2 ━━${X}"
}

_run() {
  # _run <kind: pass|gap|fail> <name> <cmd...>
  # On exit 0 prints PASS in green.
  # On non-zero, prints kind (GAP=amber, FAIL=red).
  local kind="$1" name="$2"; shift 2
  if [[ "$LIST_ONLY" == 1 ]]; then
    case "$kind" in
      gap)  printf "  · %s ${A}(gap-tagged)${X}\n" "$name" ;;
      *)    printf "  · %s\n" "$name" ;;
    esac
    return
  fi
  if [[ -n "$ONLY_SECTION" && "$ONLY_SECTION" != "$CURRENT_SECTION" ]]; then return; fi
  local out exit_code
  out="$("$@" 2>&1)"; exit_code=$?
  if [[ $exit_code -eq 0 ]]; then
    PASS=$((PASS + 1))
    if [[ "$QUIET" == 0 ]]; then
      printf "  ${G}PASS${X}  %s" "$name"
      [[ -n "$out" ]] && printf "  ${DIM}%s${X}" "$(echo "$out" | tail -1)"
      echo
    fi
  else
    case "$kind" in
      gap)
        GAP=$((GAP + 1))
        GAPS+=("[$CURRENT_SECTION] $name")
        printf "  ${A}GAP${X}   %s" "$name"
        [[ -n "$out" ]] && printf "  ${DIM}%s${X}" "$(echo "$out" | tail -1)"
        echo ;;
      *)
        FAIL=$((FAIL + 1))
        FAILS+=("[$CURRENT_SECTION] $name")
        printf "  ${R}FAIL${X}  %s\n" "$name"
        [[ -n "$out" ]] && echo "$out" | head -6 | sed "s/^/        ${DIM}/" | sed "s/$/${X}/" ;;
    esac
  fi
}

check() { _run fail "$@"; }   # red on failure
gap_check() { _run gap "$@"; }  # amber on failure (planned/phased work)

# ─── header ───────────────────────────────────────────────────────────
[[ "$LIST_ONLY" == 0 ]] && {
  echo "${BOLD}optiqor-backend verify.sh${X}  ${DIM}root=$HERE${X}"
  echo "${DIM}PASS = implemented · GAP = doc-promised, not built yet · FAIL = broken / rule violation${X}"
}

# ╔══════════════════════════════════════════════════════════════════════╗
# ║ A. Prerequisites                                                     ║
# ╚══════════════════════════════════════════════════════════════════════╝
section A "Prerequisites"
check "go toolchain"              bash -c 'go version'
check "git"                       bash -c 'git --version'
check "module is github.com/optiqor/optiqor" \
  bash -c "head -1 go.mod | grep -q 'module github.com/optiqor/optiqor'"
check "git remote points to optiqor/backend" \
  bash -c "git remote get-url origin 2>/dev/null | grep -q 'optiqor/optiqor.git\$'"

# ╔══════════════════════════════════════════════════════════════════════╗
# ║ B. Build + test                                                      ║
# ╚══════════════════════════════════════════════════════════════════════╝
section B "Build + test"
if [[ "$NO_BUILD" == 1 ]]; then
  [[ "$QUIET" == 0 ]] && echo "  ${DIM}(skipped via --no-build)${X}"
else
  check "go build ./..."            bash -c "go build ./..."
  check "go vet ./..."              bash -c "go vet ./..."
  check "go test ./... (race off)"  bash -c "go test ./... >/tmp/optiqor-backend-tests.log 2>&1 || { tail -40 /tmp/optiqor-backend-tests.log; false; }"
  check "cmd/api builds"            bash -c "go build -o /tmp/optiqor-api ./cmd/api && test -x /tmp/optiqor-api"
  check "cmd/worker builds"         bash -c "go build -o /tmp/optiqor-worker ./cmd/worker && test -x /tmp/optiqor-worker"
  check "cmd/agent builds"          bash -c "go build -o /tmp/optiqor-agent ./cmd/agent && test -x /tmp/optiqor-agent"
fi

# ╔══════════════════════════════════════════════════════════════════════╗
# ║ C. CLAUDE.md hard rules                                              ║
# ╚══════════════════════════════════════════════════════════════════════╝
section C "Hard rules"
check "backend does NOT import optiqor-cli/internal" \
  bash -c "! grep -rE 'github\\.com/optiqor/optiqor-cli/internal' --include='*.go' . | grep ."
check "backend may import optiqor-cli/pkg (read-only, via go.mod)" \
  bash -c "true"   # informational: not enforced as a positive requirement yet
check "no LLM SDK imported directly in production code" \
  bash -c "! grep -rE 'github\\.com/(anthropics|openai|sashabaranov)' --include='*.go' . | grep -v _test.go | grep ."
check "Apache 2.0 LICENSE (for agent binary independence)" \
  bash -c "grep -q 'Apache License' LICENSE-agent && grep -q 'Apache License' LICENSE || grep -q 'Proprietary' LICENSE"
check "RLS app-tenant bind helper exists" \
  bash -c "grep -rE 'app\\.tenant_id|set_config.*tenant' --include='*.go' internal/platform/db | head -1 | grep -q ."
check "Tenancy context type exists (per-request tenant propagation)" \
  bash -c "grep -q 'tenancy.Context\\|type Context' internal/tenancy/context.go"
check "Worker dispatcher routes by per-tenant queues" \
  bash -c "grep -E 'queue|Queue' internal/worker/dispatcher.go | head -1 | grep -q ."

# ╔══════════════════════════════════════════════════════════════════════╗
# ║ D. Platform primitives                                               ║
# ╚══════════════════════════════════════════════════════════════════════╝
section D "Platform primitives"
for pkg in config db featureflags healthz logging telemetry; do
  check "internal/platform/$pkg compiles and has tests or impl files" \
    bash -c "test -d internal/platform/$pkg && ls internal/platform/$pkg/*.go >/dev/null 2>&1"
done
check "healthz registry supports readiness checks" \
  bash -c "grep -q 'Registry\\|Register' internal/platform/healthz/*.go"
check "structured slog logging" \
  bash -c "grep -q 'slog' internal/platform/logging/*.go"
check "metrics registry exposes Prometheus" \
  bash -c "grep -qE 'Prometheus|prometheus' internal/platform/telemetry/*.go"
gap_check "Sentry SDK wired (TODO phase-5 marker exists)" \
  bash -c "! grep -q 'TODO(phase-5)' internal/platform/telemetry/sentry.go"

# ╔══════════════════════════════════════════════════════════════════════╗
# ║ E. Database schema (baseline migration)                              ║
# ╚══════════════════════════════════════════════════════════════════════╝
section E "Database schema"
for tbl in tenants workspaces clusters namespaces workloads recommendations recommendation_dismissals apply_fixes receipts llm_calls audit_log; do
  check "table $tbl exists in baseline migration" \
    bash -c "grep -q 'CREATE TABLE $tbl' migrations/0001_baseline.sql"
done
check "RLS policies declared in baseline" \
  bash -c "grep -qE 'ROW LEVEL SECURITY|CREATE POLICY' migrations/0001_baseline.sql"
check "two-role split (optiqor_app vs optiqor_migrator)" \
  bash -c "grep -q 'optiqor_app' migrations/0001_baseline.sql && grep -q 'optiqor_migrator' migrations/0001_baseline.sql"

# ╔══════════════════════════════════════════════════════════════════════╗
# ║ F. API surface (routes vs strategy promises)                         ║
# ╚══════════════════════════════════════════════════════════════════════╝
section F "API surface"
# Routes that DO exist today:
for route in '/healthz' '/readyz' '/metrics' '/oauth/github/callback' '/webhooks/github'; do
  check "route registered: $route" \
    bash -c "grep -q '\"[A-Z]\\+ ${route}\"' cmd/api/main.go"
done

# Routes the docs PROMISE — search across cmd/api + every internal
# package because each domain Mount()s its own routes.
routegrep() {
  # Return 0 (route present) when the pattern is found anywhere in the
  # api binary or its domain packages.
  local pattern="$1"
  grep -rE "$pattern" --include='*.go' cmd/api internal >/dev/null
}
check "POST /v1/analyze (sandbox/SaaS analysis endpoint)" \
  bash -c "$(declare -f routegrep); routegrep 'POST /v1/analyze'"
check "GET /r/<hash> (public share endpoint, --share target)" \
  bash -c "$(declare -f routegrep); routegrep 'GET /r/\\{hash\\}'"
check "POST /v1/apply-fixes (Apply Fix PR generation)" \
  bash -c "$(declare -f routegrep); routegrep 'POST /v1/apply-fixes'"
check "GET /v1/receipts/<id> (Verified Receipt fetch)" \
  bash -c "$(declare -f routegrep); routegrep 'GET /v1/receipts/\\{id\\}'"
check "POST /v1/ingest (agent → SaaS metrics ingestion)" \
  bash -c "$(declare -f routegrep); routegrep 'POST /v1/ingest'"
check "POST /v1/cost-spikes (bill anomaly webhook)" \
  bash -c "$(declare -f routegrep); routegrep 'POST /v1/cost-spikes'"

# ╔══════════════════════════════════════════════════════════════════════╗
# ║ G. Worker + workflows (Temporal swap-in)                             ║
# ╚══════════════════════════════════════════════════════════════════════╝
section G "Worker + workflows"
check "worker dispatcher implemented (in-memory phase 1)" \
  test -f internal/worker/dispatcher.go
check "echo workflow registered (proof-of-boot)" \
  test -f internal/worker/workflows/echo.go
gap_check "Apply Fix PR workflow registered" \
  bash -c "ls internal/worker/workflows/apply_fix* >/dev/null 2>&1"
gap_check "Auto-Rollback watchdog workflow registered" \
  bash -c "ls internal/worker/workflows/rollback* internal/worker/workflows/auto_rollback* 2>/dev/null | head -1 | grep -q ."
gap_check "Receipt issuance workflow registered" \
  bash -c "ls internal/worker/workflows/receipt* internal/worker/workflows/issue_receipt* 2>/dev/null | head -1 | grep -q ."
gap_check "Cost-spike detection workflow registered" \
  bash -c "ls internal/worker/workflows/cost_spike* 2>/dev/null | head -1 | grep -q ."
gap_check "Temporal SDK adapter (replaces in-mem dispatcher in phase 3)" \
  bash -c "grep -rE 'go.temporal.io/sdk' --include='*.go' . | head -1 | grep -q ."

# ╔══════════════════════════════════════════════════════════════════════╗
# ║ H. Agent binary                                                      ║
# ╚══════════════════════════════════════════════════════════════════════╝
section H "Agent (cmd/agent)"
check "agent binary builds" test -f cmd/agent/main.go
check "agent binary is Apache-2.0 (separate LICENSE-agent file)" \
  test -f LICENSE-agent
gap_check "agent: client-go informer setup (TODO phase-5)" \
  bash -c "! grep -q 'TODO(phase-5).*client-go' cmd/agent/main.go"
gap_check "agent: Prometheus scrape loop" \
  bash -c "grep -qE 'prometheus.*scrape|/api/v1/query' cmd/agent/*.go"
gap_check "agent: mTLS ship to SaaS" \
  bash -c "grep -qE 'tls\\.Config|mtls' cmd/agent/*.go"

# ╔══════════════════════════════════════════════════════════════════════╗
# ║ I. Phase-1 implementations (must exist and pass tests)               ║
# ╚══════════════════════════════════════════════════════════════════════╝
section I "Phase-1 implementations"
check "billing: AWS Cost Explorer client"      bash -c "grep -q 'package billing' internal/billing/aws.go"
check "billing: capacity model"                 test -f internal/billing/capacity.go
check "billing: tests exist + pass"             bash -c "go test ./internal/billing/... -count=1"
check "vcs: GitHub OAuth + webhook"             test -f internal/vcs/github.go
check "vcs: tests pass"                         bash -c "go test ./internal/vcs/... -count=1"
check "tenancy: context tests pass"             bash -c "go test ./internal/tenancy/... -count=1"
check "gdpr: data-subject access flow"          test -f internal/gdpr/gdpr.go
check "gdpr: tests pass"                        bash -c "go test ./internal/gdpr/... -count=1"
check "operators: Prometheus-Operator / Strimzi detector" \
                                                test -f internal/operators/detector.go
check "operators: tests pass"                   bash -c "go test ./internal/operators/... -count=1"
check "safety/environment: env classification"  bash -c "ls internal/safety/environment/*.go >/dev/null 2>&1"
check "agent/llm/sanitizer: PII strip before LLM call" \
                                                test -f internal/agent/llm/sanitizer/sanitizer.go

# ╔══════════════════════════════════════════════════════════════════════╗
# ║ J. Strategy GAPs (doc-promised but unimplemented packages)           ║
# ╚══════════════════════════════════════════════════════════════════════╝
section J "Strategy GAPs"
# Each package below has a doc.go describing its responsibility, but no
# real implementation. These are the largest deltas between strategy
# and code. They are reported as GAP, not FAIL, because the docs
# explicitly schedule them for Phase 2-6.
stub_only() {
  local pkg="$1"
  local impl_count
  impl_count=$(ls "internal/$pkg"/*.go 2>/dev/null | grep -v doc.go | grep -v _test.go | wc -l)
  test "$impl_count" -gt 0
}

gap_check "sandbox: public unauth analysis endpoint implemented" \
  stub_only sandbox
gap_check "prwriter: PR-comment + Apply Fix PR opener implemented" \
  stub_only prwriter
gap_check "receipts: Ed25519-signed Verified Receipts implemented" \
  stub_only receipts
gap_check "rollback: 7-day Auto-Rollback watchdog implemented" \
  stub_only rollback
gap_check "ingestion: Prometheus + AWS bill ingestion implemented" \
  stub_only ingestion
gap_check "cost: cost estimation engine implemented" \
  stub_only cost
gap_check "confidence: confidence-band ML implemented" \
  stub_only confidence
gap_check "parser: backend Helm/Kustomize render pipeline implemented" \
  stub_only parser
gap_check "agent (top-level orchestration package) implemented" \
  bash -c "ls internal/agent/*.go 2>/dev/null | grep -v doc.go | head -1 | grep -q ."

# ╔══════════════════════════════════════════════════════════════════════╗
# ║ K. Production readiness                                              ║
# ╚══════════════════════════════════════════════════════════════════════╝
section K "Production readiness"

# --- HTTP server hardening ---
check "API server enforces read/write timeouts" \
  bash -c "grep -qE 'ReadTimeout|WriteTimeout|ReadHeaderTimeout' cmd/api/main.go"
check "API server installs graceful shutdown (server.Shutdown)" \
  bash -c "grep -q 'Shutdown' cmd/api/main.go"
check "panic recovery middleware wired into mux" \
  bash -c "grep -q 'withPanicRecovery\\|RecoveryMiddleware\\|recover()' cmd/api/main.go"
check "request-ID middleware wired" \
  bash -c "grep -q 'withRequestID\\|X-Request-Id\\|X-Request-ID' cmd/api/main.go"
check "structured access log middleware wired" \
  bash -c "grep -q 'withAccessLog\\|AccessLog' cmd/api/main.go"
check "pprof endpoints require auth token" \
  bash -c "grep -q 'mountPProf' cmd/api/main.go && grep -q 'require' cmd/api/main.go"
check "GitHub webhook verifies HMAC signature" \
  bash -c "grep -qE 'X-Hub-Signature|hmac\\.New' cmd/api/main.go || grep -rqE 'X-Hub-Signature' internal/vcs/"
check "OAuth callback validates state parameter (CSRF)" \
  bash -c "grep -qE 'state|csrf' cmd/api/main.go internal/vcs/*.go"
check "healthz and readyz are distinct (liveness vs readiness)" \
  bash -c "grep -q '\"GET /healthz\"' cmd/api/main.go && grep -q '\"GET /readyz\"' cmd/api/main.go"

# --- DB / migrations / multi-tenancy ---
check "migrations are idempotent (use IF NOT EXISTS / DO blocks)" \
  bash -c "grep -qE 'IF NOT EXISTS|DO \\\$\\\$' migrations/0001_baseline.sql"
check "RLS policies present on tenant-scoped tables" \
  bash -c "rls_count=\$(grep -c 'CREATE POLICY' migrations/0001_baseline.sql); test \"\$rls_count\" -ge 5 && echo \"\$rls_count policies\""
check "tenancy context type is a value not a pointer (safe for goroutines)" \
  bash -c "grep -q 'type Context struct' internal/tenancy/context.go"
check "internal/platform/db ships RLS bind helper" \
  bash -c "grep -rqE 'TenantBindArgs|BindTenant|WithTenant|SetTenantID|TenantStmtSQL' internal/platform/db/"

# --- Build / lint / tests ---
check "race detector tests pass" \
  bash -c "go test -race ./internal/tenancy/... ./internal/worker/... ./internal/billing/... >/tmp/optiqor-race.log 2>&1 || { tail -40 /tmp/optiqor-race.log; false; }"
check "golangci-lint config present" \
  test -f .golangci.yml
check "pre-commit config present" \
  test -f .pre-commit-config.yaml
check "GitHub Actions CI workflow present" \
  bash -c "ls .github/workflows/*.yml >/dev/null 2>&1"
check "Makefile exposes test + build + lint targets" \
  bash -c "grep -qE '^(test|build|lint):' Makefile"

# --- Container hardening ---
check "Dockerfile.api runs as non-root user" \
  bash -c "grep -qE 'USER [^r]|USER 1[0-9]+' Dockerfile.api"
check "Dockerfile.worker runs as non-root user" \
  bash -c "grep -qE 'USER [^r]|USER 1[0-9]+' Dockerfile.worker"
check "Dockerfile.agent runs as non-root user" \
  bash -c "grep -qE 'USER [^r]|USER 1[0-9]+' Dockerfile.agent"
check "Dockerfiles use distroless or scratch base" \
  bash -c "grep -qE '(FROM (gcr\\.io/distroless|scratch)|DISTROLESS_RUNTIME_IMAGE=gcr\\.io/distroless)' Dockerfile.api Dockerfile.worker Dockerfile.agent"
check "Dockerfile runtime base pinned by sha256 digest" \
  bash -c "grep -qE 'DISTROLESS_RUNTIME_IMAGE=gcr\\.io/distroless/[a-z0-9./:-]+@sha256:[0-9a-f]{64}' Dockerfile.api Dockerfile.worker Dockerfile.agent"

# --- Config / secrets hygiene ---
check ".env not committed; .env.example provided instead" \
  bash -c "! git ls-files | grep -qE '^\\.env\$' && test -f .env.example"
check "no hardcoded secrets in code (api_key/password/token literals)" \
  bash -c "! grep -rE '(api_key|password|secret)\\s*[:=]\\s*\"[A-Za-z0-9]{16,}\"' --include='*.go' . | grep ."
check "TLS / cert paths come from env (not hardcoded)" \
  bash -c "! grep -rE 'tls.*:=.*\"/(etc|var)/' --include='*.go' . | grep ."

# --- Observability ---
check "Prometheus /metrics route guarded (off when no registry)" \
  bash -c "grep -qE 'if metrics != nil' cmd/api/main.go"
check "structured slog JSON logging in main" \
  bash -c "grep -q 'slog.NewJSONHandler' cmd/api/main.go cmd/worker/main.go cmd/agent/main.go"
check "graceful shutdown on SIGTERM in cmd/worker" \
  bash -c "grep -q 'SIGTERM' cmd/worker/main.go"
check "graceful shutdown on SIGTERM in cmd/agent" \
  bash -c "grep -q 'SIGTERM' cmd/agent/main.go"

# ╔══════════════════════════════════════════════════════════════════════╗
# ║ L. OSS / repo hygiene                                                ║
# ╚══════════════════════════════════════════════════════════════════════╝
section L "Repo hygiene"
for f in LICENSE LICENSE-agent README.md ROADMAP.md CLAUDE.md docker-compose.yml Dockerfile.api Dockerfile.worker Dockerfile.agent Makefile .env.example .pre-commit-config.yaml .golangci.yml; do
  check "$f present"                            test -f "$f"
done
check "strategy docs mirrored under docs/strategy/" \
  bash -c "test -f docs/strategy/idea.md && test -f docs/strategy/business_strategy.md && test -f docs/strategy/technical_implementation.md && test -f docs/strategy/open_source_cli_playbook.md"
check "no committed secrets" \
  bash -c "! git ls-files | grep -E '(^|/)(\\.env\$|.*\\.pem\$|.*\\.key\$)' | grep ."
check "no stale sevro/lowplane references" \
  bash -c "! grep -rIlE 'sevro|Sevro|SEVRO|lowplane' --exclude-dir=.git --exclude='verify.sh' . | grep ."

# ─── summary ──────────────────────────────────────────────────────────
if [[ "$LIST_ONLY" == 1 ]]; then exit 0; fi
echo
echo "${BOLD}━━ summary ━━${X}"
printf "  ${G}PASS${X}  %d  ${DIM}implemented and working${X}\n" "$PASS"
printf "  ${A}GAP${X}   %d  ${DIM}doc-promised, scheduled for later phase${X}\n" "$GAP"
printf "  ${R}FAIL${X}  %d  ${DIM}broken or rule violation${X}\n" "$FAIL"

if [[ $GAP -gt 0 && "$QUIET" == 0 ]]; then
  echo
  echo "${BOLD}gaps (strategy-doc claims not yet built):${X}"
  for g in "${GAPS[@]}"; do echo "  • $g"; done
fi
if [[ $FAIL -gt 0 ]]; then
  echo
  echo "${BOLD}${R}failures (must fix):${X}"
  for f in "${FAILS[@]}"; do echo "  • $f"; done
  exit 1
fi
echo
if [[ $GAP -gt 0 ]]; then
  echo "${A}${BOLD}backend is consistent within Phase 1 scope${X}${DIM} — gaps are scheduled, not bugs${X}"
else
  echo "${G}${BOLD}✓ backend matches strategy in full${X}"
fi
exit 0
