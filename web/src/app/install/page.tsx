import type { Metadata } from "next";
import { Container } from "@/components/container";
import { Eyebrow, Section } from "@/components/section";
import { ButtonLink } from "@/components/button";

export const metadata: Metadata = { title: "Install" };

export default function InstallPage() {
  return (
    <>
      <section className="pt-20 pb-12 md:pt-28 md:pb-16">
        <Container size="md">
          <Eyebrow>Install</Eyebrow>
          <h1 className="mt-3 text-[clamp(32px,5vw,52px)] leading-[1.04] tracking-[-0.025em] font-medium">
            Three minutes. <span className="text-[color:var(--color-ink-7)]">No login required.</span>
          </h1>
          <p className="mt-5 max-w-[58ch] text-[16px] leading-relaxed text-[color:var(--color-ink-8)]">
            The CLI is the fastest path from zero to a real Optiqor analysis.
            Paid plans add the in-cluster agent for exact-number savings and
            signed Receipts — those instructions live behind the trial.
          </p>
        </Container>
      </section>

      <Section border>
        <Container size="md">
          <ol className="grid gap-px bg-[color:var(--color-rule)] hairline rounded-[var(--radius-lg)] overflow-hidden">
            <Step
              n="01"
              title="Install the CLI"
              body="The npm wrapper grabs the right Go binary for your platform on postinstall."
              command="npm install -g @optiqor/cli"
            />
            <Step
              n="02"
              title="Analyze a chart"
              body="Runs entirely offline. No telemetry. Outputs cost-first with security as a bonus."
              command="optiqor analyze ./charts/your-app"
            />
            <Step
              n="03"
              title="Share the report"
              body="Opt-in HTTPS upload to optiqor.dev/r/<hash>. Paste the URL in any PR."
              command="optiqor analyze ./charts/your-app --share"
            />
          </ol>

          <div className="mt-12 flex flex-wrap items-center gap-3">
            <ButtonLink href="/sandbox" variant="primary">
              Try it without installing
              <span aria-hidden className="font-mono text-[color:var(--color-ink-6)]">→</span>
            </ButtonLink>
            <ButtonLink
              href="https://github.com/optiqor/optiqor-cli"
              external
              variant="secondary"
            >
              Read the source
            </ButtonLink>
          </div>
        </Container>
      </Section>
    </>
  );
}

function Step({
  n,
  title,
  body,
  command,
}: {
  n: string;
  title: string;
  body: string;
  command: string;
}) {
  return (
    <li className="grid bg-[color:var(--color-ink-1)] p-6 md:grid-cols-[80px_1fr] md:items-start md:gap-6 md:p-8">
      <span className="font-mono text-[11px] tracking-[0.14em] uppercase text-[color:var(--color-ink-6)]">
        step {n}
      </span>
      <div>
        <h2 className="text-[18px] font-medium">{title}</h2>
        <p className="mt-2 text-[14px] leading-relaxed text-[color:var(--color-ink-8)]">
          {body}
        </p>
        <pre className="mt-4 hairline rounded-[var(--radius-md)] bg-[color:var(--color-ink-2)] px-4 py-3 font-mono text-[13px] leading-relaxed text-[color:var(--color-ink-9)] overflow-x-auto">
          <span className="text-[color:var(--color-ink-6)]">$</span> {command}
        </pre>
      </div>
    </li>
  );
}
