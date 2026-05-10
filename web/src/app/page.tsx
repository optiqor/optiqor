import Link from "next/link";
import { Container } from "@/components/container";
import { Section, Eyebrow } from "@/components/section";
import { ButtonLink } from "@/components/button";
import { Pill } from "@/components/badge";
import { Logomark } from "@/components/logomark";
import {
  Terminal,
  SeverityToken,
  Savings,
  Muted,
  Workload,
  ConfDots,
} from "@/components/terminal";

export default function HomePage() {
  return (
    <>
      {/* ─── Hero ──────────────────────────────────────────────────────── */}
      <section className="relative overflow-hidden pt-24 pb-20 md:pt-36 md:pb-28">
        {/* single cyan glow behind the headline — not a gradient ribbon */}
        <div
          aria-hidden
          className="pointer-events-none absolute -top-32 left-1/2 -z-10 size-[640px] -translate-x-1/2 rounded-full"
          style={{
            background:
              "radial-gradient(closest-side, rgba(34, 211, 238, 0.10), transparent 70%)",
          }}
        />
        <Container size="xl">
          <div className="flex flex-col items-start gap-6">
            <Pill variant="accent">
              <span className="text-[color:var(--color-ink-7)]">v0.1 · early access</span>
            </Pill>

            <h1 className="max-w-[20ch] text-[clamp(44px,6.5vw,84px)] leading-[0.98] tracking-[-0.025em] font-medium">
              Kubernetes cost,
              <br />
              <span className="text-[color:var(--color-ink-7)]">
                proven against your bill.
              </span>
            </h1>

            <p className="max-w-[58ch] text-[18px] leading-relaxed text-[color:var(--color-ink-8)]">
              Optiqor reviews your Helm charts the way an SRE would &mdash;
              line-by-line, with measured impact and cited evidence. Every
              suggestion lands as a PR. Every dollar saved is signed.
            </p>

            <div className="mt-3 flex flex-wrap items-center gap-3">
              <ButtonLink href="/sandbox" variant="primary" size="md">
                Open the sandbox
                <span aria-hidden className="font-mono text-[color:var(--color-ink-6)]">→</span>
              </ButtonLink>
              <ButtonLink href="/how-it-works" variant="secondary" size="md">
                How it works
              </ButtonLink>
              <Link
                href="https://www.npmjs.com/package/@optiqor/cli"
                className="ml-2 font-mono text-[13px] text-[color:var(--color-ink-7)] hover:text-[color:var(--color-ink-9)] transition-colors"
              >
                npx @optiqor/cli analyze ./chart
              </Link>
            </div>
          </div>

          {/* Terminal preview anchored below the hero copy — the
              centrepiece. */}
          <div className="mt-16 md:mt-20">
            <Terminal title="optiqor analyze ./charts/api">
              <span className="text-[color:var(--color-ink-6)]">────────────────────────────────────────────────────────────────────</span>
              {"\n  "}
              <span className="text-[color:var(--color-accent)]">◐  optiqor</span>
              {"\n  "}
              <Muted>Helm chart cost optimization · security as a bonus</Muted>
              {"\n"}
              <span className="text-[color:var(--color-ink-6)]">────────────────────────────────────────────────────────────────────</span>
              {"\n\n  "}
              <Muted>Source     </Muted>
              <Workload>./charts/api</Workload>
              {"\n  "}
              <Muted>Workloads  </Muted>3 analyzed
              {"\n  "}
              <Muted>Cost       </Muted>5 optimizations · <Savings>save ~$348.20/mo (~$4,178/yr)</Savings> <Muted>±40%</Muted>
              {"\n  "}
              <Muted>Security   </Muted>12 findings — bonus, surfaced while parsing
              {"\n\n  "}
              <span className="text-[color:var(--color-accent)]">━━ Cost optimizations ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━</span>
              {"\n\n   "}
              <SeverityToken level="HIGH" />  <Workload>api</Workload>   <Savings>save ~$220.40/mo</Savings>
              {"\n    CPU request appears overprovisioned"}
              {"\n    "}
              <Muted>request 2 vs limit 2.5 — typical utilization rarely justifies this ratio.</Muted>
              {"\n    "}
              <Muted>confidence: </Muted><ConfDots level="med" /> <Muted>medium</Muted>
              {"\n\n   "}
              <SeverityToken level="MED" />   <Workload>api</Workload>   <Savings>save ~$96.40/mo</Savings>
              {"\n    Memory request appears overprovisioned"}
              {"\n    "}
              <Muted>request 1.6Gi vs limit 2Gi — halving the request keeps a 25% headroom.</Muted>
              {"\n    "}
              <Muted>confidence: </Muted><ConfDots level="med" /> <Muted>medium</Muted>
              {"\n\n   "}
              <SeverityToken level="MED" />   <Workload>worker</Workload>   <Savings>save ~$31.40/mo</Savings>
              {"\n    Replicas set to 10 without an HPA"}
              {"\n    "}
              <Muted>static high replica count pays for idle capacity 24/7.</Muted>
              {"\n    "}
              <Muted>confidence: </Muted><ConfDots level="med" /> <Muted>medium</Muted>
              {"\n\n  "}
              <span className="text-[color:var(--color-med)]">━━ Security findings  (bonus, 12) ━━━━━━━━━━━━━━━━━━━━━━━━</span>
              {"\n  "}
              <Muted>Spotted while parsing your chart. Cost is the headline; this is a bonus.</Muted>
              {"\n\n   "}
              <SeverityToken level="HIGH" />  <Workload>api    </Workload> <ConfDots level="high" />   <Muted>Container runs as root</Muted>
              {"\n   "}
              <SeverityToken level="MED" />   <Workload>api    </Workload> <ConfDots level="med" />   <Muted>capabilities.drop does not include ALL</Muted>
              {"\n   "}
              <SeverityToken level="MED" />   <Workload>worker </Workload> <ConfDots level="med" />   <Muted>ServiceAccount token auto-mounted</Muted>
              {"\n\n"}
              <span className="text-[color:var(--color-ink-6)]">────────────────────────────────────────────────────────────────────</span>
              {"\n  "}
              <Muted>estimated monthly savings:</Muted>{" "}
              <Savings>$348.20/mo</Savings> <Muted>(±40%)</Muted>
              {"\n  "}
              <Muted>→ install the agent for exact numbers: </Muted>
              <span className="text-[color:var(--color-accent)] underline underline-offset-2">optiqor.dev/get</span>
            </Terminal>

            <p className="mt-4 font-mono text-[11px] tracking-[0.08em] text-[color:var(--color-ink-6)]">
              <span aria-hidden>↑</span> live output. paste any{" "}
              <code className="text-[color:var(--color-ink-8)]">values.yaml</code>{" "}
              into{" "}
              <Link href="/sandbox" className="link-inline">
                the sandbox
              </Link>{" "}
              to get the same.
            </p>
          </div>
        </Container>
      </section>

      {/* ─── Three pillars ─────────────────────────────────────────────── */}
      <Section border>
        <Container size="xl">
          <Eyebrow>The contract</Eyebrow>
          <h2 className="mt-3 max-w-[22ch] text-[clamp(28px,4vw,44px)] leading-[1.05] tracking-[-0.02em] font-medium">
            Detect. Fix. Prove.
            <br />
            <span className="text-[color:var(--color-ink-7)]">
              Three steps. No magic. No telemetry.
            </span>
          </h2>

          <div className="mt-14 grid gap-px bg-[color:var(--color-rule)] md:grid-cols-3 md:rounded-[var(--radius-lg)] md:overflow-hidden hairline">
            <Pillar
              step="01"
              title="Detect"
              copy="A deterministic rule engine reads your Helm charts, normalises 30 detectors against the workload model, and surfaces every cost opportunity with cited evidence — not an LLM guess."
              monoLine="30 detectors · 0 LLM calls · runs offline"
            />
            <Pillar
              step="02"
              title="Fix"
              copy="In-cluster, Optiqor opens an Apply Fix PR with the exact values.yaml diff and a server-side dry-run against your live admission webhooks. Merge, or close. No surprises."
              monoLine="kubectl --dry-run=server · gated · idempotent"
            />
            <Pillar
              step="03"
              title="Prove"
              copy="A 7-day post-merge watchdog measures the realised delta against your actual cloud bill, then issues an Ed25519-signed Receipt finance can verify without trusting us."
              monoLine="±15% accuracy · transparency-log · WebCrypto-verifiable"
            />
          </div>
        </Container>
      </Section>

      {/* ─── Why ───────────────────────────────────────────────────────── */}
      <Section border>
        <Container size="xl">
          <div className="grid gap-16 md:grid-cols-[1fr_1.2fr] md:gap-24">
            <div className="space-y-6">
              <Eyebrow>Why Optiqor</Eyebrow>
              <h2 className="text-[clamp(28px,4vw,44px)] leading-[1.05] tracking-[-0.02em] font-medium">
                Every other tool tells you to{" "}
                <em className="not-italic text-[color:var(--color-ink-7)]">trust them</em>.
              </h2>
              <p className="max-w-[44ch] text-[16px] leading-relaxed text-[color:var(--color-ink-8)]">
                Dashboards promise savings. Optiqor signs them. The CLI is
                Apache&nbsp;2.0 and runs entirely offline. The agent reports
                ±15% accuracy against your AWS bill, not against a synthetic
                benchmark. The Receipt is a public artefact your CFO can
                verify in a browser.
              </p>
              <p className="max-w-[44ch] text-[16px] leading-relaxed text-[color:var(--color-ink-8)]">
                You don&apos;t need to take our word for any of it &mdash; and
                you shouldn&apos;t.
              </p>
            </div>

            <div className="grid grid-cols-2 gap-px bg-[color:var(--color-rule)] hairline rounded-[var(--radius-lg)] overflow-hidden">
              <Stat number="±40%" label="Sandbox accuracy" sublabel="static-files only" />
              <Stat number="±15%" label="Agent accuracy" sublabel="against your AWS bill" />
              <Stat number="<3s" label="Sandbox latency p95" sublabel="deterministic engine" />
              <Stat number="$0.40" label="Cost ceiling / analysis" sublabel="per CLAUDE.md hard rule" />
              <Stat number="30" label="Detectors shipped" sublabel="15 cost · 15 security bonus" />
              <Stat number="0" label="LLM calls in CLI" sublabel="never, ever — by design" />
            </div>
          </div>
        </Container>
      </Section>

      {/* ─── Receipts ──────────────────────────────────────────────────── */}
      <Section border>
        <Container size="xl">
          <Eyebrow>Verified Receipts</Eyebrow>
          <h2 className="mt-3 max-w-[24ch] text-[clamp(28px,4vw,44px)] leading-[1.05] tracking-[-0.02em] font-medium">
            A claim of savings{" "}
            <span className="text-[color:var(--color-ink-7)]">isn&apos;t a saving.</span>
          </h2>

          <div className="mt-12 grid gap-px bg-[color:var(--color-rule)] hairline rounded-[var(--radius-lg)] overflow-hidden md:grid-cols-[1.1fr_1fr]">
            <div className="bg-[color:var(--color-ink-1)] p-8 md:p-10">
              <p className="text-[15px] leading-relaxed text-[color:var(--color-ink-8)]">
                Every merged Apply Fix is watched for 7 days against the metrics
                that backed the prediction. After the window closes, Optiqor
                issues a Receipt: a short, signed document that says &ldquo;between{" "}
                <em className="not-italic text-[color:var(--color-ink-9)]">these dates</em>, your
                bill went down by{" "}
                <em className="not-italic text-[color:var(--color-ink-9)]">this much</em>, and
                here&apos;s the signature.&rdquo;
              </p>
              <p className="mt-5 text-[15px] leading-relaxed text-[color:var(--color-ink-8)]">
                Anyone &mdash; you, your CFO, an external auditor &mdash; can
                fetch{" "}
                <code className="text-[color:var(--color-ink-9)] font-mono">/v/&lt;id&gt;</code>{" "}
                and verify the signature offline against our public key.
              </p>
              <p className="mt-5 font-mono text-[12px] text-[color:var(--color-ink-7)]">
                Ed25519 · canonical JSON · transparency-logged · openly auditable
              </p>
            </div>

            <div className="bg-[color:var(--color-ink-2)] p-8 md:p-10 font-mono text-[12px] leading-[1.7] text-[color:var(--color-ink-8)]">
              <div className="text-[color:var(--color-ink-6)]"># GET /v/rcpt_01HQ0</div>
              <pre className="mt-3 whitespace-pre-wrap break-all text-[color:var(--color-ink-8)]">
{`{
  "id": "rcpt_01HQ0",
  "workload": "api",
  "observed_from_utc": "2026-04-11T00:00:00Z",
  "observed_to_utc":   "2026-05-11T00:00:00Z",
  `}
                <span className="text-[color:var(--color-ok)]">
                  {`"predicted_savings_usd_cents": 12000,`}
                </span>
                {`
  `}
                <span className="text-[color:var(--color-ok)]">
                  {`"realised_savings_usd_cents":  11500,`}
                </span>
                {`
  "cloud_bill_source": "aws/cur:2026-05",
  "issuer_key_id": "k1",
  "issued_at_utc": "2026-05-12T00:00:00Z"
}`}
              </pre>
              <div className="mt-4 text-[color:var(--color-ink-6)]">
                # signature.payload (base64url) ✓ verified
              </div>
              <div className="mt-1 break-all text-[color:var(--color-accent)]/85">
                IDtgr8u0vT1FZP…Q3Rwo
              </div>
            </div>
          </div>
        </Container>
      </Section>

      {/* ─── Open source CTA ───────────────────────────────────────────── */}
      <Section border>
        <Container size="xl">
          <div className="flex flex-col items-start gap-8 md:flex-row md:items-end md:justify-between">
            <div className="max-w-[44ch] space-y-5">
              <Eyebrow>Apache 2.0 CLI</Eyebrow>
              <h2 className="text-[clamp(28px,4vw,44px)] leading-[1.05] tracking-[-0.02em] font-medium">
                Audit it before you trust us.
              </h2>
              <p className="text-[15px] leading-relaxed text-[color:var(--color-ink-8)]">
                The detector library and the parser are open source. The
                backend imports them verbatim — there is no secondary &ldquo;real&rdquo;
                engine you can&apos;t inspect.
              </p>
            </div>
            <div className="flex items-center gap-3">
              <ButtonLink
                href="https://github.com/optiqor/optiqor-cli"
                external
                variant="primary"
              >
                Read the source
                <span aria-hidden className="font-mono text-[color:var(--color-ink-6)]">→</span>
              </ButtonLink>
              <ButtonLink href="/docs" variant="secondary">
                Documentation
              </ButtonLink>
            </div>
          </div>
        </Container>
      </Section>

      {/* ─── Accuracy disclosure (mandatory per CLAUDE.md hard rule) ──── */}
      <section className="border-t border-[color:var(--color-rule)] py-10">
        <Container size="xl">
          <div className="flex items-start gap-4">
            <Logomark size={20} glow={false} className="mt-1 text-[color:var(--color-ink-6)]" />
            <p className="font-mono text-[12px] leading-relaxed text-[color:var(--color-ink-7)]">
              Sandbox accuracy: ±40%. Install the Optiqor agent for exact
              numbers, backed by 30 days of Prometheus data and your AWS bill.{" "}
              <Link href="/how-it-works#accuracy" className="link-inline">
                Read the methodology
              </Link>
              .
            </p>
          </div>
        </Container>
      </section>
    </>
  );
}

