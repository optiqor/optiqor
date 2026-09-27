import Link from "next/link";
import { Wordmark } from "./logomark";
import { Container } from "./container";

const cols: {
  title: string;
  links: { href: string; label: string; external?: boolean }[];
}[] = [
  {
    title: "Product",
    links: [
      { href: "/sandbox", label: "Sandbox" },
      { href: "/how-it-works", label: "How it works" },
      { href: "/pricing", label: "Pricing" },
      { href: "/security", label: "Security" },
    ],
  },
  {
    title: "CLI",
    links: [
      { href: "/docs", label: "Docs" },
      {
        href: "https://www.npmjs.com/package/@optiqor/cli",
        label: "@optiqor/cli",
        external: true,
      },
      {
        href: "https://github.com/optiqor/optiqor-cli",
        label: "Source",
        external: true,
      },
      {
        href: "https://github.com/optiqor/optiqor-cli/releases",
        label: "Releases",
        external: true,
      },
    ],
  },
  {
    title: "Company",
    links: [
      { href: "/about", label: "About" },
      { href: "/contact", label: "Contact" },
      { href: "/legal", label: "Legal" },
      {
        href: "mailto:security@optiqor.dev",
        label: "Report a vulnerability",
        external: true,
      },
    ],
  },
];

export function SiteFooter() {
  return (
    <footer className="border-t border-[color:var(--color-rule)] mt-24">
      <Container size="xl" className="py-16">
        <div className="grid gap-12 md:grid-cols-[1.4fr_1fr_1fr_1fr]">
          <div className="space-y-4">
            <Wordmark />
            <p className="max-w-[28ch] text-[13px] leading-relaxed text-[color:var(--color-ink-7)]">
              Helm cost optimization for Kubernetes — terminal-first, deterministic,
              cryptographically verifiable.
            </p>
            <p className="font-mono text-[11px] tracking-[0.08em] text-[color:var(--color-ink-6)]">
              © {new Date().getFullYear()} OPTIQOR, INC.
            </p>
          </div>
          {cols.map((col) => (
            <div key={col.title} className="space-y-3">
              <h4 className="font-mono text-[11px] tracking-[0.14em] uppercase text-[color:var(--color-ink-6)]">
                {col.title}
              </h4>
              <ul className="space-y-2.5">
                {col.links.map((l) =>
                  l.external ? (
                    <li key={l.href}>
                      <a
                        href={l.href}
                        className="text-[13px] text-[color:var(--color-ink-8)] hover:text-[color:var(--color-ink-9)] transition-colors"
                        target="_blank"
                        rel="noopener noreferrer"
                      >
                        {l.label}
                      </a>
                    </li>
                  ) : (
                    <li key={l.href}>
                      <Link
                        href={l.href}
                        className="text-[13px] text-[color:var(--color-ink-8)] hover:text-[color:var(--color-ink-9)] transition-colors"
                      >
                        {l.label}
                      </Link>
                    </li>
                  ),
                )}
              </ul>
            </div>
          ))}
        </div>

        <div className="mt-12 flex flex-col gap-3 border-t border-[color:var(--color-rule)] pt-6 text-[12px] text-[color:var(--color-ink-6)] md:flex-row md:items-center md:justify-between">
          <p className="font-mono">
            Sandbox accuracy: ±40%. Install the agent for exact numbers.
          </p>
          <p className="font-mono tracking-[0.1em]">build · phase 2</p>
        </div>
      </Container>
    </footer>
  );
}
