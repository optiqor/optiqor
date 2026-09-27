---
name: test
description: Write rigorous unit + integration tests for the optiqor backend. Table-driven by default; named TestFunctionName_Scenario_ExpectedBehavior; stdlib only (no testify); race detector always; time + randomness injected; t.Helper() in helpers; testcontainers for real Postgres / Redis (not mocks); golden tests for byte-deterministic output. Use when the user says "test this", "add tests for X", "write a test", "cover this", "add integration tests", "TDD this", or similar.
---

# test

Write tests a senior Go engineer would ship. Strict table-driven layout, stdlib comparators, race-clean, time-injected. Same rules apply to unit and integration tests — only the boundary moves.

## When to invoke

- "test this" / "add tests for X" / "cover X with tests"
- "write a test" / "TDD this"
- "add integration tests for Y"
- "the test for X is failing — fix it" (read, find root cause, write the minimal repro test alongside the fix)

Skip if the user explicitly asks for an E2E / browser test — that lives in `tests/e2e/` and follows a different harness; see §E2E.

## Hard rules

These are not preferences. They are conditions for tests in this repo to be considered good.

### 1. Table-driven is the default

Any test with more than one case uses an inline table, one row per scenario, each row run inside `t.Run(tc.name, ...)`.

```go
for _, tc := range []struct {
    name string
    // …
}{
    {name: "happy path", /* … */},
    {name: "empty input", /* … */},
} {
    t.Run(tc.name, func(t *testing.T) { /* … */ })
}
```

No package-level fixtures. No shared mutable state across rows.

**No `_TableDriven` suffix in the test function name.** TDT is the default; the suffix is noise. `TestEstimator` consolidating 8 scenarios into a table is named `TestEstimator`, not `TestEstimator_TableDriven`. The subtest names (`tc.name`) carry the scenario, not the wrapper.

### 2. Naming

`TestFunctionName_Scenario_ExpectedBehavior`. Read aloud, it forms a sentence.

| Good | Bad |
|---|---|
| `TestSigner_RejectsExpired` | `TestExpiry` |
| `TestMemory_AllowsUpToLimitThenBlocks` | `TestRateLimit` |
| `TestWhoami_FallsBackToTenantHeaderContext` | `TestWhoami2` |
| `TestTransition_RejectsBackwards` | `TestBad` |
| `TestPgStore_Get_NoRowsTranslatesToNotFound` | `TestGet` |

Subtests inside `t.Run` use kebab-case scenarios: `"empty file"`, `"missing tid"`, `"3rd hit blocks"`. Lowercase, no period.

### 3. Stdlib only — no testify, no gomock, no third-party assertion libs

CLAUDE.md hard rule: "Prefer stdlib." Comparators:

- `==` for primitives, strings, numbers.
- `reflect.DeepEqual` for structs/maps/slices that don't contain funcs or unexported channels.
- `errors.Is` / `errors.As` for error checks. Never `err.Error() == "..."` unless asserting a specific user-visible string.
- `bytes.Equal` for byte slices.
- Golden files for anything multi-line or whitespace-sensitive.

### 4. Race detector always

`go test -race -count=1 ./...` is the project's CI bar. Any test that wouldn't pass `-race` is broken; fix it before shipping. If you need to call a method while another goroutine holds the mutex, you've found a real bug.

### 5. Time + randomness injected

Never call `time.Now()`, `rand.Intn`, or `crypto/rand.Read` directly from code under test. Inject a `Now func() time.Time` (or a `Clock` interface), a `*rand.Rand`, or an `io.Reader` for entropy.

In tests, pin the clock:

```go
now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
signer.Now = func() time.Time { return now }
```

This is the difference between a flaky CI run and a deterministic one. See [`internal/auth/session_test.go`](../../../internal/auth/session_test.go) for the pattern across signing/expiry.

### 6. Helpers call `t.Helper()`

Any function taking `*testing.T` that isn't itself a `TestXxx` calls `t.Helper()` on its first line. Otherwise `t.Fatalf` reports the helper line, not the caller's.

