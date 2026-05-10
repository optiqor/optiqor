import type { Metadata } from "next";
import Link from "next/link";
import { Container } from "@/components/container";
import { Eyebrow, Section } from "@/components/section";
import { ButtonLink } from "@/components/button";
import { Pill } from "@/components/badge";
import { cx } from "@/lib/cx";

export const metadata: Metadata = {
  title: "Pricing",
  description:
    "Free tier, Team, Enterprise. Optiqor's CLI is Apache 2.0 forever. Paid plans add the agent, signed Receipts, and SLA.",
};

const tiers = [
  {
    name: "Free",
    price: "$0",
    cadence: "forever",
    blurb: "Audit, sandbox, and CI on your own time.",
    bullets: [
      "Apache 2.0 CLI",
      "Unlimited sandbox analyses",
      "Up to 2 clusters",
      "1 verified Receipt / month",
      "Community support",
    ],
    cta: { label: "npx @optiqor/cli demo", href: "/sandbox" },
    accent: false,
  },
  {
    name: "Team",
    price: "$500",
    cadence: "/ month",
    blurb: "Day-to-day Apply Fix flow with a real safety net.",
    bullets: [
      "Up to 5 clusters",
      "Unlimited Receipts",
      "PR-comment SLA",
      "Apply Fix + 7-day Auto-Rollback",
      "Slack digest + dashboard",
      "Cost-spike notifications",
      "Email support, 1 business day",
    ],
    cta: { label: "Start a trial", href: "/install" },
    accent: true,
  },
  {
    name: "Enterprise",
    price: "Custom",
    cadence: "",
    blurb: "Regulated, EU-resident, in-VPC.",
    bullets: [
      "Unlimited clusters",
      "SOC 2 Type 1 (Type 2 in flight)",
      "EU residency option",
      "In-VPC SaaS deployment",
      "DPA, SSO/SAML",
      "Dedicated CSM, 4-hour SLA",
      "Custom Receipt schema versions",
    ],
    cta: { label: "Talk to sales", href: "/contact" },
    accent: false,
  },
] as const;

export default function PricingPage() {
  return (
    <>
      <section className="pt-20 pb-12 md:pt-28 md:pb-16">
        <Container size="xl">
          <Eyebrow>Pricing</Eyebrow>
          <h1 className="mt-3 max-w-[28ch] text-[clamp(36px,5.5vw,64px)] leading-[1.04] tracking-[-0.025em] font-medium">
            Free where it has to be.{" "}
            <span className="text-[color:var(--color-ink-7)]">
              Paid where it&apos;s worth it.
            </span>
          </h1>
          <p className="mt-5 max-w-[58ch] text-[16px] leading-relaxed text-[color:var(--color-ink-8)]">
            The CLI is Apache&nbsp;2.0 forever. Paid plans add the in-cluster
            agent, exact-number savings, signed Receipts you can wave at an
            auditor, and the Apply Fix automation.
          </p>
        </Container>
      </section>

      <section className="pb-20">
        <Container size="xl">
          <div className="grid gap-px bg-[color:var(--color-rule)] hairline rounded-[var(--radius-lg)] overflow-hidden md:grid-cols-3">
            {tiers.map((t) => (
              <Tier key={t.name} {...t} />
            ))}
          </div>
        </Container>
      </section>

      {/* FAQ */}
      <Section border>
        <Container size="md">
          <Eyebrow>FAQ</Eyebrow>
          <h2 className="mt-3 text-[clamp(24px,3.5vw,36px)] tracking-[-0.02em] font-medium">
            Common questions
          </h2>
          <dl className="mt-12 space-y-px bg-[color:var(--color-rule)] hairline rounded-[var(--radius-lg)] overflow-hidden">
            <QA q="Is the CLI really Apache 2.0?">
              Yes. The full source is at{" "}
              <a
                href="https://github.com/optiqor/optiqor-cli"
                className="link-inline"
                target="_blank"
                rel="noopener noreferrer"
              >
                github.com/optiqor/optiqor-cli
              </a>
              . Backend imports it via go.mod &mdash; there is no second
              &ldquo;real&rdquo; engine you can&apos;t inspect.
            </QA>
            <QA q="What counts as a Receipt?">
              A single signed claim covering one merged Apply Fix and its 7-day
              measurement window. Free tier issues one per month; Team and
              Enterprise have no cap.
            </QA>
            <QA q="Do you store our charts?">
              Sandbox uploads are content-addressed and expire after 30 days.
              Agent customers ship sanitised metrics only &mdash; never raw
              YAML &mdash; over mTLS. Full data flow in{" "}
              <Link href="/security" className="link-inline">
                Security
              </Link>
              .
            </QA>
            <QA q="Can we self-host?">
              Enterprise plans get an in-VPC deployment with the same Terraform
              we run in production. Lead time is two weeks.
            </QA>
            <QA q="What if Optiqor disappears tomorrow?">
              The CLI keeps working forever (Apache 2.0). Existing Receipts
              remain verifiable against the public-key registry indefinitely
              &mdash; that&apos;s the point of Ed25519.
            </QA>
          </dl>
        </Container>
      </Section>

      <section className="border-t border-[color:var(--color-rule)] py-10">
        <Container size="xl">
          <p className="font-mono text-[12px] text-[color:var(--color-ink-7)]">
            Pricing in USD, billed monthly. No annual lock-in.
          </p>
        </Container>
      </section>
    </>
  );
}

