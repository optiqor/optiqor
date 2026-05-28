"use client";

import { useCallback, useMemo, useState } from "react";
import { analyze, ApiError, fmtUSD, type AnalyzeResponse } from "@/lib/api";
import { SeverityBadge } from "@/components/badge";
import { cx } from "@/lib/cx";

const SAMPLE = `# Paste your Helm values.yaml here.
api:
  replicas: 3
  resources:
    requests: {cpu: 2, memory: 1.6Gi}
    limits:   {cpu: 2.5, memory: 2Gi}
  image: nginx:1.25

worker:
  replicas: 10
  resources:
    requests: {cpu: 200m, memory: 256Mi}
`;

type Status =
  | { kind: "idle" }
  | { kind: "loading" }
  | { kind: "ok"; data: AnalyzeResponse }
  | { kind: "err"; message: string };

export function SandboxClient() {
  const [values, setValues] = useState<string>(SAMPLE);
  const [status, setStatus] = useState<Status>({ kind: "idle" });
  const [copied, setCopied] = useState(false);

  const run = useCallback(async () => {
    if (!values.trim()) {
      setStatus({ kind: "err", message: "Paste a values.yaml to analyze." });
      return;
    }
    setStatus({ kind: "loading" });
    try {
      const data = await analyze(values);
      setStatus({ kind: "ok", data });
    } catch (e) {
      const msg =
        e instanceof ApiError
          ? `${e.status} · ${e.message}`
          : e instanceof Error
            ? e.message
            : "Unexpected error.";
      setStatus({ kind: "err", message: msg });
    }
  }, [values]);

  const onKey = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
        e.preventDefault();
        void run();
      }
    },
    [run],
  );

  return (
    <div className="grid gap-6 lg:grid-cols-[1.05fr_1fr]">
      <Editor
        values={values}
        setValues={setValues}
        onKey={onKey}
        onRun={run}
        loading={status.kind === "loading"}
      />
      <Results
        status={status}
        copied={copied}
        onRetry={run}
        onCopy={() => {
          if (status.kind === "ok") {
            navigator.clipboard.writeText(status.data.share_url);
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1800);
          }
        }}
      />
    </div>
  );
}

function Editor({
  values,
  setValues,
  onKey,
  onRun,
  loading,
}: {
  values: string;
  setValues: (v: string) => void;
  onKey: (e: React.KeyboardEvent<HTMLTextAreaElement>) => void;
  onRun: () => void;
  loading: boolean;
}) {
  const lineCount = values.split("\n").length;
  const bytes = useMemo(() => new Blob([values]).size, [values]);
  return (
    <div className="overflow-hidden rounded-[var(--radius-lg)] bg-[color:var(--color-ink-1)] hairline flex flex-col">
      <div className="flex items-center justify-between border-b border-[color:var(--color-rule)] px-4 py-2.5">
        <span className="font-mono text-[11px] tracking-[0.1em] uppercase text-[color:var(--color-ink-6)]">
          values.yaml
        </span>
        <span className="font-mono text-[11px] text-[color:var(--color-ink-6)]">
          {lineCount} ln · {(bytes / 1024).toFixed(1)} KiB
        </span>
      </div>
      <textarea
        value={values}
        onChange={(e) => setValues(e.target.value)}
        onKeyDown={onKey}
        spellCheck={false}
        autoComplete="off"
        autoCorrect="off"
        autoCapitalize="off"
        className={cx(
          "min-h-[420px] flex-1 resize-none bg-transparent px-5 py-4",
          "font-mono text-[13px] leading-[1.7] text-[color:var(--color-ink-8)]",
          "outline-none placeholder:text-[color:var(--color-ink-5)]",
        )}
        placeholder="Paste a Helm values.yaml here."
      />
      <div className="flex items-center justify-between border-t border-[color:var(--color-rule)] px-4 py-2.5">
        <span className="font-mono text-[11px] text-[color:var(--color-ink-6)]">
          ⌘ + Enter to analyze
        </span>
        <button
          type="button"
          onClick={onRun}
          disabled={loading}
          className={cx(
            "inline-flex items-center gap-2 rounded-[var(--radius-md)] px-3.5 h-9 text-[13px] font-medium",
            "bg-[color:var(--color-ink-9)] text-[color:var(--color-ink-0)] hover:bg-white",
            "transition-colors disabled:opacity-50 disabled:cursor-not-allowed",
          )}
        >
          {loading ? (
            <>
              <Spinner />
              <span>Analyzing</span>
            </>
          ) : (
            <>
              <span>Analyze</span>
              <span aria-hidden className="font-mono text-[color:var(--color-ink-6)]">
                ↵
              </span>
            </>
          )}
        </button>
      </div>
    </div>
  );
}

