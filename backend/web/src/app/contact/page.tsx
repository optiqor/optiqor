import type { Metadata } from "next";
import { Container } from "@/components/container";
import { Eyebrow, Section } from "@/components/section";

export const metadata: Metadata = { title: "Contact" };

const channels = [
  { label: "Sales", email: "hello@optiqor.dev", note: "trials, pricing, demos" },
  { label: "Security", email: "security@optiqor.dev", note: "vulnerability disclosure" },
  { label: "Support", email: "support@optiqor.dev", note: "paying customers" },
  { label: "Press", email: "press@optiqor.dev", note: "briefings & quotes" },
];

export default function ContactPage() {
  return (
    <Section>
      <Container size="md">
        <Eyebrow>Contact</Eyebrow>
        <h1 className="mt-3 text-[clamp(32px,5vw,52px)] leading-[1.04] tracking-[-0.025em] font-medium">
          Four inboxes. We answer them all.
        </h1>
        <ul className="mt-10 grid gap-px bg-[color:var(--color-rule)] hairline rounded-[var(--radius-lg)] overflow-hidden">
          {channels.map((c) => (
            <li
              key={c.email}
              className="grid bg-[color:var(--color-ink-1)] p-6 md:grid-cols-[120px_1fr_auto] md:items-center md:p-7"
            >
              <span className="font-mono text-[11px] tracking-[0.14em] uppercase text-[color:var(--color-ink-6)]">
                {c.label}
              </span>
              <a
                href={`mailto:${c.email}`}
                className="font-mono text-[15px] text-[color:var(--color-ink-9)] hover:text-[color:var(--color-accent)] transition-colors mt-1 md:mt-0"
              >
                {c.email}
              </a>
              <span className="text-[13px] text-[color:var(--color-ink-7)] mt-1 md:mt-0 md:text-right">
                {c.note}
              </span>
            </li>
          ))}
        </ul>
      </Container>
    </Section>
  );
}
