/**
 * rollout-state.ts — pure helpers for the rollout-truthfulness layer.
 *
 * Kept separate from project-card-compact.tsx so unit tests can exercise the
 * helper without dragging in @enclii/ui-components/badge (Jest's `exports`-
 * field resolution is sketchy in this app's config; the .tsx import would
 * fail at test time). The .tsx re-exports the types so callers can use
 * either entry point.
 *
 * Shape mirrors switchyard-api/internal/k8s/rollout_state.go. When that file
 * grows a new state or blocked reason, update this file too — the union is
 * loose-typed (`| string`) so older UI builds tolerate new reasons, but
 * adding explicit cases here gives operators better tooltip copy.
 */

// RolloutState mirrors the API field. It surfaces whether the *newest*
// ReplicaSet has actually landed (separate from the legacy `health` signal,
// which lies "healthy" while a new RS may have been failing readiness for
// days while an older RS keeps serving).
export type RolloutState = "ok" | "progressing" | "blocked";

// Reasons the rollout is blocked, classified from pod statuses by the API.
// Loose-typed (`| string`) so the UI tolerates new reasons added on the API
// side without crashing older builds — they fall back to the generic copy.
export type RolloutBlockedReason =
  | "image_pull_back_off"
  | "crash_loop_back_off"
  | "readiness_probe_failed"
  | "pending"
  | "unknown"
  | string;

// Human-readable mapping for `rollout_blocked_reason`. Pure fn so unit tests
// don't need a render. The default branch is deliberately operator-actionable
// — "investigate" — rather than empty so the badge tooltip is never blank.
export function rolloutBlockedReasonLabel(
  reason: RolloutBlockedReason | undefined | null,
): string {
  switch (reason) {
    case "image_pull_back_off":
      return "New image can't be pulled — build/publish may have failed.";
    case "crash_loop_back_off":
      return "New pods are crash-looping — check container logs.";
    case "readiness_probe_failed":
      return "New pods are running but failing readiness — probe path or boot may be off.";
    case "pending":
      return "New pods can't be scheduled — likely resource or anti-affinity constraint.";
    case "unknown":
    case "":
    case undefined:
    case null:
      return "New ReplicaSet hasn't reached Ready — investigate pod status.";
    default:
      return "New ReplicaSet hasn't reached Ready — investigate pod status.";
  }
}

// Narrow an arbitrary API string to the typed RolloutState union, returning
// `undefined` for anything we don't recognize. Used by the home + projects
// pages when mapping ApiService → CompactService.
export function asRolloutState(s: unknown): RolloutState | undefined {
  if (s === "ok" || s === "progressing" || s === "blocked") {
    return s;
  }
  return undefined;
}
