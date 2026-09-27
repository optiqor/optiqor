"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import {
  ApiError,
  fetchApplyFixes,
  fmtRelative,
  fmtUSD,
  type ApplyFixItem,
  type ApplyFixList,
} from "@/lib/api";

type State =
  | { kind: "loading" }
  | { kind: "ready"; data: ApplyFixList }
  | { kind: "error"; error: ApiError | Error };

const TIMEOUT_MS = 6_000;

export function OpenPRsCard() {
  const [state, setState] = useState<State>({ kind: "loading" });
  const [cursor, setCursor] = useState<string | undefined>(undefined);
  const [retryCount, setRetryCount] = useState(0);

  useEffect(() => {
    const ctrl = new AbortController();
    const timeout = window.setTimeout(() => ctrl.abort(), TIMEOUT_MS);
    fetchApplyFixes({ state: "open", cursor }, undefined, ctrl.signal)
      .then((data) => setState({ kind: "ready", data }))
      .catch((err: unknown) => {
        if ((err as Error)?.name === "AbortError") return;
        setState({ kind: "error", error: err as Error });
      })
      .finally(() => window.clearTimeout(timeout));
    return () => {
      window.clearTimeout(timeout);
      ctrl.abort();
    };
  }, [cursor, retryCount]);

  const onRetry = () => {
    setState({ kind: "loading" });
    setRetryCount((n) => n + 1);
  };
  // setCursor reserved for the "load more" pagination button that
  // lands once Apply Fix volume warrants it; the dashboard is read-
  // only today.
  void setCursor;

  return (
    <section
      aria-labelledby="open-prs-card-title"
      className="rounded-[var(--radius-md)] border hairline p-5 h-full bg-[color:var(--color-bg)]"
    >
      <h3 id="open-prs-card-title" className="font-medium text-[15px]">
        Open Apply Fix PRs
      </h3>
      {state.kind === "loading" ? <Skeleton /> : null}
      {state.kind === "error" ? (
        <ErrorBlock err={state.error} onRetry={onRetry} />
      ) : null}
      {state.kind === "ready" ? <Body data={state.data} /> : null}
    </section>
  );
}

function Body({ data }: { data: ApplyFixList }) {
  if (!data.items.length) {
    return (
      <div className="mt-4 text-[13px] text-[color:var(--color-ink-8)] leading-relaxed">
        <p>No Apply Fix PRs yet. Install the in-cluster agent to start them.</p>
        <Link
          href="/install"
          className="mt-3 inline-flex items-center text-[12px] font-medium underline underline-offset-4"
        >
          Install the agent →
        </Link>
      </div>
    );
  }
  return (
    <ul className="mt-4 divide-y hairline" aria-live="polite">
      {data.items.slice(0, 5).map((item) => (
        <li key={item.id} className="py-2 first:pt-0 last:pb-0">
          <Row item={item} />
        </li>
      ))}
    </ul>
  );
}

function Row({ item }: { item: ApplyFixItem }) {
  return (
    <a
      href={item.pr_url}
      target="_blank"
      rel="noreferrer noopener"
      className="flex items-start justify-between gap-3 hover:opacity-90"
    >
      <div className="min-w-0">
        <div className="truncate text-[13px] font-medium">{item.repo}</div>
        <div className="mt-0.5 text-[11px] text-[color:var(--color-ink-7)]">
          opened {fmtRelative(item.opened_at)}
        </div>
      </div>
      <div className="text-right">
        <div className="text-[13px] font-medium tabular-nums">
          {fmtUSD(item.monthly_usd_cents)}/mo
        </div>
        <div className="text-[10px] uppercase tracking-wider text-[color:var(--color-ink-7)]">
          {item.state}
        </div>
      </div>
    </a>
  );
}

function Skeleton() {
  return (
    <ul className="mt-4 space-y-3 animate-pulse" aria-hidden>
      {[0, 1, 2].map((i) => (
        <li key={i} className="flex items-start justify-between gap-3">
          <div className="flex-1">
            <div className="h-3 w-2/3 rounded bg-[color:var(--color-ink-3)]/40" />
            <div className="mt-2 h-2 w-1/3 rounded bg-[color:var(--color-ink-3)]/30" />
          </div>
          <div className="h-3 w-12 rounded bg-[color:var(--color-ink-3)]/40" />
        </li>
      ))}
    </ul>
  );
}

function ErrorBlock({ err, onRetry }: { err: Error; onRetry: () => void }) {
  const reqId = err instanceof ApiError ? err.requestId : undefined;
  return (
    <div className="mt-4 text-[13px] text-[color:var(--color-ink-8)] leading-relaxed">
      <p>Could not load Apply Fix PRs. {err.message}</p>
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
