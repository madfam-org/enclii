import { test, expect, Page } from '@playwright/test';

// The audit signs into PRODUCTION. Credentials come from the environment and
// are never written here: this file lives in a public repository, and a literal
// committed on 2026-01-22 was found by detect-secrets on 2026-09-12 (enclii #554).
const CREDENTIALS = {
  email: process.env.SSO_AUDIT_EMAIL ?? '',
  password: process.env.SSO_AUDIT_PASSWORD ?? ''
};
if (!CREDENTIALS.email || !CREDENTIALS.password) {
  throw new Error('SSO audit: set SSO_AUDIT_EMAIL and SSO_AUDIT_PASSWORD in the environment');
}

const SSO_PROVIDER = 'auth.madfam.io';

interface PlatformConfig {
  name: string;
  url: string;
  dashboardPath: string;
}

const PLATFORMS: PlatformConfig[] = [
  {
    name: 'dhanam',
    url: 'https://app.dhan.am',
    dashboardPath: '/dashboard',
  },
  {
    name: 'enclii',
    url: 'https://app.enclii.dev',
    dashboardPath: '/dashboard',
  }
];

async function loginViaJanuaSSO(page: Page, platform: string, screenshotDir: string): Promise<boolean> {
  console.log(`[${platform}] Entering credentials on Janua SSO...`);

  // Wait for the page to stabilize
  await page.waitForTimeout(2000);

  const currentUrl = page.url();
  console.log(`[${platform}] Current URL: ${currentUrl}`);

  // Find email field
  const emailInput = page.locator('input[name="email"], input[type="email"], #email').first();
  if (await emailInput.isVisible({ timeout: 5000 })) {
    await emailInput.fill(CREDENTIALS.email);
    console.log(`[${platform}] ✓ Filled email`);
  } else {
    console.log(`[${platform}] ✗ Email field not found`);
    await page.screenshot({ path: `${screenshotDir}/${platform}-error-no-email.png`, fullPage: true });
    return false;
  }

  // Find password field
  const passwordInput = page.locator('input[name="password"], input[type="password"], #password').first();
  if (await passwordInput.isVisible({ timeout: 3000 })) {
    await passwordInput.fill(CREDENTIALS.password);
    console.log(`[${platform}] ✓ Filled password`);
  } else {
    console.log(`[${platform}] ✗ Password field not found`);
    await page.screenshot({ path: `${screenshotDir}/${platform}-error-no-password.png`, fullPage: true });
    return false;
  }

  // Screenshot before submit
  await page.screenshot({ path: `${screenshotDir}/${platform}-sso-credentials-filled.png`, fullPage: true });

  // Find and click submit
  const submitButton = page.locator('button[type="submit"], button:has-text("Sign In"), button:has-text("Sign in")').first();
  if (await submitButton.isVisible({ timeout: 3000 })) {
    console.log(`[${platform}] Clicking submit...`);
    await submitButton.click();

    // Wait for response/redirect
    await page.waitForTimeout(5000);
    return true;
  }

  console.log(`[${platform}] ✗ Submit button not found`);
  return false;
}

async function clickSSOButton(page: Page, platform: string): Promise<boolean> {
  // Look for SSO/Janua login button
  const ssoSelectors = [
    'button:has-text("Sign in with Janua SSO")',
    'button:has-text("Sign in with Janua")',
    'button:has-text("Janua SSO")',
    'a:has-text("Sign in with Janua")',
    'a:has-text("Janua SSO")',
    'button:has-text("Sign in with SSO")',
  ];

  // Wait for any SSO button to appear
  console.log(`[${platform}] Looking for SSO button...`);

  for (const selector of ssoSelectors) {
    try {
      const button = page.locator(selector).first();
      if (await button.isVisible({ timeout: 5000 })) {
        console.log(`[${platform}] Found SSO button: ${selector}`);
        // Scroll into view and click
        await button.scrollIntoViewIfNeeded();
        await page.waitForTimeout(500);
        await button.click({ force: true });
        console.log(`[${platform}] Clicked SSO button`);
        await page.waitForTimeout(3000);
        return true;
      }
    } catch (e) {
      console.log(`[${platform}] Selector ${selector} not found or not clickable`);
    }
  }

  // Last resort: try clicking by text content
  try {
    const anyJanuaButton = page.getByRole('button', { name: /janua/i });
    if (await anyJanuaButton.isVisible({ timeout: 3000 })) {
      console.log(`[${platform}] Found Janua button by role`);
      await anyJanuaButton.click({ force: true });
      await page.waitForTimeout(3000);
      return true;
    }
  } catch (e) {
    console.log(`[${platform}] No Janua button found by role`);
  }

  return false;
}

