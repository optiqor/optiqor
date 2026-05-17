#!/usr/bin/env bash
# Asserts every "Phase N — <title>" header in the org-level /todo.md
# also appears verbatim in optiqor/ROADMAP.md and optiqor/todo.md
# (when the phase is in scope for the optiqor tracker).
#
# Wired into `make lint`. See docs/adr/0001-roadmap-todo-three-doc-split.md.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OPTIQOR_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
ORG_TODO="${OPTIQOR_DIR}/../todo.md"
OPTIQOR_ROADMAP="${OPTIQOR_DIR}/ROADMAP.md"
OPTIQOR_TODO="${OPTIQOR_DIR}/todo.md"

for f in "${ORG_TODO}" "${OPTIQOR_ROADMAP}" "${OPTIQOR_TODO}"; do
    if [[ ! -f "${f}" ]]; then
        echo "check-roadmap-sync: missing required file: ${f}" >&2
        exit 2
    fi
done

# Phase headers look like: "### Phase 3 — Weeks 5–6: Detectors + LLM Diff + CLI v0.1"
# We extract the "Phase N — " prefix as the stable key and the full
# header line as the value the other docs must contain verbatim.
mapfile -t org_phase_headers < <(grep -E '^#+ Phase [0-9]+ — ' "${ORG_TODO}" || true)

if [[ ${#org_phase_headers[@]} -eq 0 ]]; then
    echo "check-roadmap-sync: no Phase headers found in ${ORG_TODO}" >&2
    exit 2
fi

errors=0
for header in "${org_phase_headers[@]}"; do
    # Strip leading "### " / "## " etc. to get the body of the header.
    body="${header#"${header%%[!#]*}"}"
    body="${body# }"

    # Backend ROADMAP must contain the same Phase header body.
    if ! grep -Fq "${body}" "${OPTIQOR_ROADMAP}"; then
        echo "drift: ${OPTIQOR_ROADMAP} missing phase header: ${body}" >&2
        errors=$((errors + 1))
    fi

    # Backend todo is the sprint tracker — only phases currently in
    # in scope for optiqor tracker. We check it has at least the phase
    # *number* if not the full title.
    phase_prefix="${body%% — *}"
    if ! grep -Fq "${phase_prefix}" "${OPTIQOR_TODO}"; then
        echo "drift: ${OPTIQOR_TODO} missing phase reference: ${phase_prefix}" >&2
        errors=$((errors + 1))
    fi
done

if [[ ${errors} -gt 0 ]]; then
    echo "" >&2
    echo "check-roadmap-sync: ${errors} drift(s) found." >&2
    echo "See docs/adr/0001-roadmap-todo-three-doc-split.md for the contract." >&2
    exit 1
fi

echo "check-roadmap-sync: OK (${#org_phase_headers[@]} phases consistent)"
