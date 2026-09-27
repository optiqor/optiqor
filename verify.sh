#!/usr/bin/env bash
# verify.sh — Optiqor CLI reality check.
#
# Audits the optiqor-cli repo against:
#   ../docs/idea.md
#   ../docs/business_strategy.md
#   ../docs/technical_implementation.md
#   ../docs/open_source_cli_playbook.md
#   CLAUDE.md
#
# Three result types:
#
#   PASS — implemented and working today
#   GAP  — strategy / README commits to this; not implemented yet
#          Counted separately; does NOT fail the script.
#   FAIL — implemented incorrectly, or a hard rule is violated.
#          Fails the script (exit 1).
#
# Main goal: surface every place where shipped behaviour diverges from
# what the docs / CLAUDE.md / README promise. Run before every release
# tag.
#
# Usage:
#   ./verify.sh                 # full run
#   ./verify.sh --quiet         # only print FAIL + GAP + summary
#   ./verify.sh --section F     # one section
#   ./verify.sh --list          # list checks without running
#   ./verify.sh --no-build      # skip the build/test section
#   ./verify.sh --no-network    # implied; we never call the network
#
set -u
set -o pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$HERE"
BIN="$HERE/bin/optiqor"
FIXTURE="$HERE/testdata/fixtures/basic-chart"

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
    --no-network) : ;;
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
  local kind="$1" name="$2"; shift 2
  if [[ "$LIST_ONLY" == 1 ]]; then
    case "$kind" in
      gap) printf "  · %s ${A}(gap-tagged)${X}\n" "$name" ;;
      *)   printf "  · %s\n" "$name" ;;
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
check() { _run fail "$@"; }
gap_check() { _run gap "$@"; }

# ─── header ───────────────────────────────────────────────────────────
[[ "$LIST_ONLY" == 0 ]] && {
  echo "${BOLD}optiqor-cli verify.sh${X}  ${DIM}root=$HERE${X}"
  echo "${DIM}PASS = implemented · GAP = doc-promised, not built yet · FAIL = broken / rule violation${X}"
}

# ╔══════════════════════════════════════════════════════════════════════╗
# ║ A. Prerequisites                                                     ║
# ╚══════════════════════════════════════════════════════════════════════╝
section A "Prerequisites"
check "go toolchain"   bash -c 'go version'
check "jq present"     bash -c 'jq --version'
check "git present"    bash -c 'git --version'
check "module path is github.com/optiqor/optiqor-cli" \
  bash -c "head -1 go.mod | grep -q 'module github.com/optiqor/optiqor-cli'"
check "git remote points to optiqor/optiqor-cli" \
  bash -c "git remote get-url origin | grep -q 'optiqor/optiqor-cli.git\$'"
check "fixture chart present (used by every analysis check)" \
  test -d "$FIXTURE"

# ╔══════════════════════════════════════════════════════════════════════╗
# ║ B. Build + test                                                      ║
# ╚══════════════════════════════════════════════════════════════════════╝
section B "Build + test"
if [[ "$NO_BUILD" == 1 ]]; then
  [[ "$QUIET" == 0 ]] && echo "  ${DIM}(skipped via --no-build)${X}"
else
  check "go build ./..."   bash -c "go build ./..."
  check "go vet ./..."     bash -c "go vet ./..."
  check "go test ./..."    bash -c "go test ./... >/tmp/optiqor-cli-tests.log 2>&1 || { tail -40 /tmp/optiqor-cli-tests.log; false; }"
  check "produce bin/optiqor" \
    bash -c "go build -o bin/optiqor ./cmd/optiqor && test -x bin/optiqor"
  check "race detector clean" \
    bash -c "go test -race ./... >/tmp/optiqor-cli-race.log 2>&1 || { tail -40 /tmp/optiqor-cli-race.log; false; }"
fi

