// PostHog Reverse Proxy Worker
// Routes analytics.madfam.io → us.i.posthog.com
// Benefits: ad-blocker bypass (first-party domain), no third-party cookies
//
// Deploy: npx wrangler deploy --name posthog-proxy
// Route: analytics.madfam.io/*

const POSTHOG_HOST = "us.i.posthog.com";

export default {
  async fetch(request) {
    const url = new URL(request.url);
    url.hostname = POSTHOG_HOST;
    url.protocol = "https:";

    const headers = new Headers(request.headers);
    headers.set("Host", POSTHOG_HOST);

    const response = await fetch(
      new Request(url.toString(), {
        method: request.method,
        headers,
        body: request.method !== "GET" && request.method !== "HEAD" ? request.body : undefined,
        redirect: "follow",
      })
    );

    const responseHeaders = new Headers(response.headers);
    responseHeaders.set("Access-Control-Allow-Origin", "*");
    responseHeaders.set("Access-Control-Allow-Methods", "GET, POST, OPTIONS");
    responseHeaders.set("Access-Control-Allow-Headers", "Content-Type, Authorization");
    responseHeaders.delete("X-Frame-Options");

    // Strip upstream PostHog branding/reporting headers (AN-1, 2026-05-02).
    // Drops report-to / nel / expect-ct verbatim, then any header whose name
    // OR value contains "posthog" (case-insensitive). Preserves cache-control,
    // content-type, etag, and other legitimate response headers.
    responseHeaders.delete("report-to");
    responseHeaders.delete("nel");
    responseHeaders.delete("expect-ct");
    for (const name of [...responseHeaders.keys()]) {
      const value = responseHeaders.get(name) || "";
      if (name.toLowerCase().includes("posthog") || value.toLowerCase().includes("posthog")) {
        responseHeaders.delete(name);
      }
    }

    if (request.method === "OPTIONS") {
      return new Response(null, { status: 204, headers: responseHeaders });
    }

    return new Response(response.body, {
      status: response.status,
      statusText: response.statusText,
      headers: responseHeaders,
    });
  },
};
