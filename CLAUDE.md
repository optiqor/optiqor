# optiqor-cli — Claude Conventions

The open-source CLI: `@optiqor/cli`. Apache-2.0. The repo root is the CLI module; `backend/` holds the backend as a separate Go module with its own `CLAUDE.md` and licenses. The CLI must stay independently buildable and auditable, so nothing outside `backend/` may import backend code. Strategy reference: `docs/open_source_cli_playbook.md` in the Optiqor org docs (not public).

This file is the operating manual. Read it before writing or reviewing code.

---

## Stack

- Go 1.24, single module: `github.com/optiqor/optiqor-cli`
- Cobra for command parsing, charmbracelet/lipgloss for terminal rendering
- Pure stdlib for the rest. No HTTP framework, no logger library, no DI framework.
- npm wrapper (`@optiqor/cli`) downloads the platform-specific Go binary at install time
- GoReleaser for cross-platform builds (linux + darwin, amd64 + arm64)
- Cosign for release artifact signing
- SHA-256 verified on download (in progress, see open issues)

If you reach for a new dependency, justify it in the PR. Adding a transitive dep widens the audit surface of an Apache-2.0 binary that runs on customer laptops.

---

## Hard rules (non-negotiable)

These exist because the OSS funnel breaks if any of them slip. They are not preferences.

1. **No LLM calls.** The CLI is a deterministic rule engine. The Sonnet/Opus/Haiku-driven Apply Fix flow lives in the proprietary backend. If you find yourself wanting to call an LLM from here, the answer is "POST to the SaaS sandbox endpoint instead and let it return the LLM-enriched response."

2. **No telemetry by default.** Zero-config install must not phone home. The only opt-in network egress is `--share`, which POSTs a sanitised analysis to `optiqor.dev/r/<hash>`. Every new network call needs an explicit user opt-in and must consult `offlineMode()` in `cmd/optiqor/main.go`.

3. **Accuracy disclosure in every output.** Every renderer (text, JSON, HTML, roast) emits the mandatory disclosure: `Sandbox accuracy: ±40%. Install the Optiqor agent for exact numbers (optiqor.dev/get).` The string is canonical. The honesty is the whole pitch; don't water it down.

4. **No backend code may be imported.** The root `go.mod` must never reference `github.com/optiqor/optiqor` (the `backend/` module). The CLI is independently buildable, auditable, licensable. CI greps imports on every PR.

5. **`pkg/` is the stable public API.** External programs may import it, including the proprietary backend (which depends on `pkg/rules` and `pkg/parser`). Breaking changes go through semver and a deprecation notice. New detectors land in `pkg/rules` first; the backend picks them up immediately via `replace => ../`, and Backend CI runs on `pkg/**` changes.

6. **`internal/` is private.** Refactor freely. `internal/{analyze,render,share,config,roast}` is CLI-side composition that stays out of the public API.

---

## Distribution

- Primary: `npm install -g @optiqor/cli` or `npx @optiqor/cli analyze ./chart`. The npm `postinstall` script downloads the right binary from GitHub Releases.
- Secondary: `go install github.com/optiqor/optiqor-cli/cmd/optiqor@latest`.
- Releases are GoReleaser-built (`-trimpath`, version ldflag from `git describe`) and Cosign-signed.
- We do **not** publish to Homebrew or Cargo in Year 1. npm is where the platform engineers are.
- Windows is out of scope. `.goreleaser.yaml` and `package.json` both reflect this. Adding Windows handling needs the OSS-scope exclusion in the playbook removed first.

---

## Commands

12 commands phased over 12 months. Year 1 ships the deterministic core.

| Command | Status | Purpose |
| --- | --- | --- |
| `analyze <chart>` | shipped | Primary entry point. Reads a Helm chart dir or `values.yaml`, prints findings. |
| `demo` | shipped | Runs analysis on a bundled chart so `npx @optiqor/cli demo` shows output without input. |
| `diff <a> <b>` | shipped | Cost delta between two values files. |
| `score <chart>` | shipped | 0-100 efficiency score (qualitative band Year 1, numeric Year 2). |
| `audit <chart>` | shipped | Security-focused subset of `analyze`. Stricter `--fail-on` defaults. |
| `watch <chart>` | stub | Re-runs `analyze` on file change. Phase 2+. |
| `compare <a> <b>` | stub | Side-by-side renderer. Phase 2+. |
| `--roast` (flag) | shipped | Humorous tone on `analyze`. Findings stay accurate. |

Stubs return "not yet implemented" and exit 1. The surface is registered so the command name is locked in.

---

## Output

