/**
 * Unit tests for components/dashboard/project-card-compact.tsx
 *
 * Covers the pure shortImageRef helper used to render the running-image chip.
 * The component itself is exercised via the parent dashboard E2E tests; this
 * file isolates the format function so we can add edge cases without spinning
 * up a render.
 */

import { shortImageRef } from "./project-card-compact";

describe("shortImageRef — digest references", () => {
  it("truncates sha256 digests to algo + 12 hex chars", () => {
    expect(
      shortImageRef(
        "ghcr.io/madfam-org/svc@sha256:abc123def4567890abcdef1234567890",
      ),
    ).toBe("sha256:abc123def456");
  });

  it("handles digests without colons (rare but possible)", () => {
    expect(shortImageRef("registry.io/svc@deadbeef0123456789")).toBe(
      "deadbeef0123456789",
    );
  });
});

describe("shortImageRef — tag references", () => {
  it("returns the tag for tag-style images", () => {
    expect(shortImageRef("ghcr.io/madfam-org/svc:v1.2.3")).toBe("v1.2.3");
  });

  it("returns the tag even when the image name contains a colon-port host", () => {
    expect(shortImageRef("registry.local:5000/svc:1.0")).toBe("1.0");
  });

  it("returns empty string when there is no tag (untagged)", () => {
    expect(shortImageRef("ghcr.io/madfam-org/svc")).toBe("");
  });
});

describe("shortImageRef — empty / nullish", () => {
  it("returns empty for empty string", () => {
    expect(shortImageRef("")).toBe("");
  });

  it("returns empty for undefined", () => {
    expect(shortImageRef(undefined)).toBe("");
  });

  it("returns empty for null", () => {
    expect(shortImageRef(null)).toBe("");
  });
});
