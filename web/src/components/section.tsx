import { cx } from "@/lib/cx";

export function Section({
  children,
  className,
  border = false,
  id,
}: {
  children: React.ReactNode;
  className?: string;
  /** Render a top-side hairline rule. Use it as a visual chapter break. */
  border?: boolean;
  id?: string;
}) {
  return (
    <section
      id={id}
      className={cx(
        "py-20 md:py-28",
        border && "border-t border-[color:var(--color-rule)]",
        className,
      )}
    >
      {children}
    </section>
  );
}

/**
 * Eyebrow — small all-caps monospace label that sits above a section
 * heading. Mirrors a CLI section marker (`━━ Cost optimizations ━━`).
 */
export function Eyebrow({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <p
      className={cx(
        "font-mono text-[11px] tracking-[0.14em] uppercase text-[color:var(--color-ink-7)]",
        className,
      )}
    >
      {children}
    </p>
  );
}
