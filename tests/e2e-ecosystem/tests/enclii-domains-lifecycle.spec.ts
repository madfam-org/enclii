import { test, expect } from '@playwright/test';

/**
 * Custom domain lifecycle tests (Commercial GA bet B).
 *
 * Smoke tests run in blocking CI: routes exist and reject unauthenticated access.
 *
 * Full add → get → verify → delete runs only when staging credentials are set:
 *   DOMAIN_E2E_TOKEN, DOMAIN_E2E_SERVICE_ID, DOMAIN_E2E_ENVIRONMENT_ID
 * Optional: DOMAIN_E2E_DOMAIN (FQDN you control); otherwise uses platform subdomain.
 */

const API_BASE_URL = process.env.API_BASE_URL || 'https://api.enclii.dev';
const DOMAIN_E2E_TOKEN = process.env.DOMAIN_E2E_TOKEN;
const DOMAIN_E2E_SERVICE_ID = process.env.DOMAIN_E2E_SERVICE_ID;
const DOMAIN_E2E_ENVIRONMENT_ID = process.env.DOMAIN_E2E_ENVIRONMENT_ID;
const DOMAIN_E2E_DOMAIN = process.env.DOMAIN_E2E_DOMAIN;

const fakeServiceId = '00000000-0000-4000-8000-000000000002';
const fakeDomainId = '00000000-0000-4000-8000-000000000003';

function authHeaders(token: string) {
  return {
    Authorization: `Bearer ${token}`,
    'Content-Type': 'application/json',
  };
}

test.describe('Domains API smoke (always-on)', () => {
  test('GET /v1/domains requires authentication', async ({ request }) => {
    const response = await request.get(`${API_BASE_URL}/v1/domains`, {
      failOnStatusCode: false,
    });

    expect(response.status()).not.toBe(502);
    expect(response.status()).not.toBe(503);
    expect([401, 403, 404]).toContain(response.status());
  });

  test('GET /v1/domains/stats requires authentication', async ({ request }) => {
    const response = await request.get(`${API_BASE_URL}/v1/domains/stats`, {
      failOnStatusCode: false,
    });

    expect(response.status()).not.toBe(502);
    expect(response.status()).not.toBe(503);
    expect([401, 403, 404]).toContain(response.status());
  });

  test('GET /v1/services/:id/domains requires authentication', async ({ request }) => {
    const response = await request.get(
      `${API_BASE_URL}/v1/services/${fakeServiceId}/domains`,
      { failOnStatusCode: false },
    );

    expect(response.status()).not.toBe(502);
    expect(response.status()).not.toBe(503);
    expect([401, 403, 404]).toContain(response.status());
  });

  test('GET /v1/services/:id/domains/:domain_id requires authentication', async ({
    request,
  }) => {
    const response = await request.get(
      `${API_BASE_URL}/v1/services/${fakeServiceId}/domains/${fakeDomainId}`,
      { failOnStatusCode: false },
    );

    expect(response.status()).not.toBe(502);
    expect(response.status()).not.toBe(503);
    expect([401, 403, 404]).toContain(response.status());
  });

  test('POST /v1/services/:id/domains requires authentication', async ({ request }) => {
    const response = await request.post(
      `${API_BASE_URL}/v1/services/${fakeServiceId}/domains`,
      {
        failOnStatusCode: false,
        data: {
          environment_id: fakeDomainId,
          domain: 'smoke.example.com',
        },
      },
    );

    expect(response.status()).not.toBe(502);
    expect(response.status()).not.toBe(503);
    expect([401, 403, 404]).toContain(response.status());
  });
});

test.describe('Domains lifecycle (staging opt-in)', () => {
  test.skip(
    !DOMAIN_E2E_TOKEN || !DOMAIN_E2E_SERVICE_ID || !DOMAIN_E2E_ENVIRONMENT_ID,
    'Set DOMAIN_E2E_TOKEN, DOMAIN_E2E_SERVICE_ID, and DOMAIN_E2E_ENVIRONMENT_ID',
  );

  test('add → get → verify → delete custom domain', async ({ request }) => {
    const token = DOMAIN_E2E_TOKEN!;
    const serviceId = DOMAIN_E2E_SERVICE_ID!;
    const environmentId = DOMAIN_E2E_ENVIRONMENT_ID!;
    const headers = authHeaders(token);

    const addBody = DOMAIN_E2E_DOMAIN
      ? {
          domain: DOMAIN_E2E_DOMAIN,
          environment_id: environmentId,
          tls_provider: 'cert-manager',
        }
      : {
          environment_id: environmentId,
          is_platform_domain: true,
          tls_provider: 'cert-manager',
        };

    const addResponse = await request.post(
      `${API_BASE_URL}/v1/services/${serviceId}/domains`,
      { headers, data: addBody },
    );

    expect(addResponse.status()).toBeLessThan(500);
    expect([200, 201]).toContain(addResponse.status());

    const addJson = await addResponse.json();
    const domain = addJson.domain ?? addJson;
    expect(domain).toHaveProperty('id');
    expect(domain).toHaveProperty('domain');
    const domainId = domain.id as string;
    const fqdn = domain.domain as string;
    expect(fqdn.length).toBeGreaterThan(0);

    const getResponse = await request.get(
      `${API_BASE_URL}/v1/services/${serviceId}/domains/${domainId}`,
      { headers },
    );
    expect(getResponse.status()).toBe(200);

    const verifyResponse = await request.post(
      `${API_BASE_URL}/v1/services/${serviceId}/domains/${domainId}/verify`,
      { headers, data: {} },
    );
    expect(verifyResponse.status()).toBeLessThan(500);
    expect(verifyResponse.status()).not.toBe(502);
    expect(verifyResponse.status()).not.toBe(503);

    const verifyJson = await verifyResponse.json();
    expect(verifyJson).toHaveProperty('verified');
    expect(typeof verifyJson.verified).toBe('boolean');

    const deleteResponse = await request.delete(
      `${API_BASE_URL}/v1/services/${serviceId}/domains/${domainId}`,
      { headers },
    );
    expect(deleteResponse.status()).toBeLessThan(500);
    expect([200, 204, 404]).toContain(deleteResponse.status());
  });
});
