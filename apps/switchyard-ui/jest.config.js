/**
 * Jest configuration for switchyard-ui
 *
 * Note: E2E tests in the e2e/ directory are Playwright tests,
 * not Jest tests. They should be run with `npm run test:e2e`.
 */
const nextJest = require('next/jest');

const createJestConfig = nextJest({
  // Provide the path to your Next.js app to load next.config.js and .env files
  dir: './',
});

/** @type {import('jest').Config} */
const customJestConfig = {
  // Test environment for React components
  testEnvironment: 'jsdom',

  // Setup files to run before tests
  setupFilesAfterEnv: ['<rootDir>/jest.setup.js'],

  // Module path aliases (matching tsconfig paths)
  moduleNameMapper: {
    '^@/(.*)$': '<rootDir>/$1',
    // Jest doesn't honour the `exports` field on workspace packages,
    // so map subpath imports back to their built files explicitly.
    // - @enclii/ui-components/<foo>  → dist/components/ui/<foo>
    // - @enclii/shared-lib/utils     → dist/utils
    // These mirror the package.json `exports` map; if a new subpath is
    // added to either package's exports, add a row here.
    '^@enclii/ui-components/(.*)$': '<rootDir>/node_modules/@enclii/ui-components/dist/components/ui/$1',
    '^@enclii/shared-lib/utils$': '<rootDir>/../../packages/shared-lib/dist/utils/index.js',
  },

  // Test file patterns - only match unit/component tests
  testMatch: [
    '<rootDir>/**/*.test.{js,jsx,ts,tsx}',
    '<rootDir>/**/*.spec.{js,jsx,ts,tsx}',
    // Explicitly exclude e2e directory patterns
    '!<rootDir>/e2e/**',
  ],

  // Ignore patterns - exclude e2e tests (Playwright), build artifacts, and node_modules
  testPathIgnorePatterns: [
    '<rootDir>/node_modules/',
    '<rootDir>/.next/',
    '<rootDir>/out/',
    '<rootDir>/e2e/',  // E2E tests use Playwright, not Jest
  ],

  // Prevent Haste module naming collisions with .next/standalone
  modulePathIgnorePatterns: [
    '<rootDir>/.next/',
  ],

  // Transform ignore patterns
  transformIgnorePatterns: [
    '/node_modules/',
    '^.+\\.module\\.(css|sass|scss)$',
  ],

  // Coverage configuration
  collectCoverageFrom: [
    '**/*.{js,jsx,ts,tsx}',
    '!**/*.d.ts',
    '!**/node_modules/**',
    '!**/.next/**',
    '!**/e2e/**',
    '!**/coverage/**',
    '!jest.config.js',
    '!jest.setup.js',
    '!next.config.js',
    '!postcss.config.js',
    '!tailwind.config.js',
  ],
};

// createJestConfig is exported this way to ensure that next/jest can load the Next.js config
module.exports = createJestConfig(customJestConfig);
