/**
 * Unit tests for components/databases/memory-display.ts
 *
 * Tests the pure `memoryDisplay` helper (audit finding DB-1). The
 * React component itself is exercised manually + via the /databases
 * page; this app does not bundle @testing-library/react.
 */

import { memoryDisplay } from "./memory-display";

describe("memoryDisplay", () => {
  it('returns em-dash for missing memory', () => {
    expect(memoryDisplay(undefined)).toEqual({ label: "—", tooltip: null });
    expect(memoryDisplay(null)).toEqual({ label: "—", tooltip: null });
    expect(memoryDisplay("")).toEqual({ label: "—", tooltip: null });
  });

  it('expands the bare "shared" token into a friendly label + tooltip', () => {
    const r = memoryDisplay("shared");
    expect(r.label).toBe("Shared (cluster pool)");
    expect(r.tooltip).toContain("shared pool");
    expect(r.tooltip).toContain("enclii admin databases discover");
  });

  it("passes concrete sizes through unchanged with no tooltip", () => {
    expect(memoryDisplay("256Mi")).toEqual({ label: "256Mi", tooltip: null });
    expect(memoryDisplay("1Gi")).toEqual({ label: "1Gi", tooltip: null });
    expect(memoryDisplay("512Mi")).toEqual({ label: "512Mi", tooltip: null });
  });
});
