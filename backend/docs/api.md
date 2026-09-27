# Optiqor backend API

The Optiqor backend exposes one HTTP surface — the api binary under
[`cmd/api/`](../cmd/api/). This document is the canonical reference for every
route the server registers. Generated automatically by ourselves; if you change
a handler, change this file in the same PR.

> **Headline contract:** every analysis-bearing response carries the mandatory
> accuracy disclosure (`±40%` for sandbox, `±15%` for agent). Removing it from
> any handler is a hard-rule violation — see `CLAUDE.md`.

- [Conventions](#conventions)
- [Versioning](#versioning)
- [Errors](#errors)
- [Routes](#routes)
  - [Operational](#operational)
  - [Sandbox + share](#sandbox--share)
  - [Receipts](#receipts)
  - [Apply Fix](#apply-fix)
  - [Ingestion](#ingestion)
  - [Cost spikes](#cost-spikes)
  - [GitHub integration](#github-integration)
  - [Debug](#debug)
- [Headers](#headers)
- [Tenancy](#tenancy)
- [Body size caps](#body-size-caps)
- [Status code matrix](#status-code-matrix)

---

## Conventions

| | |
| --- | --- |
| Base URL (dev) | `http://localhost:8080` direct, or `http://localhost:3000` via the Next.js proxy |
| Base URL (prod) | `https://optiqor.dev` (frontend and API are same-origin; share + verifier pages are Go-served at `/r/{hash}` and `/v/{id}`) |
| Content type | `application/json` unless noted (`/v1/analyze` accepts `text/yaml`) |
| Auth | most routes are unauth; `/v1/apply-fixes` requires the `X-Optiqor-Tenant` header (Phase-1 dev surface; JWT in Phase 5) |
| Timestamps | RFC 3339, UTC |
| Money | USD, expressed in **cents (int64)** on the wire; human-readable `monthly_savings_usd` floats are derived |
| Hash | first 12 bytes of `sha256(canonical_body)` rendered as 24 hex chars |
| Idempotency | `POST /v1/analyze` is idempotent on identical bytes — same input → same `share_hash` |

## Versioning

All product routes are prefixed `/v1/`. The path version is the contract. We
will not change response shapes under `/v1` without a parallel `/v2`. Removed
fields go through a one-release deprecation window.

Health, OAuth, webhook, share, and verifier routes are unversioned because
their semantics are fixed and shared across the platform.

## Errors

All 4xx and 5xx responses use `text/plain` with the same wire shape as
`http.Error`: a single short line describing the failure. Examples:

```
400  parser: parser: empty document
401  missing tenant context
404  not found
413  body too large
500  store: ...
```

JSON error envelopes are deliberately not used — every error message is a
human-readable single line so support engineers can grep logs verbatim.

---

## Routes

### Operational

| Method | Path | Auth | Body | Returns |
| --- | --- | --- | --- | --- |
| `GET` | `/healthz` | — | — | `200 ok` text |
| `GET` | `/readyz` | — | — | `200`/`503` JSON readiness report |
| `GET` | `/metrics` | — | — | Prometheus text format (only when registry configured) |
| `GET` | `/v1/meta` | — | — | JSON manifest of registered endpoints |

`/healthz` is the liveness probe — always returns 200 once the process is
serving. `/readyz` runs the [`healthz.Registry`](../internal/platform/healthz/)
checks (Phase 1 ships a single `self` check; Postgres/Redis/Temporal probes
land in Phase 5) with a 2-second timeout and returns:

```json
{
  "checks": [
    {"name": "self", "ok": true, "latency_ns": 2389}
  ],
  "ok": true,
  "version": "dev"
}
```

`GET /v1/meta` is the API self-description; the frontend uses it to discover
routes and the docs build pulls from it.

---

### Sandbox + share

The public analysis surface mirrors the offline CLI's `optiqor analyze`.

#### `POST /v1/analyze`

Run the deterministic detector library against a Helm `values.yaml`.
Unauth, 1 MiB body cap.

**Request**

```http
POST /v1/analyze
Content-Type: text/yaml

api:
  resources:
    requests: {cpu: 500m, memory: 256Mi}
  image: nginx:1.25
```

**Response — 200 OK**

```json
{
  "accuracy_disclosure": "Sandbox accuracy: ±40%. Install the Optiqor agent for exact numbers (optiqor.dev/get).",
  "source": "sandbox",
  "workloads_analyzed": 1,
  "findings": [
    {
      "DetectorID": "missing-memory-limit",
      "Workload": "api",
      "Title": "Memory limit not set",
      "Detail": "This workload declares a memory request but no limit ...",
      "MonthlyUSDCents": 0,
      "Severity": "HIGH",
      "Confidence": "high",
      "Category": "security"
    }
  ],
  "cost_findings":           [/* …only category=cost */],
  "security_findings_bonus": [/* …only category=security, shown as bonus */],
  "monthly_savings_usd": 0,
  "annual_savings_usd": 0,
  "cost_estimates": [
    {
      "workload": "api",
      "region": "us-east-1",
      "replicas": 1,
      "cpu_millicores": 500,
      "memory_bytes": 268435456,
      "monthly_usd_cents": 1985,
      "cpu_monthly_cents": 1752,
      "mem_monthly_cents": 233,
      "note": "cpu+mem requests across 1 replica(s) at us-east-1 on-demand pricing",
      "accuracy_band_pct": 40
    }
  ],
  "share_hash": "1dc6707cfd958baf53d5666f",
  "share_url": "http://localhost:3000/r/1dc6707cfd958baf53d5666f"
}
```

**Status codes**

| | |
| --- | --- |
| `200` | analysis ok |
| `400` | malformed YAML / empty body |
| `405` | not `POST` |
| `413` | body exceeds 1 MiB |
| `500` | pricer failure |

**Notes**

- `share_url` is derived from the inbound request — dev sees the local origin,
  production sees `https://optiqor.dev` (or whatever `OPTIQOR_PUBLIC_URL` is
  set to). `X-Forwarded-Proto` and `X-Forwarded-Host` are honoured.
- `share_hash` is the first 12 bytes of `sha256(canonical YAML)`; resubmitting
  the same input yields the same hash so client and server never disagree on
  the URL.
- Storage TTL: 30 days. After that, `/r/<hash>` returns 404.

#### `GET /r/{hash}`

Render a previously-stored analysis. **HTML by default**, JSON on opt-in.

| Selector | Returns |
| --- | --- |
| (default) | `text/html; charset=utf-8` — full page rendered via [`pkg/htmlrender`](../../optiqor-cli/pkg/htmlrender/) (same Apache-2.0 renderer `optiqor analyze --html` uses, so the local file and the share page are byte-equivalent) |
| `Accept: application/json` | the original `POST /v1/analyze` response body |
| `?format=json` | same — query parameter wins over header |

**Status codes**

| | |
| --- | --- |
| `200` | found |
| `400` | empty `{hash}` segment |
| `404` | unknown hash or expired entry |
| `405` | not `GET` |

#### `GET /v1/meta`

Self-description of the API. Used by the frontend route table and by the docs
build.

```json
{
  "version": "dev",
  "endpoints": [
    {"method": "POST", "path": "/v1/analyze", "notes": "sandbox: returns findings + savings"},
    {"method": "GET",  "path": "/r/{hash}",   "notes": "sandbox: fetch shared analysis"},
    {"method": "GET",  "path": "/v1/receipts/{id}", "notes": "verify a signed Receipt"},
    {"method": "POST", "path": "/v1/apply-fixes",   "notes": "preview the Apply Fix PR body + diff"},
    {"method": "POST", "path": "/v1/ingest",        "notes": "agent → SaaS metrics ingestion"},
    {"method": "POST", "path": "/v1/cost-spikes",   "notes": "bill anomaly webhook"},
    {"method": "GET",  "path": "/healthz", "notes": "liveness"},
    {"method": "GET",  "path": "/readyz",  "notes": "readiness"},
    {"method": "GET",  "path": "/metrics", "notes": "prometheus metrics"}
  ]
}
```

---

### Receipts

#### `GET /v1/receipts/{id}`

Fetch a Verified Receipt as JSON, plus a live signature-status flag against
the configured public-key registry. Unauth — a Receipt is designed to be
independently verifiable; this endpoint is a convenience over offline verify.

**Response — 200 OK**

```json
{
  "receipt": {
    "id": "rcpt_01HQ0",
    "tenant_id": "tenant-abc",
    "workload": "api",
    "apply_fix_id": "afix_01HP9",
    "observed_from_utc": "2026-04-11T00:00:00Z",
    "observed_to_utc":   "2026-05-11T00:00:00Z",
    "predicted_savings_usd_cents": 12000,
    "realised_savings_usd_cents":  11500,
    "cloud_bill_source": "aws/cur:2026-05",
    "issuer_key_id": "k1",
    "issued_at_utc": "2026-05-12T00:00:00Z"
  },
  "signed":          "IDtgr8u0vT1FZP….Q3Rwo",
  "verified":        true,
  "issuer_key_id":   "k1",
  "verifier_notice": "Verification keys are published at https://optiqor.dev/keys"
}
```

`signed` is `base64url(sig) "." base64url(canonical_payload)`. Clients should
re-verify with the public key from `/keys` rather than trusting the server's
`verified` boolean alone.

| | |
| --- | --- |
| `200` | found |
| `400` | missing `{id}` |
| `404` | unknown receipt |
| `405` | not `GET` |

#### `GET /v/{id}`

The human-facing Receipt verifier page. Same data as the JSON endpoint, but
rendered as a self-contained HTML page with a live signature badge, the
canonical payload pretty-printed, the signature on its own row, and inline
offline-verify instructions. No auth, no telemetry, indexable.

| | |
| --- | --- |
| `200` | found, returns `text/html` |
| `404` | unknown receipt |
| `405` | not `GET` |

---

### Apply Fix

#### `POST /v1/apply-fixes`

Preview what an Apply Fix PR would look like for a given finding. Runs the
agent composer (sanitizer + LLM call + budget gate + render) and returns
both the markdown body and the LLM-extracted unified diff. Does NOT open a
real GitHub PR — that surface is Phase 5 once the GitHub App credentials
are wired.

**Auth**: `X-Optiqor-Tenant` header required (Phase 1 dev surface).

**Request**

```http
POST /v1/apply-fixes
Content-Type: application/json
X-Optiqor-Tenant: tenant-abc

{
  "chart": "charts/api",
  "workload": "api",
  "chart_yaml": "api:\n  resources:\n    requests: {cpu: 2}",
  "model": "claude-sonnet",
  "finding": {
    "DetectorID": "cpu-overprovisioned",
    "Workload": "api",
    "Title": "CPU request appears overprovisioned",
    "Severity": "MED",
    "Confidence": "medium",
    "Category": "cost",
    "MonthlyUSDCents": 2920
  }
}
```

**Response — 200 OK**

```json
{
  "markdown_body": "## Optiqor analysis — charts/api\n\n**Potential savings:** $29.20 / month …",
  "unified_diff":  "--- a/values.yaml\n+++ b/values.yaml\n@@\n- cpu: 2\n+ cpu: 1\n",
  "explanation":   "Halving the request from 2 to 1 still leaves 50 % headroom …",
  "sanitizer": {
    "suspicious": false,
    "reasons":    [],
    "truncated":  false
  }
}
```

| | |
| --- | --- |
| `200` | preview generated |
| `400` | missing `chart` / `chart_yaml`, malformed JSON |
| `401` | missing or empty `X-Optiqor-Tenant` |
| `402` (effective) | budget exceeded — returned as `502 bad gateway: agent: per-call budget exceeded …` |
| `405` | not `POST` |
| `413` | body exceeds 1 MiB |
| `502` | upstream LLM error or budget gate hit |

The `sanitizer` object reflects the [`internal/agent/llm/sanitizer`](../internal/agent/llm/sanitizer/) Phase-1
defence — prompt-injection markers detected, comments stripped, fields
truncated. Operators read it before authorising the PR open.

---

### Ingestion

#### `POST /v1/ingest`

The in-cluster agent ships sanitised Prometheus + CUR batches here. **Both
fields are optional, but the request must carry at least one.** Phase 1 wires
the parser only — sinks log counts. Postgres persistence lands with the
Phase-5 agent.

Body cap: 16 MiB.

**Request**

```http
POST /v1/ingest
Content-Type: application/json

{
  "tenant": "tenant-abc",
  "cluster_id": "cluster-1",
  "prometheus_json": "<bytes>",
  "cur_rows_csv":   "<bytes>"
}
```

The `prometheus_json` field is the raw bytes of a Prometheus
`/api/v1/query_range` response (matrix result type only). `cur_rows_csv` is
a CSV string in AWS Cost & Usage Report format — required columns:

```
lineItem/UsageStartDate, lineItem/UsageEndDate, lineItem/ProductCode,
lineItem/UsageType,      lineItem/UsageAmount,  lineItem/UnblendedCost,
product/region [, lineItem/ResourceId]
```

**Response — 200 OK**

```json
{
  "prom_series": 12,
  "cur_rows":    3140
}
```

| | |
| --- | --- |
| `200` | parsed and accepted |
| `400` | malformed JSON / CSV / Prometheus status != "success" / missing required CUR columns / both bytes fields empty |
| `400` | `tenant` field empty |
| `405` | not `POST` |
| `413` | body exceeds 16 MiB |
| `500` | configured sink rejected the batch |

---

### Cost spikes

#### `POST /v1/cost-spikes`

Webhook target for AWS Cost Anomaly Detection (and any other
threshold-emitting upstream). Phase 1 dispatches every above-threshold event
into the worker's `cost_spike` workflow; the signature-verification step is
Phase 2-3 (AWS SNS HMAC) and currently absent.

Body cap: 64 KiB.

**Request**

```json
{
  "tenant": "tenant-abc",
  "workload_id": "wl-42",
  "observed_delta_usd": 120.50,
  "observed_at_utc": "2026-05-11T08:00:00Z",
  "likely_pr_commit_sha": "a1b2c3d…",
  "likely_pr_url": "https://github.com/acme/api/pull/42"
}
```

**Response**

| | |
| --- | --- |
| `202` | accepted and dispatched (no body) |
| `400` | malformed JSON, missing `tenant` / `workload_id` |
| `405` | not `POST` |
| `413` | body exceeds 64 KiB |
| `502` | downstream dispatcher rejected the event |

---

### Session

#### `GET /v1/session/whoami`

Returns the caller's identity envelope. Reads, in priority order:

1. `Authorization: Bearer <jwt>` — Phase-5 JWT extractor path.
2. `Cookie: optiqor_session=<jwt>` — same JWT in cookie form (set by `/v1/session/issue`).
3. `X-Optiqor-Tenant` header — Phase-2 dev surface; populated by the tenant-context middleware.

```json
{
  "tenant_id": "tenant-1",
  "workspace_id": "ws-1",
  "subject": "alice@example.test",
  "name": "Alice",
  "source": "jwt",
  "expires_at": "2026-05-25T00:00:00Z"
}
```

| | |
| --- | --- |
| `200` | identity envelope; `source` is `"jwt"` or `"header"` |
| `401` | no usable identity |

#### `POST /v1/session/issue`

Mints a backend JWT after Auth.js has authenticated the user on the
frontend. The dashboard server action calls this with the Auth.js
session subject + the user's tenant id; the handler issues a 12h JWT
in the response body and sets an HttpOnly `optiqor_session` cookie.
Phase 5 ties this to a verified provider callback so the handler can
fail closed; Phase 2 trusts the dashboard to have validated the
subject server-side.

```json
{ "subject": "github|12345", "name": "Alice", "tenant_id": "tenant-1", "workspace_id": "ws-1" }
```

| | |
| --- | --- |
| `200` | token + expires_at; `Set-Cookie: optiqor_session=...` |
| `400` | missing `subject` / `tenant_id`, or unknown field |
| `413` | body exceeds 16 KiB |

---

### Onboarding

Tenant-scoped (same X-Optiqor-Tenant middleware as `/v1/apply-fixes`).

#### `GET /v1/onboarding/state`

Returns the tenant's current onboarding state plus the SLO table the
dashboard renders alongside the timeline. Lazily creates a
`signed_up` record on first read so the dashboard never sees a 404.

```json
{
  "current": "vcs_connected",
  "reached_at": { "signed_up": "...", "vcs_connected": "..." },
  "progress_percent": 16,
  "activated": false,
  "activation_window": "336h0m0s",
  "next_stage": "repo_selected",
  "slos": {
    "sandbox_latency": "3s",
    "install_to_first_pr": "10m0s",
    "install_to_first_reco": "30m0s",
    "install_to_first_receipt": "840h0m0s"
  }
}
```

| | |
| --- | --- |
| `200` | onboarding envelope |
| `401` | tenant context missing |

#### `POST /v1/onboarding/transition`

Advance the state machine. Forward-only; backwards transitions and
unknown stages return 400.

```json
{ "to": "agent_installed" }
```

| | |
| --- | --- |
| `200` | updated state (same shape as `GET /v1/onboarding/state`) |
| `400` | illegal transition / unknown stage / unknown field |
| `401` | tenant context missing |
| `413` | body exceeds 4 KiB |

---

### GitHub integration

#### `POST /webhooks/github`

GitHub App webhook receiver. HMAC-SHA256 verifies against `OPTIQOR_GITHUB_WEBHOOK_SECRET`;
mismatched signatures are rejected with `401`. Phase 4+ kicks a Temporal
workflow keyed off `(event, delivery)`; Phase 1 acks with 202.

| | |
| --- | --- |
| `202` | signature verified, event queued (no-op in Phase 1) |
| `401` | bad signature |
| `405` | not `POST` |
| `413` | body exceeds 8 MiB |

#### `GET /oauth/github/callback`

GitHub OAuth landing. Phase 1 acks with a stub page; session issuance lands
in Phase 5 alongside Auth.js on the frontend.

---

### Debug

#### `GET /debug/pprof/*`

Standard Go pprof endpoints (`/debug/pprof/`, `/cmdline`, `/profile`,
`/symbol`, `/trace`). Mounted only when `OPTIQOR_ADMIN_TOKEN` is non-empty;
each request must carry that token as a bearer credential via the
`Authorization: Bearer <token>` header. Constant-time compared.

---

## Headers

| Header | Direction | Purpose |
| --- | --- | --- |
| `X-Optiqor-Tenant` | client → server | Phase-2 dev tenant resolution on auth-required routes |
| `X-Optiqor-Workspace`, `X-Optiqor-Cluster`, `X-Optiqor-Namespace` | client → server | optional narrowing of the tenancy scope |
| `Authorization: Bearer <jwt>` | client → server | Phase-5+ JWT auth; whoami honours it today so the dashboard can swap in JWT issuance without server changes |
| `Cookie: optiqor_session=<jwt>` | client → server | cookie equivalent of the bearer token; set by `/v1/session/issue` |
| `Set-Cookie: optiqor_session=...` | server → client | issued by `/v1/session/issue`; `HttpOnly`, `Secure` when behind TLS, `SameSite=Lax` |
| `X-Request-Id` | server → client | per-request id from the request-id middleware; mirrored back if the client supplied one |
| `X-Forwarded-Proto`, `X-Forwarded-Host` | reverse proxy → server | honoured when constructing `share_url` and verifier links |
| `X-Hub-Signature-256` | GitHub → server | webhook HMAC; verified server-side |
| `Retry-After` | server → client | seconds-until-retry on 429 responses from the rate-limited sandbox routes |

## Tenancy

Every Postgres query runs inside a transaction that binds the tenant id to a
session-local GUC (`set_config('app.tenant_id', $1, true)`). Row-Level
Security policies on every tenant-scoped table enforce isolation server-side.
There is no admin escape hatch in application code; only the migration role
bypasses RLS, and migrations run out-of-band.

Workflow dispatch uses per-tenant Temporal task queues
(`tenant-<uuid>-default`, `tenant-<uuid>-priority`). Redis keys are prefixed
`t:<tenant_id>:`. S3 paths live under `tenants/<tenant_id>/`.

## Body size caps

| Route | Cap | Why |
| --- | --- | --- |
| `POST /v1/analyze` | 1 MiB | average chart < 50 KiB; cap blocks payload abuse |
| `POST /v1/apply-fixes` | 1 MiB | same upper bound; matches analyze |
| `POST /v1/ingest` | 16 MiB | agent batches with 30 days of Prometheus + a daily CUR row count |
| `POST /v1/cost-spikes` | 64 KiB | Cost Anomaly payloads are tiny |
| `POST /v1/session/issue` | 16 KiB | tiny JSON envelope; fits in one TCP segment |
| `POST /v1/onboarding/transition` | 4 KiB | one JSON field |
| `POST /webhooks/github` | 8 MiB | GitHub's documented worst case |

## Status code matrix

A condensed view for ops dashboards:

| Code | Meaning across all routes |
| --- | --- |
| `200` | request handled, body in declared content type |
| `202` | request accepted for async processing |
| `400` | malformed input (JSON, YAML, CSV, missing required field) |
| `401` | missing tenant / OAuth state / webhook signature |
| `404` | unknown id / hash / route |
| `405` | wrong HTTP method |
| `413` | body exceeds the route's cap |
| `500` | infrastructure or programming error — see logs |
| `502` | upstream (LLM / dispatcher / GitHub) rejected |
| `503` | readyz dependency probe failed |

---

## Where this lives in code

| Route | Handler | Tests |
| --- | --- | --- |
| `/v1/analyze`, `/r/{hash}` | [`internal/sandbox/sandbox.go`](../internal/sandbox/sandbox.go) | `internal/sandbox/sandbox_test.go` |
| `/v1/receipts/{id}`, `/v/{id}` | [`internal/receipts/handler.go`](../internal/receipts/handler.go) | `internal/receipts/handler_test.go` |
| `/v1/apply-fixes` | [`internal/prwriter/handler.go`](../internal/prwriter/handler.go) | `internal/prwriter/handler_test.go` |
| `/v1/ingest` | [`internal/ingestion/handler.go`](../internal/ingestion/handler.go) | `internal/ingestion/handler_test.go` |
| `/v1/cost-spikes` | [`internal/billing/spike_handler.go`](../internal/billing/spike_handler.go) | `internal/billing/spike_handler_test.go` |
| `/v1/session/whoami`, `/v1/session/issue` | [`internal/auth/handler.go`](../internal/auth/handler.go) | `internal/auth/handler_test.go` |
| `/v1/onboarding/state`, `/v1/onboarding/transition` | [`internal/onboarding/handler.go`](../internal/onboarding/handler.go) | `internal/onboarding/handler_test.go` |
| `/v1/meta`, `/healthz`, `/readyz`, `/metrics`, `/webhooks/github`, `/oauth/github/callback`, `/debug/pprof/*` | [`cmd/api/main.go`](../cmd/api/main.go) + [`cmd/api/routes.go`](../cmd/api/routes.go) | `cmd/api/main_test.go` + `cmd/api/routes_test.go` |
| spec (single source of truth for the public surface) | [`optiqor-cli/docs/api/openapi.yaml`](../../optiqor-cli/docs/api/openapi.yaml) | `scripts/check-openapi-parity.sh` (CI gate) |

The wire shapes are the Go structs — `AnalyzeResponse`, `VerifyResponse`,
`PreviewRequest`, `IngestRequest`, `SpikeEnvelope`. If those drift from this
doc, the doc is wrong. Update both in the same PR.
