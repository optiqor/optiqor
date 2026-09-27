import type { Metadata } from "next";
import { Container } from "@/components/container";
import { Eyebrow, Section } from "@/components/section";

export const metadata: Metadata = { title: "About" };

export default function AboutPage() {
  return (
    <Section>
      <Container size="md">
        <Eyebrow>About</Eyebrow>
        <h1 className="mt-3 text-[clamp(32px,5vw,52px)] leading-[1.04] tracking-[-0.025em] font-medium">
          Built by people who&apos;ve paid the bill.
        </h1>
        <div className="mt-8 space-y-5 text-[16px] leading-relaxed text-[color:var(--color-ink-8)]">
          <p>
            Optiqor is a small, opinionated company building one product: a
            cost-optimization layer for Kubernetes that you can verify without
            trusting us.
          </p>
          <p>
            We started from a simple frustration &mdash; every Kubernetes cost
            tool we&apos;ve used asks you to take its word for the savings.
            The dashboard says <em className="not-italic">$8,000/mo saved</em>;
            no one can prove it. The Receipt fixes that.
          </p>
          <p>
            The CLI is Apache 2.0 because regulated buyers shouldn&apos;t run
            closed-source binaries against their charts, and because the
            detector library belongs to the community as much as to us.
          </p>
        </div>
      </Container>
    </Section>
  );
}