# ╔══════════════════════════════════════════════════════════════════════╗
# ║ C. CLAUDE.md hard rules                                              ║
# ╚══════════════════════════════════════════════════════════════════════╝
section C "Hard rules"
check "no LLM SDK imports (anthropic/openai/sashabaranov)" \
  bash -c "! grep -rE 'github\\.com/(anthropics|openai|sashabaranov)' --include='*.go' --include='go.mod' --exclude-dir=backend . | grep -v _test.go | grep ."
check "no SCM SDK imports (go-github/go-gitlab/go-gitea)" \
  bash -c "! grep -rE 'github\\.com/(google/go-github|xanzy/go-gitlab|google/go-gitea)' --include='*.go' --include='go.mod' --exclude-dir=backend . | grep ."
check "no backend import (go.mod)" \
  bash -c "! grep -qE 'github\\.com/optiqor/optiqor( |/|$)' go.mod go.sum"
check "no backend import (source)" \
  bash -c "! grep -rE 'github\\.com/optiqor/optiqor/' --include='*.go' --exclude-dir=backend . | grep ."
check "no daemon / persistent listener (net.Listen)" \
  bash -c "! grep -rE 'net\\.Listen|http\\.ListenAndServe' --include='*.go' --exclude-dir=backend . | grep -v _test.go | grep ."
check "Apache 2.0 license in LICENSE" \
  bash -c "grep -q 'Apache License' LICENSE"
check "pkg/rules is a public Go package" \
  bash -c "test -f pkg/rules/types.go && head -10 pkg/rules/types.go | grep -q '^package rules'"
check "pkg/parser is a public Go package" \
  bash -c "test -f pkg/parser/helm.go && head -10 pkg/parser/helm.go | grep -q '^package parser'"
check "internal/ visibility enforced by the compiler (build succeeds)" \
  bash -c "go list -deps ./... >/dev/null"
check "no Windows-specific code paths (per playbook)" \
  bash -c "! grep -rE '//go:build windows|GOOS *= *\"windows\"' --include='*.go' --exclude-dir=backend . | grep ."

# ╔══════════════════════════════════════════════════════════════════════╗
# ║ D. Brand + identity                                                  ║
# ╚══════════════════════════════════════════════════════════════════════╝
section D "Brand + identity"
check "npm package name @optiqor/cli" \
  bash -c "jq -e '.name == \"@optiqor/cli\"' package.json >/dev/null"
check "npm homepage = optiqor.dev" \
  bash -c "jq -e '.homepage == \"https://optiqor.dev\"' package.json >/dev/null"
check "npm bin maps to optiqor" \
  bash -c "jq -e '.bin.optiqor' package.json >/dev/null"
check "no stale sevro/lowplane references" \
  bash -c "! grep -rIlE 'sevro|Sevro|SEVRO|lowplane' --exclude-dir=.git --exclude-dir=backend --exclude='verify.sh' . | xargs -I{} grep -L 'Rebrand sevro' {} 2>/dev/null | grep ."
check "logo image present (referenced by README)" \
  test -f docs/commands/optiqor-hori.jpg
check "README references the logo image path" \
  bash -c "grep -q 'docs/commands/optiqor-hori' README.md"
check "README positions cost-first / security-bonus" \
  bash -c "grep -q 'cost optimization' README.md && grep -qi 'bonus' README.md"

# ╔══════════════════════════════════════════════════════════════════════╗
# ║ E. Year-1 command surface (playbook locks this in)                   ║
# ╚══════════════════════════════════════════════════════════════════════╝
section E "Year-1 command surface"
for cmd in analyze demo diff score audit watch compare; do
  check "command registered: $cmd" bash -c "'$BIN' $cmd --help >/dev/null 2>&1"
done
check "watch returns 'not yet implemented' stub" \
  bash -c "'$BIN' watch /tmp 2>&1 | grep -qi 'not yet implemented'"

