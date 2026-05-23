/**
 * Hand-written domain types for @madfam/enclii-sdk.
 *
 * These mirror the structures the Enclii control plane returns on the wire and
 * match the Go SDK (packages/sdk-go/pkg/types) field-for-field. A fully
 * generated companion lives in `types.generated.ts` (run `pnpm generate-types`)
 * — use that when you want maximum fidelity against the OpenAPI spec; use
 * these when you want ergonomic, curated, jsdoc-documented shapes.
 */

// -----------------------------------------------------------------------------
// Common
// -----------------------------------------------------------------------------

/** ISO-8601 timestamp, e.g. "2026-04-17T14:32:00Z". */
export type ISODateTime = string;

/** UUID v4 string. */
export type UUID = string;

/** Envelope returned by every list endpoint that supports cursor pagination. */
export interface Page<T> {
  data: T[];
  nextCursor: string | null;
}

// -----------------------------------------------------------------------------
// Projects
// -----------------------------------------------------------------------------

export type CIRunnerMode = 'github' | 'self-hosted';

export interface Project {
  id: UUID;
  name: string;
  slug: string;
  ci_runner_mode: CIRunnerMode;
  created_at: ISODateTime;
  updated_at: ISODateTime;
}

export interface CreateProjectRequest {
  name: string;
  slug: string;
  ci_runner_mode?: CIRunnerMode;
}

// -----------------------------------------------------------------------------
// Services
// -----------------------------------------------------------------------------

export type HealthStatus = 'unknown' | 'healthy' | 'unhealthy' | 'degraded';
export type BuildType = 'auto' | 'dockerfile' | 'buildpack';

export interface BuildConfig {
  type: BuildType;
  dockerfile?: string;
  buildpack?: string;
  context?: string;
  build_args?: Record<string, string>;
  target?: string;
}

export interface Service {
  id: UUID;
  project_id: UUID;
  name: string;
  git_repo: string;
  app_path?: string;
  watch_paths?: string[];
  build_config: BuildConfig;
  health: HealthStatus;
  status: string;
  desired_replicas: number;
  ready_replicas: number;
  auto_deploy: boolean;
  auto_deploy_branch?: string;
  auto_deploy_env?: string;
  created_at: ISODateTime;
  updated_at: ISODateTime;
}

export interface CreateServiceRequest {
  name: string;
  git_repo: string;
  build_config?: Partial<BuildConfig>;
  app_path?: string;
}

// -----------------------------------------------------------------------------
// Releases and deployments
// -----------------------------------------------------------------------------

export type ReleaseStatus = 'building' | 'ready' | 'failed';

export interface Release {
  id: UUID;
  service_id: UUID;
  version: string;
  image_uri: string;
  git_sha: string;
  git_branch?: string;
  commit_message?: string;
  commit_author_name?: string;
  pr_number?: number;
  pr_title?: string;
  pr_url?: string;
  repo_url?: string;
  status: ReleaseStatus;
  error_message?: string | null;
  created_at: ISODateTime;
  updated_at: ISODateTime;
}

export type DeploymentStatus =
  | 'pending'
  | 'deploying'
  | 'running'
  | 'failed'
  | 'rolled_back'
  | 'superseded';

export interface Deployment {
  id: UUID;
  release_id: UUID;
  environment_id: UUID;
  service_id?: UUID;
  /**
   * Heroku-style monotonic version per service (v1, v2, ...). Nullable for
   * historical rows before P2.6 backfill landed.
   */
  version_number?: number | null;
  group_id?: UUID | null;
  deploy_order: number;
  replicas: number;
  status: DeploymentStatus;
  health: HealthStatus;
  error_message?: string | null;
  created_at: ISODateTime;
  updated_at: ISODateTime;
}

export interface DeployRequest {
  release_id: string;
  environment_name: string;
  environment?: Record<string, string>;
  replicas?: number;
}

/** Classic manifest-commit rollback (slow path). */
export interface RollbackRequest {
  to_release?: string;
}