function Tier({
  name,
  price,
  cadence,
  blurb,
  bullets,
  cta,
  accent,
}: (typeof tiers)[number]) {
  return (
    <div
      className={cx(
        "bg-[color:var(--color-ink-1)] p-8 md:p-9 flex flex-col",
        accent && "bg-[color:var(--color-ink-2)]",
      )}
    >
      <div className="flex items-baseline justify-between">
        <h3 className="text-[20px] font-medium">{name}</h3>
        {accent && (
          <Pill variant="accent">
            <span>Most teams</span>
          </Pill>
        )}
      </div>

      <div className="mt-6 flex items-baseline gap-2">
        <span className="font-mono-tabular text-[40px] tracking-[-0.02em] text-[color:var(--color-ink-9)]">
          {price}
        </span>
        {cadence && (
          <span className="font-mono text-[13px] text-[color:var(--color-ink-7)]">
            {cadence}
          </span>
        )}
      </div>

      <p className="mt-3 text-[14px] leading-relaxed text-[color:var(--color-ink-8)] min-h-[3em]">
        {blurb}
      </p>

      <ul className="mt-8 space-y-2.5">
        {bullets.map((b) => (
          <li
            key={b}
            className="flex items-start gap-2.5 text-[14px] text-[color:var(--color-ink-8)]"
          >
            <span
              aria-hidden
              className="mt-2 size-1 shrink-0 rounded-full bg-[color:var(--color-accent)]"
            />
            <span>{b}</span>
          </li>
        ))}
      </ul>

      <div className="mt-auto pt-10">
        <ButtonLink
          href={cta.href}
          variant={accent ? "primary" : "secondary"}
          className="w-full"
        >
          {cta.label}
          <span aria-hidden className="font-mono text-[color:var(--color-ink-6)]">→</span>
        </ButtonLink>
      </div>
    </div>
  );
}

function QA({ q, children }: { q: string; children: React.ReactNode }) {
  return (
    <div className="bg-[color:var(--color-ink-1)] p-6 md:p-7">
      <dt className="text-[16px] font-medium text-[color:var(--color-ink-9)]">{q}</dt>
      <dd className="mt-2 text-[14px] leading-relaxed text-[color:var(--color-ink-8)]">
        {children}
      </dd>
    </div>
  );
}