function Pillar({
  step,
  title,
  copy,
  monoLine,
}: {
  step: string;
  title: string;
  copy: string;
  monoLine: string;
}) {
  return (
    <div className="bg-[color:var(--color-ink-1)] p-8 md:p-10 min-h-[320px] flex flex-col">
      <div className="flex items-center justify-between">
        <span className="font-mono text-[11px] tracking-[0.14em] uppercase text-[color:var(--color-ink-6)]">
          {step}
        </span>
        <span
          aria-hidden
          className="size-1.5 rounded-full bg-[color:var(--color-accent)]"
        />
      </div>
      <h3 className="mt-6 text-[28px] tracking-[-0.02em] font-medium">{title}</h3>
      <p className="mt-3 text-[15px] leading-relaxed text-[color:var(--color-ink-8)]">
        {copy}
      </p>
      <div className="mt-auto pt-6 font-mono text-[11px] tracking-[0.06em] text-[color:var(--color-ink-6)]">
        {monoLine}
      </div>
    </div>
  );
}

function Stat({
  number,
  label,
  sublabel,
}: {
  number: string;
  label: string;
  sublabel: string;
}) {
  return (
    <div className="bg-[color:var(--color-ink-1)] p-6 md:p-7">
      <div className="font-mono-tabular text-[32px] tracking-[-0.02em] text-[color:var(--color-ink-9)]">
        {number}
      </div>
      <div className="mt-2 text-[13px] text-[color:var(--color-ink-8)]">{label}</div>
      <div className="mt-0.5 font-mono text-[11px] text-[color:var(--color-ink-6)]">
        {sublabel}
      </div>
    </div>
  );
}