function Results({
  status,
  copied,
  onCopy,
  onRetry,
}: {
  status: Status;
  copied: boolean;
  onCopy: () => void;
  onRetry: () => void;
}) {
  return (
    <div className="overflow-hidden rounded-[var(--radius-lg)] bg-[color:var(--color-ink-1)] hairline flex flex-col">
      <div className="flex items-center justify-between border-b border-[color:var(--color-rule)] px-4 py-2.5">
        <span className="font-mono text-[11px] tracking-[0.1em] uppercase text-[color:var(--color-ink-6)]">
          findings
        </span>
        <span className="font-mono text-[11px] text-[color:var(--color-ink-6)]">
          {status.kind === "ok"
            ? `${status.data.findings.length} total`
            : "ready"}
        </span>
      </div>

      <div className="flex-1 px-5 py-5">
        {status.kind === "idle" && <EmptyState />}
        {status.kind === "loading" && <LoadingState />}
        {status.kind === "err" && (
          <ErrorState message={status.message} onRetry={onRetry} />
        )}
        {status.kind === "ok" && (
          <ResultsBody data={status.data} copied={copied} onCopy={onCopy} />
        )}
      </div>

      <div className="border-t border-[color:var(--color-rule)] px-4 py-2.5">
        <p className="font-mono text-[11px] leading-relaxed text-[color:var(--color-ink-6)]">
          Sandbox accuracy: ±40%. Install the agent for exact numbers
          (optiqor.dev/get).
        </p>
      </div>
    </div>
  );
}

function ResultsBody({
  data,
  copied,
  onCopy,
}: {
  data: AnalyzeResponse;
  copied: boolean;
  onCopy: () => void;
}) {
  return (
    <div className="space-y-7">
      <header className="space-y-3">
        <div className="flex items-baseline gap-3">
          <span className="font-mono-tabular text-[32px] tracking-[-0.02em] text-[color:var(--color-ink-9)]">
            {data.monthly_savings_usd > 0
              ? fmtUSD(Math.round(data.monthly_savings_usd * 100))
              : "$0"}
          </span>
          <span className="font-mono text-[12px] text-[color:var(--color-ink-7)]">
            /mo
          </span>
          {data.annual_savings_usd > 0 && (
            <span className="font-mono text-[12px] text-[color:var(--color-ink-6)]">
              · ~{fmtUSD(Math.round(data.annual_savings_usd * 100))}/yr
            </span>
          )}
          <span className="font-mono text-[11px] text-[color:var(--color-ink-6)]">
            ±40%
          </span>
        </div>
        <div className="grid grid-cols-3 gap-px bg-[color:var(--color-rule)] hairline rounded-[var(--radius-md)] overflow-hidden text-center">
          <Stat label="Workloads" value={data.workloads_analyzed} />
          <Stat label="Cost opts" value={data.cost_findings.length} accent />
          <Stat
            label="Security"
            value={data.security_findings_bonus.length}
            sub="bonus"
          />
        </div>
        <ShareRow url={data.share_url} copied={copied} onCopy={onCopy} />
      </header>

      {data.cost_findings.length > 0 && (
        <FindingsSection
          eyebrow="Cost optimizations"
          accent="cyan"
          findings={sortByDollar(data.cost_findings)}
          showDollar
        />
      )}

      {data.security_findings_bonus.length > 0 && (
        <FindingsSection
          eyebrow={`Security findings · bonus · ${data.security_findings_bonus.length}`}
          accent="amber"
          findings={data.security_findings_bonus}
          showDollar={false}
        />
      )}

      {data.findings.length === 0 && (
        <div className="rounded-[var(--radius-md)] hairline bg-[color:var(--color-ink-2)] p-5">
          <p className="text-[14px] text-[color:var(--color-ok)]">
            ✓ Clean. No findings.
          </p>
          <p className="mt-2 text-[13px] text-[color:var(--color-ink-7)]">
            Either your chart is already optimised or the detectors didn&apos;t
            see enough signal in the values you supplied.
          </p>
        </div>
      )}
    </div>
  );
}

