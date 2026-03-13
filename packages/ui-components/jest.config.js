/** @type {import('jest').Config} */
module.exports = {
  testEnvironment: 'jsdom',
  transform: {
    '^.+\\.tsx?$': ['ts-jest', {
      tsconfig: 'tsconfig.json',
    }],
  },
  testMatch: [
    '<rootDir>/__tests__/**/*.test.{ts,tsx}',
  ],
  moduleFileExtensions: ['ts', 'tsx', 'js', 'jsx', 'json'],
  moduleNameMapper: {
    '^@enclii/shared-lib/utils$': '<rootDir>/../shared-lib/src/utils/index.ts',
    '^@enclii/shared-lib$': '<rootDir>/../shared-lib/src/index.ts',
  },
};
