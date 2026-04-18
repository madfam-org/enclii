import { deploymentVersionLabel } from './types';
import type { Deployment } from './types';

// Minimal shape for the tested helper. In production this matches the full
// Deployment interface — we only exercise the version_number slot.
function mk(version_number: number | undefined): Pick<Deployment, 'version_number'> {
  return { version_number } as Pick<Deployment, 'version_number'>;
}

describe('deploymentVersionLabel', () => {
  it('formats an integer as the Heroku-style v-label', () => {
    expect(deploymentVersionLabel(mk(42))).toBe('v42');
  });

  it('handles v1 (first allocation)', () => {
    expect(deploymentVersionLabel(mk(1))).toBe('v1');
  });

  it('returns null when the version has not been allocated', () => {
    // Historical rows (pre-P2.6 backfill) and rolling deploys during the
    // migration window may not have version_number set yet. The helper
    // returns null so UI callers can render a placeholder / fall back to
    // the digest shortsha.
    expect(deploymentVersionLabel(mk(undefined))).toBeNull();
  });
});
