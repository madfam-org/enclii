/**
 * PostHog client singleton for switchyard-ui.
 *
 * This file initializes posthog-js once and exports helpers for the rest of
 * the application. The actual React provider lives in PostHogProvider.tsx.
 *
 * The initialization respects the browser Do-Not-Track signal and routes all
 * traffic through analytics.madfam.io (Cloudflare reverse proxy) so that
 * ad-blockers do not interfere with product analytics.
 */
import posthog from "posthog-js";

const POSTHOG_KEY = process.env.NEXT_PUBLIC_POSTHOG_KEY ?? "";
const POSTHOG_HOST =
  process.env.NEXT_PUBLIC_POSTHOG_HOST ?? "https://analytics.madfam.io";

let initialized = false;

/**
 * Initialize the PostHog client. Safe to call multiple times -- subsequent
 * calls are no-ops.
 */
export function initPostHog(): void {
  if (initialized || typeof window === "undefined") return;
  if (!POSTHOG_KEY) {
    if (process.env.NODE_ENV === "development") {
      console.info(
        "[posthog] NEXT_PUBLIC_POSTHOG_KEY not set -- analytics disabled",
      );
    }
    return;
  }

  // Respect Do-Not-Track
  if (
    navigator.doNotTrack === "1" ||
    (window as unknown as { doNotTrack?: string }).doNotTrack === "1"
  ) {
    if (process.env.NODE_ENV === "development") {
      console.info("[posthog] Do-Not-Track detected -- analytics disabled");
    }
    return;
  }

  posthog.init(POSTHOG_KEY, {
    api_host: POSTHOG_HOST,
    capture_pageview: false,
    autocapture: false,
    respect_dnt: true,
    persistence: "localStorage+cookie",
    secure_cookie: true,
    disable_session_recording: true,
    session_recording: {
      maskAllInputs: true,
      maskTextSelector: "*",
    },
    loaded: (ph) => {
      if (window.location.hostname === "localhost") {
        ph.debug(true);
      }
    },
  });

  initialized = true;
}

/**
 * Returns the PostHog client instance, or undefined if not initialized.
 */
export function getPostHog(): typeof posthog | undefined {
  if (!initialized) return undefined;
  return posthog;
}

/**
 * Identify the current user (call after login / auth callback).
 */
export function identifyUser(
  userId: string,
  traits?: Record<string, unknown>,
): void {
  if (!initialized) return;
  posthog.identify(userId, traits);
}

/**
 * Reset the current user identity (call on logout).
 */
export function resetUser(): void {
  if (!initialized) return;
  posthog.reset();
}

/**
 * Capture a named event with optional properties.
 */
export function trackEvent(
  event: string,
  properties?: Record<string, unknown>,
): void {
  if (!initialized) return;
  posthog.capture(event, properties);
}
