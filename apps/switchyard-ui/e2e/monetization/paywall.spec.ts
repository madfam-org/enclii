import { test, expect } from '@playwright/test';
import { setupApiMocking, setupOidcSession, waitForAppReady } from '../fixtures';

/**
 * Paywall E2E Tests — "Operation Golden Key"
 *
 * Validates that the monetization paywall (requireTier + PricingModal) is correctly wired:
 * - Free/community users hitting service limits see the PricingModal
 * - Pro users (including legacy 'sovereign' tier name) can proceed without being blocked
 * - The modal checkout URL points to Dhanam with correct params (plan=enclii_pro&product=enclii)
 */

// Fake JWT token (just needs valid base64 structure for parseJwt)
function makeFakeJwt(claims: Record<string, unknown>): string {
  const header = btoa(JSON.stringify({ alg: 'RS256', typ: 'JWT' }));
  const payload = btoa(JSON.stringify({ sub: 'test-user-id', exp: Math.floor(Date.now() / 1000) + 3600, ...claims }));
  const sig = 'fakesig';
  return `${header}.${payload}.${sig}`;
}

const MOCK_PROJECT = {
  id: 'proj-001',
  name: 'Test Project',
  slug: 'test-project',
  status: 'active',
  created_at: new Date().toISOString(),
  updated_at: new Date().toISOString(),
};

// 4 services — exceeds community tier limit of 3
const MOCK_SERVICES = Array.from({ length: 4 }, (_, i) => ({
  id: `svc-${i + 1}`,
  name: `service-${i + 1}`,
  status: 'healthy',
  created_at: new Date().toISOString(),
}));

function injectAuth(tier: string | null) {
  const user = {
    id: 'test-user-id',
    email: 'test@madfam.io',
    name: 'Test User',
    roles: ['developer'],
    foundry_tier: tier,
  };
  const tokens = {
    accessToken: makeFakeJwt({ foundry_tier: tier }),
    refreshToken: 'fake-refresh',
    expiresAt: Date.now() + 3600000,
  };
  return { user, tokens };
}

const apiMocks = {
  '/v1/projects': { projects: [MOCK_PROJECT], count: 1 },
  '/v1/projects/test-project/services': { services: MOCK_SERVICES, count: MOCK_SERVICES.length },
  '/v1/health': { status: 'healthy', version: 'test' },
  '/v1/auth/silent-check': { error: 'oidc_not_enabled', message: 'OIDC authentication is not enabled' },
  '/v1/dashboard/stats': {
    stats: { totalProjects: 1, totalServices: 4, activeDeployments: 0, healthyServices: 4 },
    activities: [],
    services: [],
  },
};

test.describe('Paywall — requireTier + PricingModal', () => {
  test('community user blocked from creating service when at limit', async ({ page }) => {
    await setupApiMocking(page, apiMocks);

    const { user, tokens } = injectAuth('community');

    await setupOidcSession(page, user, tokens);

    await page.goto('/services/new');
    await waitForAppReady(page);

    // Fill minimum form fields
    await page.fill('#serviceName', 'my-new-service');
    // Wait for projects to load and auto-select
    await page.waitForSelector('#project option:not([value=""])', { state: 'attached' });

    // Submit form
    await page.click('button[type="submit"]');

    // PricingModal should appear
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible({ timeout: 5000 });
    await expect(dialog).toContainText('Deploy More Services');
  });

  test('pro user can submit without paywall', async ({ page }) => {
    // For pro tier, service limit is -1 (unlimited), so no block
    await setupApiMocking(page, {
      ...apiMocks,
      // Mock the POST to succeed
      '/v1/projects/test-project/services': { services: MOCK_SERVICES, count: MOCK_SERVICES.length },
    });

    // Also intercept the POST request for service creation
    await page.route('**/v1/projects/test-project/services', async (route) => {
      if (route.request().method() === 'POST') {
        await route.fulfill({
          status: 201,
          contentType: 'application/json',
          body: JSON.stringify({ id: 'svc-new', name: 'my-new-service', status: 'pending' }),
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ services: MOCK_SERVICES, count: MOCK_SERVICES.length }),
        });
      }
    });

    const { user, tokens } = injectAuth('sovereign'); // Legacy tier name still works

    await setupOidcSession(page, user, tokens);

    await page.goto('/services/new');
    await waitForAppReady(page);

    await page.fill('#serviceName', 'my-new-service');
    await page.waitForSelector('#project option:not([value=""])', { state: 'attached' });

    await page.click('button[type="submit"]');

    // PricingModal should NOT appear — form submits and navigates
    const dialog = page.getByRole('dialog');
    await expect(dialog).not.toBeVisible({ timeout: 3000 });
  });

  test('PricingModal shows correct Dhanam checkout URL', async ({ page }) => {
    await setupApiMocking(page, apiMocks);

    const { user, tokens } = injectAuth('community');

    await setupOidcSession(page, user, tokens);

    await page.goto('/services/new');
    await waitForAppReady(page);

    await page.fill('#serviceName', 'my-new-service');
    await page.waitForSelector('#project option:not([value=""])', { state: 'attached' });
    await page.click('button[type="submit"]');

    // Wait for modal
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible({ timeout: 5000 });

    // Find the Pro tier's CTA link
    const proLink = dialog.locator('a').filter({ hasText: 'Upgrade to Pro' });
    const href = await proLink.getAttribute('href');

    // Should point to Dhanam checkout with correct params
    expect(href).toContain('plan=enclii_pro');
    expect(href).toContain('product=enclii');
    expect(href).toContain('user_id=test-user-id');
    expect(href).toContain('return_url=');
  });
});
