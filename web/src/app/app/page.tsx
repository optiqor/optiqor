import Link from "next/link";
import { auth } from "@/auth";
import { Eyebrow } from "@/components/section";

export default async function AppHomePage() {
  const session = await auth();
  const name = session?.user?.name ?? session?.user?.email ?? "there";

  return (
    <>
      <Eyebrow>Dashboard</Eyebrow>
      <h1 className="mt-3 text-[clamp(28px,4vw,40px)] leading-[1.05] tracking-[-0.02em] font-medium">
        Welcome back, {name}.
      </h1>
      <p className="mt-4 max-w-[60ch] text-[15px] leading-relaxed text-[color:var(--color-ink-8)]">
        Optiqor sits in your PR layer. Connect a repo, install the
        in-cluster agent, and signed Receipts of realized savings start
        landing in the timeline below within 30 days of first merge.
      </p>

      <div className="mt-10 grid gap-4 md:grid-cols-2">
        <Card
          title="Analyses"
          desc="Every sandbox upload + every PR scan keyed by content hash."
          href="/app/analyses"
        />
        <Card
          title="Receipts"
          desc="Ed25519-signed proof of realized savings against your cloud bill."
          status="Phase 6"
        />
        <Card
          title="Apply Fixes"
          desc="PRs Optiqor opened and your merge history."
          status="Phase 4"
        />
        <Card
          title="Cost spikes"
          desc="Bill anomaly → most-likely-PR correlations."
          status="Phase 6"
        />
      </div>

      <div className="mt-10 rounded-[var(--radius-lg)] border hairline p-6 bg-[color:var(--color-ink-1)]">
        <Eyebrow>Onboarding</Eyebrow>
        <h2 className="mt-2 text-xl font-medium">Pick up where you left off</h2>
        <p className="mt-2 text-sm text-[color:var(--color-ink-8)]">
          Move from CLI demo to in-cluster agent to first signed Receipt.
        </p>
        <Link
          href="/install"
          className="mt-4 inline-flex items-center text-sm font-medium underline underline-offset-4"
        >
          Continue install →
        </Link>
      </div>
    </>
  );
}

function Card({
  title,
  desc,
  href,
  status,
}: {
  title: string;
  desc: string;
  href?: string;
  status?: string;
}) {
  const content = (
    <div className="rounded-[var(--radius-md)] border hairline p-5 h-full bg-[color:var(--color-bg)]">
      <div className="flex items-start justify-between">
        <h3 className="font-medium text-[15px]">{title}</h3>
        {status ? (
          <span className="text-[10px] uppercase tracking-wider text-[color:var(--color-ink-7)]">
            {status}
          </span>
        ) : null}
      </div>
      <p className="mt-2 text-[13px] text-[color:var(--color-ink-8)] leading-relaxed">
        {desc}
      </p>
    </div>
  );
  if (!href) return content;
  return (
    <Link href={href} className="hover:opacity-90 transition-opacity">
      {content}
    </Link>
  );
}
