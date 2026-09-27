import type { ReactNode } from "react";
import Link from "next/link";
import { auth, signOut } from "@/auth";
import { Container } from "@/components/container";

// Server component so the session read isn't re-run per client nav.
// Belt-and-braces under middleware.ts: if someone disables the
// middleware in dev, the layout still fails closed.
export default async function AppLayout({ children }: { children: ReactNode }) {
  const session = await auth();
  if (!session) {
    return (
      <main className="pt-20">
        <Container size="md">
          <div className="rounded-[var(--radius-lg)] border hairline p-8">
            <h1 className="text-lg font-medium">Sign in required</h1>
            <p className="mt-2 text-sm text-[color:var(--color-ink-7)]">
              The dashboard is gated by your Optiqor session. Sign in to
              see analyses, Apply Fixes, and signed Receipts.
            </p>
            <div className="mt-5 flex items-center gap-3">
              <Link
                href="/signin?from=/app"
                className="rounded-[var(--radius-sm)] bg-[color:var(--color-ink-3)] px-3.5 py-1.5 text-xs font-medium text-[color:var(--color-bg)] hover:opacity-90"
              >
                Sign in →
              </Link>
              <Link
                href="/sandbox"
                className="text-xs text-[color:var(--color-ink-7)] underline-offset-4 hover:underline"
              >
                Try the sandbox (no login)
              </Link>
            </div>
          </div>
        </Container>
      </main>
    );
  }

  const user = session.user;
  const initials = (user?.name ?? user?.email ?? "?")
    .split(/\s+/)
    .map((w) => w[0]?.toUpperCase())
    .slice(0, 2)
    .join("");

  return (
    <div className="min-h-screen bg-[color:var(--color-bg)]">
      <aside className="fixed inset-y-0 left-0 w-60 border-r hairline px-5 py-6 hidden md:flex flex-col gap-1 bg-[color:var(--color-bg)]">
        <Link href="/app" className="text-sm font-medium tracking-tight">
          Optiqor
        </Link>
        <p className="text-xs text-[color:var(--color-ink-7)] mt-0.5">Dashboard</p>

        <nav className="mt-8 flex flex-col gap-0.5 text-sm">
          <NavLink href="/app" label="Overview" />
          <NavLink href="/app/analyses" label="Analyses" />
          <NavLink href="/app/receipts" label="Receipts" disabled />
          <NavLink href="/app/apply-fixes" label="Apply Fixes" disabled />
          <NavLink href="/app/spikes" label="Cost spikes" disabled />
        </nav>

        <div className="mt-auto pt-6 border-t hairline">
          <div className="flex items-center gap-3">
            <div className="size-8 rounded-full bg-[color:var(--color-ink-3)] text-[color:var(--color-bg)] text-xs flex items-center justify-center font-medium">
              {initials}
            </div>
            <div className="text-xs">
              <div className="font-medium truncate max-w-[10rem]">
                {user?.name ?? user?.email ?? "anonymous"}
              </div>
              <form
                action={async () => {
                  "use server";
                  await signOut({ redirectTo: "/" });
                }}
              >
                <button type="submit" className="text-[color:var(--color-ink-7)] hover:text-[color:var(--color-ink-3)]">
                  Sign out
                </button>
              </form>
            </div>
          </div>
        </div>
      </aside>

      <main className="md:pl-60">
        <div className="px-6 py-8 md:px-10 md:py-12 max-w-5xl">{children}</div>
      </main>
    </div>
  );
}

function NavLink({ href, label, disabled }: { href: string; label: string; disabled?: boolean }) {
  if (disabled) {
    return (
      <span className="rounded px-2 py-1.5 text-[color:var(--color-ink-7)] cursor-default">
        {label}
        <span className="ml-2 text-[10px] uppercase tracking-wider text-[color:var(--color-ink-7)]">
          soon
        </span>
      </span>
    );
  }
  return (
    <Link
      href={href}
      className="rounded px-2 py-1.5 hover:bg-[color:var(--color-ink-1)] text-[color:var(--color-ink-3)]"
    >
      {label}
    </Link>
  );
}
