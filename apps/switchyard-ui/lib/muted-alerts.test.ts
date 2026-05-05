/**
 * Unit tests for `lib/muted-alerts.ts`.
 *
 * Covers the pure helpers — load/save/prune/predicate/mute/unmute —
 * against the documented localStorage schema (key
 * `enclii_muted_alerts_v1`, payload `{[alertId]: {mutedUntil}}`).
 */

import {
  isAlertMuted,
  loadMutedAlerts,
  muteAlert,
  MUTED_ALERTS_STORAGE_KEY,
  pruneExpiredMutes,
  saveMutedAlerts,
  unmuteAlert,
  type MutedAlertsMap,
} from "./muted-alerts";

const NOW = 1_700_000_000_000; // arbitrary fixed epoch in tests

describe("isAlertMuted", () => {
  it("returns false for unknown alert IDs", () => {
    expect(isAlertMuted({}, "alert-x", NOW)).toBe(false);
  });

  it("returns true when entry's mutedUntil is in the future", () => {
    const map: MutedAlertsMap = { "alert-x": { mutedUntil: NOW + 1000 } };
    expect(isAlertMuted(map, "alert-x", NOW)).toBe(true);
  });

  it("returns false when entry has already expired", () => {
    const map: MutedAlertsMap = { "alert-x": { mutedUntil: NOW - 1 } };
    expect(isAlertMuted(map, "alert-x", NOW)).toBe(false);
  });

  it("returns false at the exact expiry boundary (>, not >=)", () => {
    const map: MutedAlertsMap = { "alert-x": { mutedUntil: NOW } };
    expect(isAlertMuted(map, "alert-x", NOW)).toBe(false);
  });
});

describe("muteAlert / unmuteAlert", () => {
  it("muteAlert returns a new map (no input mutation)", () => {
    const input: MutedAlertsMap = {};
    const out = muteAlert(input, "alert-a", NOW + 1000);
    expect(out).toEqual({ "alert-a": { mutedUntil: NOW + 1000 } });
    expect(input).toEqual({}); // unchanged
  });

  it("muteAlert overwrites a prior entry for the same ID", () => {
    const input: MutedAlertsMap = { "alert-a": { mutedUntil: NOW - 1 } };
    const out = muteAlert(input, "alert-a", NOW + 5000);
    expect(out["alert-a"].mutedUntil).toBe(NOW + 5000);
  });

  it("unmuteAlert removes the entry without mutating input", () => {
    const input: MutedAlertsMap = { "alert-a": { mutedUntil: NOW + 1000 } };
    const out = unmuteAlert(input, "alert-a");
    expect(out).toEqual({});
    expect(input).toHaveProperty("alert-a"); // input untouched
  });

  it("unmuteAlert is a no-op for unknown IDs", () => {
    const input: MutedAlertsMap = { "alert-a": { mutedUntil: NOW + 1000 } };
    const out = unmuteAlert(input, "alert-z");
    expect(out).toBe(input); // identity-preserved when nothing changed
  });
});

describe("pruneExpiredMutes", () => {
  it("drops expired entries and keeps active ones", () => {
    const input: MutedAlertsMap = {
      "alert-old": { mutedUntil: NOW - 100 },
      "alert-fresh": { mutedUntil: NOW + 100 },
    };
    const out = pruneExpiredMutes(input, NOW);
    expect(out).toEqual({ "alert-fresh": { mutedUntil: NOW + 100 } });
  });

  it("returns an empty map when everything is expired", () => {
    const input: MutedAlertsMap = {
      "a": { mutedUntil: 1 },
      "b": { mutedUntil: 2 },
    };
    expect(pruneExpiredMutes(input, NOW)).toEqual({});
  });

  it("does not mutate input", () => {
    const input: MutedAlertsMap = { "a": { mutedUntil: NOW - 1 } };
    pruneExpiredMutes(input, NOW);
    expect(Object.keys(input)).toEqual(["a"]); // unchanged
  });
});

describe("loadMutedAlerts / saveMutedAlerts (localStorage roundtrip)", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it("returns empty when no key is present", () => {
    expect(loadMutedAlerts(NOW)).toEqual({});
  });

  it("roundtrips a simple map", () => {
    const map: MutedAlertsMap = {
      "alert-a": { mutedUntil: NOW + 1000 },
      "alert-b": { mutedUntil: NOW + 5000 },
    };
    saveMutedAlerts(map);
    expect(loadMutedAlerts(NOW)).toEqual(map);
  });

  it("filters out expired entries during load", () => {
    saveMutedAlerts({
      "expired": { mutedUntil: NOW - 1 },
      "fresh": { mutedUntil: NOW + 1 },
    });
    expect(loadMutedAlerts(NOW)).toEqual({
      "fresh": { mutedUntil: NOW + 1 },
    });
  });

  it("recovers from corrupted JSON by wiping the key", () => {
    window.localStorage.setItem(MUTED_ALERTS_STORAGE_KEY, "{not json");
    expect(loadMutedAlerts(NOW)).toEqual({});
    expect(window.localStorage.getItem(MUTED_ALERTS_STORAGE_KEY)).toBeNull();
  });

  it("ignores entries with malformed shape", () => {
    window.localStorage.setItem(
      MUTED_ALERTS_STORAGE_KEY,
      JSON.stringify({
        "good": { mutedUntil: NOW + 1000 },
        "missing-field": {},
        "wrong-type": { mutedUntil: "soon" },
        "null-entry": null,
      }),
    );
    expect(loadMutedAlerts(NOW)).toEqual({
      "good": { mutedUntil: NOW + 1000 },
    });
  });

  it("ignores top-level non-object payloads (defensive)", () => {
    window.localStorage.setItem(MUTED_ALERTS_STORAGE_KEY, JSON.stringify([1, 2]));
    expect(loadMutedAlerts(NOW)).toEqual({});
  });
});
