import type { Metadata } from "next";
import { Container } from "@/components/container";
import { Eyebrow, Section } from "@/components/section";
import { ButtonLink } from "@/components/button";

export const metadata: Metadata = {
  title: "Security",
  description:
    "How Optiqor handles your data — multi-tenant isolation, customer-managed secrets, signed Receipts, vulnerability disclosure.",
};

const sections = [
  {
    title: "Tenant isolation",
    body:
      "Every Postgres query runs inside a transaction that binds the tenant id to a session-local GUC; Row-Level Security policies on every tenant-scoped table enforce isolation server-side. There is no admin escape hatch in application code — the migration role is separate from the app role and only the migrator can bypass RLS.",
    mono: "RLS · set_config('app.tenant_id', $1, true)",
  },
  {
    title: "Customer credentials",
    body:
      "Your AWS keys, GitHub installation tokens, and any other customer secret live encrypted at rest with a per-tenant KMS data key. The plaintext is loaded into memory only inside the Temporal workflow that needs it — never logged, never serialised to disk, never shipped to Sentry.",
    mono: "KMS-envelope · per-tenant data key · 30-day rotation",
  },
  {
    title: "LLM defence",
    body:
      "Helm comments get stripped, prompt-injection markers detected and wrapped in <USER_DATA> boundaries with explicit don't-trust instructions, per-field length limits enforced before egress. Every model output goes through the same render/conform/dry-run gate as a human-written diff would.",
    mono: "internal/agent/llm/sanitizer · 4 layers",
  },
  {
    title: "Receipt verification",
    body:
      "Receipts are signed inside an AWS KMS asymmetric SIGN_VERIFY key — the private bytes never exist outside the HSM. The canonical payload is deterministic JSON; clients verify offline against our published public key. Every receipt is appended to a Sigstore-style transparency log so revocations are detectable.",
    mono: "Ed25519 · KMS · Rekor-style tlog",
  },
  {
    title: "Vulnerability disclosure",
    body:
      "Report suspected vulnerabilities to security@optiqor.dev. We acknowledge within one business day and publish a fix advisory in 30 days. CVSS-7+ findings get expedited handling; safe-harbor for good-faith research per disclose.io.",
    mono: "security@optiqor.dev · SLA: 1 day ack",
  },
  {
    title: "Compliance posture",
    body:
      "SOC 2 Type 1 audit is in flight (auditor: TBD, attestation expected Q3 2026); Type 2 follows after 6 months of observation. GDPR-ready: DPA template public, EU residency available on Enterprise, DSAR pipeline shipped in Phase 1. HIPAA / FedRAMP are not in scope for Year 1.",
    mono: "SOC 2 in-flight · GDPR · DPA public",
  },
] as const;

export default function SecurityPage() {
  return (
    <>
      <section className="pt-20 pb-12 md:pt-28 md:pb-16">
        <Container size="xl">
          <Eyebrow>Security</Eyebrow>
          <h1 className="mt-3 max-w-[28ch] text-[clamp(36px,5.5vw,64px)] leading-[1.04] tracking-[-0.025em] font-medium">
            The trust contract,{" "}
            <span className="text-[color:var(--color-ink-7)]">
              written down.
            </span>
          </h1>
          <p className="mt-5 max-w-[58ch] text-[16px] leading-relaxed text-[color:var(--color-ink-8)]">
            Cost optimisation tools see a lot of expensive data &mdash; your
            Helm charts, your AWS bill, your Prometheus metrics. Here is how
            Optiqor handles each, and how to verify we&apos;re doing what we
            say.
          </p>
        </Container>
      </section>

      <Section border>
        <Container size="md">
          <dl className="grid gap-px bg-[color:var(--color-rule)] hairline rounded-[var(--radius-lg)] overflow-hidden">
            {sections.map((s) => (
              <div
                key={s.title}
                className="grid bg-[color:var(--color-ink-1)] gap-6 p-8 md:grid-cols-[1fr_2fr] md:p-10"
              >
                <dt>
                  <h2 className="text-[20px] font-medium tracking-[-0.02em]">
                    {s.title}
                  </h2>
                  <p className="mt-3 font-mono text-[11px] leading-relaxed text-[color:var(--color-ink-6)]">
                    {s.mono}
                  </p>
                </dt>
                <dd className="text-[15px] leading-relaxed text-[color:var(--color-ink-8)]">
                  {s.body}
                </dd>
              </div>
            ))}
          </dl>
        </Container>
      </Section>

      <Section border>
        <Container size="xl">
          <div className="flex flex-col items-start gap-6 md:flex-row md:items-center md:justify-between">
            <p className="max-w-[44ch] text-[18px] tracking-[-0.01em] font-medium text-[color:var(--color-ink-9)]">
              Reading the source is the strongest verification we can offer.
            </p>
            <div className="flex items-center gap-3">
              <ButtonLink
                href="https://github.com/optiqor/optiqor-cli"
                external
                variant="primary"
              >
                CLI source
              </ButtonLink>
              <ButtonLink
                href="mailto:security@optiqor.dev"
                external
                variant="secondary"
              >
                security@optiqor.dev
              </ButtonLink>
            </div>
          </div>
        </Container>
      </Section>
    </>
  );
}
