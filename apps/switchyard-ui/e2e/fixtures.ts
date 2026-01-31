import { test as base, expect, Page, Route } from '@playwright/test';

/**
 * Enclii E2E Test Fixtures
 *
 * Extended test fixtures with API mocking, authentication helpers, and common utilities.
 */

// Test user credentials from environment
const TEST_USER = {
  email: process.env.TEST_USER_EMAIL || 'test-dev@madfam.io',
  password: process.env.TEST_USER_PASSWORD || '',
};

// Mock API responses for testing without backend
const mockApiResponses = {
  // Dashboard stats
  '/v1/dashboard/stats': {
    stats: {
      totalProjects: 5,
      totalServices: 16,
      activeDeployments: 2,
      healthyServices: 14,
    },
    activities: [
      {
        id: '1',
        type: 'deployment',
        message: 'Deployed switchyard-api to production',
        timestamp: new Date().toISOString(),
        status: 'success',
        metadata: { service_name: 'switchyard-api', version: 'v20260113' },
      },
      {
        id: '2',
        type: 'build',
        message: 'Build completed for docs-site',
        timestamp: new Date(Date.now() - 3600000).toISOString(),
        status: 'success',
        metadata: { service_name: 'docs-site' },
      },
    ],
    services: [
      { id: '1', name: 'switchyard-api', status: 'healthy' },
      { id: '2', name: 'switchyard-ui', status: 'healthy' },
      { id: '3', name: 'docs-site', status: 'healthy' },
    ],
  },

  // Activity feed
  '/v1/activity': {
    activities: [
      {
        id: 'act-1',
        timestamp: new Date().toISOString(),
        actor_email: 'dev@enclii.dev',
        action: 'deploy',
        resource_type: 'service',
        resource_name: 'switchyard-api',
        outcome: 'success',
      },
    ],
    count: 1,
    limit: 10,
    offset: 0,
  },

  // Health check
  '/v1/health': { status: 'healthy', version: 'v20260113' },
  '/health': { status: 'ok' },

  // Silent auth check - returns error to skip silent auth in tests
  '/v1/auth/silent-check': { error: 'oidc_not_enabled', message: 'OIDC authentication is not enabled' },

  // Projects
  '/v1/projects': { projects: [], count: 0 },

  // Services
  '/v1/services': { services: [], count: 0 },
};

// Type for mock configuration
type MockConfig = {
  enableMocking: boolean;
  mockResponses?: Record<string, unknown>;
};

// Extend base test with custom fixtures
export const test = base.extend<{
  // Authenticated page (logged in)
  authenticatedPage: typeof base;
  // Page with API mocking enabled
  mockApiPage: Page;
}>({
  // Fixture that provides an authenticated session
  authenticatedPage: async ({ page }, use) => {
    // Check if we have test credentials
    if (!TEST_USER.password) {
      console.warn('TEST_USER_PASSWORD not set - skipping authentication');
      await use(base);
      return;
    }

    // Navigate to login
    await page.goto('/login');

    // Click SSO login button
    const ssoButton = page.getByRole('button', { name: /sign in with janua|continue with sso/i });
    if (await ssoButton.isVisible()) {
      await ssoButton.click();

      // Wait for redirect to Janua
      await page.waitForURL('**/auth.madfam.io/**', { timeout: 10000 });

      // Fill Janua credentials
      await page.fill('[name="email"], [type="email"]', TEST_USER.email);
      await page.fill('[name="password"], [type="password"]', TEST_USER.password);
      await page.getByRole('button', { name: /sign in|log in/i }).click();

      // Wait for redirect back to app
      await page.waitForURL('**/', { timeout: 15000 });
    }

    await use(base);
  },

  // Fixture that provides a page with mocked API responses
  mockApiPage: async ({ page }, use) => {
    // Set up API mocking
    await setupApiMocking(page);
    await use(page);
  },
});

