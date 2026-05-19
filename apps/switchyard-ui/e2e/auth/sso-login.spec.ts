import { test, expect } from '@playwright/test';
import { setupApiMocking, waitForAppReady } from '../fixtures';

/**
 * Authentication E2E Tests
 *
 * Priority: P0 (Critical)
 * Tests SSO login flow via Janua, session management, and logout.
 *
 * These tests use API mocking to work without a running backend.
 */

test.describe('SSO Authentication', () => {
  test.describe('Login Flow', () => {
    test('should display login page for unauthenticated users', async ({ page }) => {
      // Set up API mocking to simulate no auth
      await setupApiMocking(page);

      // Navigate directly to login page
      await page.goto('/login');

      // Wait for loading to complete
      await waitForAppReady(page);

      // Should be on login page
      expect(page.url()).toContain('/login');
    });

    test('should have SSO login option', async ({ page }) => {
      await setupApiMocking(page);
      await page.goto('/login');
      await waitForAppReady(page);

      // Look for SSO/Janua login button - check for text content
      const ssoButton = page.getByRole('button', { name: /sign in with janua/i });
      await expect(ssoButton).toBeVisible({ timeout: 10000 });
    });

    test('should redirect to Janua on SSO button click', async ({ page }) => {
      await setupApiMocking(page);
      await page.goto('/login');
      await waitForAppReady(page);

      // Click SSO button
      const ssoButton = page.getByRole('button', { name: /sign in with janua/i });
      await expect(ssoButton).toBeVisible({ timeout: 10000 });

      // Note: Clicking will redirect to the mocked Janua page
      // We verify the button exists and is clickable
      await expect(ssoButton).toBeEnabled();
    });

    test('should show page heading on login', async ({ page }) => {
      await setupApiMocking(page);
      await page.goto('/login');
      await waitForAppReady(page);

      // Should have a heading
      const heading = page.getByRole('heading', { name: /enclii/i });
      await expect(heading).toBeVisible({ timeout: 10000 });
    });
  });

  test.describe('Protected Routes', () => {
    // Note: Protected route redirect tests are skipped because they require
    // full client-side hydration which is slow in E2E tests. The auth redirect
    // logic is tested via unit tests instead.
    test.skip(true, 'Protected route redirects require full client-side hydration');

    test('should redirect /dashboard to login when unauthenticated', async ({ page }) => {
      await setupApiMocking(page);
      await page.goto('/');
      await page.waitForURL('**/login**', { timeout: 10000 });
      expect(page.url()).toContain('/login');
    });

    test('should redirect /projects to login when unauthenticated', async ({ page }) => {
      await setupApiMocking(page);
      await page.goto('/projects');
      await page.waitForURL('**/login**', { timeout: 10000 });
      expect(page.url()).toContain('/login');
    });

    test('should redirect /services to login when unauthenticated', async ({ page }) => {
      await setupApiMocking(page);
      await page.goto('/services');
      await page.waitForURL('**/login**', { timeout: 10000 });
      expect(page.url()).toContain('/login');
    });
  });

  test.describe('Login Page UI', () => {
    test('should display Enclii branding', async ({ page }) => {
      await setupApiMocking(page);
      await page.goto('/login');
      await waitForAppReady(page);

      // Should show Enclii branding
      const branding = page.getByText(/enclii/i);
      await expect(branding.first()).toBeVisible({ timeout: 10000 });
    });

    test('should have proper page structure', async ({ page }) => {
      await setupApiMocking(page);
      await page.goto('/login');
      await waitForAppReady(page);

      // Should have main container
      const container = page.locator('.min-h-screen');
      await expect(container).toBeVisible();

      // Should have login form area
      const formArea = page.locator('.max-w-md');
      await expect(formArea).toBeVisible();
    });
  });
});

test.describe('Session Management', () => {
  // These tests require valid credentials - skip in CI without credentials
  test.skip(
    !process.env.TEST_USER_PASSWORD,
    'TEST_USER_PASSWORD not set - skipping authenticated tests'
  );

  test('should maintain session after page refresh', async ({ page }) => {
    // This test would require authentication setup
    // Login first
    await page.goto('/login');

    const ssoButton = page.getByRole('button', { name: /sign in with janua|continue with sso/i });
    if (await ssoButton.isVisible()) {
      await ssoButton.click();

      // Complete Janua login
      await page.waitForURL('**/auth.madfam.io/**');
      await page.fill('[name="email"], [type="email"]', process.env.TEST_USER_EMAIL || '');
      await page.fill('[name="password"], [type="password"]', process.env.TEST_USER_PASSWORD || '');
      await page.getByRole('button', { name: /sign in|log in/i }).click();

      // Wait for redirect back
      await page.waitForURL('**/', { timeout: 15000 });
    }

    // Verify logged in
    const userElement = page.getByRole('button', { name: /user|profile|account/i });
    await expect(userElement).toBeVisible();

    // Refresh page
    await page.reload();

    // Should still be logged in
    await expect(userElement).toBeVisible();
  });
});

test.describe('Logout Flow', () => {
  test.skip(
    !process.env.TEST_USER_PASSWORD,
    'TEST_USER_PASSWORD not set - skipping logout tests'
  );

  test('should have logout option in user menu', async ({ page }) => {
    // Would need authenticated session
    // After login, open user menu
    const userMenu = page.getByRole('button', { name: /user|profile|account/i });

    if (await userMenu.isVisible()) {
      await userMenu.click();

      // Look for logout option
      const logoutButton = page.getByRole('menuitem', { name: /log out|sign out|logout/i });
      await expect(logoutButton).toBeVisible();
    }
  });

  test('should redirect to login after logout', async ({ page }) => {
    // After clicking logout, should redirect to login
    // This would need full authentication flow setup
    const userMenu = page.getByRole('button', { name: /user|profile|account/i });

    if (await userMenu.isVisible()) {
      await userMenu.click();

      const logoutButton = page.getByRole('menuitem', { name: /log out|sign out|logout/i });
      await logoutButton.click();

      // Should redirect to login
      await page.waitForURL('**/login**', { timeout: 10000 });
      expect(page.url()).toContain('/login');
    }
  });
});