/** Instant (P0.5) selector-flip rollback request. */
export interface InstantRollbackRequest {
  target_deployment_id: string;
  reason?: string;
  /** Required for production environments. */
  change_ticket_url?: string;
}

export interface InstantRollbackResponse {
  message: string;
  took_ms: number;
  scaled_up: boolean;
  from_deployment_id?: string;
  to_deployment_id: string;
  /** v-number of the deployment being rolled back from. */
  from_version?: number | null;
  /** v-number of the target deployment. */
  to_version?: number | null;
  ready_replicas: number;
  strategy: string;
  namespace: string;
}

// -----------------------------------------------------------------------------
// Canary (P2.7)
// -----------------------------------------------------------------------------

export type CanaryRolloutState =
  | 'pending'
  | 'running'
  | 'validating'
  | 'promoting'
  | 'succeeded'
  | 'auto_rolled_back'
  | 'manual_rolled_back'
  | 'failed';

export interface CanaryStartRequest {
  /** Image digest of the candidate — must already be built via a Release. */
  digest: string;
  /** 5-50 — percentage of traffic to shift to the canary. */
  percentage: number;
  /** 1-60 — how long the canary must stay healthy before auto-promotion. */
  validation_window_minutes?: number;
  /** Path on the canary service (e.g. "/health/deep") that must return 200. */
  smoke_endpoint?: string;
  /** 0.0-0.5 — max fraction of 5xx responses during validation. Defaults to 0.05. */
  error_rate_threshold?: number;
  environment_name?: string;
  change_ticket_url?: string;
  total_replicas?: number;
}

export interface CanaryRollout {
  id: UUID;
  service_id: UUID;
  environment_id: UUID;
  stable_deployment_id: UUID;
  canary_deployment_id: UUID;
  new_stable_deployment_id?: UUID | null;
  canary_digest: string;
  canary_percentage: number;
  total_replicas: number;
  canary_replicas: number;
  stable_replicas: number;
  validation_window_seconds: number;
  smoke_endpoint?: string;
  error_rate_threshold: number;
  state: CanaryRolloutState;
  started_at?: ISODateTime | null;
  validating_started_at?: ISODateTime | null;
  promoting_started_at?: ISODateTime | null;
  terminal_at?: ISODateTime | null;
  change_ticket_url?: string;
  last_error?: string;
  rollback_reason?: string;
  /** Computed = canary_replicas / total_replicas. */
  actual_percentage?: number;
  created_at: ISODateTime;
  updated_at: ISODateTime;
}

// -----------------------------------------------------------------------------
// Logs
// -----------------------------------------------------------------------------

export type LogLevel = 'debug' | 'info' | 'warn' | 'error' | 'fatal';

export interface LogEntry {
  timestamp: ISODateTime;
  pod: string;
  message: string;
  level?: LogLevel | string;
  /** Container name (for multi-container pods). */
  container?: string;
}

export interface LogHistoryOptions {
  /** Max rows to return; server-side cap applies. */
  limit?: number;
  /** Filter to rows at or above this level. */
  level?: LogLevel | string;
  /** ISO-8601 lower bound (inclusive). */
  since?: string;
  /** ISO-8601 upper bound (exclusive). */
  until?: string;
  /** Pagination cursor. */
  cursor?: string;
}

export interface LogTailOptions {
  /** Filter to rows at or above this level. */
  level?: LogLevel | string;
  /** Specific pod name to follow (otherwise all). */
  pod?: string;
  /** Container name (for multi-container pods). */
  container?: string;
  /** Abort signal for graceful shutdown. */
  signal?: AbortSignal;
}

// -----------------------------------------------------------------------------
// Audit / activity log
// -----------------------------------------------------------------------------

export interface AuditEvent {
  id: UUID;
  actor_id?: UUID;
  actor_email?: string;
  action: string;
  resource_type: string;
  resource_id?: string;
  project_id?: UUID;
  service_id?: UUID;
  metadata?: Record<string, unknown>;
  created_at: ISODateTime;
}

