"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import {
  ApiError,
  fetchSavingsSummary,
  fmtUSD,
  type SavingsSummary,
} from "@/lib/api";

type State =
  | { kind: "loading" }
  | { kind: "ready"; data: SavingsSummary }
  | { kind: "error"; error: ApiError | Error };

const REFRESH_MS = 30_000;
const TIMEOUT_MS = 6_000;

export function SavingsCard() {
  const [state, setState] = useState<State>({ kind: "loading" });
  const [retryCount, setRetryCount] = useState(0);

  useEffect(() => {
    const ctrl = new AbortController();
    const timeout = window.setTimeout(() => ctrl.abort(), TIMEOUT_MS);
    const load = (signal: AbortSignal) =>
      fetchSavingsSummary(undefined, signal)
        .then((data) => setState({ kind: "ready", data }))
        .catch((err: unknown) => {
          if ((err as Error)?.name === "AbortError") return;
          setState({ kind: "error", error: err as Error });
        });
    void load(ctrl.signal).finally(() => window.clearTimeout(timeout));
    const tick = window.setInterval(() => {
      const refreshCtrl = new AbortController();
      window.setTimeout(() => refreshCtrl.abort(), TIMEOUT_MS);
      void load(refreshCtrl.signal);
    }, REFRESH_MS);
    return () => {
      window.clearTimeout(timeout);
      window.clearInterval(tick);
      ctrl.abort();
    };
  }, [retryCount]);

  const onRetry = () => {
    setState({ kind: "loading" });
    setRetryCount((n) => n + 1);
  };

  return (
    <section
      aria-labelledby="savings-card-title"
      className="rounded-[var(--radius-md)] border hairline p-5 h-full bg-[color:var(--color-bg)]"
    >
      <div className="flex items-start justify-between">
        <h3 id="savings-card-title" className="font-medium text-[15px]">
          Savings
        </h3>
        {state.kind === "ready" && state.data.is_demo ? (
          <span
            className="text-[10px] uppercase tracking-wider text-[color:var(--color-warn,#a96b1a)]"
            title={state.data.demo_disclaimer ?? "Demo data shown until the agent reports real merges."}
          >
            Demo
          </span>
        ) : null}
      </div>

      {state.kind === "loading" ? <SkeletonBlock /> : null}
      {state.kind === "error" ? <ErrorBlock err={state.error} onRetry={onRetry} /> : null}
      {state.kind === "ready" ? <Body data={state.data} /> : null}
    </section>
  );
}

function Body({ data }: { data: SavingsSummary }) {
  const empty = data.lifetime_cents === 0 && !data.is_demo;
  if (empty) {
    return (
      <div className="mt-4 text-[13px] text-[color:var(--color-ink-8)] leading-relaxed">
        <p>No realized savings yet. Open the sandbox to see what we&rsquo;d find on your charts.</p>
        <Link
          href="/sandbox"
          className="mt-3 inline-flex items-center text-[12px] font-medium underline underline-offset-4"
        >
          Open the sandbox →
        </Link>
      </div>
    );
  }
  return (
    <dl className="mt-4 grid grid-cols-3 gap-3" aria-live="polite">
      <Stat label="Lifetime" cents={data.lifetime_cents} />
      <Stat label="MTD" cents={data.month_to_date_cents} />
      <Stat label="YTD" cents={data.year_to_date_cents} />
      <div className="col-span-3 mt-1 text-[11px] text-[color:var(--color-ink-7)]">
        {data.merged_count} Apply Fix{data.merged_count === 1 ? "" : "es"} merged
      </div>
    </dl>
  );
}

function Stat({ label, cents }: { label: string; cents: number }) {
  return (
    <div>
      <dt className="text-[10px] uppercase tracking-wider text-[color:var(--color-ink-7)]">
        {label}
      </dt>
      <dd className="mt-1 text-[18px] font-medium tabular-nums">
        {fmtUSD(cents)}
      </dd>
    </div>
  );
}

function SkeletonBlock() {
  return (
    <div className="mt-4 grid grid-cols-3 gap-3 animate-pulse" aria-hidden>
      {[0, 1, 2].map((i) => (
        <div key={i}>
          <div className="h-2 w-12 rounded bg-[color:var(--color-ink-3)]/30" />
          <div className="mt-2 h-5 w-20 rounded bg-[color:var(--color-ink-3)]/40" />
        </div>
      ))}
    </div>
  );
}

function ErrorBlock({ err, onRetry }: { err: Error; onRetry: () => void }) {
  const reqId = err instanceof ApiError ? err.requestId : undefined;
  return (
    <div className="mt-4 text-[13px] text-[color:var(--color-ink-8)] leading-relaxed">
      <p>Could not load savings. {err.message}</p>
      {reqId ? (
        <p className="mt-1 text-[11px] text-[color:var(--color-ink-7)]">
          Reference: <code>{reqId}</code>
        </p>
      ) : null}
      <button
        type="button"
        onClick={onRetry}
        className="mt-3 inline-flex items-center rounded-[var(--radius-sm)] border hairline px-3 py-1.5 text-[12px] font-medium hover:bg-[color:var(--color-ink-1)]"
      >
        Retry
      </button>
    </div>
  );
}
