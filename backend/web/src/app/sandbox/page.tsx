import type { Metadata } from "next";
import { Container } from "@/components/container";
import { Eyebrow } from "@/components/section";
import { SandboxClient } from "./sandbox-client";

export const metadata: Metadata = {
  title: "Sandbox",
  description:
    "Paste a Helm values.yaml. Get cost optimization findings in under three seconds. No login, no agent.",
};

export default function SandboxPage() {
  return (
    <section className="pt-12 pb-24">
      <Container size="xl">
        <div className="max-w-[58ch] space-y-4">
          <Eyebrow>Public sandbox</Eyebrow>
          <h1 className="text-[clamp(32px,5vw,52px)] leading-[1.04] tracking-[-0.025em] font-medium">
            Paste a values.yaml. <span className="text-[color:var(--color-ink-7)]">See the bill.</span>
          </h1>
          <p className="text-[16px] leading-relaxed text-[color:var(--color-ink-8)]">
            Runs the same deterministic detector library as the CLI. No auth,
            no telemetry, no data persisted beyond your share URL. Capped at
            1&nbsp;MiB.
          </p>
        </div>

        <div className="mt-10">
          <SandboxClient />
        </div>
      </Container>
    </section>
  );
}
