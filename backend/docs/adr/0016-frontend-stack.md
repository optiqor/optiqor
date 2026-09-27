## ADR 0016: Frontend stack — Next.js 15 App Router with Go-served share pages

- **Status:** Accepted
- **Date:** 2026-05-24
- **Authors:** @shivam

> Sequencing note: this ADR was originally tracked as "ADR-0001 — Frontend framework" in `todo.md`, but ADR-0001 had already been allocated to `0001-three-tier-execution.md`. ADR-0016 is the same decision under the next free slot.

## Context

Phase 2 ships a public sandbox web UI (`optiqor.dev/sandbox`) plus a small marketing surface (`/`, `/pricing`, `/security`, etc.), with an auth-gated customer dashboard following in Phase 4-5. By Day 1 of Phase 2 we needed to commit to a frontend stack so engineering, design, and content could move in parallel.

Strategy doc constraints that bear on this decision ([`docs/strategy/technical_implementation.md`](../strategy/technical_implementation.md) and [`docs/strategy/business_strategy.md`](../strategy/business_strategy.md)):

- **Public share pages must be raw-HTTP indexable** — Slack and GitHub do not run JS for their link unfurls; if `/r/<hash>` ships as a Next.js client-rendered page, every PR-comment preview shows a spinner.
- **`/r/<hash>` and `optiqor analyze --html` must produce byte-identical output** — single source of truth for "what an analysis looks like" so the CLI fixture and the sandbox share page can never drift.
- **The CLI is Apache-2.0 and independently auditable** — anything that renders a finding must be importable from `optiqor-cli/pkg/`, not from a proprietary repo.
- **Customer dashboard, billing, and auth stay proprietary** — Stripe portal, Receipt browser, Apply Fix history, Cost spike timeline all sit behind tenant auth.
- **Brand discipline** — Editorial × Engineering visual language (near-black ink scale, electric-cyan accent on data only, hairline borders, no gradients). Brand tokens live in `optiqor-cli/brand/tokens.json` so the CLI's terminal output and the web UI cannot disagree about the palette.
- **One language wherever possible** — backend is Go end-to-end (CLAUDE.md anti-pattern: "Don't introduce a second language"). The frontend has to be JS, but the share/verifier rendering must stay in Go so we don't fork the renderer.

## Decision

The Optiqor web surface is split across two implementations chosen for different audiences:

1. **Public share + verifier pages are served by the Go API** through `github.com/optiqor/optiqor-cli/pkg/htmlrender` (Apache-2.0). Routes `GET /r/<hash>` and `GET /v/<id>` return fully-rendered HTML with no client-side JS dependency. The same `pkg/htmlrender` powers `optiqor analyze --html` so a CLI-generated local report and a sandbox share page are byte-identical.

2. **The proprietary web surface (`web/`) is Next.js 15 App Router + TypeScript strict + pnpm + Tailwind 4 + Geist Sans/Mono.** This houses marketing pages, the sandbox paste-and-go UI (`/sandbox`), and the auth-gated dashboard (`/app/*`). Auth.js handles GitHub + GitLab OAuth (Phase 4 deliverable). TanStack Query for server state; Zod for validation at the API edge.

The two layers share `optiqor-cli/brand/tokens.json` as the brand source of truth. Both consume tokens directly: the Go renderer compiles them into the static HTML, and `web/src/lib/brand.ts` re-exports them as CSS custom properties.

Same-origin proxy via `next.config.ts` rewrites `/v1/*`, `/r/*`, `/v/*`, `/oauth/*`, `/webhooks/*`, `/healthz`, `/readyz` to the Go API at `OPTIQOR_API_UPSTREAM` (default `http://localhost:8080`). The browser never sees a cross-origin call; CORS never gates a sandbox request; production-behind-reverse-proxy matches dev under any sane gateway config.

Vercel preview deploys per PR for the first two phases. Self-hosted Next standalone behind CloudFront when SOC 2 binds (Phase 9).

