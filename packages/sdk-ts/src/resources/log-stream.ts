/**
 * Shared plumbing for the two log-stream clients (`logs.tail()` and
 * `nodeLogsTail()`), both of which open `GET /services/{id}/logs/stream`
 * (`StreamServiceLogsWS` in apps/switchyard-api/internal/api/logs_handlers.go).
 *
 * Handshake requirements of that route, as the server enforces them:
 *   - Auth: `Authorization: Bearer <token>` header, or a `token` query
 *     parameter when a header cannot be set (auth/jwt_middleware.go).
 *   - Origin (api/ws_upgrade.go): an `Origin` header, when sent, must exactly
 *     match one of the configured WebSocket origins
 *     (`ENCLII_WEBSOCKET_ALLOWED_ORIGINS`). An upgrade without `Origin` is
 *     accepted only when it authenticates with the `Authorization` header;
 *     a query-token upgrade without `Origin` is refused with 403. Servers
 *     before this rule refused every upgrade without an allowed `Origin`.
 *   - Query: `env` (default `development`; an unknown env is a 404),
 *     `lines` (default 100), `timestamps=true`, and `since` (an RFC3339
 *     timestamp or a positive Go duration; limits the backlog to newer
 *     lines; a bad value is a 400 before the upgrade), all of which these
 *     helpers send when set.
 */

import type { LogStreamMessage, LogTailOptions } from '../types-ops';

export function buildLogStreamUrl(
  baseUrl: string,
  serviceId: string,
  options: LogTailOptions,
  queryToken?: string | null,
): string {
  const base = baseUrl.replace(/^http(s?):\/\//, 'ws$1://');
  const params = new URLSearchParams();
  if (options.env) params.append('env', options.env);
  if (options.lines !== undefined) {
    if (!Number.isInteger(options.lines) || options.lines < 1) {
      throw new Error('logs stream: lines must be a positive integer');
    }
    params.append('lines', String(options.lines));
  }
  if (options.timestamps) params.append('timestamps', 'true');
  if (options.since !== undefined) {
    assertLogsSince(options.since, 'logs stream');
    params.append('since', options.since);
  }
  if (queryToken) params.append('token', queryToken);
  const qs = params.toString();
  return `${base}/services/${encodeURIComponent(serviceId)}/logs/stream${qs ? `?${qs}` : ''}`;
}

const RFC3339 =
  /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(\.\d+)?(Z|[+-]\d{2}:\d{2})$/;
const GO_DURATION = /^\+?(?:(?:\d+(?:\.\d*)?|\.\d+)(?:ns|us|µs|μs|ms|s|m|h))+$/;

/**
 * Check a `since` value the way the log endpoints read it (`parseLogsSince`
 * in apps/switchyard-api/internal/api/logs_since.go): an RFC3339 timestamp
 * such as `2026-09-24T10:00:00Z` (fractional seconds and a `±hh:mm` offset
 * allowed), or a positive Go duration such as `90s`, `15m` or `1h30m`.
 * Anything else throws, because the server would answer 400, and a browser
 * WebSocket reports a refused upgrade without its status or body.
 *
 * Not checked here: the server also rejects a timestamp in the future by its
 * own clock, and caps the window at 30 days.
 */
export function assertLogsSince(since: string, caller: string): void {
  const hint =
    `${caller}: since must be an RFC3339 timestamp (2026-09-24T10:00:00Z) ` +
    `or a positive duration (90s, 15m, 1h30m); got ${JSON.stringify(since)}`;
  if (typeof since !== 'string') throw new Error(hint);
  const ts = RFC3339.exec(since);
  if (ts) {
    const [, , month, day, hour, minute, second] = ts;
    const inRange =
      Number(month) >= 1 && Number(month) <= 12 &&
      Number(day) >= 1 && Number(day) <= 31 &&
      Number(hour) <= 23 && Number(minute) <= 59 && Number(second) <= 59;
    if (inRange && !Number.isNaN(Date.parse(since))) return;
    throw new Error(hint);
  }
  // A Go duration is positive when any of its numbers has a non-zero digit.
  if (GO_DURATION.test(since) && /[1-9]/.test(since)) return;
  throw new Error(hint);
}

/** Parse one frame; returns null for anything that is not a stream message. */
export function parseLogFrame(raw: string): LogStreamMessage | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  if (
    typeof parsed !== 'object' ||
    parsed === null ||
    typeof (parsed as { type?: unknown }).type !== 'string'
  ) {
    return null;
  }
  return parsed as LogStreamMessage;
}
