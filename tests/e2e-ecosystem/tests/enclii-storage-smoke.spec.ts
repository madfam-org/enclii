import { test, expect } from '@playwright/test';

/**
 * Persistent volume / service storage tests (Commercial GA bet C).
 *
 * Smoke: service settings and PATCH routes require authentication.
 * Opt-in: round-trip volumes on a service via STORAGE_E2E_* env vars.
 */

const API_BASE_URL = process.env.API_BASE_URL || 'https://api.enclii.dev';
const STORAGE_E2E_TOKEN = process.env.STORAGE_E2E_TOKEN;
const STORAGE_E2E_SERVICE_ID = process.env.STORAGE_E2E_SERVICE_ID;

const fakeServiceId = '00000000-0000-4000-8000-000000000002';

function authHeaders(token: string) {
  return {
    Authorization: `Bearer ${token}`,
    'Content-Type': 'application/json',
  };
}

test.describe('Service storage API smoke (always-on)', () => {
  test('GET /v1/services/:id/settings requires authentication', async ({ request }) => {
    const response = await request.get(
      `${API_BASE_URL}/v1/services/${fakeServiceId}/settings`,
      { failOnStatusCode: false },
    );

    expect(response.status()).not.toBe(502);
    expect(response.status()).not.toBe(503);
    expect([401, 403, 404]).toContain(response.status());
  });

  test('PATCH /v1/services/:id requires authentication', async ({ request }) => {
    const response = await request.patch(`${API_BASE_URL}/v1/services/${fakeServiceId}`, {
      failOnStatusCode: false,
      data: { volumes: [] },
    });

    expect(response.status()).not.toBe(502);
    expect(response.status()).not.toBe(503);
    expect([401, 403, 404]).toContain(response.status());
  });
});

test.describe('Service volumes round-trip (staging opt-in)', () => {
  test.skip(!STORAGE_E2E_TOKEN || !STORAGE_E2E_SERVICE_ID, 'Set STORAGE_E2E_TOKEN and STORAGE_E2E_SERVICE_ID');

  test('patch volumes and read back via settings', async ({ request }) => {
    const token = STORAGE_E2E_TOKEN!;
    const serviceId = STORAGE_E2E_SERVICE_ID!;
    const headers = authHeaders(token);

    const volumes = [
      {
        name: 'e2e-data',
        mount_path: '/data/e2e',
        size: '1Gi',
        storage_class_name: 'longhorn',
        access_mode: 'ReadWriteOnce',
      },
    ];

    const patchResponse = await request.patch(`${API_BASE_URL}/v1/services/${serviceId}`, {
      headers,
      data: { volumes },
    });
    expect(patchResponse.status()).toBeLessThan(500);
    expect([200, 204]).toContain(patchResponse.status());

    const settingsResponse = await request.get(
      `${API_BASE_URL}/v1/services/${serviceId}/settings`,
      { headers },
    );
    expect(settingsResponse.status()).toBe(200);

    const body = await settingsResponse.json();
    const saved = body.settings?.volumes ?? [];
    expect(Array.isArray(saved)).toBeTruthy();
    expect(saved.some((v: { name?: string }) => v.name === 'e2e-data')).toBeTruthy();

    await request.patch(`${API_BASE_URL}/v1/services/${serviceId}`, {
      headers,
      data: { volumes: [] },
    });
  });
});
