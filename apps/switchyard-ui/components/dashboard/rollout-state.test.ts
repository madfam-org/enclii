/**
 * Unit tests for components/dashboard/rollout-state.ts
 *
 * Covers:
 *   - rolloutBlockedReasonLabel: tooltip copy mapping for the "Rollout blocked"
 *     badge surfaced by RolloutStateIndicator on project-card-compact.tsx. The
 *     badge appears when the newest ReplicaSet is stuck > 10 min while an
 *     older RS keeps serving — the dishonest case the dashboard hid before
 *     this change.
 *   - asRolloutState: narrowing guard used by the home + projects pages when
 *     mapping ApiService.rollout_state → CompactService.rolloutState.
 *
 * Render-level coverage of the indicator lives in the parent dashboard E2E
 * tests — this app intentionally does not pull in @testing-library/react.
 */

import { rolloutBlockedReasonLabel, asRolloutState } from "./rollout-state";

describe("rolloutBlockedReasonLabel — known reasons", () => {
  it("explains image_pull_back_off as a build/publish failure hint", () => {
    expect(rolloutBlockedReasonLabel("image_pull_back_off")).toMatch(
      /can't be pulled/i,
    );
  });

  it("points operators to logs for crash_loop_back_off", () => {
    expect(rolloutBlockedReasonLabel("crash_loop_back_off")).toMatch(
      /crash-looping/i,
    );
  });

  it("flags readiness_probe_failed with a hint about probe path", () => {
    expect(rolloutBlockedReasonLabel("readiness_probe_failed")).toMatch(
      /readiness/i,
    );
  });

  it("calls out scheduling failure for pending", () => {
    expect(rolloutBlockedReasonLabel("pending")).toMatch(/scheduled/i);
  });
});

describe("rolloutBlockedReasonLabel — fallbacks", () => {
  it("returns generic copy for unknown reason", () => {
    expect(rolloutBlockedReasonLabel("unknown")).toMatch(/investigate/i);
  });

  it("returns generic copy for empty string", () => {
    expect(rolloutBlockedReasonLabel("")).toMatch(/investigate/i);
  });

  it("returns generic copy for undefined", () => {
    expect(rolloutBlockedReasonLabel(undefined)).toMatch(/investigate/i);
  });

  it("returns generic copy for null", () => {
    expect(rolloutBlockedReasonLabel(null)).toMatch(/investigate/i);
  });

  it("returns generic copy for an unanticipated reason string from a future API version", () => {
    // The union is loose-typed (`| string`) so the UI tolerates new reasons
    // added on the API side — assert we never produce empty tooltip text.
    const label = rolloutBlockedReasonLabel("network_policy_block");
    expect(label.length).toBeGreaterThan(0);
    expect(label).toMatch(/investigate/i);
  });
});

describe("asRolloutState — narrowing guard", () => {
  it("passes through 'ok'", () => {
    expect(asRolloutState("ok")).toBe("ok");
  });

  it("passes through 'progressing'", () => {
    expect(asRolloutState("progressing")).toBe("progressing");
  });

  it("passes through 'blocked'", () => {
    expect(asRolloutState("blocked")).toBe("blocked");
  });

  it("rejects unknown strings (future API states)", () => {
    expect(asRolloutState("draining")).toBeUndefined();
  });

  it("rejects empty string", () => {
    expect(asRolloutState("")).toBeUndefined();
  });

  it("rejects undefined", () => {
    expect(asRolloutState(undefined)).toBeUndefined();
  });

  it("rejects null", () => {
    expect(asRolloutState(null)).toBeUndefined();
  });

  it("rejects non-string values", () => {
    expect(asRolloutState(42)).toBeUndefined();
    expect(asRolloutState({})).toBeUndefined();
  });
});