function FindingsSection({
  eyebrow,
  accent,
  findings,
  showDollar,
}: {
  eyebrow: string;
  accent: "cyan" | "amber";
  findings: AnalyzeResponse["findings"];
  showDollar: boolean;
}) {
  const tone =
    accent === "cyan"
      ? "text-[color:var(--color-accent)]"
      : "text-[color:var(--color-med)]";
  return (
    <div>
      <div className="mb-3 flex items-center gap-3">
        <span className={cx("font-mono text-[11px] tracking-[0.1em] uppercase", tone)}>
          {eyebrow}
        </span>
        <span className="flex-1 term-rule" />
      </div>
      <ul className="space-y-2.5">
        {findings.map((f, i) => (
          <li
            key={`${f.DetectorID}-${f.Workload}-${i}`}
            className="rounded-[var(--radius-md)] hairline bg-[color:var(--color-ink-2)]/70 p-4"
          >
            <div className="flex items-center gap-3">
              <SeverityBadge level={f.Severity} />
              <span className="font-mono text-[12px] text-[color:var(--color-accent)] tracking-[0.04em]">
                {f.Workload}
              </span>
              {showDollar && f.MonthlyUSDCents > 0 && (
                <span className="ml-auto font-mono-tabular text-[13px] text-[color:var(--color-ok)]">
                  save ~{fmtUSD(f.MonthlyUSDCents)}/mo
                </span>
              )}
            </div>
            <div className="mt-2 text-[14px] text-[color:var(--color-ink-9)]">
              {f.Title}
            </div>
            {f.Detail && (
              <div className="mt-1.5 text-[13px] leading-relaxed text-[color:var(--color-ink-7)]">
                {f.Detail}
              </div>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}

function Stat({
  label,
  value,
  sub,
  accent,
}: {
  label: string;
  value: number;
  sub?: string;
  accent?: boolean;
}) {
  return (
    <div className="bg-[color:var(--color-ink-1)] py-3">
      <div
        className={cx(
          "font-mono-tabular text-[20px] tracking-[-0.02em]",
          accent ? "text-[color:var(--color-accent)]" : "text-[color:var(--color-ink-9)]",
        )}
      >
        {value}
      </div>
      <div className="mt-0.5 font-mono text-[10px] tracking-[0.1em] uppercase text-[color:var(--color-ink-6)]">
        {label}
        {sub && <span className="ml-1 text-[color:var(--color-ink-7)]">· {sub}</span>}
      </div>
    </div>
  );
}

function ShareRow({
  url,
  copied,
  onCopy,
}: {
  url: string;
  copied: boolean;
  onCopy: () => void;
}) {
  return (
    <div className="flex items-center gap-2 rounded-[var(--radius-md)] hairline bg-[color:var(--color-ink-2)] px-3 py-2.5">
      <span aria-hidden className="font-mono text-[12px] text-[color:var(--color-ink-6)]">
        share
      </span>
      <code className="flex-1 truncate font-mono text-[12px] text-[color:var(--color-ink-8)]">
        {url}
      </code>
      <button
        type="button"
        onClick={onCopy}
        className="inline-flex h-7 items-center rounded-[var(--radius-sm)] px-2 font-mono text-[11px] text-[color:var(--color-ink-8)] hover:bg-[color:var(--color-ink-3)] transition-colors"
      >
        {copied ? "✓ copied" : "copy"}
      </button>
    </div>
  );
}

function EmptyState() {
  return (
    <div className="space-y-2 py-12 text-center">
      <p className="font-mono text-[12px] tracking-[0.08em] uppercase text-[color:var(--color-ink-6)]">
        ready · ⌘ + Enter
      </p>
      <p className="max-w-[34ch] mx-auto text-[13px] text-[color:var(--color-ink-7)]">
        The detector library will run against your values and surface cost
        opportunities sorted by dollar impact.
      </p>
    </div>
  );
}

function LoadingState() {
  return (
    <div className="space-y-3 py-12 text-center">
      <Spinner />
      <p className="font-mono text-[11px] tracking-[0.08em] uppercase text-[color:var(--color-ink-6)]">
        parsing · running detectors · pricing
      </p>
    </div>
  );
}

function ErrorState({
  message,
  onRetry,
}: {
  message: string;
  onRetry: () => void;
}) {
  return (
    <div
      role="alert"
      className="rounded-[var(--radius-md)] hairline bg-[color:var(--color-ink-2)] p-4"
    >
      <p className="font-mono text-[11px] tracking-[0.08em] uppercase text-[color:var(--color-high)]">
        could not analyze
      </p>
      <p className="mt-2 text-[13px] leading-relaxed text-[color:var(--color-ink-8)] break-words">
        {message}
      </p>
      <div className="mt-3 flex items-center gap-3">
        <button
          type="button"
          onClick={onRetry}
          className="inline-flex h-7 items-center rounded-[var(--radius-sm)] bg-[color:var(--color-ink-9)] px-3 font-mono text-[11px] text-[color:var(--color-ink-0)] hover:bg-white transition-colors"
        >
          Retry analysis
        </button>
        <span className="font-mono text-[10px] tracking-[0.08em] uppercase text-[color:var(--color-ink-6)]">
          editing the yaml then ⌘+Enter also re-runs
        </span>
      </div>
    </div>
  );
}

function Spinner() {
  return (
    <span
      role="status"
      aria-label="Analyzing"
      className="inline-block size-3 animate-spin rounded-full border border-[color:var(--color-ink-6)] border-t-[color:var(--color-accent)]"
    />
  );
}

function sortByDollar(f: AnalyzeResponse["findings"]) {
  return [...f].sort((a, b) => {
    if ((a.MonthlyUSDCents > 0) !== (b.MonthlyUSDCents > 0)) {
      return a.MonthlyUSDCents > 0 ? -1 : 1;
    }
    return b.MonthlyUSDCents - a.MonthlyUSDCents;
  });
}
