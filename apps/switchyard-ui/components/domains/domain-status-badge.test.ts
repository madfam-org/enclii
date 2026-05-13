/**
 * Unit tests for components/domains/domain-status-badge.tsx
 *
 * Covers the pure `deriveDomainHealth` mapping from a backend Domain row to
 * a UI health bucket (active / provisioning / failed / orphaned / unknown).
 */

import { deriveDomainHealth } from './domain-status-badge';
import type { Domain } from '@/types/domain';

const baseDomain: Domain = {
  id: 'd-1',
  service_id: 's-1',
  environment_id: 'e-1',
  domain: 'api.example.com',
  verified: true,
  tls_enabled: true,
  status: 'active',
  is_platform_domain: false,
  zero_trust_enabled: false,
  created_at: '2026-04-01T00:00:00Z',
  updated_at: '2026-04-01T00:00:00Z',
  service_name: 'api',
  environment_name: 'prod',
  project_slug: 'example',
};

describe('deriveDomainHealth — active', () => {
  it('returns "active" when status=active and verified', () => {
    expect(deriveDomainHealth({ ...baseDomain })).toBe('active');
  });

  it('returns "provisioning" when status=active but unverified', () => {
    expect(
      deriveDomainHealth({ ...baseDomain, verified: false }),
    ).toBe('provisioning');
  });

  it('returns "active" when public evidence proves HTTPS reachability even if DB verifier is stale', () => {
    expect(
      deriveDomainHealth({
        ...baseDomain,
        verified: false,
        tls_enabled: false,
        status: 'pending',
        evidence: {
          source: 'public-probe',
          checked_at: '2026-05-13T07:31:00Z',
          public_dns_status: 'resolved',
          public_tls_status: 'valid',
          public_http_status: 404,
          public_http_reachable: true,
        },
      }),
    ).toBe('active');
  });
});

describe('deriveDomainHealth — provisioning', () => {
  it('returns "provisioning" for status=verifying', () => {
    expect(
      deriveDomainHealth({ ...baseDomain, status: 'verifying' }),
    ).toBe('provisioning');
  });

  it('returns "provisioning" for status=pending', () => {
    expect(
      deriveDomainHealth({ ...baseDomain, status: 'pending' }),
    ).toBe('provisioning');
  });
});

describe('deriveDomainHealth — failed', () => {
  it('returns "failed" for status=error', () => {
    expect(deriveDomainHealth({ ...baseDomain, status: 'error' })).toBe(
      'failed',
    );
  });
});

describe('deriveDomainHealth — orphaned', () => {
  it('returns "orphaned" when service_name is missing', () => {
    expect(
      deriveDomainHealth({ ...baseDomain, service_name: undefined }),
    ).toBe('orphaned');
  });

  it('returns "orphaned" when environment_name is missing', () => {
    expect(
      deriveDomainHealth({ ...baseDomain, environment_name: undefined }),
    ).toBe('orphaned');
  });

  it('returns "orphaned" even when status is active (orphan check wins)', () => {
    expect(
      deriveDomainHealth({
        ...baseDomain,
        service_name: undefined,
        status: 'active',
        verified: true,
      }),
    ).toBe('orphaned');
  });
});

describe('deriveDomainHealth — unknown / fallback', () => {
  it('returns "active" for unknown future status when verified', () => {
    expect(
      deriveDomainHealth({ ...baseDomain, status: 'some-future-status' }),
    ).toBe('active');
  });

  it('returns "unknown" for unknown future status when unverified', () => {
    expect(
      deriveDomainHealth({
        ...baseDomain,
        status: 'some-future-status',
        verified: false,
      }),
    ).toBe('unknown');
  });
});
