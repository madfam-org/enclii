import { test, expect } from '@playwright/test';

/**
 * Self-serve signup API smoke (Commercial GA GTM).
 *
 * Always-on: routes respond without 502/503 (404 when signup disabled is OK).
 * Opt-in: full wizard when SIGNUP_E2E_RUN=1 (manual; uses real email flow).
 */

const API_BASE_URL = process.env.API_BASE_URL || 'https://api.enclii.dev';
const APP_BASE_URL = process.env.APP_BASE_URL || 'https://app.enclii.dev';
const fakeSignupId = '00000000-0000-4000-8000-000000000099';

test.describe('Signup API smoke (always-on)', () => {
  test('POST /v1/signup responds without gateway errors', async ({ request }) => {
    const response = await request.post(`${API_BASE_URL}/v1/signup`, {
      failOnStatusCode: false,
      data: { email: 'e2e-smoke@example.com', company_name: 'E2E Smoke' },
    });

    expect(response.status()).not.toBe(502);
    expect(response.status()).not.toBe(503);
    // 201 when enabled; 404 when ENCLII_SIGNUP_ENABLED=false; 400 on validation edge cases
    expect([201, 400, 404, 409, 429]).toContain(response.status());
  });

  test('GET /v1/signups/:id/status responds without gateway errors', async ({ request }) => {
    const response = await request.get(`${API_BASE_URL}/v1/signup/${fakeSignupId}/status`, {
      failOnStatusCode: false,
    });

    expect(response.status()).not.toBe(502);
    expect(response.status()).not.toBe(503);
    expect([200, 404, 400]).toContain(response.status());
  });

  test('GET /signup page loads without auth redirect loop', async ({ page }) => {
    const response = await page.goto(`${APP_BASE_URL}/signup`, {
      waitUntil: 'domcontentloaded',
    });

    expect(response?.status()).toBeLessThan(500);
    await expect(page).not.toHaveURL(/\/login\?.*error/);
    await expect(page.locator('body')).toBeVisible();
  });
});

test.describe('Signup wizard (staging opt-in)', () => {
  test.skip(process.env.SIGNUP_E2E_RUN !== '1', 'Set SIGNUP_E2E_RUN=1 for manual full signup proof');

  test('signup page shows email step', async ({ page }) => {
    await page.goto(`${APP_BASE_URL}/signup`);
    await expect(page.getByRole('heading', { name: /sign up|create.*account/i })).toBeVisible({
      timeout: 15000,
    });
  });
});