async function findLogoutButton(page: Page, platform: string): Promise<boolean> {
  // First try to open user menu if present
  const menuSelectors = [
    'button[aria-label*="user" i]',
    'button[aria-label*="account" i]',
    'button[aria-label*="profile" i]',
    '[data-testid="user-menu"]',
    '.user-menu',
    '.avatar',
  ];

  for (const selector of menuSelectors) {
    try {
      const menu = page.locator(selector).first();
      if (await menu.isVisible({ timeout: 1500 })) {
        console.log(`[${platform}] Opening user menu: ${selector}`);
        await menu.click();
        await page.waitForTimeout(1000);
        break;
      }
    } catch (e) {
      // Continue
    }
  }

  // Look for logout button
  const logoutSelectors = [
    'button:has-text("Logout")',
    'button:has-text("Log out")',
    'button:has-text("Sign out")',
    'a:has-text("Logout")',
    'a:has-text("Log out")',
    'a:has-text("Sign out")',
    '[data-testid="logout"]',
  ];

  for (const selector of logoutSelectors) {
    try {
      const button = page.locator(selector).first();
      if (await button.isVisible({ timeout: 2000 })) {
        console.log(`[${platform}] Found logout: ${selector}`);
        await button.click();
        return true;
      }
    } catch (e) {
      // Continue
    }
  }

  return false;
}

