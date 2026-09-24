/**
 * Wire types for the operational resources: logs, audit, secrets (env-vars)
 * and jobs. Split out of `types.ts` to keep both files small; everything here
 * is re-exported from the package root exactly like `types.ts`.
 *
 * Each shape mirrors the JSON the Switchyard API handler writes today; the
 * handler is named in each section so the contract can be re-checked.
 */

import type { ISODateTime, UUID } from './types';

// -----------------------------------------------------------------------------
// Logs (apps/switchyard-api/internal/api/logs_handlers.go)
// -----------------------------------------------------------------------------

/** @deprecated No log endpoint accepts a level filter; kept for type compatibility. */
export type LogLevel = 'debug' | 'info' | 'warn' | 'error' | 'fatal';

/** `type` values the stream handler sends (`LogStreamMessage` in logs_handlers.go). */
export type LogStreamMessageType =
  | 'log'
  | 'error'
  | 'info'
  | 'connected'
  | 'disconnected';

/**
 * One WebSocket frame from `GET /services/{id}/logs/stream`. Only `log` frames
 * carry a log line; `connected`/`disconnected` are stream status and `error`
 * reports a Kubernetes log-stream error (the stream stays open).
 */
export interface LogStreamMessage {
  type: LogStreamMessageType;
  /** Set on `log` frames. */
  pod?: string;
  /** Set on `log` frames. */
  container?: string;
  timestamp: ISODateTime;
  message: string;
}

/** @deprecated Renamed to `LogStreamMessage`, which is what the stream yields. */
export type LogEntry = LogStreamMessage;

export interface LogHistoryOptions {
  /** Environment name; the API defaults to `development`. */
  env?: string;
  /** Number of lines, 1 to 10000; the API defaults to 100. */
  lines?: number;
  /**
   * Only lines newer than this: an RFC3339 timestamp (`2026-09-24T10:00:00Z`)
   * or a positive Go duration (`15m`, `24h`). Sent as the `since` query
   * parameter; a malformed value throws before sending. Servers before
   * enclii #622 ignore it.
   */
  since?: string;
}

/** Response of `GET /services/{id}/logs/history`. */
export interface LogHistory {
  service_id: UUID;
  service_name: string;
  environment: string;
  namespace: string;
  /** Raw log text for the service's pods, newline-separated. */
  logs: string;
  /** Number of lines the server requested per pod. */
  lines: number;
}

export interface LogTailOptions {
  /** Environment name; the API defaults to `development`. */
  env?: string;
  /** Lines of backlog per pod before following; the API defaults to 100. */
  lines?: number;
  /** Ask Kubernetes to prefix lines with timestamps. */
  timestamps?: boolean;
  /**
   * Limit the backlog to lines newer than this: an RFC3339 timestamp
   * (`2026-09-24T10:00:00Z`) or a positive Go duration (`15m`, `24h`), as
   * `enclii logs --follow --since` sends it. A malformed value throws before
   * connecting. `nodeLogsTail` sends the same value on every reconnect, so a
   * duration is measured from each reconnect. Servers before enclii #625
   * ignore it.
   */
  since?: string;
  /** Abort signal for graceful shutdown. */
  signal?: AbortSignal;
}

// -----------------------------------------------------------------------------
// Audit / activity log (apps/switchyard-api/internal/api/activity_handlers.go)
// -----------------------------------------------------------------------------

/** One row of `GET /activity` (`types.AuditLog` in packages/sdk-go). */
export interface AuditEvent {
  id: UUID;
  timestamp: ISODateTime;
  actor_id?: UUID;
  actor_email: string;
  actor_role: string;
  action: string;
  resource_type: string;
  resource_id: string;
  resource_name: string;
  project_id?: UUID;
  environment_id?: UUID;
  ip_address: string;
  user_agent: string;
  /** `success`, `failure`, or `denied`. */
  outcome: string;
  context: Record<string, unknown> | null;
  metadata?: Record<string, unknown>;
}

export interface AuditQueryOptions {
  action?: string;
  resource_type?: string;
  /** A UUID; the API silently ignores a value that is not one. */
  project_id?: string;
  /** A UUID; the API silently ignores a value that is not one. */
  actor_id?: string;
  /** Page size, 1 to 100 (the API default is 50). */
  limit?: number;
  /** Opaque cursor from a previous page's `nextCursor`. */
  cursor?: string;
}

// -----------------------------------------------------------------------------
// Secrets / env-vars (apps/switchyard-api/internal/api/envvar_handlers.go)
// -----------------------------------------------------------------------------

export interface EnvVar {
  id: UUID;
  service_id: UUID;
  /** Absent when the variable applies to every environment. */
  environment_id?: UUID;
  key: string;
  /** The plaintext value, or the mask `••••••••` when `is_secret` is true. */
  value: string;
  is_secret: boolean;
  created_at: ISODateTime;
  updated_at: ISODateTime;
}

export interface SetEnvVarRequest {
  key: string;
  value: string;
  is_secret?: boolean;
  /** Scope to one environment (UUID). Omit for all environments. */
  environment_id?: string;
}

/** A single entry of a bulk upsert; scope is set once per call. */
export type BulkEnvVar = Omit<SetEnvVarRequest, 'environment_id'>;

/** Response of `POST /services/{id}/env-vars/bulk`. */
export interface BulkSetEnvVarsResponse {
  message: string;
  count: number;
}

// -----------------------------------------------------------------------------
// Jobs / Timetable (apps/switchyard-api/internal/api/timetable_handlers.go)
// -----------------------------------------------------------------------------

export interface CronJob {
  id: UUID;
  project_id: UUID;
  service_id: UUID;
  name: string;
  schedule: string;
  command: string;
  image?: string;
  timeout: number;
  retries: number;
  suspended: boolean;
  concurrency: 'allow' | 'forbid' | 'replace';
  created_at: ISODateTime;
  updated_at: ISODateTime;
  last_run_at?: ISODateTime | null;
  next_run_at?: ISODateTime | null;
}

export interface CreateCronJobRequest {
  name: string;
  schedule: string;
  command: string;
  service_id: string;
  image?: string;
  timeout?: number;
  retries?: number;
  concurrency?: 'allow' | 'forbid' | 'replace';
}

export interface CreateOneOffJobRequest {
  name: string;
  command: string;
  service_id: string;
  image?: string;
  timeout?: number;
  /** RFC 3339 time; omit to run now. */
  run_at?: string;
}

export interface OneOffJob {
  id: UUID;
  project_id: UUID;
  service_id: UUID;
  name: string;
  command: string;
  image?: string;
  timeout: number;
  run_at?: ISODateTime | null;
  status: 'pending' | 'running' | 'completed' | 'failed';
  exit_code?: number | null;
  /** Why the job failed before producing a pod (for example an admission denial). */
  failure_reason?: string;
  created_at: ISODateTime;
  started_at?: ISODateTime | null;
  ended_at?: ISODateTime | null;
}

export interface CronJobRun {
  id: UUID;
  cron_job_id: UUID;
  status: 'running' | 'completed' | 'failed';
  exit_code?: number | null;
  started_at: ISODateTime;
  ended_at?: ISODateTime | null;
  log_output?: string;
}
