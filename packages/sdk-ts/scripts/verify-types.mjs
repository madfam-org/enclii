#!/usr/bin/env node
/**
 * CI guard: fail if the generated types file is out of date with the committed
 * OpenAPI spec. Regenerates to a temp file, diffs against the checked-in copy,
 * and exits non-zero on drift.
 */

import { execSync } from 'node:child_process';
import { existsSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const PACKAGE_ROOT = new URL('..', import.meta.url).pathname;
const SPEC = join(PACKAGE_ROOT, '../../docs/api/openapi.yaml');
const COMMITTED = join(PACKAGE_ROOT, 'src/types.generated.ts');

if (!existsSync(SPEC)) {
  console.error(`verify-types: spec not found at ${SPEC}`);
  process.exit(1);
}
if (!existsSync(COMMITTED)) {
  console.error(
    `verify-types: ${COMMITTED} missing. Run \`pnpm -F @madfam/enclii-sdk run generate-types\` and commit.`,
  );
  process.exit(1);
}

const tmp = mkdtempSync(join(tmpdir(), 'enclii-sdk-types-'));
const candidate = join(tmp, 'types.generated.ts');

try {
  execSync(`npx openapi-typescript "${SPEC}" -o "${candidate}"`, {
    stdio: 'inherit',
  });
  const existing = readFileSync(COMMITTED, 'utf8');
  const fresh = readFileSync(candidate, 'utf8');
  if (existing !== fresh) {
    console.error('verify-types: src/types.generated.ts is out of date.');
    console.error(
      'Run `pnpm -F @madfam/enclii-sdk run generate-types` and commit the change.',
    );
    process.exit(2);
  }
  console.log('verify-types: OK');
} finally {
  rmSync(tmp, { recursive: true, force: true });
}