### 7. Subtests are `t.Run`

Use `t.Run(tc.name, ...)` so failures name the row (`TestParse/empty_file: ...`), not the loop line.

### 8. No mocking the database

Per CLAUDE.md: integration tests use `testcontainers` for real Postgres + Redis. Unit tests against code that binds to the DB use a narrow Go interface that the production code consumes (`PgExec` in [`internal/sandbox/pg_store.go`](../../../internal/sandbox/pg_store.go) is the pattern). Mock interfaces **you own**, never SDK interfaces from third parties.

### 9. Golden tests for byte streams

Renderers, canonical JSON, signed payloads, table-formatted output — anything that should be byte-identical run-to-run — uses a golden test. Goldens live in `testdata/golden/` (Go skips the `testdata/` name during build / vet).

### 10. One concern per test

A test asserts one thing the code must do. If you find yourself testing three behaviours in one function, split into three named subtests or three `TestXxx` functions. A failure should name the single broken contract.

---

## Decision: unit vs integration vs E2E

| Type | Where | Imports | Dependencies | Run by |
|---|---|---|---|---|
| **Unit** | `_test.go` next to the code, same package | stdlib + project | fakes, in-memory stores, injected clocks | `go test ./...` (default) |
| **Integration** | `tests/integration/<pkg>/<name>_test.go` | testcontainers, real driver | live Postgres / Redis / Temporal via containers | `go test -tags=integration ./tests/integration/...` |
| **E2E** | `tests/e2e/` | docker-compose + http client | full stack: api + worker + agent + DB + Redis | `make test-e2e` |

The unit boundary is **"no I/O, no real network, no real DB, no real clock."** Cross that boundary → integration. Cross the integration boundary into "the whole binary running" → E2E.

If you find yourself asking "should this be a unit or integration test?": prefer unit. Push integration work down into smaller pure functions and unit-test those.

---

## Templates (copy-paste, edit)

### Template A — table-driven unit test

```go
func TestSession_ValidEnforcesRequiredClaims(t *testing.T) {
    now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
    for _, tc := range []struct {
        name string
        mut  func(*Session)
        want string // substring expected in the error
    }{
        {"missing tid", func(s *Session) { s.TenantID = "" }, "tid"},
        {"missing sub", func(s *Session) { s.Subject = "" }, "sub"},
        {"missing exp", func(s *Session) { s.ExpiresAt = time.Time{} }, "exp"},
    } {
        t.Run(tc.name, func(t *testing.T) {
            ses := validSession(now)
            tc.mut(&ses)
            err := ses.Valid()
            if err == nil || !strings.Contains(err.Error(), tc.want) {
                t.Errorf("want error mentioning %q, got %v", tc.want, err)
            }
        })
    }
}
```

Notes:
- Anonymous struct literal inline — no shared package-level table.
- `name` first so the row is greppable.
- `mut` is a closure that turns one base case into N — clean for invariant-style tests.
- `t.Run` so each row's failure names itself.

### Template B — HTTP handler test (`httptest`)

```go
func TestWhoami_FromBearerToken(t *testing.T) {
    now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
    signer := NewSigner([]byte("test-secret-32-bytes-not-real-secret"))
    signer.Now = func() time.Time { return now }
    h := &Handler{Signer: signer}

    token, _ := signer.Issue(Session{
        TenantID: "tenant-1", Subject: "alice@example.test",
        IssuedAt: now, ExpiresAt: now.Add(time.Hour),
    })

    req := httptest.NewRequest(http.MethodGet, "/v1/session/whoami", http.NoBody)
    req.Header.Set("Authorization", "Bearer "+token)
    rec := httptest.NewRecorder()

    h.Whoami(rec, req)

    if rec.Code != http.StatusOK {
        t.Fatalf("status: got %d, want 200; body=%s", rec.Code, rec.Body.String())
    }
    var got WhoamiResponse
    if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
        t.Fatalf("unmarshal: %v", err)
    }
    if got.TenantID != "tenant-1" || got.Source != "jwt" {
        t.Errorf("response: %+v", got)
    }
}
```

