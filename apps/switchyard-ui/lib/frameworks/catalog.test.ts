/**
 * Unit tests for lib/frameworks/catalog.ts.
 *
 * Verifies the TS catalog stays in sync with the Go catalog (priority
 * order, slug coverage) and that slug lookup behavior matches the
 * backend contract.
 */

import {
  catalog,
  get,
  getOrUnknown,
  knownSlugs,
  mapBuildpackID,
  type Framework,
} from "./catalog";

describe("catalog integrity", () => {
  it("has at least 20 entries (minimum framework coverage)", () => {
    expect(catalog.length).toBeGreaterThanOrEqual(20);
  });

  it("all entries have non-empty slug, displayName, and iconSVG", () => {
    for (const fw of catalog) {
      expect(fw.slug).not.toBe("");
      expect(fw.displayName).not.toBe("");
      expect(fw.iconSVG.startsWith("<svg")).toBe(true);
    }
  });

  it("slugs are unique", () => {
    const seen = new Set<string>();
    for (const fw of catalog) {
      expect(seen.has(fw.slug)).toBe(false);
      seen.add(fw.slug);
    }
  });

  it("contains the 'unknown' sentinel", () => {
    expect(catalog.some((fw) => fw.slug === "unknown")).toBe(true);
  });

  it("preserves priority: nextjs appears before react", () => {
    const nextIdx = catalog.findIndex((fw) => fw.slug === "nextjs");
    const reactIdx = catalog.findIndex((fw) => fw.slug === "react");
    expect(nextIdx).toBeGreaterThanOrEqual(0);
    expect(reactIdx).toBeGreaterThan(nextIdx);
  });

  it("preserves priority: nuxt appears before vue", () => {
    const nuxtIdx = catalog.findIndex((fw) => fw.slug === "nuxtjs");
    const vueIdx = catalog.findIndex((fw) => fw.slug === "vue");
    expect(vueIdx).toBeGreaterThan(nuxtIdx);
  });
});

describe("get()", () => {
  it("returns the entry for a known slug", () => {
    const fw = get("nextjs");
    expect(fw).toBeDefined();
    expect(fw!.displayName).toBe("Next.js");
  });

  it("is case-insensitive", () => {
    expect(get("NEXTJS")?.slug).toBe("nextjs");
    expect(get("NextJS")?.slug).toBe("nextjs");
  });

  it("returns undefined for unknown slug", () => {
    expect(get("nonexistent")).toBeUndefined();
  });

  it("returns undefined for empty/null input", () => {
    expect(get("")).toBeUndefined();
    expect(get(null)).toBeUndefined();
    expect(get(undefined)).toBeUndefined();
  });
});

describe("getOrUnknown()", () => {
  it("returns the entry for known slug", () => {
    expect(getOrUnknown("django").slug).toBe("django");
  });

  it("returns the 'unknown' sentinel for unrecognized input", () => {
    expect(getOrUnknown("nonexistent").slug).toBe("unknown");
    expect(getOrUnknown(undefined).slug).toBe("unknown");
    expect(getOrUnknown(null).slug).toBe("unknown");
  });

  it("never returns undefined", () => {
    const fw: Framework = getOrUnknown("anything");
    expect(fw).toBeDefined();
  });
});

describe("knownSlugs()", () => {
  it("returns all slugs except 'unknown'", () => {
    const slugs = knownSlugs();
    expect(slugs).not.toContain("unknown");
    expect(slugs).toContain("nextjs");
    expect(slugs).toContain("go-stdlib");
    expect(slugs.length).toBe(catalog.length - 1);
  });
});

describe("mapBuildpackID()", () => {
  it("maps go buildpack to first Go catalog entry (go-fiber)", () => {
    expect(mapBuildpackID("paketo-buildpacks/go")).toBe("go-fiber");
  });

  it("strips version suffix after @", () => {
    expect(mapBuildpackID("paketo-buildpacks/go@4.8.0")).toBe("go-fiber");
  });

  it("maps nodejs buildpack to first Node catalog entry (nextjs)", () => {
    expect(mapBuildpackID("paketo-buildpacks/nodejs")).toBe("nextjs");
  });

  it("maps python buildpack to first Python catalog entry (django)", () => {
    expect(mapBuildpackID("paketo-buildpacks/python")).toBe("django");
  });

  it("maps web-servers buildpack to static", () => {
    expect(mapBuildpackID("paketo-buildpacks/web-servers")).toBe("static");
  });

  it("returns 'unknown' for unmapped buildpack", () => {
    expect(mapBuildpackID("foo/bar")).toBe("unknown");
  });

  it("returns 'unknown' for empty input", () => {
    expect(mapBuildpackID("")).toBe("unknown");
  });
});
