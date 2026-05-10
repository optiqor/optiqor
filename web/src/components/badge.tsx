import { cx } from "@/lib/cx";

type Severity = "HIGH" | "MED" | "LOW" | "INFO" | "OK";

const styles: Record<Severity, string> = {
  HIGH: "bg-[color:var(--color-high)]/12 text-[color:var(--color-high)]",
  MED:  "bg-[color:var(--color-med)]/12  text-[color:var(--color-med)]",
  LOW:  "bg-[color:var(--color-low)]/12  text-[color:var(--color-low)]",
  INFO: "bg-[color:var(--color-ink-5)]/40 text-[color:var(--color-ink-8)]",
  OK:   "bg-[color:var(--color-ok)]/12 text-[color:var(--color-ok)]",
};

/**
 * SeverityBadge — mirrors the CLI's HIGH/MED/LOW badges. Uppercase
 * mono, tight tracking, fixed letter-spacing so a column of badges
 * aligns visually like a table.
 */
export function SeverityBadge({
  level,
  className,
}: {
  level: Severity;
  className?: string;
}) {
  return (
    <span
      className={cx(
        "inline-flex items-center justify-center min-w-[44px] px-2 py-[2px]",
        "font-mono text-[11px] tracking-[0.08em] font-medium",
        "rounded-[var(--radius-sm)]",
        styles[level],
        className,
      )}
    >
      {level}
    </span>
  );
}

/**
 * Pill — tag-style label for status, version, "Year 1", etc.
 */
export function Pill({
  children,
  variant = "default",
  className,
}: {
  children: React.ReactNode;
  variant?: "default" | "accent";
  className?: string;
}) {
  return (
    <span
      className={cx(
        "inline-flex items-center gap-1.5 px-2.5 py-[3px]",
        "font-mono text-[11px] tracking-[0.08em]",
        "rounded-[var(--radius-pill,999px)]",
        variant === "accent"
          ? "text-[color:var(--color-accent)] hairline-subtle"
          : "text-[color:var(--color-ink-7)] hairline-subtle",
        className,
      )}
    >
      {variant === "accent" && (
        <span
          aria-hidden
          className="size-1.5 rounded-full bg-[color:var(--color-accent)]"
        />
      )}
      {children}
    </span>
  );
}
