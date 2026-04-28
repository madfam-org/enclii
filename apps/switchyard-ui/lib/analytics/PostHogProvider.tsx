"use client";

/**
 * PostHogProvider -- wraps the application with PostHog analytics context.
 *
 * Drop this into the root layout's <Providers> tree:
 *
 *   import { PostHogProvider } from "@/lib/analytics/PostHogProvider";
 *
 *   <PostHogProvider>
 *     {children}
 *   </PostHogProvider>
 *
 * The provider:
 *  - Initializes posthog-js on mount (client-side only).
 *  - Tracks Next.js route changes via the App Router pathname.
 *  - Respects the browser Do-Not-Track signal.
 *  - Is a no-op when NEXT_PUBLIC_POSTHOG_KEY is not set.
 *
 * The route-tracker is split into its own component wrapped in Suspense
 * because `useSearchParams` triggers a CSR bailout during static export
 * (Next.js prerender of `/_not-found` would fail otherwise).
 */

import { Suspense, useEffect, useRef } from "react";
import { usePathname, useSearchParams } from "next/navigation";
import { initPostHog, getPostHog } from "./posthog";

interface PostHogProviderProps {
  children: React.ReactNode;
}

function PostHogRouteTracker() {
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const prevPathnameRef = useRef<string | null>(null);

  useEffect(() => {
    const ph = getPostHog();
    if (!ph) return;

    // Avoid double-firing on initial mount (posthog.init already captures
    // the first pageview when capture_pageview is true).
    if (prevPathnameRef.current === null) {
      prevPathnameRef.current = pathname;
      return;
    }

    const url = [pathname, searchParams?.toString()].filter(Boolean).join("?");
    ph.capture("$pageview", { $current_url: url });
    prevPathnameRef.current = pathname;
  }, [pathname, searchParams]);

  return null;
}

export function PostHogProvider({ children }: PostHogProviderProps) {
  // Initialize PostHog once on mount.
  useEffect(() => {
    initPostHog();
  }, []);

  // No context provider needed -- posthog-js is a singleton.
  // Components use the helpers from ./posthog.ts directly.
  return (
    <>
      <Suspense fallback={null}>
        <PostHogRouteTracker />
      </Suspense>
      {children}
    </>
  );
}