# Flags claimed by README + CLAUDE.md
for flag in --json --no-color --severity --fail-on --detector --config --offline --share --roast; do
  check "flag advertised: $flag" \
    bash -c "'$BIN' analyze --help 2>&1 | grep -q -- '$flag'"
done

check "--offline defaults to true (zero-config never phones home)" \
  bash -c "'$BIN' analyze --help 2>&1 | grep -qE 'offline.*default.*true'"
check "--share is OFF by default" \
  bash -c "'$BIN' analyze --help 2>&1 | grep -q -- '--share' && ! '$BIN' analyze --help 2>&1 | grep -qE 'share.*default *true'"

# Persistent config (CLAUDE.md says flags+env vars; .optiqor.yaml is documented)
check ".optiqor.yaml loader respects OPTIQOR_CONFIG env var" \
  bash -c "grep -q 'OPTIQOR_CONFIG' internal/config/config.go"
check "NO_COLOR env var disables ANSI" \
  bash -c "out=\$(NO_COLOR=1 '$BIN' demo); ! printf '%s' \"\$out\" | grep -q \$'\\x1b'"

# ╔══════════════════════════════════════════════════════════════════════╗
# ║ F. Output contracts (the trust layer)                                ║
# ╚══════════════════════════════════════════════════════════════════════╝
section F "Output contracts"

check "demo runs without stdin"             bash -c "'$BIN' demo </dev/null >/dev/null"
check "demo output carries ±40% disclosure" bash -c "'$BIN' demo | grep -q '±40%'"
check "demo output carries agent CTA"       bash -c "'$BIN' demo | grep -q 'optiqor.dev/get'"
check "demo --no-color emits zero ANSI" \
  bash -c "out=\$('$BIN' demo --no-color); ! printf '%s' \"\$out\" | grep -q \$'\\x1b'"
check "demo --json is valid JSON" \
  bash -c "'$BIN' demo --json | jq . >/dev/null"
check "JSON carries the accuracy disclosure" \
  bash -c "'$BIN' demo --json | jq -e '.accuracy_disclosure | test(\"40%\")' >/dev/null"
check "JSON groups into cost_findings + security_findings_bonus" \
  bash -c "'$BIN' demo --json | jq -e '(.cost_findings|type==\"array\") and (.security_findings_bonus|type==\"array\")' >/dev/null"
check "JSON includes annual_savings_usd" \
  bash -c "'$BIN' demo --json | jq -e '.annual_savings_usd|type==\"number\"' >/dev/null"

# Cost-first ordering — the core re-design.
check "cost section appears BEFORE security section in text output" \
  bash -c "out=\$('$BIN' demo --no-color); c=\$(printf '%s\\n' \"\$out\" | grep -n 'Cost optimizations' | head -1 | cut -d: -f1); s=\$(printf '%s\\n' \"\$out\" | grep -n 'Security findings' | head -1 | cut -d: -f1); test -n \"\$c\" && test -n \"\$s\" && test \"\$c\" -lt \"\$s\""
check "security section labelled 'bonus'" \
  bash -c "'$BIN' demo --no-color | grep -qE 'Security findings.*bonus'"
check "header tagline contains 'cost optimization'" \
  bash -c "'$BIN' demo --no-color | grep -q 'cost optimization'"
check "biggest dollar savings finding leads the cost section" \
  bash -c "out=\$('$BIN' demo --no-color); first_sav=\$(printf '%s\\n' \"\$out\" | awk '/Cost optimizations/{p=1;next} p && /save ~\\\$/ {n=match(\$0,/[0-9]+\\.?[0-9]*/); print substr(\$0,RSTART,RLENGTH); exit}'); biggest=\$('$BIN' demo --json | jq -r '[.cost_findings[] | select(.MonthlyUSDCents>0)] | sort_by(-.MonthlyUSDCents) | .[0].MonthlyUSDCents/100'); test -n \"\$first_sav\" && awk -v a=\"\$first_sav\" -v b=\"\$biggest\" 'BEGIN{exit !(a+0 == b+0)}' && echo \"leads with \\\$\$first_sav\""