The CLI's output is the product. Treat it with the same care as code.

### Stream discipline

- `stdout` is data: the report, JSON, or HTML when piped.
- `stderr` is everything else: status, warnings, errors, share URLs, the accuracy disclosure when output is redirected.
- Mixing the two breaks pipelines. `optiqor analyze ./chart | jq` must always work without `2>/dev/null` tricks.

### Color

- Respect `NO_COLOR` (de-facto standard, https://no-color.org).
- Respect `--no-color`.
- Auto-detect via `isatty(stderr)`. Don't auto-color when piped.

### Exit codes

- `0` success, no findings at or above `--fail-on`
- `1` runtime error (parse failure, file not found, invalid flag)
- `2` findings at or above `--fail-on` severity

Reserved: `3-127` for future use. Don't repurpose Unix-standard codes (130 SIGINT, 143 SIGTERM).

### Error messages

Errors reach the user. Each one must include:

1. What happened, in user terms.
2. What to do next, specific.

**Bad:**

```
Error: invalid input
```

**Good:**

```
error: --severity must be one of low, med, high (got: "medium")
```

For multi-line context, use a newline and indent the actionable next step.

### Help text

Every command and flag has help text. It's product surface: what users read before they try anything.

- One-line summary on the command line, one paragraph in the long description.
- The long description names the input shape, the output shape, and at least one runnable example.
- Examples use `./my-chart`, not `path/to/chart`.

**Bad flag description:**

```
"output format"
```

**Good:**

```
"emit machine-readable JSON instead of the human table"
```

---

## Determinism

The CLI is deterministic. Same input, same output, byte-identical.

- No timestamps in output unless explicitly requested.
- No random map iteration. Sort before serialising.
- No reads of `$HOME` or `$USER` in the analyze path. Config file path is a separate concern.
- Time and randomness injected. Never `time.Now()` directly in any package with a state machine.
- Goroutine concurrency only for parallel rule execution, with results merged in deterministic order.

Golden tests in `testdata/golden/` assert byte-identical output across runs. A flake here is a determinism bug, not a test bug. Regenerate with `-update`; eyeball the diff before committing.

---

## Testing

- Table-driven by default. One `Test<Func>` per SUT method, one row per scenario, `t.Run(tc.name, ...)` so failures name the row. Do **not** suffix names with `_TableDriven` — every test should already be table-driven, so the marker is noise.
- Unit tests beside the code: `foo.go` + `foo_test.go`.
- Stdlib only — no testify, no gomock. `errors.Is` / `errors.As` for error checks, `bytes.Equal` for bytes, golden files for multi-line / whitespace-sensitive output.
- Race detector always: `go test -race -count=1 ./...` is the CI bar.
- Inject time + randomness — `time.Date(...)`, `bytes.NewReader`, never `time.Now()` / `rand.Intn` in code under test.
- Helpers call `t.Helper()` on the first line.
- Golden tests in `testdata/golden/` for every renderer mode. The mandatory ±40% accuracy disclosure is asserted byte-identical across modes. Regenerate with `UPDATE_GOLDEN=1 go test ./...` and eyeball the diff before committing.
- Every detector in `pkg/rules` needs at least one positive and one negative table row.
- `go test ./...` is the only test command. No separate integration suite — the CLI is pure functions over Helm input.

`pkg/` (the public surface) has stricter test discipline. Coverage target: 90%. External tests (`pkg/foo_test.go` in package `foo_test`) for any exported symbol with a non-trivial contract.

See `.claude/skills/test/SKILL.md` for the full procedure: templates, anti-patterns, coverage targets, and references to clean examples in the repo.

## Comments

Comments explain WHY, never what. Decoration is debt; every comment is a thing a future engineer must keep in sync with the code. When in doubt, delete. Re-add only when a reader would miss a non-obvious constraint, trade-off, ADR reference, security/performance rationale, or invariant.

The CLI is **Apache-2.0 OSS**, so `pkg/` is the public surface that external Go programs import. Lean slightly more conservative there: one-line godoc on every exported symbol is appropriate when it documents a contract beyond the name (`// Run executes all detectors against the workloads and returns findings in detector-declaration order` is a real contract; `// Run runs the detectors.` is not).

**Banned:**

- Markdown headers (`#`, `##`, `###`) inside `//` comments.
- `Note:`, `Important:`, `Caution:`, `Warning:` labels. If it's important, the code structure should make the rule unmissable.
- Em-dash-heavy narrative essays in package or function docstrings. One terse sentence beats five lines of glue.
- Decorative section dividers: `// ─── Helpers ───`, `// ====== validators ======`. Use blank lines.
- Multi-paragraph package docstrings narrating "Layout philosophy:", "Implementation notes:", "Design constraints:". Compress to one or two terse sentences.
- Bullet-listed enumerations of struct fields or function returns — the type tags already enumerate them.
- Restating the signature (`// Foo returns a Foo.`) or the next line of code.
- "Helper function to...", "Utility for...", "This function..." preambles.
- Emojis. None, anywhere.
- TODOs without a name and issue: `// TODO(@shivam, #42): ...` is fine; `// TODO: do later` is not.

**Always keep:**

- The CLI's hard-rule pins: no LLM in CLI, no telemetry by default, ±40% accuracy disclosure is mandatory in every renderer output. Comments that pin these stay verbatim.
- Detector references to CIS / NSA hardening guides — those anchors are load-bearing for security findings.
- Cross-file references to other packages, ADRs, or upstream specs.

**How to audit a comment**: ask *"if I delete this, what does the next reader fail to understand?"* If the answer is "nothing — the name + signature + types already say it", delete. If the answer names a specific constraint, trade-off, or hard-rule pin, keep but compress.

**Reference commits**: the 2026-05-24 cleanup (`feba8e0` openapi header, `9f817cd` cli sweep) stripped ~400 lines of comment cruft from this repo. Read those diffs for the tone applied at scale; new code should land at that compression level from the start.

## Don't

---

## Code style

### Naming

- Functions are verbs (`ParseValues`, `RenderReport`).
- Predicates: `isLeader`, `hasQuorum`, not `leaderFlag`.
- Acronyms keep case: `httpClient`, `parseURL`, `IDToken`. Not `HttpClient`.
- Cobra command vars: `analyzeCmd`. Constructors: `newAnalyzeCmd()`.

### Functions

- One thing per function. If the name has "and", split.
- Early return for guard clauses. No `else` after `return`.
- Receiver names: one or two letters, consistent across all methods of a type.

### Errors

- Wrap with `%w`, never `%s`, when the caller might `errors.Is` / `errors.As`.
- Sentinel errors exported only when callers branch on them.
- Never `panic` for control flow. Reserved for "this cannot happen".
- Distinguish bad input (user error, exit 1, friendly message) from bug (program error, exit 1, full context).

### Comments

Comments explain why, never what. Code says what; you say why it has to.

**Bad (states the obvious, AI-shaped):**

```go
// Iterates through detectors and runs each one.
for _, d := range detectors {
    d.Run(w)
}
```

**Good (explains the why):**

```go
// Sequential on purpose. Detectors share a finding map that's faster
// to merge than to lock per-write. ADR-0003.
for _, d := range detectors {
    d.Run(w)
}
```

**Bad (AI godoc):**

```go
// RunDetectors runs all detectors on the workloads.
// Returns a slice of findings.
func RunDetectors(ws []Workload, ds []Detector) []Finding
```

**Good (godoc that earns its keep):**

```go
// RunDetectors evaluates ds against every workload in ws and returns
// findings sorted by (severity desc, detector id asc) for stable
// golden tests. Detectors are evaluated in registration order; the
// public rules.All() returns them in canonical order for reproducible
// output.
func RunDetectors(ws []Workload, ds []Detector) []Finding
```

Reference real things: issue numbers, commit SHAs, ADR numbers, RFC sections, vendor docs.

```go
// Helm v3 sets release.Namespace at render time, not template time.
// We default to "default" so values.yaml that uses .Release.Namespace
// in a template doesn't crash. See helm/helm#7702.
```

**Banned in comments:**

- Markdown headers (`#`, `##`, `###`).
- `Note:`, `Important:`, `Caution:`, `Warning:` labels.
- Emojis. None, anywhere.
- Restating what the code says.
- "Helper function to...", "Utility for...". The name should describe it.
- Author tags. Git blame exists.
- TODOs without a name or issue link.

Godoc on every exported symbol. The first sentence starts with the symbol name and is one line. Examples for non-trivial APIs go in `example_test.go`.

### Files and packages

- Lowercase file names, no underscores except `_test.go`.
- `doc.go` per package, package comment lives there.
- Package names: short, single-word, no plurals, no `util`, no `helpers`.

---

## Brand and tone

The CLI ships in front of users. The text is the product.

- `brand/tokens.json` is the single source of truth for colors, font tokens, glyph references. Imported by `pkg/htmlrender` (Go) and by the backend's `web/src/lib/brand.ts` (TS). Drift here breaks visual parity.
- Tone is editorial × engineering. Direct, sharp, opinionated, never glib. No exclamation marks in default output. `--roast` exists for the snark.
- Numbers are accurate or absent. We never round up. `±40%` is canonical.
- No emojis in default output. `--roast` may use a small whitelist (in `internal/roast`).

---

## Security

CLI binaries run on customer laptops. The supply chain is a primary attack surface.

- Releases are GoReleaser-built with `-trimpath` and `-buildvcs=true`. Build determinism is asserted by the release job.
- Cosign signs the archive and `checksums.txt`. The npm postinstall verifies the SHA-256 of the downloaded archive against `checksums.txt` before extracting.
- The CLI never opens files outside the supplied chart path without an explicit flag. Path traversal in `--config`, `--html`, `--output` is a hard rejection, not a warning.
- YAML deserialization uses `gopkg.in/yaml.v3` with anchor depth bounded. Billion-laughs is rejected at parse time.
- npm `postinstall` writes only to the package's own `vendor/` directory. Any path that resolves above `node_modules/@optiqor/cli/` is a P0.
- No `exec.Command` in the binary. The CLI is a pure-Go process. No shelling out, no `kubectl` spawning, no helm-CLI dependency.

---

## Workflow

### Branches

- `<type>/<short-kebab-slug>`. Same prefixes as commits: `feat/`, `fix/`, `chore/`, `docs/`, `test/`, `refactor/`.
- Branch off `main`. Never branch off a feature branch.
- Delete merged branches.

### Commits

Conventional Commits. One concern per commit. DCO sign-off required (`-s` flag, enforced by CI).

Local quality gate before pushing: `gofmt -l .` (must be empty), `go vet ./...`, `go build ./...`, `go test -race -count=1 ./...`, `golangci-lint run --timeout=2m ./...`.

### Pull requests

- Open against `main`. Branch protection ruleset is active. Admin bypass only for emergencies.
- Title is a Conventional Commits subject; it becomes the squash-merge subject.
- Body tells the reviewer the story: what changed, why, how to verify.
- Link the issue: `Closes #N`.
- DCO sign-off enforced by CI (`.github/workflows/commit-lint.yml`).
- PR size labels auto-apply (`size/XS` through `size/XL`). XL is a smell. Split.
- markdown-link-check runs in CI. Cross-repo relative links (`../`) won't resolve in standalone checkout; use absolute URLs or remove the link.

### Reviews

One inline comment per issue, anchored to the exact line. Short summary at the end. Terse maintainer voice — no em-dashes, no markdown headers in the comment body, no bold-labels.

### Issues

One issue per finding. Repo-correct (`optiqor-cli/` for OSS-surface bugs, `optiqor/` for backend). Title is a single declarative sentence. Body is plain prose, no `## Problem` / `## Expected Behavior` headers.

### Decisions

ADRs in `docs/adr/` for non-trivial architectural changes. Template at `docs/adr/0000-template.md`. Examples:

- A new exported package under `pkg/`
- A change to the deterministic-output contract
- A new dependency that adds network or filesystem reach
- A change to brand tokens that affects existing renderers
- A change to the public `rules.Finding` or `rules.Workload` shapes (downstream backend depends on these)

---

## Anti-patterns ("don't")

### Code

- Don't import any GitHub / GitLab API client. The CLI doesn't talk to source control.
- Don't add a daemon, watcher, or persistent process. CLI invocations are short-lived.
- Don't add config files unless absolutely necessary. Flags + env vars only. `OPTIQOR_*` env vars are documented in README.
- Don't add platform-specific code paths. The CLI runs identically on linux and macOS.
- Don't use `init()` except to register Cobra commands.
- Don't write a singleton. Pass dependencies through constructors.
- Don't depend on `init()` ordering.
- Don't return `interface{}` / `any` unless you've considered generics.
- Don't shell out (`exec.Command`). Pure-Go binary.
- Don't read `$HOME` or `$USER` in the analyze path. Config-file lookup is separate and documented.

### Output

- Don't print to stdout from anywhere except the renderer.
- Don't auto-color when piped.
- Don't restate the input in the output ("Analyzing ./chart...") unless `--verbose`.
- Don't print stack traces to users. They go to stderr only if `DEBUG=1`.

### Process

- Don't import any code from `github.com/optiqor/optiqor`. The CLI's audit story is "Apache-2.0 and you can read every line."
- Don't commit binary artifacts. Generated files go in `.gitignore`.
- Don't TODO without an issue. Open the issue, paste the link, then write the TODO.
- Don't merge to `main` without a PR.
- Don't bump dependencies without a one-line justification in the commit message.
- Don't break `pkg/` without a semver bump and a deprecation notice. External programs import it; `backend/` imports it via `replace`.
