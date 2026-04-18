/**
 * E2E tests for the P3.2 self-serve signup wizard.
 *
 * These tests mock the /v1/signup API responses so we can drive the UI
 * without a live backend. They cover the three happy-path stops plus a
 * 404 fallback for when the feature flag is off server-side.
 */

import { test, expect } from '@playwright/test';

const SIGNUP_ID = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa';

test.describe('Self-serve signup wizard', () => {
  test('step 1 → step 2: enter email, see check-email screen', async ({ page }) => {
    await page.route('**/v1/signup', async (route) => {
      if (route.request().method() !== 'POST') return route.continue();
      await route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({
          signup_id: SIGNUP_ID,
          email: 'new@example.com',
          status: 'pending_verification',
          next_step: 'verify_email',
        }),
      });
    });

    await page.route(`**/v1/signup/${SIGNUP_ID}/status`, async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          signup_id: SIGNUP_ID,
          email: 'new@example.com',
          status: 'pending_verification',
          next_step: 'verify_email',
        }),
      });
    });

    await page.goto('/signup');
    await expect(page.getByRole('heading', { name: /create your enclii account/i })).toBeVisible();

    await page.getByLabel(/work email/i).fill('new@example.com');
    await page.getByLabel(/i agree to the/i).check();
    await page.getByRole('button', { name: /continue/i }).click();

    await expect(page.getByRole('heading', { name: /check your email/i })).toBeVisible();
    await expect(page.getByText('new@example.com')).toBeVisible();
  });

  test('step 3: verified state shows Connect GitHub CTA', async ({ page }) => {
    await page.route(`**/v1/signup/${SIGNUP_ID}/status`, async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          signup_id: SIGNUP_ID,
          email: 'new@example.com',
          status: 'verified',
          next_step: 'connect_github',
        }),
      });
    });

    await page.goto(`/signup?signup_id=${SIGNUP_ID}`);
    await expect(page.getByRole('heading', { name: /connect github/i })).toBeVisible();
    await expect(page.getByRole('button', { name: /connect github/i })).toBeEnabled();
  });

  test('feature flag off: shows friendly not-available message', async ({ page }) => {
    await page.route('**/v1/signup', async (route) => {
      if (route.request().method() !== 'POST') return route.continue();
      await route.fulfill({
        status: 404,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'not found' }),
      });
    });

    await page.goto('/signup');
    await page.getByLabel(/work email/i).fill('new@example.com');
    await page.getByLabel(/i agree to the/i).check();
    await page.getByRole('button', { name: /continue/i }).click();

    await expect(page.getByText(/signup isn.t open yet/i)).toBeVisible();
  });

  test('oauth denied redirect shows retry prompt', async ({ page }) => {
    await page.route(`**/v1/signup/${SIGNUP_ID}/status`, async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          signup_id: SIGNUP_ID,
          email: 'new@example.com',
          status: 'verified',
          next_step: 'connect_github',
        }),
      });
    });

    await page.goto(`/signup?signup_id=${SIGNUP_ID}&error=oauth_denied`);
    await expect(page.getByText(/declined github access/i)).toBeVisible();
  });
});
