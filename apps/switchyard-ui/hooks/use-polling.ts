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

  const tick = useCallback(() => {
    savedCallback.current();
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
