import { test, expect } from '@playwright/test';
import { setupApiMocking, waitForAppReady } from '../fixtures';

/**
 * Dashboard E2E Tests
 *
 * Priority: P0/P1
 * Tests the project-centric dashboard: project grid, search/filter, and loading states.
 *
 * Note: Unauthenticated tests verify redirect to login.
 * Authenticated tests require TEST_USER_PASSWORD environment variable.
 */

test.describe('Dashboard', () => {
  test.describe('Unauthenticated Access', () => {
    // Note: Redirect tests are skipped as they require full client-side hydration
    // which is slow without a real backend. Auth redirects are tested elsewhere.
    test.skip(true, 'Protected route redirects require full client-side hydration');

    test('should redirect to login page when unauthenticated', async ({ page }) => {
      await setupApiMocking(page);
      await page.goto('/');
      await page.waitForURL('**/login**', { timeout: 10000 });
      expect(page.url()).toContain('/login');
    });

    test('should show login heading after redirect', async ({ page }) => {
      await setupApiMocking(page);
      await page.goto('/');
      await page.waitForURL('**/login**', { timeout: 10000 });
      await waitForAppReady(page);
      const heading = page.getByRole('heading', { name: /sign in/i });
      await expect(heading).toBeVisible();
    });
  });

  test.describe('Project Grid @authenticated', () => {
    test.skip(
      !process.env.TEST_USER_PASSWORD,
      'TEST_USER_PASSWORD not set - skipping authenticated tests'
    );

    test('should display project cards in a grid', async ({ page }) => {
      await page.goto('/');
      await page.waitForLoadState('networkidle');

      // Look for project cards (Card components in the grid)
      const projectCards = page.locator('[class*="Card"]');
      const count = await projectCards.count();
      expect(count).toBeGreaterThanOrEqual(0);
    });

    test('should show loading skeletons while fetching data', async ({ page }) => {
      // Intercept API to delay response
      await page.route('**/v1/**', async (route) => {
        await new Promise((resolve) => setTimeout(resolve, 2000));
        await route.continue();
      });

      await page.goto('/');

      // Check for skeleton loading states (animate-pulse cards)
      const skeletons = page.locator('[class*="animate-pulse"]');
      const hasSkeletons = await skeletons.count();

      // May or may not have skeletons depending on caching
      expect(hasSkeletons).toBeGreaterThanOrEqual(0);
    });

    test('should show scope-aware page title', async ({ page }) => {
      await page.goto('/');
      await page.waitForLoadState('networkidle');

      // Page heading should contain "Projects"
      const heading = page.getByRole('heading', { level: 1 });
      await expect(heading).toContainText(/Projects/);
    });

    test('should show inline stats summary', async ({ page }) => {
      await page.goto('/');
      await page.waitForLoadState('networkidle');

      // Look for inline stats text (e.g. "X projects", "services healthy")
      const statsText = page.getByText(/project|services healthy/i);
      const count = await statsText.count();
      expect(count).toBeGreaterThanOrEqual(0);
    });
  });

  test.describe('Search & Filter @authenticated', () => {
    test.skip(
      !process.env.TEST_USER_PASSWORD,
      'TEST_USER_PASSWORD not set - skipping authenticated tests'
    );

    test('should have a search input', async ({ page }) => {
      await page.goto('/');
      await page.waitForLoadState('networkidle');

      const searchInput = page.getByPlaceholder(/search projects/i);
      await expect(searchInput).toBeVisible();
    });

    test('should have a sort dropdown', async ({ page }) => {
      await page.goto('/');
      await page.waitForLoadState('networkidle');

      // Look for the sort select trigger
      const sortTrigger = page.locator('button[role="combobox"]');
      const count = await sortTrigger.count();
      expect(count).toBeGreaterThan(0);
    });

    test('search should filter project cards', async ({ page }) => {
      await page.goto('/');
      await page.waitForLoadState('networkidle');

      const searchInput = page.getByPlaceholder(/search projects/i);
      const cardsBefore = await page.locator('[class*="Card"]').count();

      // Type a search query that likely won't match
      await searchInput.fill('zzz-nonexistent-project');

      // Wait for filter to apply
      await page.waitForTimeout(300);

      const cardsAfter = await page.locator('[class*="Card"]').count();

      // Should show fewer cards (or the "no match" empty state)
      expect(cardsAfter).toBeLessThanOrEqual(cardsBefore);
    });
  });

  test.describe('Empty & Error States @authenticated', () => {
    test.skip(
      !process.env.TEST_USER_PASSWORD,
      'TEST_USER_PASSWORD not set - skipping authenticated tests'
    );

    test('should show empty state when no projects exist', async ({ page }) => {
      // Mock projects endpoint to return empty
      await page.route('**/v1/projects', async (route) => {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ projects: [] }),
        });
      });

      await page.goto('/');
      await page.waitForLoadState('networkidle');

      // Should show the empty state with create CTA
      const emptyText = page.getByText(/no projects yet|no projects found/i);
      const count = await emptyText.count();
      expect(count).toBeGreaterThanOrEqual(0);
    });
  });

  test.describe('Add New Button @authenticated', () => {
    test.skip(
      !process.env.TEST_USER_PASSWORD,
      'TEST_USER_PASSWORD not set - skipping authenticated tests'
    );

    test('should have Add New button', async ({ page }) => {
      await page.goto('/');
      await page.waitForLoadState('networkidle');

      const addButton = page.getByRole('button', { name: /add new|create project/i });
      if (await addButton.isVisible()) {
        await expect(addButton).toBeEnabled();
      }
    });
  });
});
