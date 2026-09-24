/**
 * Cursor adapter for the API's `limit`/`offset` endpoints (`GET /activity`,
 * `GET /lifecycle-webhooks/{id}/deliveries`). The SDK keeps its `Page<T>`
 * contract: the cursor is the decimal offset of the next page, and
 * `nextCursor` is null once a page comes back shorter than the page size the
 * server actually applied (it echoes that as `limit`).
 */

export function offsetFromCursor(
  method: string,
  cursor: string | undefined,
): number | undefined {
  if (cursor === undefined || cursor === '') return undefined;
  if (!/^\d+$/.test(cursor)) {
    throw new Error(
      `${method}: invalid cursor ${JSON.stringify(cursor)} (expected a nextCursor returned by a previous call)`,
    );
  }
  return Number.parseInt(cursor, 10);
}

export function assertLimit(
  method: string,
  limit: number | undefined,
  max: number,
): void {
  if (limit === undefined) return;
  // The API silently replaces an out-of-range limit with its default, which
  // would make a caller's page size lie; reject it here instead.
  if (!Number.isInteger(limit) || limit < 1 || limit > max) {
    throw new Error(`${method}: limit must be an integer from 1 to ${max}`);
  }
}

export function nextOffsetCursor(
  count: number,
  resp: { limit?: number; offset?: number },
): string | null {
  const limit = resp.limit ?? 0;
  if (count === 0 || limit <= 0 || count < limit) return null;
  return String((resp.offset ?? 0) + count);
}
