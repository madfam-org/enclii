import { timingSafeEqual } from 'crypto'

/**
 * Constant-time bearer-token check.
 *
 * Use this for ANY token compared against a secret env var. String `===`
 * leaks the matching-prefix length via timing; `timingSafeEqual` does not.
 *
 * Returns false for missing/empty headers or secrets, and for any length
 * mismatch (timingSafeEqual throws on mismatch, so we guard first).
 */
export function timingSafeBearer(
  authHeader: string | null | undefined,
  secret: string | null | undefined,
): boolean {
  if (!authHeader || !secret) return false
  const expected = Buffer.from(`Bearer ${secret}`)
  const received = Buffer.from(authHeader)
  if (expected.length !== received.length) return false
  return timingSafeEqual(expected, received)
}
