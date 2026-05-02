// Verdaccio Edge Worker
// Routes npm.madfam.io/* → ingress (Cloudflare Tunnel)
//
// Purpose (NP-2, 2026-05-02): Verdaccio renders its version number into the
// dashboard HTML inside `__VERDACCIO_BASENAME_UI_OPTIONS` (e.g. `"version":"5.33.0"`).
// Verdaccio 5.x has no config flag to suppress this — fingerprint is published
// regardless of `web.darkMode` / `web.title` overrides. This Worker rewrites the
// version string to a generic label on HTML responses for `/` and `/-/web/*`.
//
// Everything else (package fetches, tarball downloads, /-/v1/search 401, /-/ping,
// /-/metrics, /api/v1/api-keys/verify) is proxied verbatim — npm CLI behavior
// must remain identical.
//
// Deploy: npx wrangler deploy --env production
// Route: npm.madfam.io/* (zone: madfam.io)

const VERSION_FINGERPRINT_RE = /"version":"\d+\.\d+\.\d+(?:-[\w.]+)?"/g;
const VERSION_REPLACEMENT = '"version":"hidden"';

export default {
  async fetch(request) {
    const upstream = await fetch(request, { redirect: "follow" });

    // Only rewrite HTML responses on dashboard paths — leave all package
    // metadata (application/json) and tarball (application/octet-stream)
    // responses untouched so the npm CLI sees byte-identical bytes.
    const contentType = upstream.headers.get("content-type") || "";
    if (!contentType.includes("text/html")) {
      return upstream;
    }

    const url = new URL(request.url);
    const isDashboard = url.pathname === "/" || url.pathname.startsWith("/-/web");
    if (!isDashboard) {
      return upstream;
    }

    const body = await upstream.text();
    const rewritten = body.replace(VERSION_FINGERPRINT_RE, VERSION_REPLACEMENT);

    const headers = new Headers(upstream.headers);
    // Recompute content-length since the rewritten body length may differ.
    headers.delete("content-length");
    // Defence-in-depth: HSTS at the edge (NP-3 follow-up; not in scope but cheap).
    headers.set("strict-transport-security", "max-age=31536000; includeSubDomains");

    return new Response(rewritten, {
      status: upstream.status,
      statusText: upstream.statusText,
      headers,
    });
  },
};
