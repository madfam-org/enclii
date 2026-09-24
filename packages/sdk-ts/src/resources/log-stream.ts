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
 *     `lines` (default 100), `timestamps=true`, and `since` (RFC3339 or a
 *     Go duration; limits the backlog), which these helpers do not send.
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
  if (queryToken) params.append('token', queryToken);
  const qs = params.toString();
  return `${base}/services/${encodeURIComponent(serviceId)}/logs/stream${qs ? `?${qs}` : ''}`;
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