# Audit / score sub-commands
check "audit surfaces only security findings (and reports 'no cost waste')" \
  bash -c "out=\$('$BIN' audit '$FIXTURE' --no-color); echo \"\$out\" | grep -qE 'Security findings' && echo \"\$out\" | grep -qE 'no cost waste detected'"
check "score command produces a band/score line" \
  bash -c "'$BIN' score '$FIXTURE' --no-color 2>&1 | grep -qiE 'score|efficiency|grade'"
check "diff command runs on two values files" \
  bash -c "tmp1=\$(mktemp); tmp2=\$(mktemp); echo 'resources: {requests: {cpu: 1}}' > \$tmp1; echo 'resources: {requests: {cpu: 2}}' > \$tmp2; '$BIN' diff \$tmp1 \$tmp2 --no-color >/dev/null 2>&1; rc=\$?; rm -f \$tmp1 \$tmp2; test \"\$rc\" -eq 0"

# ╔══════════════════════════════════════════════════════════════════════╗
# ║ G. Detector library (30 detectors, two categories)                   ║
# ╚══════════════════════════════════════════════════════════════════════╝
section G "Detector library"
check "rules.Category type exported"          bash -c "grep -q 'type Category string' pkg/rules/types.go"
check "rules.CategoryCost / CategorySecurity constants exist" \
  bash -c "grep -q 'CategoryCost' pkg/rules/types.go && grep -q 'CategorySecurity' pkg/rules/types.go"
check "Detector interface requires Category()" \
  bash -c "awk '/type Detector interface/,/^}/' pkg/rules/types.go | grep -q 'Category()'"
check "All() registers exactly 30 detectors" \
  bash -c "n=\$(awk '/func All\\(\\)/,/^}/' pkg/rules/types.go | grep -cE '\\bnew[A-Z][a-zA-Z]+\\(\\)'); test \"\$n\" -eq 30 && echo \"\$n detectors\""
check "exactly 15 cost detectors declared in categories.go" \
  bash -c "n=\$(grep -cE 'Category\\(\\)[ ]*Category[ ]*\\{ return CategoryCost \\}' pkg/rules/categories.go); test \"\$n\" -eq 15 && echo \"\$n cost\""
check "exactly 15 security detectors declared in categories.go" \
  bash -c "n=\$(grep -cE 'Category\\(\\)[ ]*Category[ ]*\\{ return CategorySecurity \\}' pkg/rules/categories.go); test \"\$n\" -eq 15 && echo \"\$n security\""
check "every runtime finding carries a Category" \
  bash -c "'$BIN' demo --json | jq -e 'all(.findings[]; .Category==\"cost\" or .Category==\"security\")' >/dev/null"
check "detectors are deterministic (two runs produce identical findings)" \
  bash -c "a=\$('$BIN' demo --json | jq '.findings'); b=\$('$BIN' demo --json | jq '.findings'); [ \"\$a\" = \"\$b\" ]"
check "golden tests present for detectors" \
  bash -c "ls testdata/golden/*.txt >/dev/null"

# ╔══════════════════════════════════════════════════════════════════════╗
# ║ H. Exit codes (the CI-gating contract)                               ║
# ╚══════════════════════════════════════════════════════════════════════╝
section H "Exit codes"
check "exit 0 on successful demo run" \
  bash -c "'$BIN' demo >/dev/null"
check "exit 2 on invalid path" \
  bash -c "'$BIN' analyze /nonexistent/xyz123 >/dev/null 2>&1; test \"\$?\" -eq 2"
check "--fail-on=high → exit 1 when HIGH findings exist" \
  bash -c "'$BIN' analyze '$FIXTURE' --fail-on=high >/dev/null 2>&1; test \"\$?\" -eq 1"
