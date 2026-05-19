import { test, expect } from '@playwright/test';
import { viewports, setupApiMocking, waitForAppReady } from '../fixtures';

/**
 * Responsive Design E2E Tests
 *
 * Priority: P0/P1
 * Tests layout at different viewport sizes and overflow handling.
 *
 * Note: Tests run on login page (accessible without auth).
 * Navigation/hamburger tests require authentication.
 */

test.describe('Responsive Design', () => {
  test.describe('Login Page - Mobile (375px)', () => {
    test.use({ viewport: viewports.mobile });

    test('should not have horizontal overflow', async ({ page }) => {
      await setupApiMocking(page);
      await page.goto('/login');
      await waitForAppReady(page);

      // Check for horizontal scroll
      const hasOverflow = await page.evaluate(() => {
        return document.documentElement.scrollWidth > document.documentElement.clientWidth;
      });

      expect(hasOverflow).toBeFalsy();
    });

    test('should display login form properly', async ({ page }) => {
      await setupApiMocking(page);
      await page.goto('/login');
      await waitForAppReady(page);

      // Login form should be visible and properly sized
      const formContainer = page.locator('.max-w-md, [class*="Card"]').first();
      await expect(formContainer).toBeVisible();

      // Check form doesn't overflow viewport
      const box = await formContainer.boundingBox();
      if (box) {
        expect(box.width).toBeLessThanOrEqual(375);
      }
    });

    test('should have visible SSO button', async ({ page }) => {
      await setupApiMocking(page);
      await page.goto('/login');
      await waitForAppReady(page);

      const ssoButton = page.getByRole('button', { name: /sign in with janua/i });
      await expect(ssoButton).toBeVisible();
    });
  });

  test.describe('Login Page - Tablet (768px)', () => {
    test.use({ viewport: viewports.tablet });

    test('should not have horizontal overflow', async ({ page }) => {
      await setupApiMocking(page);
      await page.goto('/login');
      await waitForAppReady(page);

      const hasOverflow = await page.evaluate(() => {
        return document.documentElement.scrollWidth > document.documentElement.clientWidth;
      });

      expect(hasOverflow).toBeFalsy();
    });

    test('should center login form', async ({ page }) => {
      await setupApiMocking(page);
      await page.goto('/login');
      await waitForAppReady(page);

      const formContainer = page.locator('.max-w-md, [class*="Card"]').first();
      const box = await formContainer.boundingBox();

      if (box) {
        // Form should be roughly centered (with some tolerance)
        const viewportWidth = 768;
        const expectedCenter = viewportWidth / 2;
        const actualCenter = box.x + box.width / 2;
        expect(Math.abs(actualCenter - expectedCenter)).toBeLessThan(100);
      }
    });
  });

  test.describe('Login Page - Desktop (1280px)', () => {
    test.use({ viewport: viewports.desktop });

    test('should not have horizontal overflow', async ({ page }) => {
      await setupApiMocking(page);
      await page.goto('/login');
      await waitForAppReady(page);

      const hasOverflow = await page.evaluate(() => {
        return document.documentElement.scrollWidth > document.documentElement.clientWidth;
      });

      expect(hasOverflow).toBeFalsy();
    });

    test('should display branding and login form', async ({ page }) => {
      await setupApiMocking(page);
      await page.goto('/login');
      await waitForAppReady(page);

      // Should have Enclii branding
      const branding = page.getByText(/enclii/i);
      await expect(branding.first()).toBeVisible();

      // Login form should be visible
      const ssoButton = page.getByRole('button', { name: /sign in with janua/i });
      await expect(ssoButton).toBeVisible();
    });
  });

  test.describe('Authenticated Layout @authenticated', () => {
    test.skip(
      !process.env.TEST_USER_PASSWORD,
      'TEST_USER_PASSWORD not set - skipping authenticated responsive tests'
    );

    test.describe('Mobile Navigation', () => {
      test.use({ viewport: viewports.mobile });

      test('should show hamburger menu', async ({ page }) => {
        await page.goto('/');

        // Look for hamburger/menu button
        const hamburger = page.getByRole('button', { name: /menu|navigation/i });
        await expect(hamburger).toBeVisible();
      });

      test('hamburger menu should open mobile navigation', async ({ page }) => {
        await page.goto('/');

        // Click hamburger
        const hamburger = page.getByRole('button', { name: /menu|navigation/i });
        await hamburger.click();

        // Mobile nav should appear (Sheet/Drawer component)
        const mobileNav = page.locator('[role="dialog"], [data-testid="mobile-nav"], .sheet-content');
        await expect(mobileNav).toBeVisible();
      });

      test('mobile menu should contain navigation links', async ({ page }) => {
        await page.goto('/');

        const hamburger = page.getByRole('button', { name: /menu|navigation/i });
        await hamburger.click();

        // Check for navigation links
        const dashboardLink = page.getByRole('link', { name: /dashboard/i });
        await expect(dashboardLink).toBeVisible();
      });
    });

    test.describe('Desktop Navigation', () => {
      test.use({ viewport: viewports.desktop });

      test('should show full desktop navigation', async ({ page }) => {
        await page.goto('/');

        // Desktop nav should be visible
        const navLinks = page.locator('nav a, header a').filter({
          hasText: /dashboard|projects|services|deployments/i,
        });

        const count = await navLinks.count();
        expect(count).toBeGreaterThan(0);
      });

      test('hamburger menu should be hidden', async ({ page }) => {
        await page.goto('/');

        // Hamburger should not be visible on desktop
        const hamburger = page.getByRole('button', { name: /menu/i });
        const visible = await hamburger.isVisible().catch(() => false);
        expect(visible).toBeFalsy();
      });
    });

    test.describe('Project Grid - Mobile', () => {
      test.use({ viewport: viewports.mobile });

      test('project cards should stack vertically on mobile', async ({ page }) => {
        await page.goto('/');

        const cards = page.locator('[class*="Card"]');
        const count = await cards.count();

        if (count >= 2) {
          const card1Box = await cards.nth(0).boundingBox();
          const card2Box = await cards.nth(1).boundingBox();

          if (card1Box && card2Box) {
            // Cards should be stacked (different Y positions) in single column
            expect(card2Box.y).toBeGreaterThan(card1Box.y);
          }
        }
      });
    });

    test.describe('Project Grid - Tablet', () => {
      test.use({ viewport: viewports.tablet });

      test('project cards should display in 2-column grid', async ({ page }) => {
        await page.goto('/');

        const cards = page.locator('[class*="Card"]');
        const count = await cards.count();

        if (count >= 2) {
          const card1Box = await cards.nth(0).boundingBox();
          const card2Box = await cards.nth(1).boundingBox();

          if (card1Box && card2Box) {
            // Cards should be side by side (similar Y, different X)
            const sameRow = Math.abs(card1Box.y - card2Box.y) < 50;
            const differentX = Math.abs(card1Box.x - card2Box.x) > 50;
            expect(sameRow && differentX).toBeTruthy();
          }
        }
      });
    });

    test.describe('Project Grid - Desktop', () => {
      test.use({ viewport: viewports.desktop });

      test('project cards should display in 2-column grid with sidebar', async ({ page }) => {
        await page.goto('/');

        const cards = page.locator('[class*="Card"]');
        const count = await cards.count();

        if (count >= 2) {
          // Find project cards specifically (inside the main 9-col area)
          const projectCards = page.locator('[class*="ProjectCard"], [class*="Card"]');
          const card1Box = await projectCards.nth(0).boundingBox();
          const card2Box = await projectCards.nth(1).boundingBox();

          if (card1Box && card2Box) {
            // First 2 project cards should be side by side (similar Y, different X)
            const sameRow = Math.abs(card1Box.y - card2Box.y) < 50;
            const differentX = Math.abs(card1Box.x - card2Box.x) > 50;
            expect(sameRow && differentX).toBeTruthy();
          }
        }
      });
    });
  });

  test.describe('All Viewports - Basic Checks', () => {
    const allViewports = [
      { name: 'mobile', ...viewports.mobile },
      { name: 'tablet', ...viewports.tablet },
      { name: 'desktop', ...viewports.desktop },
    ];

    for (const vp of allViewports) {
      test(`no critical JavaScript errors on ${vp.name}`, async ({ page }) => {
        await setupApiMocking(page);
        await page.setViewportSize({ width: vp.width, height: vp.height });

        const errors: string[] = [];
        page.on('console', (msg) => {
          if (msg.type() === 'error') {
            errors.push(msg.text());
          }
        });

        await page.goto('/login');
        await waitForAppReady(page);

        // Filter out known non-critical errors
        const criticalErrors = errors.filter(
          (e) =>
            !e.includes('favicon') &&
            !e.includes('Failed to load resource') &&
            !e.includes('net::ERR_')
        );

        expect(criticalErrors).toHaveLength(0);
      });

      test(`main content is visible on ${vp.name}`, async ({ page }) => {
        await setupApiMocking(page);
        await page.setViewportSize({ width: vp.width, height: vp.height });
        await page.goto('/login');
        await waitForAppReady(page);

        // Main content or login form should be visible
        const main = page.locator('main, [role="main"], .min-h-screen');
        await expect(main.first()).toBeVisible();
      });
    }
  });
});