export interface AuditQueryOptions {
  action?: string;
  resource_type?: string;
  project_id?: string;
  actor_id?: string;
  limit?: number;
  cursor?: string;
}

// -----------------------------------------------------------------------------
// Outbound lifecycle webhooks (P2.3)
// -----------------------------------------------------------------------------

export type OutboundWebhookEventType =
  | 'deploy.started'
  | 'deploy.succeeded'
  | 'deploy.failed'
  | 'rollback.succeeded'
  | 'secret.rotated'
  | 'service.scaled';

export type OutboundWebhookDeliveryStatus =
  | 'pending'
  | 'delivering'
  | 'delivered'
  | 'failed'
  | 'dlq';

export interface OutboundWebhookSubscription {
  id: UUID;
  project_id: UUID;
  name: string;
  url: string;
  /** First 8 chars of SHA-256(secret); use to identify which secret is active. */
  secret_sha256_prefix: string;
  /** Empty array = subscribe to all event types. */
  event_types: OutboundWebhookEventType[];
  active: boolean;
  created_by: string;
  created_at: ISODateTime;
  updated_at: ISODateTime;
  last_success_at?: ISODateTime | null;
  last_failure_at?: ISODateTime | null;
  consecutive_failures: number;
  auto_disabled_at?: ISODateTime | null;
}

export interface CreateWebhookSubscriptionRequest {
  name: string;
  /** Must be https:// */
  url: string;
  event_types?: OutboundWebhookEventType[];
}

export interface UpdateWebhookSubscriptionRequest {
  name?: string;
  url?: string;
  event_types?: OutboundWebhookEventType[];
  active?: boolean;
}

export interface CreateWebhookSubscriptionResponse {
  subscription: OutboundWebhookSubscription;
  /**
   * Raw HMAC signing secret. **Returned exactly once.** Persist immediately —
   * the server stores only a SHA-256 hash and cannot return this value again.
   */
  signing_secret: string;
  note: string;
}

export interface OutboundWebhookDelivery {
  id: UUID;
  subscription_id: UUID;
  lifecycle_event_id?: UUID | null;
  event_id: string;
  event_type: OutboundWebhookEventType | string;
  payload?: Record<string, unknown>;
  payload_sha256: string;
  attempt_number: number;
  status: OutboundWebhookDeliveryStatus;
  http_status?: number | null;
  response_snippet?: string;
  error_message?: string;
  attempted_at?: ISODateTime | null;
  delivered_at?: ISODateTime | null;
  duration_ms?: number | null;
  next_retry_at?: ISODateTime | null;
  created_at: ISODateTime;
}

/** Canonical JSON body posted to a subscriber. */
export interface OutboundWebhookEnvelope<TData = Record<string, unknown>> {
  id: string;
  type: OutboundWebhookEventType | string;
  created_at: ISODateTime;
  /** Matches `OutboundWebhookAPIVersion` on the server; bumped on breaking changes. */
  api_version: string;
  data: TData;
}

// -----------------------------------------------------------------------------
// Secrets (via RFC 0005 bridge)
// -----------------------------------------------------------------------------

export interface EnvVar {
  id: UUID;
  service_id: UUID;
  key: string;
  /** Present only when value is not a secret or caller has reveal permission. */
  value?: string;
  is_secret: boolean;
  created_at: ISODateTime;
  updated_at: ISODateTime;
}

export interface SetEnvVarRequest {
  key: string;
  value: string;
  is_secret?: boolean;
}

// -----------------------------------------------------------------------------
// Preview environments
// -----------------------------------------------------------------------------

export type PreviewEnvironmentStatus =
  | 'pending'
  | 'building'
  | 'deploying'
  | 'active'
  | 'sleeping'
  | 'failed'
  | 'closed';