check "--fail-on=high → exit 0 when threshold not met" \
  bash -c "'$BIN' analyze '$FIXTURE' --fail-on=high --severity=high --detector=memory-overprovisioned >/dev/null 2>&1; test \"\$?\" -eq 0"
check "--severity=high filters out lower severities" \
  bash -c "out=\$('$BIN' analyze '$FIXTURE' --no-color --severity=high); echo \"\$out\" | grep -q HIGH && ! echo \"\$out\" | grep -qE '^\\s*MED|^\\s*LOW'"
check "garbage input rejected with exit code, not panic" \
  bash -c "tmp=\$(mktemp); echo '::: NOT YAML :::' > \$tmp; '$BIN' analyze \$tmp >/dev/null 2>&1; rc=\$?; rm -f \$tmp; test \"\$rc\" -ge 2 && test \"\$rc\" -lt 100"

# ╔══════════════════════════════════════════════════════════════════════╗
# ║ I. Production readiness                                              ║
# ╚══════════════════════════════════════════════════════════════════════╝
section I "Production readiness"

# Distribution surface (playbook-mandated)
check "GoReleaser config present"           test -f .goreleaser.yaml
check "GoReleaser builds darwin/linux × amd64/arm64" \
  bash -c "grep -qE 'darwin' .goreleaser.yaml && grep -qE 'linux' .goreleaser.yaml && grep -qE 'arm64' .goreleaser.yaml && grep -qE 'amd64' .goreleaser.yaml"
gap_check "GoReleaser signs releases with cosign (CLAUDE.md release plan)" \
  bash -c "grep -qiE 'cosign|signs:' .goreleaser.yaml"
check "GitHub Actions CI workflow present"  test -f .github/workflows/ci.yml
check "CI runs go test"                     bash -c "grep -qE 'go test' .github/workflows/ci.yml"
check "CI runs golangci-lint or go vet"     bash -c "grep -qE 'golangci|go vet' .github/workflows/ci.yml"
check "golangci-lint config present"        test -f .golangci.yml
gap_check "pre-commit config present (nice-to-have, not blocking)" \
  test -f .pre-commit-config.yaml
check "Makefile exposes test + build targets" \
  bash -c "grep -qE '^(test|build):' Makefile"

# npm wrapper surface
check "npm postinstall script present"      test -f npm/postinstall.js
check "npm runtime entrypoint present"      test -f npm/index.js
check "npm postinstall is opt-out-able via env" \
  bash -c "grep -q 'OPTIQOR_SKIP_POSTINSTALL' npm/postinstall.js"

# Output stability + observability
check "binary embeds version flag" \
  bash -c "'$BIN' --version 2>&1 | grep -qE 'optiqor [0-9a-zA-Z.+_-]+'"
check "--help works for every registered command" \
  bash -c "for c in analyze demo diff score audit watch compare; do '$BIN' \$c --help >/dev/null || exit 1; done"
check "JSON output is line-stable (no map iteration randomness)" \
  bash -c "a=\$('$BIN' demo --json | jq -S .); b=\$('$BIN' demo --json | jq -S .); [ \"\$a\" = \"\$b\" ]"
check "stdout is the only thing humans read; errors go to stderr" \
  bash -c "out=\$('$BIN' analyze /nonexistent 2>/dev/null); test -z \"\$out\""
check "binary refuses to phone home by default (no http.Client in analyze path)" \
  bash -c "! grep -rE 'http\\.Get|http\\.Post|http\\.Client' --include='*.go' internal/analyze internal/render internal/config | grep ."

# Trust / supply chain
check "SECURITY.md publishes a disclosure email" \
  bash -c "grep -qE 'security@optiqor\\.dev|mailto:security' SECURITY.md"
check "LICENSE is Apache 2.0 (auditable for regulated buyers)" \
  bash -c "grep -q 'Apache License' LICENSE"
check ".gitignore prevents accidental .env / *.pem commits" \
  bash -c "grep -qE '^\\.env\$|^\\*\\.pem\$' .gitignore"
