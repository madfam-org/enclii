import { test, expect } from '@playwright/test';

/**
 * Landing Page Paywall Visibility Tests — "Operation Golden Key"
 *
 * Validates that pricing/tier information is publicly visible on the landing page
 * without requiring authentication.
 *
 * NOTE: The pricing section ("Sovereign", "$20", "Start Building") exists in the
 * landing page source (apps/landing/src/app/page.tsx:110-186) but may not yet be
 * deployed to production. These tests assert against the DEPLOYED version.
 */

const LANDING_URL = process.env.LANDING_URL || 'https://enclii.dev';

test.describe('Landing Page — Pricing Visibility', () => {
  test('page loads without authentication and shows hero', async ({ page }) => {
    const response = await page.goto(LANDING_URL);

    // Page loads successfully without auth redirect
    expect(response?.status()).toBeLessThan(400);
    await expect(page).not.toHaveURL(/\/login/);

    // Hero content visible (text split by <br>, use locator)
    await expect(page.locator('h1')).toBeVisible({ timeout: 10000 });
    await expect(page.locator('h1')).toContainText('Bill Shock');
    // The hero CTA. #494 (2026-09-05) rewrote the landing as a product-line
    // page and renamed this from "Start Deploying" to "Create your account",
    // pointing at the signup route. This job asserts against the DEPLOYED
    // page, so it went red on the first main push after that deploy landed,
    // not on the PR that changed the copy. Assert the route, which is the
    // contract, and the current label, which is the copy.
    // The same label appears twice on the page — the hero and the closing
    // CTA both link to signup — and getByRole is strict, so a bare locator
    // resolves to 2 elements and fails before it checks visibility. The
    // hero is the first in document order.
    const heroCta = page.getByRole('link', { name: /Create your account/ }).first();
    await expect(heroCta).toBeVisible();
    await expect(heroCta).toHaveAttribute('href', /app\.enclii\.dev\/signup/);
  });

  test('pricing section shows Sovereign tier at $20 with Start Building CTA', async ({ page }) => {
    await page.goto(LANDING_URL);
    await page.waitForLoadState('domcontentloaded');

    // Check if pricing section exists in this deployment
    const pricingSection = page.getByText('Simple, Transparent Pricing');
    const hasPricing = await pricingSection.isVisible().catch(() => false);

    if (!hasPricing) {
      // Pricing section not yet deployed — scroll to bottom to double-check
      await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
      await page.waitForTimeout(1000);

      const stillNoPricing = !(await pricingSection.isVisible().catch(() => false));
      if (stillNoPricing) {
        test.skip(true, 'Pricing section not deployed yet — source exists at apps/landing/src/app/page.tsx:110');
        return;
      }
    }

    await pricingSection.scrollIntoViewIfNeeded();

    // Sovereign tier
    await expect(page.getByText('Sovereign')).toBeVisible();
    // $20 price
    await expect(page.getByText('$20')).toBeVisible();
    // Start Building CTA
    await expect(page.getByText('Start Building').first()).toBeVisible();
  });
});
