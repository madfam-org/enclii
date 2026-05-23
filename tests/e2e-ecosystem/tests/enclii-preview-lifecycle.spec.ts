import { test, expect } from '@playwright/test';

/**
 * Preview environment lifecycle tests (Commercial GA bet A).
 *
 * Smoke tests always run against production API — they verify routes exist and
 * reject unauthenticated access (never 502/503).
 *
 * Full create → get → close lifecycle runs only when staging credentials are
 * provided (PREVIEW_E2E_TOKEN + PREVIEW_E2E_SERVICE_ID). Wire those in a
 * manual/staging workflow or GitHub environment — not required for blocking CI.
 */

const API_BASE_URL = process.env.API_BASE_URL || 'https://api.enclii.dev';
const PREVIEW_E2E_TOKEN = process.env.PREVIEW_E2E_TOKEN;
const PREVIEW_E2E_SERVICE_ID = process.env.PREVIEW_E2E_SERVICE_ID;

const fakePreviewId = '00000000-0000-4000-8000-000000000001';
const fakeServiceId = '00000000-0000-4000-8000-000000000002';

function authHeaders(token: string) {
  return {
    Authorization: `Bearer ${token}`,
    'Content-Type': 'application/json',
  };
}

test.describe('Preview API smoke (always-on)', () => {
  test('GET /v1/previews/:id requires authentication', async ({ request }) => {
    const response = await request.get(`${API_BASE_URL}/v1/previews/${fakePreviewId}`, {
      failOnStatusCode: false,
    });

    expect(response.status()).not.toBe(502);
    expect(response.status()).not.toBe(503);
    expect([401, 403, 404]).toContain(response.status());
  });

  test('GET /v1/services/:id/previews requires authentication', async ({ request }) => {
    const response = await request.get(
      `${API_BASE_URL}/v1/services/${fakeServiceId}/previews`,
      { failOnStatusCode: false },
    );

    expect(response.status()).not.toBe(502);
    expect(response.status()).not.toBe(503);
    expect([401, 403, 404]).toContain(response.status());
  });

  test('POST /v1/previews requires authentication', async ({ request }) => {
    const response = await request.post(`${API_BASE_URL}/v1/previews`, {
      failOnStatusCode: false,
      data: {
        service_id: fakeServiceId,
        pr_number: 1,
        pr_branch: 'feature/smoke',
        commit_sha: 'abc1234567890abcdef1234567890abcdef1234',
      },
    });

    expect(response.status()).not.toBe(502);
    expect(response.status()).not.toBe(503);
    expect([401, 403, 404]).toContain(response.status());
  });
});

test.describe('Preview lifecycle (staging opt-in)', () => {
  test.skip(
    !PREVIEW_E2E_TOKEN || !PREVIEW_E2E_SERVICE_ID,
    'Set PREVIEW_E2E_TOKEN and PREVIEW_E2E_SERVICE_ID for full lifecycle proof',
  );

  test('create → get → close preview (TC-01 API slice)', async ({ request }) => {
    const token = PREVIEW_E2E_TOKEN!;
    const serviceId = PREVIEW_E2E_SERVICE_ID!;
    const prNumber = 900_000 + Math.floor(Date.now() / 1000) % 100_000;
    const commitSha = `e2e${Date.now().toString(16).padStart(40, '0')}`.slice(0, 40);

    const createResponse = await request.post(`${API_BASE_URL}/v1/previews`, {
      headers: authHeaders(token),
      data: {
        service_id: serviceId,
        pr_number: prNumber,
        pr_title: 'E2E preview lifecycle',
        pr_branch: `e2e/preview-${prNumber}`,
        pr_base_branch: 'main',
        commit_sha: commitSha,
      },
    });

    expect(createResponse.status()).toBeLessThan(500);
    expect([200, 201]).toContain(createResponse.status());

    const createBody = await createResponse.json();
    const preview = createBody.preview ?? createBody;
    expect(preview).toHaveProperty('id');
    expect(preview).toHaveProperty('preview_url');
    expect(String(preview.preview_url)).toMatch(/^https:\/\//);

    const previewId = preview.id as string;

    const getResponse = await request.get(`${API_BASE_URL}/v1/previews/${previewId}`, {
      headers: authHeaders(token),
    });
    expect(getResponse.status()).toBe(200);

    const getBody = await getResponse.json();
    expect(getBody.preview?.id ?? getBody.id).toBe(previewId);

    const closeResponse = await request.post(`${API_BASE_URL}/v1/previews/${previewId}/close`, {
      headers: authHeaders(token),
      data: {},
    });
    expect(closeResponse.status()).toBe(200);

    const closedGet = await request.get(`${API_BASE_URL}/v1/previews/${previewId}`, {
      headers: authHeaders(token),
    });
    expect(closedGet.status()).toBe(200);
    const closedBody = await closedGet.json();
    const closedPreview = closedBody.preview ?? closedBody;
    expect(closedPreview.status).toBe('closed');
  });
});
