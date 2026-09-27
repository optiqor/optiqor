import { auth } from "@/auth";

// Gates /app/* behind an Auth.js session. Unauthenticated requests
// redirect to /signin and round-trip back via callbackUrl.
export default auth((req) => {
  const isApp = req.nextUrl.pathname.startsWith("/app");
  if (!isApp) return;
  if (!req.auth) {
    const url = new URL("/signin", req.nextUrl.origin);
    url.searchParams.set("callbackUrl", req.nextUrl.pathname + req.nextUrl.search);
    return Response.redirect(url);
  }
});

export const config = {
  matcher: ["/app/:path*"],
};
