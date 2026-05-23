import { defineConfig, devices } from '@playwright/test';

/**
 * Ecosystem E2E Configuration
 *
 * These tests validate that production Enclii services are operational.
 * They are designed to be blocking in CI - if any test fails, deployment is blocked.
 */
export default defineConfig({
  testDir: './tests',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: process.env.CI ? 'github' : 'list',
  timeout: 30000,

  use: {
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },

  projects: [
    {
      name: 'api-health',
      testMatch: /enclii-health\.spec\.ts/,
      use: { ...devices['Desktop Chrome'] },
    },
    {
      name: 'sso-flow',
      testMatch: /enclii-login\.spec\.ts/,
      use: { ...devices['Desktop Chrome'] },
    },
    {
      name: 'paywall',
      testMatch: /enclii-paywall\.spec\.ts/,
      use: { ...devices['Desktop Chrome'] },
    },
    {
      name: 'preview-lifecycle',
      testMatch: /enclii-preview-lifecycle\.spec\.ts/,
      use: { ...devices['Desktop Chrome'] },
    },
    {
      name: 'domains-lifecycle',
      testMatch: /enclii-domains-lifecycle\.spec\.ts/,
      use: { ...devices['Desktop Chrome'] },
    },
    {
      name: 'storage-smoke',
      testMatch: /enclii-storage-smoke\.spec\.ts/,
      use: { ...devices['Desktop Chrome'] },
    },
    {
      name: 'signup-smoke',
      testMatch: /enclii-signup-smoke\.spec\.ts/,
      use: { ...devices['Desktop Chrome'] },
    },
  ],
});
