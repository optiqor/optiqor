import Link from "next/link";
import { cx } from "@/lib/cx";

type Variant = "primary" | "secondary" | "ghost";
type Size = "sm" | "md";

const base =
  "inline-flex items-center justify-center font-medium transition-colors duration-200 ease-[var(--ease-out)] disabled:cursor-not-allowed disabled:opacity-50";

const sizes: Record<Size, string> = {
  sm: "h-9 px-3 text-[13px] gap-2",
  md: "h-11 px-5 text-[14px] gap-2.5",
};

const variants: Record<Variant, string> = {
  primary:
    // Solid ink-9 button with mono accent on the trailing arrow.
    // Looks more like a CLI command target than a marketing CTA.
    "bg-[color:var(--color-ink-9)] text-[color:var(--color-ink-0)] hover:bg-white",
  secondary:
    "bg-transparent text-[color:var(--color-ink-9)] hairline hover:hairline-strong hover:bg-[color:var(--color-ink-2)]",
  ghost:
    "bg-transparent text-[color:var(--color-ink-8)] hover:text-[color:var(--color-ink-9)] hover:bg-[color:var(--color-ink-2)]",
};

type CommonProps = {
  variant?: Variant;
  size?: Size;
  className?: string;
  children: React.ReactNode;
};

export function Button({
  variant = "secondary",
  size = "md",
  className,
  children,
  ...rest
}: CommonProps & React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      className={cx(
        base,
        sizes[size],
        variants[variant],
        "rounded-[var(--radius-md)]",
        className,
      )}
      {...rest}
    >
      {children}
    </button>
  );
}

export function ButtonLink({
  variant = "secondary",
  size = "md",
  className,
  href,
  external,
  children,
}: CommonProps & { href: string; external?: boolean }) {
  if (external) {
    return (
      <a
        href={href}
        className={cx(
          base,
          sizes[size],
          variants[variant],
          "rounded-[var(--radius-md)]",
          className,
        )}
        target="_blank"
        rel="noopener noreferrer"
      >
        {children}
      </a>
    );
  }
  return (
    <Link
      href={href}
      className={cx(
        base,
        sizes[size],
        variants[variant],
        "rounded-[var(--radius-md)]",
        className,
      )}
    >
      {children}
    </Link>
  );
}
