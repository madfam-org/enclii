/**
 * Unit tests for components/dashboard/service-link.tsx
 *
 * This app does not have @testing-library/react installed (see jest.setup.js
 * and package.json). Following the project convention (see
 * project-card-compact.test.ts, last-sync-badge.test.ts, framework-icon.test.ts),
 * we test the pure helper functions exhaustively. Render-time behavior
 * (clipboard, copy-feedback animation, parent-card click propagation) is
 * exercised via the e2e Playwright suite rather than Jest.
 *
 * Coverage:
 *   - truncateUrl: short pass-through, long truncation, empty
 *   - envBadgeClass: each env produces a distinct, non-empty class string
 *   - envLabel: each env maps to its short uppercase label
 *   - normalizeEnv: production/staging/preview/development variants and
 *     fallback for empty/unknown values
 */

import {
  truncateUrl,
  envBadgeClass,
  envLabel,
  normalizeEnv,
  type ServiceEnv,
} from "./service-link";

describe("truncateUrl", () => {
  it("returns the URL unchanged when shorter than max", () => {
    expect(truncateUrl("api.example.com", 30)).toBe("api.example.com");
  });

  it("returns the URL unchanged when exactly at max", () => {
    const s = "a".repeat(30);
    expect(truncateUrl(s, 30)).toBe(s);
  });

  it("truncates with ellipsis when longer than max", () => {
    const r = truncateUrl("very-long-staging-host.example.com/v1/admin", 20);
    expect(r.length).toBe(20);
    expect(r.endsWith("…")).toBe(true);
  });

  it("returns empty string for empty input", () => {
    expect(truncateUrl("", 30)).toBe("");
  });

  it("uses a default max length when none is supplied", () => {
    // The component itself relies on the default — guard against accidental
    // signature changes that would silently break the card layout.
    const out = truncateUrl("a".repeat(100));
    expect(out.length).toBeLessThanOrEqual(30);
    expect(out.endsWith("…")).toBe(true);
  });
});

describe("envBadgeClass", () => {
  it("returns a non-empty class string for production", () => {
    const c = envBadgeClass("production");
    expect(c).toBeTruthy();
    // Production is the default destination — uses the muted neutral
    // styling shared with the framework chip.
    expect(c).toContain("muted-foreground");
  });

  it("returns warning-tone classes for staging", () => {
    const c = envBadgeClass("staging");
    expect(c).toContain("status-warning");
  });

  it("returns info-tone classes for preview", () => {
    const c = envBadgeClass("preview");
    expect(c).toContain("status-info");
  });

  it("returns muted classes for development", () => {
    const c = envBadgeClass("development");
    expect(c).toContain("muted");
  });

  it("never returns an empty string for any defined env", () => {
    const envs: ServiceEnv[] = [
      "production",
      "staging",
      "preview",
      "development",
    ];
    for (const e of envs) {
      expect(envBadgeClass(e).length).toBeGreaterThan(0);
    }
  });
});

describe("envLabel", () => {
  it("returns short labels for each env", () => {
    expect(envLabel("production")).toBe("prod");
    expect(envLabel("staging")).toBe("staging");
    expect(envLabel("preview")).toBe("preview");
    expect(envLabel("development")).toBe("dev");
  });
});

describe("normalizeEnv", () => {
  it("recognizes canonical production", () => {
    expect(normalizeEnv("production")).toBe("production");
  });

  it("recognizes the prod shorthand", () => {
    expect(normalizeEnv("prod")).toBe("production");
  });

  it("is case-insensitive", () => {
    expect(normalizeEnv("PRODUCTION")).toBe("production");
    expect(normalizeEnv("Staging")).toBe("staging");
  });

  it("trims whitespace", () => {
    expect(normalizeEnv("  staging  ")).toBe("staging");
  });

  it("recognizes staging variants", () => {
    expect(normalizeEnv("staging")).toBe("staging");
    expect(normalizeEnv("stage")).toBe("staging");
  });

  it("recognizes preview", () => {
    expect(normalizeEnv("preview")).toBe("preview");
  });

  it("recognizes development variants", () => {
    expect(normalizeEnv("development")).toBe("development");
    expect(normalizeEnv("dev")).toBe("development");
  });

  it("falls back to production when input is empty", () => {
    // Discovered during this PR: API often returns auto_deploy_env="" for
    // services that have a working domain but no auto-deploy configured.
    // Fallback to production keeps the link clickable rather than masking
    // a real URL behind an [internal] chip.
    expect(normalizeEnv("")).toBe("production");
    expect(normalizeEnv(undefined)).toBe("production");
    expect(normalizeEnv(null)).toBe("production");
  });

  it("falls back to production for unknown values", () => {
    expect(normalizeEnv("qa")).toBe("production");
    expect(normalizeEnv("review-app")).toBe("production");
  });
});