Notes:
- `http.NoBody` not `nil` for GET bodies (gocritic lint rule the project enforces).
- `httptest.NewRequest` + `httptest.NewRecorder`, never spin a real socket.
- Status assertion uses `t.Fatalf` (body matters only when status is right).
- Body assertion is structured (Unmarshal into typed response), never a string match.

For tenant-scoped handlers, wrap the request context with the tenancy helper:

```go
ctx := tenancy.WithContext(req.Context(), tenancy.Context{TenantID: "tenant-1"})
req = req.WithContext(ctx)
```

See [`internal/onboarding/handler_test.go`](../../../internal/onboarding/handler_test.go) for the table-driven variant.

### Template C — fake collaborator (race-safe)

When the code under test depends on an interface, write a fake in the same package as the test:

```go
type fakeRecorder struct {
    mu      sync.Mutex
    records []CallRecord
}

func (f *fakeRecorder) Record(_ context.Context, _ tenancy.Context, c CallRecord) error {
    f.mu.Lock()
    defer f.mu.Unlock()
    f.records = append(f.records, c)
    return nil
}

func (f *fakeRecorder) snapshot() []CallRecord {
    f.mu.Lock()
    defer f.mu.Unlock()
    out := make([]CallRecord, len(f.records))
    copy(out, f.records)
    return out
}
```

Notes:
- Mutex-guarded; race-detector-clean.
- `snapshot()` returns a copy so assertions don't race with later writes.
- Lowercase / unexported — fakes are test infrastructure, not API surface.

See [`internal/agent/fake.go`](../../../internal/agent/fake.go) for the shipped `FakeLLMClient` + `FakeRecorder` doing exactly this.

### Template D — integration test against real Postgres (testcontainers)

```go
//go:build integration
// +build integration

package sandbox_test

import (
    "context"
    "testing"

    "github.com/testcontainers/testcontainers-go"
    "github.com/testcontainers/testcontainers-go/modules/postgres"

    "github.com/optiqor/optiqor/internal/sandbox"
)

func TestPgStore_RoundTrip_Integration(t *testing.T) {
    ctx := context.Background()
    pg, err := postgres.RunContainer(ctx,
        testcontainers.WithImage("postgres:16-alpine"),
        postgres.WithDatabase("optiqor_test"),
        postgres.WithUsername("test"), postgres.WithPassword("test"),
    )
    if err != nil {
        t.Fatalf("container: %v", err)
    }
    t.Cleanup(func() { _ = pg.Terminate(ctx) })

    dsn, _ := pg.ConnectionString(ctx, "sslmode=disable")
    db := mustOpen(t, dsn)
    mustRunMigrations(t, db, "../../../migrations/")

    store := sandbox.NewPgStore(pgxAdapter{db})
    // real Put / Get round trip; real expiry filter; real RLS contract
}
```

Notes:
- Build tag `integration` keeps these out of the default `go test ./...` run.
- `t.Cleanup` over `defer` — runs even on subtest cancellation.
- One container per test (not shared) unless setup cost dominates; share via `TestMain` only when needed, and never share *state*.
- Always run real migrations against the test container — never recreate the schema by hand.

### Template E — golden test

```go
func TestRender_Receipt_GoldenStable(t *testing.T) {
    in := loadFixture(t, "receipt_cloud_tier.json")
    var buf bytes.Buffer
    if err := receipts.RenderCanonical(&buf, in); err != nil {
        t.Fatal(err)
    }
    goldenAssert(t, "receipt_canonical_cloud", buf.Bytes())
}

func goldenAssert(t *testing.T, name string, got []byte) {
    t.Helper()
    path := filepath.Join("testdata", "golden", name+".txt")
    if os.Getenv("UPDATE_GOLDEN") == "1" {
        if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
            t.Fatal(err)
        }
        if err := os.WriteFile(path, got, 0o644); err != nil {
            t.Fatal(err)
        }
    }
    want, err := os.ReadFile(path)
    if err != nil {
        t.Fatalf("read golden %s (set UPDATE_GOLDEN=1 to create): %v", path, err)
    }
    if !bytes.Equal(got, want) {
        t.Errorf("golden mismatch %s\n--- want ---\n%s\n--- got ---\n%s",
            name, want, got)
    }
}
```

