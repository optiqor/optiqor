import Link from "next/link";
import { Eyebrow } from "@/components/section";
import { auth } from "@/auth";

// Placeholder until /v1/analyses lands in Phase 4. The CTAs surface
// the share-URL workflow so a signed-in user has something to do here.
export default async function AnalysesPage() {
  await auth();
  return (
    <>
      <Eyebrow>Analyses</Eyebrow>
      <h1 className="mt-3 text-[clamp(28px,4vw,40px)] leading-[1.05] tracking-[-0.02em] font-medium">
        Your analyses
      </h1>
      <p className="mt-4 max-w-[60ch] text-[15px] leading-relaxed text-[color:var(--color-ink-8)]">
        Every <code className="rounded bg-[color:var(--color-ink-1)] px-1.5 py-0.5 text-[13px]">optiqor analyze --share</code>{" "}
        run, every sandbox upload, every PR scan — addressed by content
        hash so reruns of the same values.yaml collapse onto a stable URL.
      </p>

      <div className="mt-10 rounded-[var(--radius-lg)] border hairline p-8 text-center bg-[color:var(--color-ink-1)]">
        <h2 className="text-lg font-medium">No analyses yet</h2>
        <p className="mt-2 text-[14px] text-[color:var(--color-ink-8)]">
          Run an analysis from the CLI, the sandbox, or by opening a PR
          on a connected repo. Results land here.
        </p>
        <div className="mt-6 flex flex-wrap justify-center gap-3 text-sm">
          <Link
            href="/sandbox"
            className="rounded-[var(--radius-sm)] border hairline px-3 py-1.5 hover:bg-[color:var(--color-bg)]"
          >
            Open sandbox
          </Link>
          <Link
            href="/install"
            className="rounded-[var(--radius-sm)] border hairline px-3 py-1.5 hover:bg-[color:var(--color-bg)]"
          >
            Install CLI
          </Link>
        </div>
      </div>

      <p className="mt-8 text-xs text-[color:var(--color-ink-7)]">
        Phase 4 wires the live list once <code>/v1/analyses</code> ships;
        the page shape and routing stay the same.
      </p>
    </>
  );
}
