const nextJest = require('next/jest');

const createJestConfig = nextJest({
  dir: './',
});

/** @type {import('jest').Config} */
const customJestConfig = {
  testEnvironment: 'jsdom',
  setupFilesAfterEnv: ['<rootDir>/jest.setup.js'],
  moduleNameMapper: {
    '^@/(.*)$': '<rootDir>/$1',
    '^@enclii/ecosystem-tenants$': '<rootDir>/../../packages/ecosystem-tenants/src/index.ts',
    '^@enclii/shared-lib/utils$': '<rootDir>/../../packages/shared-lib/src/utils/index.ts',
    '^@enclii/ui-components$': '<rootDir>/../../packages/ui-components/src/index.ts',
    '^@enclii/ui-components/(.*)$': '<rootDir>/../../packages/ui-components/src/components/ui/$1.tsx',
  },
  testMatch: [
    '<rootDir>/**/*.test.{js,jsx,ts,tsx}',
    '<rootDir>/**/*.spec.{js,jsx,ts,tsx}',
    '!<rootDir>/e2e/**',
  ],
  testPathIgnorePatterns: [
    '<rootDir>/node_modules/',
    '<rootDir>/.next/',
  ],
  modulePathIgnorePatterns: ['<rootDir>/.next/'],
  transformIgnorePatterns: [
    '/node_modules/',
    '^.+\\.module\\.(css|sass|scss)$',
  ],
  collectCoverageFrom: [
    '**/*.{js,jsx,ts,tsx}',
    '!**/*.d.ts',
    '!**/node_modules/**',
    '!**/.next/**',
    '!jest.config.js',
    '!jest.setup.js',
    '!next.config.js',
    '!postcss.config.js',
    '!tailwind.config.js',
  ],
};

module.exports = createJestConfig(customJestConfig);
