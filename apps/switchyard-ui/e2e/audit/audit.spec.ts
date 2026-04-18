import { test, expect, Route } from '@playwright/test';
import { setupApiMocking, waitForAppReady } from '../fixtures';

/**
 * Consolidated audit log E2E test (P1.5).
 *
 * Focuses on the three user-visible contracts:
 *   1. Rows render, ordered newest-first across sources.
 *   2. Filtering by category narrows the result set.
 *   3. Clicking a row opens the drawer with the event's Details payload.
 */

function makeFakeJwt(claims: Record<string, unknown>): string {
  const header = btoa(JSON.stringify({ alg: 'RS256', typ: 'JWT' }));
  const payload = btoa(
    JSON.stringify({
      sub: 'test-admin-id',
      exp: Math.floor(Date.now() / 1000) + 3600,
      ...claims,
    }),
  );
  return `${header}.${payload}.fakesig`;
}

function adminAuth() {
  const user = {
    id: 'test-admin-id',
    email: 'admin@madfam.io',
    name: 'Test Admin',
    roles: ['admin'],
  };
  const tokens = {
    accessToken: makeFakeJwt({ roles: ['admin'] }),
    refreshToken: 'fake-refresh',
    expiresAt: Date.now() + 3600000,
  };
  return { user, tokens };
}

// Realistic event payload — two auth events (from Janua), two deploy
// events (from Switchyard), one secret event (Selva). The Playwright
// route matcher returns everything or filters by ?category=… so we can
// assert that the filter round-trip actually narrows the display.
const FAKE_EVENTS = [
  {
    timestamp: '2026-04-15T12:05:00Z',
    actor: 'sub-alice',
    actor_email: 'alice@madfam.io',
    source: 'janua',
    category: 'auth',
    action: 'login',
    target: '',
    outcome: 'success',
    details: { ip_address: '10.0.0.1' },
  },
  {
    timestamp: '2026-04-15T12:04:00Z',
    actor: 'sub-bob',
    actor_email: 'bob@madfam.io',
    source: 'janua',
    category: 'auth',
    action: 'logout',
    target: '',
    outcome: 'success',
    details: {},
  },
  {
    timestamp: '2026-04-15T12:03:00Z',
    actor: 'github-actions',
    source: 'switchyard',
    category: 'deploy',
    action: 'deploy_healthy',
    target: 'madfam-org/enclii@abc1234',
    outcome: 'success',
    details: { commit_sha: 'abc1234' },
  },
  {
    timestamp: '2026-04-15T12:02:00Z',
    actor: 'github-actions',
    source: 'switchyard',
    category: 'deploy',
    action: 'build_failed',
    target: 'madfam-org/tezca@def5678',
    outcome: 'failure',
    details: { commit_sha: 'def5678' },
  },
  {
    timestamp: '2026-04-15T12:01:00Z',
    actor: 'sub-alice',
    actor_email: 'alice@madfam.io',
    source: 'selva_secret',
    category: 'secret',
    action: 'write',
    target: 'prod/karafiel/karafiel-secrets:STRIPE_SECRET_KEY',
    outcome: 'success',
    details: {
      rationale: 'quarterly rotation',
      value_sha256_prefix: 'deadbeef',
      approval_chain: [{ user_sub: 'sub-bob' }],
    },
  },
];

async function setupAuditMock(page: any) {
  // The filter-aware intercept: we honor ?category= so the filter test
  // can verify the narrowing actually happens in the UI (via re-fetch)
  // rather than client-side-only.
  await page.route('**/v1/audit*', async (route: Route) => {
    const url = new URL(route.request().url());
    const category = url.searchParams.get('category');
    let events = [...FAKE_EVENTS];
    if (category) {
      events = events.filter((e) => e.category === category);
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ events, next_cursor: null }),
    });
  });
}

test.describe('Consolidated audit log (/audit)', () => {
  test.beforeEach(async ({ page }) => {
    await setupApiMocking(page);
    await setupAuditMock(page);
    const { user, tokens } = adminAuth();
    await page.addInitScript(
      ({ user, tokens }) => {
        localStorage.setItem('enclii_user', JSON.stringify(user));
        localStorage.setItem('enclii_tokens', JSON.stringify(tokens));
      },
      { user, tokens },
    );
  });

  test('renders merged rows and narrows by category filter, drawer shows details', async ({
    page,
  }) => {
    await page.goto('/audit');
    await waitForAppReady(page);

    // 1. All 5 rows visible at first load.
    const rows = page.locator('[data-testid="audit-row"]');
    await expect(rows).toHaveCount(5, { timeout: 10_000 });

    // Newest first: first row should be the 12:05 login.
    await expect(rows.first()).toContainText('login');
    await expect(rows.first()).toContainText('alice@madfam.io');

    // 2. Filter by category=secret → only 1 row remains (the Selva secret write).
    await page.selectOption('select:near(:text("Category"))', 'secret');
    await expect(rows).toHaveCount(1, { timeout: 10_000 });
    await expect(rows.first()).toContainText('write');
    await expect(rows.first()).toContainText('selva_secret');

    // 3. Click the row → drawer opens and shows the Details JSON payload.
    await rows.first().click();
    const drawer = page.getByRole('dialog');
    await expect(drawer).toBeVisible();
    await expect(drawer).toContainText('write'); // action is the drawer title
    await expect(drawer).toContainText('quarterly rotation'); // rationale from details
    await expect(drawer).toContainText('deadbeef'); // hash prefix from details
  });
});
