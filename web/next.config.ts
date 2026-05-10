import type { NextConfig } from "next";

// Dev rewrites: the Next.js dev server proxies backend routes to the
// Go API so the browser sees a single origin and never has to deal
// with CORS. The same-origin model also matches how we deploy in prod
// (Next behind a reverse proxy that does the same).
//
// Override the upstream with OPTIQOR_API_UPSTREAM for split-host dev
// (e.g. running the Go API in a remote container).
const apiUpstream =
  process.env.OPTIQOR_API_UPSTREAM ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  async rewrites() {
    return [
      // POST /v1/analyze, GET /v1/receipts/:id, POST /v1/apply-fixes,
      // POST /v1/ingest, POST /v1/cost-spikes, GET /v1/meta
      { source: "/v1/:path*", destination: `${apiUpstream}/v1/:path*` },
      // Public share + verifier pages — Go-rendered, not Next.js.
      { source: "/r/:path*", destination: `${apiUpstream}/r/:path*` },
      { source: "/v/:path*", destination: `${apiUpstream}/v/:path*` },
      // Webhook + OAuth endpoints sit on the Go API.
      {
        source: "/webhooks/:path*",
        destination: `${apiUpstream}/webhooks/:path*`,
      },
      {
        source: "/oauth/:path*",
        destination: `${apiUpstream}/oauth/:path*`,
      },
      // Health endpoints — useful when the LB front-ends Next.
      { source: "/healthz", destination: `${apiUpstream}/healthz` },
      { source: "/readyz",  destination: `${apiUpstream}/readyz` },
    ];
  },
};

export default nextConfig;
