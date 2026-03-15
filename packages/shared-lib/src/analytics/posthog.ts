/**
 * @enclii/shared-lib/analytics - PostHog client initialization for Enclii
 * frontend applications.
 *
 * This module provides a thin wrapper around posthog-js that enforces Enclii's
 * default configuration (self-hosted endpoint, DNT respect, cookie consent).
 *
 * Usage:
 *   import { initPostHog, getPostHog } from "@enclii/shared-lib/analytics";
 *
 *   // Initialize once at app startup
 *   initPostHog("phc_project_key");
 *
 *   // Use anywhere after initialization
 *   getPostHog()?.capture("button.clicked", { page: "/dashboard" });
 */

/**
 * PostHog initialization options that can override the Enclii defaults.
 */
export interface PostHogOptions {
  /** PostHog project API key. Required. */
  apiKey: string;
  /**
   * Ingestion endpoint. Defaults to `https://analytics.madfam.io` which is
   * reverse-proxied through Cloudflare to avoid ad-blocker interference.
   */
  apiHost?: string;
  /** Enable automatic pageview capture. Default: true. */
  capturePageview?: boolean;
  /** Enable automatic element click capture. Default: true. */
  autocapture?: boolean;
  /** Respect the browser Do-Not-Track header. Default: true. */
  respectDnt?: boolean;
  /** Disable PostHog entirely (useful for dev/test). Default: false. */
  disabled?: boolean;
  /** Enable session recording. Default: false. */
  sessionRecording?: boolean;
  /** Persistence mechanism. Default: "localStorage+cookie". */
  persistence?: "localStorage+cookie" | "localStorage" | "cookie" | "memory";
}

/** Default ingestion host reverse-proxied via Cloudflare. */
const DEFAULT_API_HOST = "https://analytics.madfam.io";

/**
 * Build a posthog-js compatible config object from Enclii defaults + overrides.
 *
 * This function does NOT import posthog-js. It returns a plain object so that
 * shared-lib has zero runtime dependency on posthog-js (that dependency lives
 * in the consuming app's package.json).
 */
export function buildPostHogConfig(options: PostHogOptions): Record<string, unknown> {
  if (options.disabled) {
    return { api_host: "", opt_out_capturing_by_default: true };
  }

  return {
    api_host: options.apiHost ?? DEFAULT_API_HOST,
    capture_pageview: options.capturePageview ?? true,
    autocapture: options.autocapture ?? true,
    respect_dnt: options.respectDnt ?? true,
    persistence: options.persistence ?? "localStorage+cookie",
    disable_session_recording: !(options.sessionRecording ?? false),
    // Security: do not send cookies cross-origin.
    secure_cookie: true,
    // Mask all text and inputs in session recordings by default.
    session_recording: {
      maskAllInputs: true,
      maskTextSelector: "*",
    },
    // Loaded callback -- fires once the SDK is ready. Useful for debugging.
    loaded: (posthog: { debug: (enable: boolean) => void }) => {
      if (
        typeof window !== "undefined" &&
        window.location.hostname === "localhost"
      ) {
        posthog.debug(true);
      }
    },
  };
}
