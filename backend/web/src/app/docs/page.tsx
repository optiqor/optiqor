import type { Metadata } from "next";
import Link from "next/link";
import { Container } from "@/components/container";
import { Eyebrow, Section } from "@/components/section";
import { ButtonLink } from "@/components/button";

export const metadata: Metadata = { title: "Docs" };

export default function DocsPage() {
  return (
    <Section>
      <Container size="md">
        <Eyebrow>Documentation</Eyebrow>
        <h1 className="mt-3 text-[clamp(32px,5vw,52px)] leading-[1.04] tracking-[-0.025em] font-medium">
          The docs site is rendering on{" "}
          <span className="font-mono text-[color:var(--color-accent)]">
            docs.optiqor.dev
          </span>
          .
        </h1>
        <p className="mt-5 text-[16px] leading-relaxed text-[color:var(--color-ink-8)]">
          The Apache-2.0 docs site (Astro + MDX) is built from the CLI repo so
          external contributors can edit guides without a CLA. Until it ships,
          the canonical reference is the CLI&apos;s README and source.
        </p>
        <div className="mt-8 flex flex-wrap items-center gap-3">
          <ButtonLink
            href="https://github.com/optiqor/optiqor-cli#readme"
            external
            variant="primary"
          >
            CLI README
          </ButtonLink>
          <ButtonLink
            href="https://github.com/optiqor/optiqor-cli/tree/main/pkg/rules"
            external
            variant="secondary"
          >
            Detector catalogue
          </ButtonLink>
          <Link href="/how-it-works" className="link-inline font-mono text-[13px]">
            How it works →
          </Link>
        </div>
      </Container>
    </Section>
  );
}
