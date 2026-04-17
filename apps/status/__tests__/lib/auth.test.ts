/**
 * Unit tests for constant-time bearer auth used by the incidents and
 * status/record routes. Covers the three failure paths (missing header,
 * missing secret, length mismatch) and the two success/fail-on-equal-
 * length cases.
 */
import { timingSafeBearer } from '@/lib/auth'

describe('timingSafeBearer', () => {
  it('returns true for a matching Bearer token', () => {
    expect(timingSafeBearer('Bearer hunter2', 'hunter2')).toBe(true)
  })

  it('returns false when the header is missing', () => {
    expect(timingSafeBearer(null, 'hunter2')).toBe(false)
    expect(timingSafeBearer(undefined, 'hunter2')).toBe(false)
    expect(timingSafeBearer('', 'hunter2')).toBe(false)
  })

  it('returns false when the secret is missing', () => {
    expect(timingSafeBearer('Bearer anything', null)).toBe(false)
    expect(timingSafeBearer('Bearer anything', undefined)).toBe(false)
    expect(timingSafeBearer('Bearer anything', '')).toBe(false)
  })

  it('returns false when the token length differs from expected', () => {
    // Different lengths must short-circuit before timingSafeEqual (which
    // throws on mismatch). Regression guard for the underlying API.
    expect(timingSafeBearer('Bearer short', 'much-longer-secret')).toBe(false)
    expect(timingSafeBearer('Bearer way-longer', 'x')).toBe(false)
  })

  it('returns false when the prefix is wrong but length matches', () => {
    // "Beerer abc" has same byte length as "Bearer abc" — still invalid.
    expect(timingSafeBearer('Beerer abc', 'abc')).toBe(false)
  })

  it('returns false when the token value is wrong at equal length', () => {
    expect(timingSafeBearer('Bearer wrongxx', 'righttt')).toBe(false)
  })

  it('is case-sensitive on the token', () => {
    expect(timingSafeBearer('Bearer HUNTER2', 'hunter2')).toBe(false)
  })
})
