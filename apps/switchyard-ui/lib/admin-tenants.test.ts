/**
 * Unit tests for the master-admin tenant-switching client wrappers.
 *
 * Why this layer (and not the React context): switchyard-ui has no
 * @testing-library/react dep — existing tests are pure-logic only
 * (see jest.config.js + lib/utils.test.ts pattern). The context's
 * enter/exit/refresh logic is a thin wrapper over these helpers, so we
 * exercise the helpers directly with apiGet/apiPost mocked. The pure
 * payload-translation helpers in ScopeContext (adminTenantToScope,
 * activeSessionToScope) are also covered here for completeness.
 */

jest.mock('@/lib/api', () => ({
  apiGet: jest.fn(),
  apiPost: jest.fn(),
}));

import { apiGet, apiPost } from '@/lib/api';
import {
  enterTenantSession,
  exitTenantSession,
  fetchActiveActingSession,
  fetchAdminTenants,
} from './admin-tenants';
import { activeSessionToScope, adminTenantToScope } from '@/contexts/ScopeContext';
import { formatTimeRemaining } from '@/components/AdminActingBanner';

const mockedApiGet = apiGet as jest.MockedFunction<typeof apiGet>;
const mockedApiPost = apiPost as jest.MockedFunction<typeof apiPost>;

beforeEach(() => {
  jest.clearAllMocks();
});

describe('fetchAdminTenants', () => {
  it('returns the tenants array from the response', async () => {
    mockedApiGet.mockResolvedValueOnce({
      tenants: [
        {
          id: 'team-1',
          name: 'Acme',
          slug: 'acme',
          created_at: '2026-01-01T00:00:00Z',
        },
      ],
    });
    const tenants = await fetchAdminTenants();
    expect(mockedApiGet).toHaveBeenCalledWith('/v1/admin/tenants');
    expect(tenants).toHaveLength(1);
    expect(tenants[0].slug).toBe('acme');
  });

  it('coerces a missing tenants field to an empty array', async () => {
    mockedApiGet.mockResolvedValueOnce({});
    const tenants = await fetchAdminTenants();
    expect(tenants).toEqual([]);
  });
});

describe('fetchActiveActingSession', () => {
  it('hits /v1/admin/tenants/active and returns the payload verbatim', async () => {
    const payload = { active: false };
    mockedApiGet.mockResolvedValueOnce(payload);
    const out = await fetchActiveActingSession();
    expect(mockedApiGet).toHaveBeenCalledWith('/v1/admin/tenants/active');
    expect(out).toEqual(payload);
  });
});

describe('enterTenantSession', () => {
  it('POSTs the slug-scoped enter URL with an empty body when no extras provided', async () => {
    mockedApiPost.mockResolvedValueOnce({ active: true });
    await enterTenantSession('acme');
    expect(mockedApiPost).toHaveBeenCalledWith('/v1/admin/tenants/acme/enter', {});
  });

  it('includes reason and duration_seconds when provided', async () => {
    mockedApiPost.mockResolvedValueOnce({ active: true });
    await enterTenantSession('acme', 'support ticket #123', 600);
    expect(mockedApiPost).toHaveBeenCalledWith('/v1/admin/tenants/acme/enter', {
      reason: 'support ticket #123',
      duration_seconds: 600,
    });
  });

  it('encodes slugs with reserved characters', async () => {
    mockedApiPost.mockResolvedValueOnce({ active: true });
    await enterTenantSession('acme/bad slug');
    expect(mockedApiPost).toHaveBeenCalledWith(
      '/v1/admin/tenants/acme%2Fbad%20slug/enter',
      {},
    );
  });

  it('omits a falsy duration_seconds (e.g. 0)', async () => {
    mockedApiPost.mockResolvedValueOnce({ active: true });
    await enterTenantSession('acme', undefined, 0);
    expect(mockedApiPost).toHaveBeenCalledWith('/v1/admin/tenants/acme/enter', {});
  });

  it('propagates errors from apiPost', async () => {
    mockedApiPost.mockRejectedValueOnce(new Error('boom'));
    await expect(enterTenantSession('acme')).rejects.toThrow('boom');
  });
});

describe('exitTenantSession', () => {
  it('POSTs the exit endpoint with an empty body', async () => {
    mockedApiPost.mockResolvedValueOnce({ active: false });
    const out = await exitTenantSession();
    expect(mockedApiPost).toHaveBeenCalledWith('/v1/admin/tenants/exit', {});
    expect(out).toEqual({ active: false });
  });
});

describe('adminTenantToScope', () => {
  it('translates a tenant payload into a Scope and defaults plan to "Team"', () => {
    const scope = adminTenantToScope({
      id: 't1',
      name: 'Acme',
      slug: 'acme',
      avatar_url: 'https://cdn/x.png',
      created_at: '2026-01-01T00:00:00Z',
    });
    expect(scope).toEqual({
      id: 't1',
      type: 'team',
      name: 'Acme',
      slug: 'acme',
      plan: 'Team',
      avatarUrl: 'https://cdn/x.png',
    });
  });

  it('drops null avatar_url to undefined', () => {
    const scope = adminTenantToScope({
      id: 't1',
      name: 'A',
      slug: 'a',
      avatar_url: null,
      created_at: '2026-01-01T00:00:00Z',
    });
    expect(scope.avatarUrl).toBeUndefined();
  });
});

describe('activeSessionToScope', () => {
  it('returns null when no session is active', () => {
    expect(activeSessionToScope({ active: false })).toBeNull();
    expect(activeSessionToScope(null)).toBeNull();
    expect(activeSessionToScope(undefined)).toBeNull();
  });

  it('returns null when active=true but tenant is missing', () => {
    expect(activeSessionToScope({ active: true })).toBeNull();
  });

  it('returns a Scope when active and tenant present', () => {
    const scope = activeSessionToScope({
      active: true,
      tenant: {
        id: 't1',
        name: 'Acme',
        slug: 'acme',
        created_at: '2026-01-01T00:00:00Z',
      },
    });
    expect(scope).not.toBeNull();
    expect(scope!.slug).toBe('acme');
    expect(scope!.type).toBe('team');
  });
});

describe('formatTimeRemaining', () => {
  const now = new Date('2026-05-02T12:00:00Z');

  it('returns empty string for nullish input', () => {
    expect(formatTimeRemaining(null, now)).toBe('');
    expect(formatTimeRemaining(undefined, now)).toBe('');
  });

  it('returns empty string for unparseable input', () => {
    expect(formatTimeRemaining('not-a-date', now)).toBe('');
  });

  it('returns "expired" when the timestamp is in the past', () => {
    expect(formatTimeRemaining('2026-05-02T11:00:00Z', now)).toBe('expired');
  });

  it('returns minute-granularity for sub-hour windows', () => {
    expect(formatTimeRemaining('2026-05-02T12:30:00Z', now)).toBe('30 minutes remaining');
    expect(formatTimeRemaining('2026-05-02T12:01:00Z', now)).toBe('1 minute remaining');
  });

  it('returns hour granularity on whole hours', () => {
    expect(formatTimeRemaining('2026-05-02T14:00:00Z', now)).toBe('2 hours remaining');
  });

  it('returns mixed h/m for partial hours', () => {
    expect(formatTimeRemaining('2026-05-02T15:30:00Z', now)).toBe('3h 30m remaining');
  });

  it('handles less than a minute remaining', () => {
    expect(formatTimeRemaining('2026-05-02T12:00:30Z', now)).toBe('less than 1 minute remaining');
  });
});