export interface PreviewEnvironment {
  id: UUID;
  project_id: UUID;
  service_id: UUID;
  pr_number: number;
  pr_title?: string;
  pr_url?: string;
  pr_author?: string;
  pr_author_avatar_url?: string;
  pr_branch: string;
  pr_base_branch: string;
  commit_sha: string;
  commit_url?: string;
  repository_url?: string;
  preview_subdomain: string;
  preview_url: string;
  status: PreviewEnvironmentStatus;
  status_message?: string;
  auto_sleep_after: number;
  last_accessed_at?: ISODateTime;
  sleeping_since?: ISODateTime;
  deployment_id?: UUID;
  build_logs_url?: string;
  created_at: ISODateTime;
  updated_at: ISODateTime;
  closed_at?: ISODateTime;
}

export interface PreviewEnvironmentListResponse {
  previews: PreviewEnvironment[];
  count?: number;
}

export interface CreatePreviewRequest {
  service_id: UUID;
  pr_number: number;
  pr_title?: string;
  pr_url?: string;
  pr_author?: string;
  pr_branch: string;
  pr_base_branch?: string;
  commit_sha: string;
}

export interface PreviewComment {
  id: UUID;
  preview_id: UUID;
  user_id?: UUID;
  user_email: string;
  user_name?: string;
  content: string;
  path?: string;
  resolved: boolean;
  created_at: ISODateTime;
  updated_at: ISODateTime;
}

export interface PreviewCommentListResponse {
  comments: PreviewComment[];
  count?: number;
}

// -----------------------------------------------------------------------------
// Billing (Waybill proxy via Switchyard)
// -----------------------------------------------------------------------------

export type BudgetPeriod = 'monthly' | 'weekly' | 'quarterly';

/** Project spend budget configured in Waybill. */
export interface ProjectBudget {
  id: UUID;
  project_id: UUID;
  amount_cents: number;
  currency: string;
  period: BudgetPeriod;
  alert_thresholds: number[];
  hard_throttle: boolean;
  created_at: ISODateTime;
  updated_at: ISODateTime;
}

export interface ProjectBudgetListResponse {
  budgets: ProjectBudget[];
}

export interface CreateProjectBudgetRequest {
  amount_cents: number;
  currency?: string;
  period?: BudgetPeriod;
  alert_thresholds?: number[];
  hard_throttle?: boolean;
}

export interface UpdateProjectBudgetRequest {
  amount_cents?: number;
  currency?: string;
  period?: BudgetPeriod;
  alert_thresholds?: number[];
  hard_throttle?: boolean;
}

/** Aggregated cost for a project over a billing period. */
export interface ProjectCostResponse {
  project_id: UUID;
  period_start: ISODateTime;
  period_end: ISODateTime;
  total_cents: number;
  group_by: string;
  series?: Array<{
    bucket: string;
    cost_cents: number;
    by_metric?: Record<string, number>;
  }>;
  breakdown?: Array<{ key: string; cost_cents: number }>;
}

/** Budget threshold alert dispatched (or pending) for a period. */
export interface BudgetAlertEvent {
  id: UUID;
  budget_id: UUID;
  project_id: UUID;
  period_start: ISODateTime;
  period_end: ISODateTime;
  threshold: number;
  actual_cents: number;
  budget_cents: number;
  dispatched_at?: ISODateTime | null;
  dispatch_attempts: number;
  last_error?: string;
  created_at: ISODateTime;
}

export interface BudgetAlertListResponse {
  alerts: BudgetAlertEvent[];
}

/** Active or historical deploy throttle when a budget is exceeded. */
export interface BudgetThrottle {
  id: UUID;
  project_id: UUID;
  reason: string;
  env_scope: string;
  activated_at: ISODateTime;
  cleared_at?: ISODateTime | null;
}

export interface BudgetThrottleListResponse {
  throttles: BudgetThrottle[];
}

// -----------------------------------------------------------------------------
// Persistent volumes (service spec)
// -----------------------------------------------------------------------------

export interface ServiceVolume {
  name: string;
  mount_path: string;
  size: string;
  storage_class_name?: string;
  access_mode?: string;
}

// -----------------------------------------------------------------------------
// Jobs / Timetable
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
