import { test, expect, type Route } from '@playwright/test';
import { setupApiMocking, setupOidcSession, waitForAppReady } from '../fixtures';

/**
 * P2.1 in-UI log tail E2E.
 *
 * Single spec file covering the user-visible contract:
 *   1. Page loads with a historical window rendered.
 *   2. Toggling live tail opens a WebSocket and new entries appear
 *      when the mock WS pushes them.
 *   3. Level filter chips narrow the result set via a re-fetch.
 *
 * Uses the same Playwright route-mocking pattern as the audit spec.
 * WebSocket is intercepted with `page.routeWebSocket` (Playwright 1.48+).
 */

const PROJECT_SLUG = 'madfam';
const SERVICE_ID = '11111111-1111-1111-1111-111111111111';

function authedLocalStorage() {
  const user = {
    id: 'test-dev-id',
    email: 'dev@madfam.io',
    name: 'Test Dev',
    roles: ['developer'],
  };
  // Fake JWT — the UI only decodes for display, route-mocking handles auth.
  const header = btoa(JSON.stringify({ alg: 'RS256', typ: 'JWT' }));
  const payload = btoa(
    JSON.stringify({
      sub: user.id,
      exp: Math.floor(Date.now() / 1000) + 3600,
      roles: user.roles,
    }),
  );
  const tokens = {
    accessToken: `${header}.${payload}.fakesig`,
    refreshToken: 'fake-refresh',
    expiresAt: Date.now() + 3600000,
  };
  return { user, tokens };
}

// Historical entries returned by the mocked GET /v1/services/:id/logs.
// We mix levels so the level-filter test can assert narrowing.
const FAKE_ENTRIES = [
  {
    timestamp: '2026-04-17T12:00:00.000000000Z',
    level: 'info',
    pod: 'karafiel-api-abc',
    message: 'server listening on :8080',
  },
  {
    timestamp: '2026-04-17T12:00:01.000000000Z',
    level: 'warn',
    pod: 'karafiel-api-abc',
    message: 'slow query detected (1200ms)',
  },
  {
    timestamp: '2026-04-17T12:00:02.000000000Z',
    level: 'error',
    pod: 'karafiel-api-abc',
    message: 'request timeout: context deadline exceeded',
  },
  {
    timestamp: '2026-04-17T12:00:03.000000000Z',
    level: 'info',
    pod: 'karafiel-api-def',
    message: 'cache hit ratio 94.2%',
  },
];

test.describe('Service logs page (P2.1)', () => {
  test.beforeEach(async ({ page }) => {
    await setupApiMocking(page);

    // Service-by-id lookup for header hydration.
    await page.route(`**/v1/services/${SERVICE_ID}`, async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: SERVICE_ID,
          name: 'karafiel-api',
          project_id: 'proj-1',
          project_name: 'Karafiel',
          project_slug: PROJECT_SLUG,
          environment: 'production',
        }),
      });
    });

    // Historical log window — filter by `level` when present so we can
    // assert the narrowing round-trip.
    await page.route(
      `**/v1/services/${SERVICE_ID}/logs?*`,
      async (route: Route) => {
        const url = new URL(route.request().url());
        const levels = url.searchParams.getAll('level');
        let entries = [...FAKE_ENTRIES];
        if (levels.length > 0) {
          entries = entries.filter((e) => levels.includes(e.level));
        }
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            entries,
            reached_live_tail: true,
          }),
        });
      },
    );

    const { user, tokens } = authedLocalStorage();
    await setupOidcSession(page, user, tokens);
  });

  test('renders history, toggles live tail, and narrows by level filter', async ({
    page,
  }) => {
    // Live tail toggle: the viewer opens a WS in live mode. Register the
    // route before navigation so we catch the initial connection as well as
    // any reconnect caused by later interactions.
    await page.routeWebSocket(
      new RegExp(`/v1/services/${SERVICE_ID}/logs/tail`),
      (ws) => {
        let sends = 0;
        const timer = setInterval(() => {
          sends += 1;
          ws.send(
            JSON.stringify({
              type: 'entry',
              entry: {
                timestamp: new Date().toISOString(),
                level: 'info',
                pod: 'karafiel-api-abc',
                message: 'live-tail hello',
              },
            }),
          );
          if (sends >= 5) {
            clearInterval(timer);
          }
        }, 300);
      },
    );

    await page.goto(
      `/projects/${PROJECT_SLUG}/services/${SERVICE_ID}/logs`,
    );
    await waitForAppReady(page);

    // 1. Four historical entries render (one per line). Use `role="log"`
    //    which the viewer exposes for a11y — same selector the screen
    //    reader will use.
    const logRegion = page.getByRole('log');
    await expect(logRegion).toBeVisible({ timeout: 10_000 });
    await expect(logRegion).toContainText('server listening on :8080');
    await expect(logRegion).toContainText('request timeout');
    await expect(logRegion).toContainText('cache hit ratio');

    // Level badge on the error row is styled red — we don't care about
    // the exact class, just that the text appears.
    await expect(logRegion).toContainText('ERROR');

    // 2. Toggle "Error" level → only the error line remains after re-fetch.
    await page.getByLabel('error logs').check();
    // Re-fetch happens via useEffect on `levels` change. Give it a beat.
    await expect(logRegion).toContainText('request timeout', { timeout: 5000 });
    await expect(logRegion).not.toContainText('server listening');

    // Uncheck to restore, then assert all 4 lines are back.
    await page.getByLabel('error logs').uncheck();
    await expect(logRegion).toContainText('server listening', { timeout: 5000 });

    // 3. Ensure live is on (default when time range = live).
    const liveButton = page.getByRole('button', { name: /live|paused/i });
    // If toggled off by the earlier filter interaction, re-enable it.
    const label = await liveButton.textContent();
    if (label && /paused/i.test(label)) {
      await liveButton.click();
    }

    // Assert the WS-pushed entry appears.
    await expect(logRegion).toContainText('live-tail hello', { timeout: 10_000 });
  });
});
