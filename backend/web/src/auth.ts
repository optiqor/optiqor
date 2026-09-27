import NextAuth from "next-auth";
import GitHub from "next-auth/providers/github";
import Credentials from "next-auth/providers/credentials";

// Auth.js v5's provider union type lives behind internal entrypoints, so
// we infer the element type from the providers themselves.
type AuthProvider = ReturnType<typeof Credentials> | ReturnType<typeof GitHub>;

// Required env vars (see web/.env.example):
//   AUTH_SECRET, AUTH_GITHUB_ID, AUTH_GITHUB_SECRET.
// The Credentials fallback only loads when NODE_ENV !== "production",
// so a missing GitHub OAuth App in prod fails closed at boot.

const providers: AuthProvider[] = [];

if (process.env.AUTH_GITHUB_ID && process.env.AUTH_GITHUB_SECRET) {
  providers.push(
    GitHub({
      clientId: process.env.AUTH_GITHUB_ID,
      clientSecret: process.env.AUTH_GITHUB_SECRET,
    }),
  );
}

if (process.env.NODE_ENV !== "production" && providers.length === 0) {
  providers.push(
    Credentials({
      id: "dev",
      name: "Dev user",
      credentials: {
        email: { label: "Email", type: "email", placeholder: "you@example.test" },
        name: { label: "Name", type: "text", placeholder: "Dev User" },
      },
      authorize: (raw) => {
        const email = String(raw?.email ?? "").trim();
        const name = String(raw?.name ?? "").trim() || "Dev User";
        if (!email || !email.includes("@")) return null;
        return { id: email, email, name };
      },
    }),
  );
}

// trustHost lets dev + Vercel previews authenticate against the
// inferred callback URL. Prod pins AUTH_TRUST_HOST=false + AUTH_URL.
export const { handlers, auth, signIn, signOut } = NextAuth({
  providers,
  trustHost: true,
  pages: { signIn: "/signin" },
  callbacks: {
    // Surface the provider's user id on the session so the dashboard can
    // pass it to the backend's /v1/session/issue.
    async session({ session, token }) {
      if (session.user && token.sub) {
        (session.user as { id?: string }).id = token.sub;
      }
      return session;
    },
  },
});

// hasGitHubAuth tells the sign-in page which form to render.
export const hasGitHubAuth = Boolean(
  process.env.AUTH_GITHUB_ID && process.env.AUTH_GITHUB_SECRET,
);
