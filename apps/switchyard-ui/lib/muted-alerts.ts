/**
 * Local mute store for alerts.
 *
 * Phase 1 (this PR) backs alert mute / acknowledge entirely in
 * localStorage so an operator gets immediate feedback even though the
 * backend persistence endpoints (`POST /v1/observability/alerts/<id>/ack`
 * etc.) don't exist yet. Phase 2 will swap the storage layer for a
 * server-side API call without changing the consumer surface.
 *
 * Storage shape (`enclii_muted_alerts_v1`):
 *   {
 *     "<alert.id>": { mutedUntil: <epoch ms> }
 *   }
 *
 * Versioning: the `_v1` suffix is intentional — when the schema needs
 * to change (e.g. tracking ack vs mute separately), bump to `_v2` and
 * read-migrate so existing operator state isn't dropped silently.
 */

export const MUTED_ALERTS_STORAGE_KEY = "enclii_muted_alerts_v1";

export interface MuteEntry {
  /** Unix epoch milliseconds at which this mute expires. */
  mutedUntil: number;
}

export type MutedAlertsMap = Record<string, MuteEntry>;

/**
 * SSR-safe localStorage probe. Server-rendered routes (`app/`) call
 * client modules during hydration; swallowing the missing-window case
 * here keeps the consumer code straight-line.
 */
function getStorage(): Storage | null {
  if (typeof window === "undefined") return null;
  try {
    return window.localStorage;
  } catch {
    // Some embedded webviews throw on .localStorage access; treat as
    // unavailable rather than crashing the dashboard.
    return null;
  }
}

/**
 * Hydrate the mute map from localStorage, dropping malformed and
 * already-expired entries.
 *
 * `now` is injectable for deterministic tests.
 */
export function loadMutedAlerts(now: number = Date.now()): MutedAlertsMap {
  const storage = getStorage();
  if (!storage) return {};

  const raw = storage.getItem(MUTED_ALERTS_STORAGE_KEY);
  if (!raw) return {};

  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    // Corrupted payload — recover by wiping rather than throwing into
    // the alerts polling loop where it would surface as a broken UI.
    storage.removeItem(MUTED_ALERTS_STORAGE_KEY);
    return {};
  }

  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    return {};
  }

  const out: MutedAlertsMap = {};
  for (const [id, entry] of Object.entries(parsed as Record<string, unknown>)) {
    if (
      entry &&
      typeof entry === "object" &&
      "mutedUntil" in entry &&
      typeof (entry as { mutedUntil: unknown }).mutedUntil === "number" &&
      (entry as MuteEntry).mutedUntil > now
    ) {
      out[id] = { mutedUntil: (entry as MuteEntry).mutedUntil };
    }
  }
  return out;
}

/**
 * Persist a mute map. No-op on the server.
 */
export function saveMutedAlerts(map: MutedAlertsMap): void {
  const storage = getStorage();
  if (!storage) return;
  try {
    storage.setItem(MUTED_ALERTS_STORAGE_KEY, JSON.stringify(map));
  } catch {
    // Quota exceeded or private-mode lockout — silently ignore. The
    // worst case is the operator sees the alert again next refresh.
  }
}

/**
 * Drop expired entries from the supplied map, returning a new map
 * (does not mutate input). Pure — callable from tests without storage.
 */
export function pruneExpiredMutes(
  map: MutedAlertsMap,
  now: number = Date.now(),
): MutedAlertsMap {
  const out: MutedAlertsMap = {};
  for (const [id, entry] of Object.entries(map)) {
    if (entry.mutedUntil > now) out[id] = entry;
  }
  return out;
}

/**
 * True iff `alertId` has a non-expired mute entry in `map`.
 */
export function isAlertMuted(
  map: MutedAlertsMap,
  alertId: string,
  now: number = Date.now(),
): boolean {
  const entry = map[alertId];
  return !!entry && entry.mutedUntil > now;
}

/**
 * Return a NEW map with `alertId` muted until `mutedUntil`. Pure.
 */
export function muteAlert(
  map: MutedAlertsMap,
  alertId: string,
  mutedUntil: number,
): MutedAlertsMap {
  return { ...map, [alertId]: { mutedUntil } };
}

/**
 * Return a NEW map with `alertId` removed. Pure.
 */
export function unmuteAlert(
  map: MutedAlertsMap,
  alertId: string,
): MutedAlertsMap {
  if (!(alertId in map)) return map;
  // Discard the entry at `alertId` via destructuring rest. The `_removed`
  // binding is intentionally unused — the dynamic key form is the
  // canonical way to drop one entry without mutating the input.
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { [alertId]: _removed, ...rest } = map;
  return rest;
}

/** Convenience: ms in one hour. Default mute window. */
export const ONE_HOUR_MS = 60 * 60 * 1000;