// Re-export expect for convenience
export { expect };

/**
 * Set up API mocking for a page.
 * Intercepts API calls and returns mock responses.
 */
export async function setupApiMocking(page: Page, customMocks?: Record<string, unknown>): Promise<void> {
  // Combine default mocks with custom ones
  const mocks = { ...mockApiResponses, ...customMocks };

  // Route all API requests (using wildcard to catch localhost:4200 and any other host)
  await page.route('**/v1/**', async (route: Route) => {
    const url = new URL(route.request().url());
    const path = url.pathname;

    // Check if we have a mock for this path
    const sortedKeys = Object.keys(mocks).sort((a, b) => b.length - a.length);
    const mockKey = sortedKeys.find((key) => path.endsWith(key) || path.includes(key));

    if (mockKey) {
      const mockData = mocks[mockKey as keyof typeof mocks];

      // Special handling for auth endpoints that should return error status codes
      if (path.includes('silent-check')) {
        await route.fulfill({
          status: 400,
          contentType: 'application/json',
          body: JSON.stringify(mockData),
        });
        return;
      }

      // Return mock response
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(mockData),
      });
    } else {
      // Return 404 for unrecognized API paths
      await route.fulfill({
        status: 404,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'Not found', path }),
      });
    }
  });

  // Also mock health endpoints at root level
  await page.route('**/health', async (route: Route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ status: 'ok' }),
    });
  });

  // Mock the auth.madfam.io endpoints for OIDC
  await page.route('**/auth.madfam.io/**', async (route: Route) => {
    await route.fulfill({
      status: 200,
      contentType: 'text/html',
      body: '<html><body><h1>Mock Janua SSO</h1></body></html>',
    });
  });
}

/**
 * Wait for the app to finish loading (no "Checking session..." or loading spinners)
 */
export async function waitForAppReady(page: Page, timeout = 10000): Promise<void> {
  // Wait for loading states to disappear
  await page.waitForFunction(
    () => {
      const body = document.body.innerText;
      return (
        !body.includes('Checking session...') &&
        !body.includes('Loading...') &&
        !body.includes('Signing you in...')
      );
    },
    { timeout }
  );
}

/**
 * Navigate to a page with API mocking and wait for it to be ready
 */
export async function navigateWithMocking(
  page: Page,
  path: string,
  customMocks?: Record<string, unknown>
): Promise<void> {
  await setupApiMocking(page, customMocks);
  await page.goto(path);
  await waitForAppReady(page);
}

/**
 * Test data-testid selectors for consistent element targeting
 */
export const testIds = {
  // Navigation
  desktopNav: 'desktop-nav',
  mobileNav: 'mobile-nav',
  hamburgerMenu: 'hamburger-menu',
  themeToggle: 'theme-toggle',
  userMenu: 'user-menu',

  // Dashboard
  statCard: 'stat-card',
  recentActivity: 'recent-activity',
  servicesTable: 'services-table',
  loadingSkeleton: 'loading-skeleton',

  // Auth
  loginButton: 'login-button',
  logoutButton: 'logout-button',
  userAvatar: 'user-avatar',
  ssoButton: 'sso-login-button',
};

/**
 * Common viewport sizes for responsive testing
 */
export const viewports = {
  mobile: { width: 375, height: 812 },
  tablet: { width: 768, height: 1024 },
  desktop: { width: 1280, height: 800 },
  desktopLarge: { width: 1920, height: 1080 },
};

/**
 * Wait for page to be fully loaded (no network activity)
 */
export async function waitForPageLoad(page: Page): Promise<void> {
  await page.waitForLoadState('networkidle');
}

/**
 * Check for console errors during test
 */
export function setupConsoleErrorCapture(page: Page): string[] {
  const errors: string[] = [];

  page.on('console', (msg) => {
    if (msg.type() === 'error') {
      errors.push(msg.text());
    }
  });

  return errors;
}