Notes:
- `UPDATE_GOLDEN=1 go test ./...` regenerates; eyeball the diff before committing.
- Whitespace-sensitive on purpose — don't trim or normalise. That defeats the byte-identity contract that Receipt signing depends on.

### Template F — parallel subtests

When the test is fully isolated (no shared mutable state, no global env vars, no fixed ports), enable `t.Parallel()`:

```go
func TestPureClassifier(t *testing.T) {
    t.Parallel()
    for _, tc := range cases {
        tc := tc // capture
        t.Run(tc.name, func(t *testing.T) {
            t.Parallel()
            // ...
        })
    }
}
```

The `tc := tc` capture is mandatory before Go 1.22 and harmless after — keep it for compatibility.

Do NOT use `t.Parallel()` if the test:
- mutates `os.Setenv` (use `t.Setenv` instead, but it disables parallelism)
- opens a fixed TCP port
- writes to a shared file under `testdata/`
- relies on a wall-clock interval
- modifies a process-wide singleton (Prometheus registry, default slog handler)

---

## Process for a new test

```
1. Read the code under test
   - What's the contract? (godoc + signatures)
   - What collaborators exist? Are they interfaces or concrete?
   - What happens at boundaries: nil, empty, max, expired, wrong type?

2. Enumerate scenarios
   - One row per behaviour the code must guarantee
   - Cover: happy path, every error branch, every boundary
   - Don't test code you don't own (the stdlib works; assume it)

3. Pick the test type (unit vs integration)
   - Default to unit
   - Integration only when behaviour spans a real boundary the unit can't fake

4. Write the table
   - Anonymous struct inline
   - name string field first
   - Distinct expected output per row

5. Wire t.Run subtests
   - Each row is a t.Run(tc.name, func(t *testing.T) { ... })
   - Use t.Helper() in any helpers you call

6. Inject time / randomness
   - Pin time.Date(...) for any time-dependent assertion
   - For RNG, accept io.Reader and pass bytes.NewReader(...)

7. Run the gate (see below)

8. Commit per the project's commit skill
   - `test(<pkg>): cover <thing>` is the conventional subject
```

## Quality gate (run before declaring done)

From the repo root:

```bash
gofmt -l .                              # must be empty
go vet ./...                            # must be clean
go test -race -count=1 ./...            # must pass; -count=1 disables result caching
golangci-lint run --timeout=2m ./...    # must report 0 issues
./verify.sh                             # must stay 125 PASS / 0 FAIL / 4 GAP
```

For integration tests (when they exist):

```bash
go test -tags=integration -race -count=1 ./tests/integration/...
```

If any gate fails: read the failure, do not silence by deleting the failing assertion. Fix the test or the code.

## Coverage targets (per CLAUDE.md)

- Domain packages (`internal/agent`, `internal/auth`, `internal/billing`, `internal/cost`, `internal/onboarding`, `internal/receipts`, `internal/sandbox`, `internal/safety`, etc.): **70%**
- Platform packages (`internal/platform/*`): **50%**
- Higher when the path is hot — signing, rate-limit, tenant resolution, sanitizer.
- `cmd/*`: untested glue is acceptable; the wired-in handlers carry the contract tests.

Check with:

```bash
go test -cover ./internal/...
```

When coverage drops below target, write the missing rows in the existing table — don't write a new `TestFunctionName_Coverage` named after the metric.

## Anti-patterns

