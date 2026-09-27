import type { Metadata } from "next";
import Link from "next/link";
import { Container } from "@/components/container";
import { Section, Eyebrow } from "@/components/section";
import { ButtonLink } from "@/components/button";

export const metadata: Metadata = {
  title: "How it works",
  description:
    "The three layers of Optiqor: deterministic detection, gated Apply Fix, and cryptographically verifiable Receipts.",
};

const steps = [
  {
    n: "01",
    title: "Detection",
    body:
      "Helm values are normalised into a workload model. Thirty detectors — 15 cost, 15 security — run as pure functions over that model. No LLM is involved; the engine is byte-identical between the offline CLI and the SaaS sandbox.",
    detail: "github.com/optiqor/optiqor-cli/pkg/rules",
  },
  {
    n: "02",
    title: "Pricing",
    body:
      "Cost-bearing findings get a monthly dollar estimate against your region's on-demand rate. Sandbox uses a static m6i baseline; the agent uses your actual CUR. Estimates ship with a confidence band — Low, Medium, or High — derived from how much measured data backed the call.",
    detail: "internal/cost · internal/confidence",
  },
  {
    n: "03",
    title: "Validation",
    body:
      "Before any Apply Fix opens a PR, the candidate goes through a render → conform → dry-run gate that runs against your live API server's admission webhooks. We never ship a diff that wouldn't apply cleanly.",
    detail: "kubectl --dry-run=server · kubeconform · helm template",
  },
  {
    n: "04",
    title: "Auto-Rollback",
    body:
      "The agent watches the workload for 7 days after merge. If the observed P95 latency, error rate, or CPU saturation crosses the bound the prediction promised, an automated rollback PR is opened — no human in the loop required.",
    detail: "internal/rollback · 7-day window · 3 signals",
  },
  {
    n: "05",
    title: "Verified Receipt",
    body:
      "After the window closes cleanly, Optiqor issues a single signed document with the predicted and realised savings, the observation window, and the cloud-bill source. Anyone can verify the Ed25519 signature offline.",
    detail: "Ed25519 · KMS-asymmetric · transparency-logged",
  },
];

export default function HowItWorksPage() {
  return (
    <>
      <section className="pt-20 pb-12 md:pt-28 md:pb-16">
        <Container size="xl">
          <Eyebrow>How it works</Eyebrow>
          <h1 className="mt-3 max-w-[26ch] text-[clamp(36px,5.5vw,64px)] leading-[1.04] tracking-[-0.025em] font-medium">
            Five layers. <span className="text-[color:var(--color-ink-7)]">Each one auditable.</span>
          </h1>
          <p className="mt-5 max-w-[58ch] text-[16px] leading-relaxed text-[color:var(--color-ink-8)]">
            Optiqor is a small product on purpose. The pipeline below is the
            whole thing &mdash; from a Helm chart in your repo to a Receipt your
            CFO can verify with `curl` and `openssl`.
          </p>
        </Container>
      </section>

      <Section border>
        <Container size="xl">
          <ol className="grid gap-px bg-[color:var(--color-rule)] hairline rounded-[var(--radius-lg)] overflow-hidden">
            {steps.map((s) => (
              <li
                key={s.n}
                className="grid bg-[color:var(--color-ink-1)] p-8 md:p-10 md:grid-cols-[120px_1fr_280px] md:items-start md:gap-8"
              >
                <span className="font-mono text-[11px] tracking-[0.14em] uppercase text-[color:var(--color-ink-6)]">
                  step {s.n}
                </span>
                <div className="mt-3 md:mt-0">
                  <h2 className="text-[24px] font-medium tracking-[-0.02em]">
                    {s.title}
                  </h2>
                  <p className="mt-3 max-w-[58ch] text-[15px] leading-relaxed text-[color:var(--color-ink-8)]">
                    {s.body}
                  </p>
                </div>
                <div className="mt-4 md:mt-0 md:text-right">
                  <span className="inline-block font-mono text-[11px] leading-relaxed text-[color:var(--color-ink-7)] hairline rounded-[var(--radius-sm)] px-2.5 py-1.5">
                    {s.detail}
                  </span>
                </div>
              </li>
            ))}
          </ol>
        </Container>
      </Section>

      <Section id="accuracy" border>
        <Container size="md">
          <Eyebrow>Accuracy</Eyebrow>
          <h2 className="mt-3 text-[clamp(24px,3.5vw,36px)] tracking-[-0.02em] font-medium">
            Two numbers, neither of them invented.
          </h2>
          <div className="mt-10 grid gap-px bg-[color:var(--color-rule)] hairline rounded-[var(--radius-lg)] overflow-hidden md:grid-cols-2">
            <div className="bg-[color:var(--color-ink-1)] p-8">
              <div className="font-mono-tabular text-[40px] tracking-[-0.02em]">
                ±40%
              </div>
              <p className="mt-1 font-mono text-[11px] tracking-[0.08em] uppercase text-[color:var(--color-ink-6)]">
                Sandbox
              </p>
              <p className="mt-4 text-[14px] leading-relaxed text-[color:var(--color-ink-8)]">
                Static analysis sees only the chart. Without a live workload we
                can&apos;t know your real P95 utilisation. ±40% is the band that
                holds across our test corpus when the input has CPU + memory
                requests; charts missing those get flagged unpriceable.
              </p>
            </div>
            <div className="bg-[color:var(--color-ink-1)] p-8">
              <div className="font-mono-tabular text-[40px] tracking-[-0.02em] text-[color:var(--color-accent)]">
                ±15%
              </div>
              <p className="mt-1 font-mono text-[11px] tracking-[0.08em] uppercase text-[color:var(--color-ink-6)]">
                Agent
              </p>
              <p className="mt-4 text-[14px] leading-relaxed text-[color:var(--color-ink-8)]">
                In-cluster, with 30 days of Prometheus history and your AWS
                CUR, the band tightens. The ±15% is published, not promised —
                miss it and Optiqor refunds the month, signed and dated.
              </p>
            </div>
          </div>
        </Container>
      </Section>

      <Section border>
        <Container size="xl">
          <div className="flex flex-col items-start gap-6 md:flex-row md:items-center md:justify-between">
            <p className="max-w-[44ch] text-[18px] tracking-[-0.01em] font-medium text-[color:var(--color-ink-9)]">
              See the same engine the SaaS uses, in your terminal.
            </p>
            <div className="flex items-center gap-3">
              <ButtonLink
                href="https://github.com/optiqor/optiqor-cli"
                external
                variant="primary"
              >
                Read the source
                <span aria-hidden className="font-mono text-[color:var(--color-ink-6)]">→</span>
              </ButtonLink>
              <Link
                href="/sandbox"
                className="font-mono text-[13px] text-[color:var(--color-ink-7)] hover:text-[color:var(--color-ink-9)] transition-colors"
              >
                or paste a values.yaml in the sandbox →
              </Link>
            </div>
          </div>
        </Container>
      </Section>
    </>
  );
}
