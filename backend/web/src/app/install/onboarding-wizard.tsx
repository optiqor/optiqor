"use client";

import { useCallback, useEffect, useState } from "react";
import {
  ApiError,
  fetchOnboardingState,
  stageLabels,
  transitionOnboarding,
  type OnboardingStage,
  type OnboardingState,
} from "@/lib/api";

// 5s is well above the p95 budget for /v1/onboarding/state; past it
// the user is staring at a spinner. We swap to a retry surface so they
// see what's wrong instead of an infinite "Loading…".
const ONBOARDING_LOAD_TIMEOUT_MS = 5000;

// "Mark complete" is the Phase-2 manual surface; Phase 5+ wires
// transitions automatically off VCS install + agent events. Tenant id
// flows through the X-Optiqor-Tenant header until JWT auth ships.
export function OnboardingWizard({ tenantId }: { tenantId: string }) {
  const [state, setState] = useState<OnboardingState | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [loadAttempt, setLoadAttempt] = useState(0);

  const tenantHeader = { "X-Optiqor-Tenant": tenantId };

  const load = useCallback(() => {
    setError(null);
    setState(null);
    setLoadAttempt((n) => n + 1);
  }, []);

  useEffect(() => {
    let cancelled = false;
    const controller = new AbortController();
    const timeout = window.setTimeout(() => {
      controller.abort();
    }, ONBOARDING_LOAD_TIMEOUT_MS);

    fetchOnboardingState(tenantHeader, controller.signal)
      .then((s) => {
        if (cancelled) return;
        setState(s);
      })
      .catch((e: unknown) => {
        if (cancelled) return;
        if (controller.signal.aborted) {
          setError(
            "Timed out after 5s reaching the api. Check your network or try again.",
          );
          return;
        }
        const msg =
          e instanceof ApiError
            ? `${e.status} · ${e.message}`
            : e instanceof Error
              ? e.message
              : String(e);
        setError(msg);
      })
      .finally(() => {
        window.clearTimeout(timeout);
      });
    return () => {
      cancelled = true;
      controller.abort();
      window.clearTimeout(timeout);
    };
    // Depend on tenantId + loadAttempt so a retry refires the effect.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tenantId, loadAttempt]);

  async function advance(to: OnboardingStage) {
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      const next = await transitionOnboarding(to, tenantHeader);
      setState(next);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  if (error) {
    return (
      <div
        role="alert"
        className="rounded-[var(--radius-md)] border hairline bg-[color:var(--color-ink-1)] p-5"
      >
        <p className="text-sm text-[color:var(--color-ink-8)]">
          Could not load onboarding state:{" "}
          <code className="text-[color:var(--color-ink-3)]">{error}</code>
        </p>
        <button
          type="button"
          onClick={load}
          className="mt-3 inline-flex h-7 items-center rounded-[var(--radius-sm)] bg-[color:var(--color-ink-3)] px-3 text-xs font-medium text-[color:var(--color-bg)] hover:opacity-90"
        >
          Retry
        </button>
      </div>
    );
  }

  if (!state) {
    return (
      <div
        role="status"
        aria-live="polite"
        className="rounded-[var(--radius-md)] border hairline p-5"
      >
        <p className="text-sm text-[color:var(--color-ink-7)]">
          Loading your progress… (up to 5s)
        </p>
      </div>
    );
  }

  return (
    <div className="rounded-[var(--radius-lg)] border hairline bg-[color:var(--color-ink-1)] p-6">
      <div className="flex items-baseline justify-between">
        <h2 className="text-lg font-medium">Your install progress</h2>
        <span className="text-xs text-[color:var(--color-ink-7)] tabular-nums">
          {state.progress_percent}% complete
        </span>
      </div>

      <ol className="mt-6 grid gap-2">
        {(Object.keys(stageLabels) as OnboardingStage[]).map((s) => {
          const reached = Boolean(state.reached_at[s]);
          const isCurrent = state.current === s;
          return (
            <li
              key={s}
              className={`flex items-center gap-3 rounded px-3 py-2 ${
                isCurrent ? "bg-[color:var(--color-bg)] border hairline" : ""
              }`}
            >
              <span
                className={`size-2.5 rounded-full ${
                  reached
                    ? "bg-[color:var(--color-accent)]"
                    : "bg-[color:var(--color-ink-5)] opacity-30"
                }`}
                aria-hidden
              />
              <span className="text-[14px]">{stageLabels[s]}</span>
              {reached && state.reached_at[s] ? (
                <span className="ml-auto text-[11px] tabular-nums text-[color:var(--color-ink-7)]">
                  {new Date(state.reached_at[s]).toLocaleDateString()}
                </span>
              ) : null}
            </li>
          );
        })}
      </ol>

      {state.next_stage ? (
        <div className="mt-6 flex items-center justify-between gap-4 border-t hairline pt-5">
          <div className="text-[13px] text-[color:var(--color-ink-8)]">
            Next step: <strong>{stageLabels[state.next_stage]}</strong>
          </div>
          <button
            type="button"
            disabled={busy}
            onClick={() => advance(state.next_stage as OnboardingStage)}
            className="rounded-[var(--radius-sm)] border hairline bg-[color:var(--color-ink-3)] px-3 py-1.5 text-xs font-medium text-[color:var(--color-bg)] hover:opacity-90 disabled:opacity-40"
          >
            {busy ? "Saving…" : "Mark complete"}
          </button>
        </div>
      ) : (
        <p className="mt-6 text-sm text-[color:var(--color-ink-7)] border-t hairline pt-5">
          All stages complete — you should be seeing signed Receipts on
          merged Apply Fixes.
        </p>
      )}

      <p className="mt-4 text-[11px] text-[color:var(--color-ink-7)]">
        Sandbox p95 budget {state.slos.sandbox_latency} · install → first PR{" "}
        {state.slos.install_to_first_pr} · install → first Receipt{" "}
        {state.slos.install_to_first_receipt}
      </p>
    </div>
  );
}