test.describe('Production SSO Audit', () => {
  test.describe.configure({ mode: 'serial' });

  for (const platform of PLATFORMS) {
    test(`${platform.name}: Full SSO flow test`, async ({ page }) => {
      test.setTimeout(90000);

      const screenshotDir = 'tests/sso-audit/screenshots';

      console.log(`\n${'='.repeat(60)}`);
      console.log(`TESTING: ${platform.name.toUpperCase()} (${platform.url})`);
      console.log(`${'='.repeat(60)}`);

      // STEP 1: Navigate to platform
      console.log(`\n[${platform.name}] STEP 1: Navigate to platform`);

      const response = await page.goto(platform.url, {
        waitUntil: 'domcontentloaded',
        timeout: 30000
      });

      const status = response?.status() || 0;
      console.log(`[${platform.name}] HTTP Status: ${status}`);

      if (status >= 500) {
        await page.screenshot({ path: `${screenshotDir}/${platform.name}-01-server-error.png`, fullPage: true });
        throw new Error(`Server error: ${status}`);
      }

      // Wait for app to load (handle SPA loading states)
      // Wait up to 10 seconds for "Checking session" to disappear
      console.log(`[${platform.name}] Waiting for loading to complete...`);
      try {
        await page.waitForFunction(() => {
          return !document.body.innerText.includes('Checking session') &&
                 !document.body.innerText.includes('Loading');
        }, { timeout: 15000 });
        console.log(`[${platform.name}] Loading complete`);
      } catch (e) {
        console.log(`[${platform.name}] ⚠ Loading state timeout, waiting more...`);
        await page.waitForTimeout(5000);
      }

      await page.waitForTimeout(2000);

      await page.screenshot({ path: `${screenshotDir}/${platform.name}-01-landing.png`, fullPage: true });
      console.log(`[${platform.name}] ✓ Screenshot: 01-landing.png`);

      // STEP 2: Initiate SSO login
      console.log(`\n[${platform.name}] STEP 2: Initiate SSO login`);

      let onJanuaSSO = page.url().includes(SSO_PROVIDER);

      if (!onJanuaSSO) {
        // Wait for SSO button to become visible (handles slow loading SPAs)
        console.log(`[${platform.name}] Waiting for SSO button to appear...`);
        try {
          await page.waitForSelector('button:has-text("Janua"), button:has-text("SSO")', { timeout: 20000 });
          console.log(`[${platform.name}] SSO button visible`);
        } catch (e) {
          console.log(`[${platform.name}] SSO button wait timeout, checking for login button`);
        }

        // Check if we need to click SSO button
        const clickedSSO = await clickSSOButton(page, platform.name);

        if (!clickedSSO) {
          // Maybe there's a generic login button first
          const loginButton = page.locator('button:has-text("Sign in"), button:has-text("Login"), a:has-text("Sign in")').first();
          if (await loginButton.isVisible({ timeout: 3000 })) {
            console.log(`[${platform.name}] Clicking generic login button...`);
            await loginButton.click();
            await page.waitForTimeout(2000);

            // Now try SSO button again
            await clickSSOButton(page, platform.name);
          }
        }

        await page.waitForTimeout(2000);
        onJanuaSSO = page.url().includes(SSO_PROVIDER);
      }

      const afterSSOClickUrl = page.url();
      console.log(`[${platform.name}] URL after SSO click: ${afterSSOClickUrl}`);

      await page.screenshot({ path: `${screenshotDir}/${platform.name}-02-sso-redirect.png`, fullPage: true });
      console.log(`[${platform.name}] ✓ Screenshot: 02-sso-redirect.png`);

      // STEP 3: Enter credentials on Janua
      console.log(`\n[${platform.name}] STEP 3: Enter credentials`);

      if (page.url().includes(SSO_PROVIDER) || page.url().includes('login') || page.url().includes('auth')) {
        const loginSuccess = await loginViaJanuaSSO(page, platform.name, screenshotDir);

        if (loginSuccess) {
          console.log(`[${platform.name}] Credentials submitted, waiting for redirect...`);
          await page.waitForTimeout(8000);
        }
      } else {
        console.log(`[${platform.name}] ⚠ Not on SSO page, skipping credential entry`);
      }

      // STEP 4: Verify dashboard/authenticated state
      console.log(`\n[${platform.name}] STEP 4: Verify authenticated state`);

      const finalUrl = page.url();
      console.log(`[${platform.name}] Final URL: ${finalUrl}`);

      await page.screenshot({ path: `${screenshotDir}/${platform.name}-03-post-login.png`, fullPage: true });
      console.log(`[${platform.name}] ✓ Screenshot: 03-post-login.png`);

      // Check for error indicators
      const pageContent = await page.content();
      const hasError = pageContent.includes('INTERNAL_ERROR') ||
                       pageContent.includes('error') && pageContent.includes('unexpected') ||
                       pageContent.includes('Error:');

      if (hasError) {
        console.log(`[${platform.name}] ⚠ ERROR detected on page!`);
        await page.screenshot({ path: `${screenshotDir}/${platform.name}-ERROR-detected.png`, fullPage: true });
      }

      // Check for authenticated indicators
      const authIndicators = [
        CREDENTIALS.email,
        'admin',
        'Dashboard',
        'Settings',
        'Profile',
        'Logout',
        'Sign out'
      ];

      let authenticated = false;
      for (const indicator of authIndicators) {
        if (pageContent.toLowerCase().includes(indicator.toLowerCase())) {
          console.log(`[${platform.name}] ✓ Found auth indicator: "${indicator}"`);
          authenticated = true;
          break;
        }
      }

      if (!authenticated) {
        console.log(`[${platform.name}] ⚠ Could not verify authenticated state`);
      }

      // STEP 5: Logout (if authenticated)
      console.log(`\n[${platform.name}] STEP 5: Logout`);

      const loggedOut = await findLogoutButton(page, platform.name);

      if (loggedOut) {
        await page.waitForTimeout(3000);
        await page.screenshot({ path: `${screenshotDir}/${platform.name}-04-post-logout.png`, fullPage: true });
        console.log(`[${platform.name}] ✓ Screenshot: 04-post-logout.png`);
      } else {
        console.log(`[${platform.name}] ⚠ Logout button not found (may not be authenticated)`);
        await page.screenshot({ path: `${screenshotDir}/${platform.name}-04-no-logout.png`, fullPage: true });
      }

      console.log(`\n[${platform.name}] ✓ Test completed`);
      console.log(`${'='.repeat(60)}\n`);
    });
  }
});
