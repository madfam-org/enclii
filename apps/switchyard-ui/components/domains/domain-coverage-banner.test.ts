/**
 * Unit tests for the pure `decideBanners` helper.
 *
 * Covers parity-audit gap DM-1..4 — operator clarity on the /domains
 * page when the inventory is partial or the verifier is wedged.
 */

import {
  decideBanners,
  STALE_VERIFIER_THRESHOLD_SECONDS,
} from './domain-coverage-banner';
import type { DomainCoverage } from '@/types/domain';

const healthyCoverage: DomainCoverage = {
  sync_configured: true,
  projects_total: 5,
  projects_with_domains: 5,
  domains_total: 12,
  oldest_unverified_age_seconds: -1, // sentinel: nothing unverified
};

describe('decideBanners — null coverage', () => {
  it('returns no banners when coverage is null (older API build)', () => {
    expect(decideBanners(null)).toEqual([]);
  });
});

describe('decideBanners — healthy', () => {
  it('returns no banners when everything is configured and complete', () => {
    expect(decideBanners(healthyCoverage)).toEqual([]);
  });
});

describe('decideBanners — sync not configured', () => {
  it('flags sync-not-configured as error severity', () => {
    const banners = decideBanners({
      ...healthyCoverage,
      sync_configured: false,
    });
    expect(banners).toHaveLength(1);
    expect(banners[0].kind).toBe('sync-not-configured');
    expect(banners[0].severity).toBe('error');
    expect(banners[0].title).toContain('Cloudflare');
  });
});

describe('decideBanners — inventory incomplete', () => {
  it('flags warning when projects_with_domains < projects_total', () => {
    const banners = decideBanners({
      ...healthyCoverage,
      projects_total: 10,
      projects_with_domains: 8,
    });
    expect(banners).toHaveLength(1);
    expect(banners[0].kind).toBe('inventory-incomplete');
    expect(banners[0].severity).toBe('warning');
    expect(banners[0].body).toContain('2 projects');
  });

  it('uses singular "project" when only 1 missing', () => {
    const banners = decideBanners({
      ...healthyCoverage,
      projects_total: 5,
      projects_with_domains: 4,
    });
    expect(banners[0].body).toContain('1 project ');
    expect(banners[0].body).not.toContain('1 projects');
  });

  it('does NOT flag when projects_total is 0 (unknown — avoid false alarms)', () => {
    const banners = decideBanners({
      ...healthyCoverage,
      projects_total: 0,
      projects_with_domains: 0,
    });
    expect(banners).toEqual([]);
  });

  it('does NOT flag when coverage is exact', () => {
    const banners = decideBanners({
      ...healthyCoverage,
      projects_total: 7,
      projects_with_domains: 7,
    });
    expect(banners).toEqual([]);
  });
});

describe('decideBanners — verifier stale', () => {
  it('does NOT flag when sentinel -1 (everything verified)', () => {
    const banners = decideBanners({
      ...healthyCoverage,
      oldest_unverified_age_seconds: -1,
    });
    expect(banners).toEqual([]);
  });

  it('does NOT flag when age is below threshold', () => {
    const banners = decideBanners({
      ...healthyCoverage,
      oldest_unverified_age_seconds: 60 * 60, // 1h
    });
    expect(banners).toEqual([]);
  });

  it('flags when age exceeds threshold', () => {
    const banners = decideBanners({
      ...healthyCoverage,
      oldest_unverified_age_seconds:
        STALE_VERIFIER_THRESHOLD_SECONDS + 3600,
    });
    expect(banners).toHaveLength(1);
    expect(banners[0].kind).toBe('verifier-stale');
    expect(banners[0].severity).toBe('error');
    expect(banners[0].body).toMatch(/\d+h/);
  });
});

describe('decideBanners — multiple banners', () => {
  it('returns banners in priority order: sync → inventory → stale', () => {
    const banners = decideBanners({
      sync_configured: false,
      projects_total: 10,
      projects_with_domains: 3,
      domains_total: 5,
      oldest_unverified_age_seconds:
        STALE_VERIFIER_THRESHOLD_SECONDS + 1,
    });
    expect(banners.map((b) => b.kind)).toEqual([
      'sync-not-configured',
      'inventory-incomplete',
      'verifier-stale',
    ]);
  });
});
