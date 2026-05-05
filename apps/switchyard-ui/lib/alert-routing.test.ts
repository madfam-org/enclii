/**
 * Unit tests for `lib/alert-routing.ts`.
 *
 * The routing contract is the single source of truth for "where does
 * clicking alert X take me", so every documented prefix gets an
 * explicit test row plus the negative cases (unknown prefix,
 * service-scoped alert missing service_id).
 */

import {
  alertHref,
  alertActionLabel,
  FALLBACK_HREF,
  type RoutableAlert,
} from "./alert-routing";

const SVC = "550e8400-e29b-41d4-a716-446655440000";

function alert(id: string, service_id?: string): RoutableAlert {
  return { id, service_id };
}

describe("alertHref — global metric prefixes", () => {
  it("alert-error-rate-high → /observability", () => {
    expect(alertHref(alert("alert-error-rate-high"))).toBe("/observability");
  });

  it("alert-latency-high → /observability", () => {
    expect(alertHref(alert("alert-latency-high"))).toBe("/observability");
  });

  it("alert-cache-hit-low → /observability#cache", () => {
    expect(alertHref(alert("alert-cache-hit-low"))).toBe(
      "/observability#cache",
    );
  });

  it("alert-db-conn-high → /observability#database", () => {
    expect(alertHref(alert("alert-db-conn-high"))).toBe(
      "/observability#database",
    );
  });

  it("alert-build-failures → /observability#builds", () => {
    expect(alertHref(alert("alert-build-failures"))).toBe(
      "/observability#builds",
    );
  });
});

describe("alertHref — service-scoped prefixes", () => {
  it("alert-service-replicas-<id> → /services/<id>", () => {
    expect(alertHref(alert(`alert-service-replicas-${SVC}`, SVC))).toBe(
      `/services/${SVC}`,
    );
  });

  it("alert-service-unhealthy-<id> → /services/<id>", () => {
    expect(alertHref(alert(`alert-service-unhealthy-${SVC}`, SVC))).toBe(
      `/services/${SVC}`,
    );
  });

  it("alert-service-failed-<id> → /services/<id> (deployments tab)", () => {
    expect(alertHref(alert(`alert-service-failed-${SVC}`, SVC))).toBe(
      `/services/${SVC}`,
    );
  });

  it("service-scoped alert WITHOUT service_id → fallback (no /services/undefined)", () => {
    // This is the regression guard for the broken-link case: backend
    // could in theory emit a malformed payload, and we must never let
    // the literal string "undefined" leak into a path segment.
    const broken = alertHref(alert("alert-service-replicas-"));
    expect(broken).toBe(FALLBACK_HREF);
    expect(broken).not.toContain("undefined");
  });

  it("falls back when service_id is empty string", () => {
    expect(
      alertHref({ id: "alert-service-unhealthy-x", service_id: "" }),
    ).toBe(FALLBACK_HREF);
  });
});

describe("alertHref — overage prefix", () => {
  it("alert-usage-overage-<metric> → /usage", () => {
    expect(alertHref(alert("alert-usage-overage-builds"))).toBe("/usage");
    expect(alertHref(alert("alert-usage-overage-bandwidth"))).toBe("/usage");
  });
});

describe("alertHref — fallback behaviour", () => {
  it("unknown prefix → /observability", () => {
    expect(alertHref(alert("alert-totally-novel-class-2099"))).toBe(
      FALLBACK_HREF,
    );
  });

  it("non-alert ID → /observability (defensive)", () => {
    expect(alertHref(alert("garbage-id"))).toBe(FALLBACK_HREF);
  });

  it("never returns an empty string", () => {
    expect(alertHref(alert(""))).toBe(FALLBACK_HREF);
  });
});

describe("alertActionLabel", () => {
  it('returns "Open service" for replica/unhealthy alerts', () => {
    expect(
      alertActionLabel(alert(`alert-service-replicas-${SVC}`, SVC)),
    ).toBe("Open service");
    expect(
      alertActionLabel(alert(`alert-service-unhealthy-${SVC}`, SVC)),
    ).toBe("Open service");
  });

  it('returns "View deployment history" for failed deployments', () => {
    expect(
      alertActionLabel(alert(`alert-service-failed-${SVC}`, SVC)),
    ).toBe("View deployment history");
  });

  it('returns "View usage" for overage alerts', () => {
    expect(alertActionLabel(alert("alert-usage-overage-builds"))).toBe(
      "View usage",
    );
  });

  it('returns "Open observability" for global metric alerts', () => {
    expect(alertActionLabel(alert("alert-error-rate-high"))).toBe(
      "Open observability",
    );
    expect(alertActionLabel(alert("alert-cache-hit-low"))).toBe(
      "Open observability",
    );
  });

  it('returns "Open observability" for unknown prefixes', () => {
    expect(alertActionLabel(alert("alert-novel-99"))).toBe(
      "Open observability",
    );
  });

  it('returns "Open observability" for service alerts missing service_id', () => {
    // Mirrors the alertHref fallback: if we can't link to the service
    // page we shouldn't claim the action will open it.
    expect(alertActionLabel(alert("alert-service-failed-"))).toBe(
      "Open observability",
    );
  });
});
