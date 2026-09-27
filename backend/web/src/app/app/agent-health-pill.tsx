"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import {
  ApiError,
  fetchAgentHealth,
  fmtRelative,
  type AgentHealth,
} from "@/lib/api";

type State =
  | { kind: "loading" }
  | { kind: "ready"; data: AgentHealth }
  | { kind: "error"; error: ApiError | Error };

const REFRESH_MS = 30_000;
const TIMEOUT_MS = 6_000;

export function AgentHealthPill() {
  const [state, setState] = useState<State>({ kind: "loading" });

  useEffect(() => {
    const ctrl = new AbortController();
    const timeout = window.setTimeout(() => ctrl.abort(), TIMEOUT_MS);
    const load = (signal: AbortSignal) =>
      fetchAgentHealth(undefined, signal)
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
  }, []);

  return (
    <section
      aria-labelledby="agent-health-pill-title"
      className="rounded-[var(--radius-md)] border hairline p-5 h-full bg-[color:var(--color-bg)]"
    >
      <h3 id="agent-health-pill-title" className="font-medium text-[15px]">
        Agent health
      </h3>
      {state.kind === "loading" ? <Skeleton /> : null}
      {state.kind === "error" ? <Error err={state.error} /> : null}
      {state.kind === "ready" ? <Body data={state.data} /> : null}
    </section>
  );
}

function Body({ data }: { data: AgentHealth }) {
  if (data.status === "unknown") {
    return (
      <div className="mt-4 text-[13px] text-[color:var(--color-ink-8)] leading-relaxed">
        <p>The in-cluster agent hasn&rsquo;t reported yet.</p>
        <Link
          href="/install"
          className="mt-3 inline-flex items-center text-[12px] font-medium underline underline-offset-4"
        >
          Install the agent →
        </Link>
      </div>
    );
  }
  const { variant, label } = classify(data);
  return (
    <div className="mt-4" aria-live="polite">
      <div className="flex items-center gap-2">
        <span
          className={`inline-block h-2 w-2 rounded-full ${variant}`}
          aria-hidden
        />
        <span className="text-[14px] font-medium capitalize">{label}</span>
      </div>
      <dl className="mt-3 grid grid-cols-2 gap-3 text-[12px]">
        <div>
          <dt className="text-[10px] uppercase tracking-wider text-[color:var(--color-ink-7)]">
            Last check-in
          </dt>
          <dd className="mt-1 tabular-nums">{fmtRelative(data.last_checkin)}</dd>
        </div>
        <div>
          <dt className="text-[10px] uppercase tracking-wider text-[color:var(--color-ink-7)]">
            Data freshness
          </dt>
          <dd className="mt-1 tabular-nums">{fmtFreshness(data.data_freshness_seconds)}</dd>
        </div>
      </dl>
      {data.version ? (
        <p className="mt-3 text-[10px] text-[color:var(--color-ink-7)]">
          agent {data.version}
        </p>
      ) : null}
    </div>
  );
}

type Variant = {
  variant: string;
  label: string;
};

function classify(data: AgentHealth): Variant {
  const freshMin = data.data_freshness_seconds / 60;
  if (data.status === "offline" || freshMin > 360) {
    return { variant: "bg-[color:var(--color-err,#a03030)]", label: "offline" };
  }
  if (data.status === "degraded" || freshMin > 60) {
    return { variant: "bg-[color:var(--color-warn,#a96b1a)]", label: "degraded" };
  }
  return { variant: "bg-[color:var(--color-ok,#1c7c3a)]", label: "healthy" };
}

function fmtFreshness(seconds: number): string {
  if (!seconds || seconds < 60) return `${seconds || 0}s`;
  const min = Math.floor(seconds / 60);
  if (min < 60) return `${min} min`;
  const hr = Math.floor(min / 60);
  return `${hr} hr`;
}

function Skeleton() {
  return (
    <div className="mt-4 animate-pulse" aria-hidden>
      <div className="h-3 w-24 rounded bg-[color:var(--color-ink-3)]/40" />
      <div className="mt-3 grid grid-cols-2 gap-3">
        {[0, 1].map((i) => (
          <div key={i}>
            <div className="h-2 w-16 rounded bg-[color:var(--color-ink-3)]/30" />
            <div className="mt-2 h-3 w-20 rounded bg-[color:var(--color-ink-3)]/40" />
          </div>
        ))}
      </div>
    </div>
  );
}

function Error({ err }: { err: globalThis.Error }) {
  const reqId = err instanceof ApiError ? err.requestId : undefined;
  return (
    <div className="mt-4 text-[13px] text-[color:var(--color-ink-8)] leading-relaxed">
      <p>Could not load agent status. {err.message}</p>
      {reqId ? (
        <p className="mt-1 text-[11px] text-[color:var(--color-ink-7)]">
          Reference: <code>{reqId}</code>
        </p>
      ) : null}
    </div>
  );
}
