import type { Metadata } from "next";
import { Container } from "@/components/container";
import { Eyebrow, Section } from "@/components/section";

export const metadata: Metadata = { title: "Legal" };

const docs = [
  { name: "Terms of Service", href: "/legal/terms" },
  { name: "Privacy Policy", href: "/legal/privacy" },
  { name: "Data Processing Addendum", href: "/legal/dpa" },
  { name: "Subprocessors", href: "/legal/subprocessors" },
  { name: "Acceptable Use", href: "/legal/aup" },
];

export default function LegalPage() {
  return (
    <Section>
      <Container size="md">
        <Eyebrow>Legal</Eyebrow>
        <h1 className="mt-3 text-[clamp(32px,5vw,52px)] leading-[1.04] tracking-[-0.025em] font-medium">
          Short documents, plainly written.
        </h1>
        <p className="mt-5 text-[16px] leading-relaxed text-[color:var(--color-ink-8)]">
          Drafts are in review with counsel; markdown sources land in the
          backend repo under <code className="font-mono text-[color:var(--color-ink-9)]">docs/legal/</code>{" "}
          when signed.
        </p>
        <ul className="mt-10 grid gap-px bg-[color:var(--color-rule)] hairline rounded-[var(--radius-lg)] overflow-hidden">
          {docs.map((d) => (
            <li
              key={d.href}
              className="bg-[color:var(--color-ink-1)] p-5 flex items-center justify-between"
            >
              <span className="text-[15px] text-[color:var(--color-ink-8)]">
                {d.name}
              </span>
              <span className="font-mono text-[11px] tracking-[0.08em] uppercase text-[color:var(--color-ink-6)]">
                draft
              </span>
            </li>
          ))}
        </ul>
      </Container>
    </Section>
  );
}