check "no committed secrets" \
  bash -c "! git ls-files | grep -E '(^|/)(\\.env\$|.*\\.pem\$|.*\\.key\$)' | grep ."

# Performance: demo should finish under ~3s on the playbook promise.
check "demo runs in under 3 seconds" \
  bash -c "start=\$(date +%s%N); '$BIN' demo --no-color >/dev/null; end=\$(date +%s%N); elapsed_ms=\$(( (end-start)/1000000 )); test \"\$elapsed_ms\" -lt 3000 && echo \"\${elapsed_ms}ms\""

# ╔══════════════════════════════════════════════════════════════════════╗
# ║ J. Strategy GAPs (README / playbook / roadmap claims not yet built)  ║
# ╚══════════════════════════════════════════════════════════════════════╝
section J "Strategy GAPs"

gap_check "--share actually uploads (Phase 2 — sandbox endpoint must be live)" \
  bash -c "! grep -q 'no upload in Phase 1' internal/share/share.go && ! grep -qE 'Phase 1 ships only the local hashing' internal/share/share.go"
gap_check "watch implemented (currently 'not yet implemented' stub)" \
  bash -c "! '$BIN' watch /tmp 2>&1 | grep -qi 'not yet implemented'"
gap_check "compare ships richer-than-diff output (currently an alias)" \
  bash -c "! '$BIN' compare --help 2>&1 | grep -qi 'currently a diff alias'"
gap_check "SARIF output (for GitHub Security tab integration)" \
  bash -c "'$BIN' analyze --help 2>&1 | grep -qi -- '--sarif'"
gap_check "fix / apply preview command (Apply Fix in CLI form)" \
  bash -c "'$BIN' fix --help >/dev/null 2>&1 || '$BIN' apply --help >/dev/null 2>&1"
gap_check "baseline / drift mode (--baseline flag)" \
  bash -c "'$BIN' analyze --help 2>&1 | grep -q -- '--baseline'"
gap_check "--explain <detector-id> introspection" \
  bash -c "'$BIN' --explain cpu-overprovisioned >/dev/null 2>&1 || '$BIN' analyze --explain cpu-overprovisioned >/dev/null 2>&1"
gap_check "pre-commit hook config shipped (.pre-commit-hooks.yaml)" \
  test -f .pre-commit-hooks.yaml

# ╔══════════════════════════════════════════════════════════════════════╗
# ║ K. Repo hygiene                                                      ║
# ╚══════════════════════════════════════════════════════════════════════╝
section K "Repo hygiene"
for f in LICENSE README.md CONTRIBUTING.md CODE_OF_CONDUCT.md SECURITY.md CLAUDE.md package.json package-lock.json Makefile .golangci.yml .goreleaser.yaml; do
  check "$f present"                          test -f "$f"
done
check "no stray binary blob commits (e.g. random *.bin)" \
  bash -c "! git ls-files | grep -E '\\.(bin|exe|tar|zip)\$' | grep -v 'cmd/.*/demo/' | grep ."

# ─── summary ──────────────────────────────────────────────────────────
if [[ "$LIST_ONLY" == 1 ]]; then exit 0; fi
echo
echo "${BOLD}━━ summary ━━${X}"
printf "  ${G}PASS${X}  %d  ${DIM}implemented and working${X}\n" "$PASS"
printf "  ${A}GAP${X}   %d  ${DIM}doc/README promise, not built yet${X}\n" "$GAP"
printf "  ${R}FAIL${X}  %d  ${DIM}broken or rule violation${X}\n" "$FAIL"
if [[ $GAP -gt 0 && "$QUIET" == 0 ]]; then
  echo
  echo "${BOLD}gaps:${X}"
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
  echo "${A}${BOLD}cli is consistent within Year-1 scope${X}${DIM} — gaps are roadmapped, not bugs${X}"
else
  echo "${G}${BOLD}✓ cli matches strategy in full${X}"
fi
exit 0