| ❌ Do not | ✅ Instead |
|---|---|
| Mock `*sql.DB` or `pgx.Pool` directly | Define a narrow Go interface (`PgExec`) the production code calls, use testcontainers for the real contract |
| Compare `err.Error() == "expected text"` | `errors.Is(err, ErrSentinel)` |
| Share a `var fixture = ...` across tests | Build fresh data inside each test or table row |
| `time.Sleep(100 * time.Millisecond)` to wait for async work | Synchronise with channels or polling with deadline (`time.After`) |
| `os.Setenv(...)` without `t.Setenv(...)` | `t.Setenv(...)` — auto-restores on test exit |
| Catch panics in tests unless testing the panic | Let panic fail the test; the project's `withPanicRecovery` middleware is the safety net, not a contract |
| `TestMain` for global setup | Push setup into each test or into a `setUpXxx(t *testing.T)` helper; reserve `TestMain` for genuine package-level state (rare) |
| `init()` to register fixtures | Same as above; no `init()` in tests |
| Table-driven test with side effects in row construction | Inline literal only; build dependencies inside the subtest |
| `assert.Equal(t, want, got)` (testify pattern) | Stdlib: `if got != want { t.Errorf(...) }` |
| Long single-line test: `if a == 1 && b == 2 && c == 3 { t.Errorf(...) }` | Split into individual `t.Errorf` calls so each failure names the field |
| Asserting on log output | Don't. Logs are narrative for ops, not contract for tests. If a behaviour matters, surface it through a metric / return value / state change. |
| Test that requires running other tests first | Each test runs independently. Period. |
| Hit a real network endpoint (Anthropic, GitHub, AWS) | Use a `FakeLLMClient`, an `httptest.Server`, or a mocked SDK interface you own |

## E2E (`tests/e2e/`)

E2E spins up the full stack via `docker-compose.test.yml` and exercises the system through its public API. One full happy-path scenario, not a comprehensive suite — that's what unit + integration are for.

Out of scope for this skill. When the user asks for E2E coverage, ask whether they want unit or integration first; E2E is the last resort for "the contract is the wire and nothing else."

## Examples in this repo (read these for tone)

The cleanest references for the patterns above:

- **Table-driven**: [`internal/auth/session_test.go`](../../../internal/auth/session_test.go) — `TestSession_ValidEnforcesRequiredClaims` shows the anonymous-struct + `t.Run(tc.name, ...)` form. [`internal/onboarding/handler_test.go`](../../../internal/onboarding/handler_test.go) shows the same pattern for HTTP handlers.
- **Fake collaborator**: [`internal/agent/fake.go`](../../../internal/agent/fake.go) — `FakeLLMClient` + `FakeRecorder`; mutex-guarded, snapshot-via-copy.
- **Time injection**: every test in [`internal/auth/session_test.go`](../../../internal/auth/session_test.go) and [`internal/platform/ratelimit/ratelimit_test.go`](../../../internal/platform/ratelimit/ratelimit_test.go) — `signer.Now = func() time.Time { return now }` and `NewMemory(...).WithClock(...)`.
- **HTTP handler with `httptest`**: [`internal/auth/handler_test.go`](../../../internal/auth/handler_test.go) covers bearer + cookie + tenant-header fallback + 401 paths.
- **Migration invariant tests**: [`migrations/migrations_test.go`](../../../migrations/migrations_test.go) — `loadBaseline` / `loadMigration` helpers with `t.Helper()`, fail-fast read, parse-once. Tests assert structural invariants (RLS on every tenant table, role split, region constraint) so a future migration can't silently weaken the contract.
- **Race-aware fake store**: [`internal/sandbox/store.go`](../../../internal/sandbox/store.go) `InMemoryStore` — `sync.RWMutex`, expiry on read.
- **Pre-test in-process server**: [`internal/sandbox/perf_test.go`](../../../internal/sandbox/perf_test.go) — drives `Handler.Analyze` directly via `httptest.NewRecorder` to measure engine latency without the socket. Same pattern works for any latency-budget assertion.
- **PgStore against fake `PgExec`**: [`internal/sandbox/pg_store_test.go`](../../../internal/sandbox/pg_store_test.go) shows the narrow-interface pattern that lets the production type stay driver-agnostic while tests run without a real DB.
- **Signer with alg=none guard**: [`internal/auth/session_test.go:TestSigner_RejectsAlgNone`](../../../internal/auth/session_test.go) — security-critical case where the test name carries the contract.

When in doubt, read one of those and match its shape.
