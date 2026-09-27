import type { NextConfig } from "next";

// Dev rewrites keep the browser on a single origin so CORS never
// enters the picture; prod mirrors this with a reverse proxy in front
// of Next. Override the upstream with OPTIQOR_API_UPSTREAM for
// split-host dev (Go API in a remote container).
const apiUpstream =
  process.env.OPTIQOR_API_UPSTREAM ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  async rewrites() {
    return [
      { source: "/v1/:path*", destination: `${apiUpstream}/v1/:path*` },
      // /r/* and /v/* are Go-rendered share + verifier pages.
      { source: "/r/:path*", destination: `${apiUpstream}/r/:path*` },
      { source: "/v/:path*", destination: `${apiUpstream}/v/:path*` },
      {
        source: "/webhooks/:path*",
        destination: `${apiUpstream}/webhooks/:path*`,
      },
      {
        source: "/oauth/:path*",
        destination: `${apiUpstream}/oauth/:path*`,
      },
      { source: "/healthz", destination: `${apiUpstream}/healthz` },
      { source: "/readyz",  destination: `${apiUpstream}/readyz` },
    ];
  },
};

export default nextConfig;
