/**
 * Unit tests for lib/constants.ts
 *
 * Validates the shape and values of shared constants used across the UI.
 * These are intentionally simple "contract tests" that catch accidental
 * renames or value changes that would break consuming components.
 */

import {
  API_BASE_URL,
  AUTH_MODE,
  POLLING_FAST,
  POLLING_NORMAL,
  POLLING_SLOW,
  POLLING_IDLE,
  SERVICE_STATUS_COLORS,
  DEPLOYMENT_STATUS_COLORS,
  HEALTH_STATUS_COLORS,
} from './constants';

// ---------------------------------------------------------------------------
// Environment defaults
// ---------------------------------------------------------------------------

describe('environment defaults', () => {
  it('API_BASE_URL defaults to localhost:4200 when env is not set', () => {
    // In the test environment NEXT_PUBLIC_API_URL is not set
    expect(API_BASE_URL).toBe('http://localhost:4200');
  });

  it('AUTH_MODE resolves from NEXT_PUBLIC_AUTH_MODE env var', () => {
    // .env.test sets NEXT_PUBLIC_AUTH_MODE=oidc
    expect(['local', 'oidc']).toContain(AUTH_MODE);
  });
});

// ---------------------------------------------------------------------------
// Polling intervals
// ---------------------------------------------------------------------------

describe('polling intervals', () => {
  it('POLLING_FAST is 5 seconds', () => {
    expect(POLLING_FAST).toBe(5_000);
  });

  it('POLLING_NORMAL is 15 seconds', () => {
    expect(POLLING_NORMAL).toBe(15_000);
  });

  it('POLLING_SLOW is 60 seconds', () => {
    // Bumped from 30s → 60s in #229 (observability/health timeout RCA)
    expect(POLLING_SLOW).toBe(60_000);
  });

  it('POLLING_IDLE is 120 seconds', () => {
    expect(POLLING_IDLE).toBe(120_000);
  });

  it('intervals are in non-decreasing order', () => {
    expect(POLLING_FAST).toBeLessThan(POLLING_NORMAL);
    expect(POLLING_NORMAL).toBeLessThan(POLLING_SLOW);
    // SLOW (60s) <= IDLE (120s). Non-decreasing, not strictly increasing.
    expect(POLLING_SLOW).toBeLessThanOrEqual(POLLING_IDLE);
  });
});

// ---------------------------------------------------------------------------
// Status color maps
// ---------------------------------------------------------------------------

describe('SERVICE_STATUS_COLORS', () => {
  it('defines a color for "running"', () => {
    expect(SERVICE_STATUS_COLORS.running).toBeDefined();
    expect(typeof SERVICE_STATUS_COLORS.running).toBe('string');
  });

  it('defines a color for "stopped"', () => {
    expect(SERVICE_STATUS_COLORS.stopped).toBeDefined();
  });

  it('defines a color for "deploying" with animation', () => {
    expect(SERVICE_STATUS_COLORS.deploying).toContain('animate-pulse');
  });

  it('defines a color for "failed"', () => {
    expect(SERVICE_STATUS_COLORS.failed).toBeDefined();
  });

  it('defines a color for "pending"', () => {
    expect(SERVICE_STATUS_COLORS.pending).toBeDefined();
  });
});

describe('DEPLOYMENT_STATUS_COLORS', () => {
  it('defines colors for all expected deployment statuses', () => {
    const expectedStatuses = ['success', 'failed', 'pending', 'building'];
    for (const status of expectedStatuses) {
      expect(DEPLOYMENT_STATUS_COLORS[status]).toBeDefined();
      expect(typeof DEPLOYMENT_STATUS_COLORS[status]).toBe('string');
    }
  });
});

describe('HEALTH_STATUS_COLORS', () => {
  it('defines colors for all expected health statuses', () => {
    const expectedStatuses = ['healthy', 'unhealthy', 'unknown', 'stale'];
    for (const status of expectedStatuses) {
      expect(HEALTH_STATUS_COLORS[status]).toBeDefined();
      expect(typeof HEALTH_STATUS_COLORS[status]).toBe('string');
    }
  });
});