## Consequences

**Positive**

- Share pages indexable by Slack/GitHub unfurls on Day 1; PR-comment previews render the analysis, not a spinner.
- Single source of truth for analysis rendering: the same Go function produces local CLI output and SaaS share pages, so a regression that breaks one breaks the other and CI catches both at once.
- Editorial × Engineering brand stays consistent: terminal output and web UI consume the same tokens; designers can't accidentally diverge them.
- Customer dashboard remains proprietary (Auth.js, billing, Receipt browser) while the report renderer is Apache-2.0; the OSS/commercial split mirrors the same boundary the CLI already establishes.
- Next.js 15 App Router gives us server components + edge rendering for the marketing surface without locking us into client-only React.
- Vercel preview deploys are cheap and fast for Phase 2-3 iteration; the migration off Vercel is a single `next build` standalone output away when SOC 2 demands self-hosting.

**Negative**

- Two rendering paths to maintain (Go for share/verifier, Next for marketing/dashboard). Mitigation: the Go path imports `pkg/htmlrender` so it shares the bulk of the rendering surface with the CLI; only the marketing/dashboard shell is "duplicate" UI work.
- Brand tokens are imported by both repos via different mechanisms (Go embeds at build time, Next.js imports at module time). A token change requires releases of both the backend binary and the web bundle. Mitigation: token changes are rare and tracked in `optiqor-cli/brand/tokens.json` with a version field.
- Vercel as Phase-2-3 infrastructure is a vendor concentration risk. Mitigation: Next standalone build is the migration path; CloudFront + S3 + Lambda@Edge is the SOC-2-time landing place.
- Auth.js is a heavier dependency than rolling our own cookie-issuing session helper. Accepted because Phase-5 dashboard work needs OAuth-with-callback for both GitHub and GitLab and Auth.js handles the multi-provider matrix cleanly.

## Alternatives considered

- **Remix** — same React-as-Hypermedia philosophy as App Router, smaller community, less Vercel-native. Rejected because the marketing pages benefit from Next 15's static route generation and edge runtime, and the team's React/Next experience is deeper than Remix.
- **Vite + React SPA** — lighter bundle, simpler dev story, no server-side rendering. Rejected because share/verifier pages need SSR-quality first-paint for link unfurls; SPA would force us to write a second renderer in TS to match the Go output. Splits the source-of-truth contract.
- **Render share pages from Next.js too** — would let us ship one renderer in TS instead of one in Go and one in TS. Rejected because the CLI is Apache-2.0 Go; the renderer must live in `optiqor-cli/pkg/htmlrender` to be importable by both the CLI binary and the Go API. Forcing it into Next would mean either rewriting it in TS (drift risk) or exec-shelling the CLI from a Next route (latency + ops complexity).
- **Astro for marketing + Next for dashboard** — strong static-site story for marketing pages. Rejected because the marginal value over Next 15 App Router's static export is small and adds a third toolchain (`astro` + `next` + `go`) for what is, today, ~12 marketing pages.
- **Pure Go templates everywhere** — would maximise the "one language" anti-pattern from CLAUDE.md. Rejected because the customer dashboard (Auth.js, TanStack Query for live state, Stripe Elements for billing) needs the React/TS ecosystem.

## Links

- [`docs/strategy/technical_implementation.md`](../strategy/technical_implementation.md) §6 (sandbox surface)
- [`docs/strategy/business_strategy.md`](../strategy/business_strategy.md) §6.3 (`works-with/*` page commitments)
- `optiqor-cli/pkg/htmlrender/` — the shared renderer
- `optiqor-cli/brand/tokens.json` — brand source of truth
- ADR-0007 (LLM isolation) — same OSS/proprietary boundary pattern this ADR applies to the frontend
- ADR-0006 (pure methodology library) — same "single source of truth in `pkg/`" pattern
