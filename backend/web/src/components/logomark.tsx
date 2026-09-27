import { cx } from "@/lib/cx";

// Logomark mirrors the optiqor-hori.jpg brand asset as inline SVG so it
// stays crisp at every size and inherits currentColor for theming.
export function Logomark({
  size = 28,
  className,
  glow = true,
}: {
  size?: number;
  className?: string;
  glow?: boolean;
}) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 64 64"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      role="img"
      aria-label="optiqor"
      className={cx("shrink-0", className)}
    >
      {glow && (
        <defs>
          <radialGradient id="opt-dot-glow" cx="0.5" cy="0.5" r="0.5">
            <stop offset="0%" stopColor="#22D3EE" stopOpacity="0.7" />
            <stop offset="60%" stopColor="#22D3EE" stopOpacity="0" />
          </radialGradient>
        </defs>
      )}
      <path
        d="M32 6 a26 26 0 1 1 -18.4 44.4"
        stroke="currentColor"
        strokeWidth="4"
        strokeLinecap="round"
      />
      <line
        x1="32"
        y1="32"
        x2="50"
        y2="50"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
      />
      {glow && (
        <circle cx="22" cy="32" r="14" fill="url(#opt-dot-glow)" opacity="0.9" />
      )}
      <circle cx="22" cy="32" r="4.5" fill="#22D3EE" />
      <circle cx="50" cy="50" r="2.5" fill="#22D3EE" />
    </svg>
  );
}

export function Wordmark({
  size = 22,
  className,
}: {
  size?: number;
  className?: string;
}) {
  return (
    <span
      className={cx(
        "inline-flex items-center gap-2 text-[color:var(--color-ink-9)]",
        className,
      )}
    >
      <Logomark size={size} glow={false} />
      <span className="font-mono text-[13px] tracking-[0.18em] uppercase">
        Optiqor
      </span>
    </span>
  );
}
