/**
 * Augments Jest's matcher types with jest-dom's DOM-aware matchers
 * (`toBeInTheDocument`, `toHaveAttribute`, …). Loaded automatically via
 * the `include` list in `tsconfig.json`. Does not affect runtime — the
 * actual matcher runtime is registered in `jest.setup.js`.
 */

import "@testing-library/jest-dom";
