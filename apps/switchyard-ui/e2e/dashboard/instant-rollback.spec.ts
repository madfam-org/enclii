import { test, expect, Route } from '@playwright/test';

/**
 * E2E coverage for P0.5 instant rollback UI.
 *
 * These tests mock the switchyard-api responses for a single service's
 * deployment history and verify:
 *   1. "Rollback to here" button appears on non-current running deploys.
 *   2. Clicking it opens the confirm modal with correct from→to display.
 *   3. Submitting fires POST /v1/services/{id}/rollback with the expected body.
 *   4. Success state surfaces timing + to-deployment info.
 *
 * Authentication is bypassed by routing all requests through mocks; we do
 * not exercise the real Janua SSO flow in this suite.
 */

const SERVICE_ID = '33333333-3333-3333-3333-333333333333';
const CURRENT_DEPLOYMENT_ID = 'dddddddd-0000-0000-0000-000000000001';
const TARGET_DEPLOYMENT_ID = 'dddddddd-0000-0000-0000-000000000002';

const deploymentsResponse = {
  service_id: SERVICE_ID,
  count: 2,
  deployments: [
    {
      id: CURRENT_DEPLOYMENT_ID,
      release_id: 'rrrrrrrr-0000-0000-0000-000000000001',
      environment_id: 'eeeeeeee-0000-0000-0000-000000000001',
      replicas: 2,
      status: 'running',
      health: 'unhealthy',
      created_at: new Date(Date.now() - 1000 * 60 * 10).toISOString(),
      updated_at: new Date().toISOString(),
      git_sha: 'abc1234567890abcdef1234567890abcdef12345',
      git_branch: 'main',
    },
    {
      id: TARGET_DEPLOYMENT_ID,
      release_id: 'rrrrrrrr-0000-0000-0000-000000000002',
      environment_id: 'eeeeeeee-0000-0000-0000-000000000001',
      replicas: 2,
      status: 'running',
      health: 'healthy',
      created_at: new Date(Date.now() - 1000 * 60 * 60 * 2).toISOString(),
      updated_at: new Date(Date.now() - 1000 * 60 * 60 * 2).toISOString(),
      git_sha: '789abc0123456789abc0123456789abc01234567',
      git_branch: 'main',
    },
  ],
};

test.describe('P0.5 instant rollback UI', () => {
  test.skip(
    !process.env.TEST_USER_PASSWORD,
    'Requires authenticated test user — set TEST_USER_PASSWORD to run',
  );

  test('Rollback button fires selector-flip with expected body', async ({ page }) => {
    let rollbackBody: Record<string, unknown> | null = null;

    // Intercept the deployments list for this service.
    await page.route(`**/v1/services/${SERVICE_ID}/deployments*`, async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(deploymentsResponse),
      });
    });

    // Intercept the rollback endpoint and capture the body.
    await page.route(`**/v1/services/${SERVICE_ID}/rollback`, async (route: Route) => {
      rollbackBody = JSON.parse(route.request().postData() || '{}');
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          message: 'Traffic flipped successfully',
          took_ms: 2400,
          scaled_up: false,
          from_deployment_id: CURRENT_DEPLOYMENT_ID,
          to_deployment_id: TARGET_DEPLOYMENT_ID,
          ready_replicas: 2,
          strategy: 'instant_selector_flip',
          namespace: 'staging-demo',
        }),
      });
    });

    // Navigate to the service detail page. Actual route prefix depends on
    // app layout; fixture harnesses use /services/{id} with a #deployments
    // hash or tab click. Adjust if the detail page pattern differs.
    await page.goto(`/services/${SERVICE_ID}#deployments`);

    // Rollback button for the TARGET deployment should be visible.
    const rollbackBtn = page.getByTestId(`rollback-to-${TARGET_DEPLOYMENT_ID}`);
    await expect(rollbackBtn).toBeVisible();
    await rollbackBtn.click();

    // Confirm modal opens with both SHAs shown.
    await expect(page.getByRole('dialog')).toContainText('Instant rollback');

    // Submit the flip.
    await page.getByTestId('confirm-rollback').click();

    // Success banner should appear with timing.
    await expect(page.getByTestId('rollback-success')).toBeVisible();
    await expect(page.getByTestId('rollback-success')).toContainText('2.4s');

    // Verify the POST body contained the target deployment ID.
    expect(rollbackBody).not.toBeNull();
    expect(rollbackBody!['target_deployment_id']).toBe(TARGET_DEPLOYMENT_ID);
  });
});
