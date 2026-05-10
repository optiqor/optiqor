import { cx } from "@/lib/cx";

/**
 * Terminal — visual frame of a CLI session. Renders children inside a
 * monospace surface with a chrome bar carrying a window title + a
 * caret indicator. Strict in styling so the marketing terminal blocks
 * always look like the real CLI output.
 *
 * Use SeverityToken / Savings / Muted to colour data the way the CLI
 * theme does (hairline borders, colored bg-tints — no rainbow).
 */
export function Terminal({
  title = "optiqor analyze ./charts/api",
  children,
  className,
}: {
  title?: string;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cx(
        "overflow-hidden rounded-[var(--radius-lg)] bg-[color:var(--color-ink-1)] hairline",
        className,
      )}
    >
      <div className="flex items-center justify-between border-b border-[color:var(--color-rule)] px-4 py-2.5">
        <div className="flex items-center gap-2">
          <span aria-hidden className="size-2.5 rounded-full bg-[color:var(--color-ink-5)]" />
          <span aria-hidden className="size-2.5 rounded-full bg-[color:var(--color-ink-5)]" />
          <span aria-hidden className="size-2.5 rounded-full bg-[color:var(--color-ink-5)]" />
        </div>
        <span className="font-mono text-[11px] tracking-[0.1em] text-[color:var(--color-ink-6)]">
          {title}
        </span>
        <span className="font-mono text-[11px] text-[color:var(--color-ink-6)]">
          ◐
        </span>
      </div>
      <pre className="overflow-x-auto bg-[color:var(--color-ink-1)] px-4 py-5 font-mono text-[13px] leading-[1.7] text-[color:var(--color-ink-8)]">
        {children}
      </pre>
    </div>
  );
}

export function SeverityToken({ level }: { level: "HIGH" | "MED" | "LOW" }) {
  const color =
    level === "HIGH"
      ? "var(--color-high)"
      : level === "MED"
        ? "var(--color-med)"
        : "var(--color-low)";
  return (
    <span
      className="rounded-[3px] px-1.5 py-[1px] text-[11px] font-medium tracking-[0.06em]"
      style={{ backgroundColor: `${color}24`, color }}
    >
      {level.padEnd(4, " ")}
    </span>
  );
}

export function Savings({ children }: { children: React.ReactNode }) {
  return <span className="text-[color:var(--color-ok)]">{children}</span>;
}

export function Muted({ children }: { children: React.ReactNode }) {
  return <span className="text-[color:var(--color-ink-6)]">{children}</span>;
}

export function Workload({ children }: { children: React.ReactNode }) {
  return <span className="text-[color:var(--color-accent)]">{children}</span>;
}

export function ConfDots({ level }: { level: "high" | "med" | "low" }) {
  if (level === "high")
    return <span className="text-[color:var(--color-ok)]">●●●</span>;
  if (level === "med")
    return (
      <span>
        <span className="text-[color:var(--color-med)]">●●</span>
        <span className="text-[color:var(--color-ink-6)]">○</span>
      </span>
    );
  return (
    <span>
      <span className="text-[color:var(--color-ink-7)]">●</span>
      <span className="text-[color:var(--color-ink-6)]">○○</span>
    </span>
  );
}
