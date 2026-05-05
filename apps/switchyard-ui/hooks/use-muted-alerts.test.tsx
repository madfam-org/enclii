/**
 * Tests for `hooks/use-muted-alerts.ts`.
 *
 * Exercises the React state lifecycle and the localStorage round-trip
 * via `renderHook`, covering:
 *   - hydration from localStorage on mount (mute survives reload)
 *   - mute / unmute mutation paths (in-memory + persisted)
 *   - automatic clearing of expired mutes on the prune interval
 */

import { act, renderHook } from "@testing-library/react";
import { useMutedAlerts } from "./use-muted-alerts";
import {
  MUTED_ALERTS_STORAGE_KEY,
  type MutedAlertsMap,
} from "@/lib/muted-alerts";

function readStorage(): MutedAlertsMap {
  const raw = window.localStorage.getItem(MUTED_ALERTS_STORAGE_KEY);
  return raw ? (JSON.parse(raw) as MutedAlertsMap) : {};
}

beforeEach(() => {
  window.localStorage.clear();
  jest.useRealTimers();
});

afterEach(() => {
  jest.useRealTimers();
});

describe("useMutedAlerts — hydration", () => {
  it("hydrates from localStorage so a mute survives reload", () => {
    // Simulate a previous session by pre-populating localStorage
    // before the hook mounts.
    const future = Date.now() + 60_000;
    window.localStorage.setItem(
      MUTED_ALERTS_STORAGE_KEY,
      JSON.stringify({ "alert-a": { mutedUntil: future } }),
    );

    const { result } = renderHook(() => useMutedAlerts());
    // After mount the useEffect has populated state.
    expect(result.current.isMuted("alert-a")).toBe(true);
    expect(result.current.mutedUntil("alert-a")).toBe(future);
  });

  it("starts empty when localStorage has no key", () => {
    const { result } = renderHook(() => useMutedAlerts());
    expect(result.current.mutedAlerts).toEqual({});
    expect(result.current.isMuted("anything")).toBe(false);
  });
});

describe("useMutedAlerts — mute / unmute", () => {
  it("mute writes to memory + localStorage", () => {
    const { result } = renderHook(() => useMutedAlerts());

    act(() => {
      result.current.mute("alert-x", 60_000);
    });

    expect(result.current.isMuted("alert-x")).toBe(true);
    const persisted = readStorage();
    expect(persisted["alert-x"]).toBeDefined();
    expect(persisted["alert-x"].mutedUntil).toBeGreaterThan(Date.now());
  });

  it("unmute clears in-memory + localStorage", () => {
    const { result } = renderHook(() => useMutedAlerts());

    act(() => {
      result.current.mute("alert-x", 60_000);
    });
    expect(result.current.isMuted("alert-x")).toBe(true);

    act(() => {
      result.current.unmute("alert-x");
    });
    expect(result.current.isMuted("alert-x")).toBe(false);
    expect(readStorage()["alert-x"]).toBeUndefined();
  });
});

describe("useMutedAlerts — expiry pruning", () => {
  it("auto-clears expired mutes when the prune interval fires", () => {
    jest.useFakeTimers();
    const { result } = renderHook(() => useMutedAlerts());

    act(() => {
      // Mute for 100 ms — well shorter than the 60 s prune interval.
      result.current.mute("alert-x", 100);
    });
    expect(result.current.isMuted("alert-x")).toBe(true);

    // Advance past the mute window AND past the prune interval so the
    // setInterval tick re-prunes the in-memory map.
    act(() => {
      jest.advanceTimersByTime(60_000 + 1);
    });

    expect(result.current.isMuted("alert-x")).toBe(false);
  });
});
