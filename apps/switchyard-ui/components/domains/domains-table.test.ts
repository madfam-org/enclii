/**
 * Unit tests for components/domains/domains-table.tsx
 *
 * Covers the pure helpers used by the table: tunnel-mode derivation,
 * filtering, and sorting. The component itself renders via the parent page;
 * these tests pin the deterministic logic so future refactors don't silently
 * regress sort order or filter semantics.
 */

import {
  describeExternalEvidence,
  deriveTunnelMode,
  filterDomains,
  sortDomains,
} from './domains-table';
import type { Domain } from '@/types/domain';

const NOW = new Date('2026-04-26T12:00:00Z').getTime();
const DAY = 86_400_000;

function makeDomain(overrides: Partial<Domain>): Domain {
  return {
    id: overrides.id ?? Math.random().toString(36).slice(2),
    service_id: overrides.service_id ?? 'svc-1',
    environment_id: overrides.environment_id ?? 'env-1',
    domain: overrides.domain ?? 'example.com',
    verified: overrides.verified ?? true,
    tls_enabled: overrides.tls_enabled ?? true,
    status: overrides.status ?? 'active',
    is_platform_domain: overrides.is_platform_domain ?? false,
    zero_trust_enabled: overrides.zero_trust_enabled ?? false,
    created_at: overrides.created_at ?? '2026-04-01T00:00:00Z',
    updated_at: overrides.updated_at ?? '2026-04-01T00:00:00Z',
    service_name: overrides.service_name ?? 'api',
    environment_name: overrides.environment_name ?? 'prod',
    project_slug: overrides.project_slug ?? 'example',
    ...overrides,
  };
}

describe('deriveTunnelMode', () => {
  it('returns "tunneled" when cloudflare_tunnel_id is present', () => {
    expect(
      deriveTunnelMode(makeDomain({ cloudflare_tunnel_id: 'tun-123' })),
    ).toBe('tunneled');
  });

  it('returns "unknown" for platform domains without a tunnel id', () => {
    expect(
      deriveTunnelMode(
        makeDomain({ cloudflare_tunnel_id: null, is_platform_domain: true }),
      ),
    ).toBe('unknown');
  });

  it('returns "direct" for non-platform domains without a tunnel id', () => {
    expect(
      deriveTunnelMode(
        makeDomain({ cloudflare_tunnel_id: null, is_platform_domain: false }),
      ),
    ).toBe('direct');
  });
});

describe('describeExternalEvidence', () => {
  it('treats valid TLS plus any HTTP response as external proof', () => {
    const summary = describeExternalEvidence(
      makeDomain({
        verified: false,
        tls_enabled: false,
        evidence: {
          source: 'public-probe',
          checked_at: '2026-05-13T07:31:00Z',
          public_dns_status: 'resolved',
          public_tls_status: 'valid',
          public_http_status: 404,
          public_http_reachable: true,
        },
      }),
    );

    expect(summary.label).toBe('HTTPS valid');
    expect(summary.detail).toBe('HTTP 404');
  });

  it('returns a muted summary when no public probe evidence exists', () => {
    const summary = describeExternalEvidence(makeDomain({ evidence: null }));

    expect(summary.label).toBe('No probe');
    expect(summary.detail).toBe('No public evidence');
  });
});

describe('filterDomains', () => {
  const dataset: Domain[] = [
    makeDomain({
      id: '1',
      domain: 'dhan.am',
      project_slug: 'dhanam',
      service_name: 'web',
    }),
    makeDomain({
      id: '2',
      domain: 'api.karafiel.mx',
      project_slug: 'karafiel',
      service_name: 'api',
    }),
    makeDomain({
      id: '3',
      domain: 'orphan.example.com',
      service_name: undefined, // orphaned
      project_slug: 'example',
    }),
    makeDomain({
      id: '4',
      domain: 'failing.example.com',
      status: 'error',
      project_slug: 'example',
    }),
  ];

  it('filters by substring match across domain/project/service', () => {
    expect(
      filterDomains(dataset, {
        search: 'karafiel',
        status: 'all',
        project: 'all',
      }).map((d) => d.id),
    ).toEqual(['2']);
  });

  it('filters case-insensitively', () => {
    expect(
      filterDomains(dataset, {
        search: 'KARAFIEL',
        status: 'all',
        project: 'all',
      }).map((d) => d.id),
    ).toEqual(['2']);
  });

  it('filters by health status (orphaned)', () => {
    expect(
      filterDomains(dataset, {
        search: '',
        status: 'orphaned',
        project: 'all',
      }).map((d) => d.id),
    ).toEqual(['3']);
  });

  it('filters by health status (failed)', () => {
    expect(
      filterDomains(dataset, {
        search: '',
        status: 'failed',
        project: 'all',
      }).map((d) => d.id),
    ).toEqual(['4']);
  });

  it('filters by project slug', () => {
    expect(
      filterDomains(dataset, {
        search: '',
        status: 'all',
        project: 'example',
      }).map((d) => d.id),
    ).toEqual(['3', '4']);
  });

  it('combines filters (AND semantics)', () => {
    expect(
      filterDomains(dataset, {
        search: 'failing',
        status: 'failed',
        project: 'example',
      }).map((d) => d.id),
    ).toEqual(['4']);
  });

  it('returns all when no filters active', () => {
    expect(
      filterDomains(dataset, {
        search: '',
        status: 'all',
        project: 'all',
      }).length,
    ).toBe(4);
  });
});

describe('sortDomains — by expiry', () => {
  const dataset: Domain[] = [
    makeDomain({ id: 'far', tls_expires_at: new Date(NOW + 60 * DAY).toISOString() }),
    makeDomain({ id: 'soon', tls_expires_at: new Date(NOW + 5 * DAY).toISOString() }),
    makeDomain({ id: 'mid', tls_expires_at: new Date(NOW + 20 * DAY).toISOString() }),
    makeDomain({ id: 'unknown', tls_expires_at: null }),
  ];

  it('sorts ascending (worst first), unknown to bottom', () => {
    expect(sortDomains(dataset, 'expiry-asc').map((d) => d.id)).toEqual([
      'soon',
      'mid',
      'far',
      'unknown',
    ]);
  });

  it('sorts descending, unknown still to bottom', () => {
    expect(sortDomains(dataset, 'expiry-desc').map((d) => d.id)).toEqual([
      'far',
      'mid',
      'soon',
      'unknown',
    ]);
  });

  it('does not mutate the input array', () => {
    const before = dataset.map((d) => d.id);
    sortDomains(dataset, 'expiry-asc');
    expect(dataset.map((d) => d.id)).toEqual(before);
  });
});

describe('sortDomains — by domain name', () => {
  const dataset: Domain[] = [
    makeDomain({ id: '1', domain: 'zeta.com' }),
    makeDomain({ id: '2', domain: 'alpha.com' }),
    makeDomain({ id: '3', domain: 'mid.com' }),
  ];

  it('sorts alphabetically ascending', () => {
    expect(sortDomains(dataset, 'domain-asc').map((d) => d.domain)).toEqual([
      'alpha.com',
      'mid.com',
      'zeta.com',
    ]);
  });

  it('sorts alphabetically descending', () => {
    expect(sortDomains(dataset, 'domain-desc').map((d) => d.domain)).toEqual([
      'zeta.com',
      'mid.com',
      'alpha.com',
    ]);
  });
});
