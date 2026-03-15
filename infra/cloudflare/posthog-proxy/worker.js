// PostHog Reverse Proxy Worker
// Routes analytics.enclii.dev → us.i.posthog.com
// Benefits: ad-blocker bypass (first-party domain), no third-party cookies
//
// Deploy: npx wrangler deploy --name posthog-proxy
// Route: analytics.enclii.dev/*

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
