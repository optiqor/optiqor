import { Container } from "@/components/container";
import { Eyebrow } from "@/components/section";
import { auth, hasGitHubAuth, signIn } from "@/auth";
import { redirect } from "next/navigation";

// hasGitHubAuth picks the button vs the dev Credentials form. The dev
// path can't load in prod (see @/auth), so this branch is unreachable
// from a real deploy.
//
// sanitizeCallbackUrl rejects anything that isn't a same-origin path so
// /signin?callbackUrl=https://attacker.example/phish can't bounce a
// freshly-authenticated user off our origin. Same-origin = starts with
// "/" and not "//" (protocol-relative) and not "/\\" (some browsers
// decode backslashes as path separators after redirect).
function sanitizeCallbackUrl(raw: string | undefined): string {
  if (!raw) return "/app";
  if (!raw.startsWith("/")) return "/app";
  if (raw.startsWith("//")) return "/app";
  if (raw.startsWith("/\\")) return "/app";
  return raw;
}

export default async function SignInPage({
  searchParams,
}: {
  searchParams: Promise<{ callbackUrl?: string }>;
}) {
  const session = await auth();
  const sp = await searchParams;
  const callbackUrl = sanitizeCallbackUrl(sp.callbackUrl);
  if (session) redirect(callbackUrl);

  return (
    <Container size="md">
      <div className="pt-24 pb-16 md:pt-32">
        <Eyebrow>Sign in</Eyebrow>
        <h1 className="mt-3 text-[clamp(28px,4vw,40px)] leading-[1.05] tracking-[-0.02em] font-medium">
          Welcome back.
        </h1>
        <p className="mt-4 max-w-[58ch] text-[15px] leading-relaxed text-[color:var(--color-ink-8)]">
          Sign in to view your analyses, signed Receipts, and Apply Fix history.
          No password — Optiqor only sees what GitHub already lets you share.
        </p>

        {hasGitHubAuth ? (
          <form
            action={async () => {
              "use server";
              await signIn("github", { redirectTo: callbackUrl });
            }}
            className="mt-10"
          >
            <button
              type="submit"
              className="rounded-[var(--radius-md)] border hairline bg-[color:var(--color-ink-3)] px-5 py-3 text-sm font-medium text-[color:var(--color-bg)] hover:opacity-90"
            >
              Continue with GitHub
            </button>
          </form>
        ) : (
          <DevBypassForm callbackUrl={callbackUrl} />
        )}

        <p className="mt-10 text-xs text-[color:var(--color-ink-7)]">
          GitLab sign-in lands in Phase 8 alongside the GitLab App.
        </p>
      </div>
    </Container>
  );
}

function DevBypassForm({ callbackUrl }: { callbackUrl: string }) {
  return (
    <div className="mt-10 rounded-[var(--radius-lg)] border hairline bg-[color:var(--color-ink-1)] p-6 max-w-[28rem]">
      <p className="text-xs uppercase tracking-wider text-[color:var(--color-ink-7)]">
        Dev mode
      </p>
      <p className="mt-2 text-[13px] text-[color:var(--color-ink-8)]">
        No GitHub OAuth App configured (set <code>AUTH_GITHUB_ID</code> +{" "}
        <code>AUTH_GITHUB_SECRET</code> in <code>web/.env.local</code> for the real
        flow). Sign in as any identity for local testing.
      </p>
      <form
        action={async (data: FormData) => {
          "use server";
          await signIn("dev", {
            email: String(data.get("email") ?? ""),
            name: String(data.get("name") ?? ""),
            redirectTo: callbackUrl,
          });
        }}
        className="mt-5 grid gap-3"
      >
        <label className="grid gap-1 text-xs">
          <span className="text-[color:var(--color-ink-7)]">Email</span>
          <input
            name="email"
            type="email"
            required
            placeholder="you@example.test"
            className="rounded-[var(--radius-sm)] border hairline bg-[color:var(--color-bg)] px-3 py-2 text-sm"
          />
        </label>
        <label className="grid gap-1 text-xs">
          <span className="text-[color:var(--color-ink-7)]">Name</span>
          <input
            name="name"
            type="text"
            placeholder="Dev User"
            className="rounded-[var(--radius-sm)] border hairline bg-[color:var(--color-bg)] px-3 py-2 text-sm"
          />
        </label>
        <button
          type="submit"
          className="mt-2 rounded-[var(--radius-md)] border hairline bg-[color:var(--color-ink-3)] px-4 py-2.5 text-sm font-medium text-[color:var(--color-bg)] hover:opacity-90"
        >
          Continue as dev user
        </button>
      </form>
    </div>
  );
}
