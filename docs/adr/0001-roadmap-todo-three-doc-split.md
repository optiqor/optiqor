# ADR 0001: Three-doc roadmap split (org / optiqor-roadmap / optiqor-todo)

- **Status:** Accepted
- **Date:** 2026-05-17
- **Authors:** @shivam

## Context

Roadmap state currently lives in three files with overlapping content:

| File | Lines | Scope |
| --- | --- | --- |
| `/todo.md` | 1023 | Org-level Year-1 roadmap; cross-repo phase view + Year-1 GTM milestones |
| `/optiqor/ROADMAP.md` | 944 | Backend-repo standalone view of the org arc: Year-1 detail + Year 2–6 themes |
| `/optiqor/todo.md` | 504 | Backend engineering tracker; sprint-grain checklists for the current phase |

An external audit (2026-05-17) initially flagged this as drift risk: "ROADMAP mirrors todo verbatim" and recommended a sync script. Inspection showed the files are *not* identical mirrors — they have intentionally different scopes (org-vs-optiqor-vs-sprint, Year 1 detail vs Year 2–6 themes). The risk is real but the fix is documentation + a narrow sanity check, not unification.

## Decision

We keep all three documents. We commit to the following invariants:

1. **Single source of truth per concern.**
   - Year-1 phase names + week ranges + exit criteria: `/todo.md` is canonical. The other two reference (don't duplicate) the canonical definitions.
   - Year 2–6 themes + named milestones: `/optiqor/ROADMAP.md` is canonical (so the backend repo can be cloned standalone and still ship the full arc).
   - In-flight engineering checklists for the active backend phase: `/optiqor/todo.md` is canonical.
2. **Phase names are stable strings.** "Phase 1 — Foundation", "Phase 2 — Public Sandbox", … never get reworded mid-flight. Renaming a phase requires updating all three files in the same commit.
3. **Sanity check in CI.** `optiqor/scripts/check-roadmap-sync.sh` asserts every `Phase N — <title>` header that appears in `/todo.md` also appears verbatim in `/optiqor/ROADMAP.md` and `/optiqor/todo.md` (when that phase is in scope for the optiqor tracker). The script is wired into `make lint`.
4. **Strategy docs stay byte-mirrored.** `/docs/*.md` ↔ `/optiqor/docs/strategy/*.md` are byte-identical copies (root is canonical). This is enforced separately by the existing CI gate; no change.

## Consequences

**Positive:**
- Each audience reads the one doc that matches their lens (founder / engineer cloning backend standalone / engineer working a sprint).
- Phase-name drift is detected before it ships.
- No content duplication — the three docs reference each other for shared facts.

**Negative:**
- Three places to update when a Year-1 phase definition genuinely changes (one of them is the script's job to catch).
- The Year-1 GTM milestones appear in both `/todo.md` and `/optiqor/ROADMAP.md` (different framings). Drift here is not caught by the sanity check; flagged for future ADR if it becomes a problem.

## Alternatives considered

- **Make `ROADMAP.md` a symlink to `todo.md`.** Rejected: they have intentionally different scopes (org-wide arc vs Year-1 sprint tracker), and symlinks don't render correctly on GitHub.
- **Generate `ROADMAP.md` from `todo.md` + a Years 2–6 fragment.** Rejected: build tooling overhead for a doc that changes monthly at most. Revisit if drift becomes painful.
- **Collapse `/todo.md` into `/optiqor/ROADMAP.md` and link from root.** Rejected: the org root is the natural home for the cross-repo Year-1 view; pushing it into `backend/` privileges one repo over the OSS CLI repo.

## Links

- Audit (2026-05-17) that surfaced this — see commit history of this ADR.
- `optiqor/scripts/check-roadmap-sync.sh` — the sanity check.
- `/CLAUDE.md` "Roadmap" section — points readers at the right file for their need.
