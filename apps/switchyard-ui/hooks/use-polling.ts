'use client';

import { useEffect, useRef, useCallback } from 'react';

interface UsePollingOptions {
  /** Whether polling is enabled (default true) */
  enabled?: boolean;
}

/**
 * Generic polling hook with auto-cleanup and Page Visibility API support.
 *
 * Pauses polling when the browser tab is hidden and resumes when visible.
 */
export function usePolling(
  callback: () => void | Promise<void>,
  intervalMs: number,
  options: UsePollingOptions = {},
) {
  const { enabled = true } = options;
  const savedCallback = useRef(callback);

  // Keep the ref up-to-date without causing re-renders
  useEffect(() => {
    savedCallback.current = callback;
  }, [callback]);

  // Skip a tick if the previous one is still running. Without this guard,
  // a slow API (e.g., /v1/observability/health under load) causes ticks
  // to pile up faster than they resolve, producing the cascade of
  // ERR_ABORTED requests visible in the dashboard's network panel and
  // making "loading" indistinguishable from "broken" in the UI.
  const inFlight = useRef(false);
  const tick = useCallback(async () => {
    if (inFlight.current) return;
    inFlight.current = true;
    try {
      await savedCallback.current();
    } finally {
      inFlight.current = false;
    }
  }, []);

  useEffect(() => {
    if (!enabled || intervalMs <= 0) return;

    let id: ReturnType<typeof setInterval> | null = null;

    const start = () => {
      if (id) return;
      id = setInterval(tick, intervalMs);
    };

    const stop = () => {
      if (id) {
        clearInterval(id);
        id = null;
      }
    };

    const handleVisibility = () => {
      if (document.hidden) {
        stop();
      } else {
        // Fire immediately on return then restart the interval
        tick();
        start();
      }
    };

    start();
    document.addEventListener('visibilitychange', handleVisibility);

    return () => {
      stop();
      document.removeEventListener('visibilitychange', handleVisibility);
    };
  }, [tick, intervalMs, enabled]);
}
