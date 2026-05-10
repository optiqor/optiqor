import Link from "next/link";
import { Wordmark } from "./logomark";
import { ButtonLink } from "./button";

const nav = [
  { href: "/sandbox", label: "Sandbox" },
  { href: "/how-it-works", label: "How it works" },
  { href: "/pricing", label: "Pricing" },
  { href: "/security", label: "Security" },
  { href: "/docs", label: "Docs" },
] as const;

export function SiteHeader() {
  return (
    <header className="sticky top-0 z-40 border-b border-[color:var(--color-rule)] bg-[color:var(--color-ink-0)]/85 backdrop-blur-[6px]">
      <div className="mx-auto flex h-14 max-w-7xl items-center justify-between px-6 md:px-10">
        <Link href="/" aria-label="Optiqor home" className="-ml-1 p-1">
          <Wordmark />
        </Link>
        <nav className="hidden md:flex items-center gap-7">
          {nav.map((item) => (
            <Link
              key={item.href}
              href={item.href}
              className="text-[13px] text-[color:var(--color-ink-7)] hover:text-[color:var(--color-ink-9)] transition-colors duration-150"
            >
              {item.label}
            </Link>
          ))}
        </nav>
        <div className="flex items-center gap-2">
          <ButtonLink
            href="https://github.com/optiqor/optiqor-cli"
            external
            variant="ghost"
            size="sm"
            className="hidden sm:inline-flex"
          >
            GitHub
          </ButtonLink>
          <ButtonLink href="/sandbox" variant="primary" size="sm">
            Try it
            <span aria-hidden className="font-mono text-[color:var(--color-ink-6)]">
              →
            </span>
          </ButtonLink>
        </div>
      </div>
    </header>
  );
}
