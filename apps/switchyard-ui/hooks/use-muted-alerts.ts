"use client";

/**
 * `useMutedAlerts` — React state wrapper around the localStorage-backed
 * mute store in `lib/muted-alerts.ts`.
 *
 * Surface:
 *   - `mutedAlerts` map (current non-expired entries)
 *   - `isMuted(alertId)` predicate
 *   - `mute(alertId, durationMs)` to mute optimistically
 *   - `unmute(alertId)` to clear
 *   - `mutedUntil(alertId)` for badge rendering
 *
 * Hydration: the hook starts with an empty map (so SSR + first paint
 * match) and reads from localStorage in a `useEffect`. Cross-tab sync
 * is wired via the `storage` event so muting in one tab dims the row
 * in another.
 */

import { useCallback, useEffect, useState } from "react";
import {
  isAlertMuted,
  loadMutedAlerts,
  muteAlert,
  ONE_HOUR_MS,
  pruneExpiredMutes,
  saveMutedAlerts,
  unmuteAlert,
  MUTED_ALERTS_STORAGE_KEY,
  type MutedAlertsMap,
} from "@/lib/muted-alerts";

export interface UseMutedAlertsResult {
  mutedAlerts: MutedAlertsMap;
  isMuted: (alertId: string) => boolean;
  mute: (alertId: string, durationMs?: number) => void;
  unmute: (alertId: string) => void;
  mutedUntil: (alertId: string) => number | null;
}

/**
 * Re-prune the in-memory map every minute so a row whose mute just
 * expired clears itself without waiting for the next polling tick. The
 * 60 s cadence aligns with `POLLING_SLOW`.
 */
const PRUNE_INTERVAL_MS = 60_000;

export function useMutedAlerts(): UseMutedAlertsResult {
  const [mutedAlerts, setMutedAlerts] = useState<MutedAlertsMap>({});

  // Hydrate from localStorage on mount.
  useEffect(() => {
    setMutedAlerts(loadMutedAlerts());
  }, []);

  // Periodic pruning — drop expired entries from memory + storage so
  // the dimmed-row affordance lifts on schedule even if the operator
  // doesn't refresh the page.
  useEffect(() => {
    const tick = () => {
      setMutedAlerts((prev) => {
        const pruned = pruneExpiredMutes(prev);
        if (Object.keys(pruned).length !== Object.keys(prev).length) {
          saveMutedAlerts(pruned);
        }
        return pruned;
      });
    };
    const interval = setInterval(tick, PRUNE_INTERVAL_MS);
    return () => clearInterval(interval);
  }, []);

  // Cross-tab sync via the native storage event. Only react to changes
  // on our key — other localStorage writes (e.g. theme) shouldn't
  // re-render the alerts list.
  useEffect(() => {
    if (typeof window === "undefined") return;
    const onStorage = (event: StorageEvent) => {
      if (event.key && event.key !== MUTED_ALERTS_STORAGE_KEY) return;
      setMutedAlerts(loadMutedAlerts());
    };
    window.addEventListener("storage", onStorage);
    return () => window.removeEventListener("storage", onStorage);
  }, []);

  const isMuted = useCallback(
    (alertId: string) => isAlertMuted(mutedAlerts, alertId),
    [mutedAlerts],
  );

  const mute = useCallback(
    (alertId: string, durationMs: number = ONE_HOUR_MS) => {
      const mutedUntilMs = Date.now() + durationMs;
      setMutedAlerts((prev) => {
        const next = muteAlert(prev, alertId, mutedUntilMs);
        saveMutedAlerts(next);
        return next;
      });
    },
    [],
  );

  const unmute = useCallback((alertId: string) => {
    setMutedAlerts((prev) => {
      const next = unmuteAlert(prev, alertId);
      if (next !== prev) saveMutedAlerts(next);
      return next;
    });
  }, []);

  const mutedUntil = useCallback(
    (alertId: string) => mutedAlerts[alertId]?.mutedUntil ?? null,
    [mutedAlerts],
  );

  return { mutedAlerts, isMuted, mute, unmute, mutedUntil };
}
