/**
 * Lifecycle event types — mirror of `packages/sdk-go/pkg/types/deployment_lifecycle.go`
 *
 * The Switchyard API exposes the canonical deployment lifecycle event chain
 * via `GET /v1/lifecycle/timeline/:owner/:repo`. The shape here mirrors the
 * Go `DeploymentLifecycleEvent` struct returned in the `events` array.
 *
 * Source of truth: apps/switchyard-api/internal/api/lifecycle_event_handlers.go
 */

// ---------------------------------------------------------------------------
// Event type constants — values match Go side (see deployment_lifecycle.go).
// ---------------------------------------------------------------------------

export const LIFECYCLE_EVENT_TYPES = [
  'push_received',
  'build_started',
  'build_succeeded',
  'build_failed',
  'image_pushed',
  'digest_committed',
  'deploy_started',
  'deploy_synced',
  'deploy_healthy',
  'deploy_degraded',
  'deploy_failed',
  'preview_created',
  'preview_destroyed',
] as const;

export type LifecycleEventType = (typeof LIFECYCLE_EVENT_TYPES)[number];

/**
 * Coarse categories used for color/icon selection. Rollback events
 * (`deploy.rolled_back`, `deploy.rollback_failed`) are emitted with
 * ad-hoc strings outside the LIFECYCLE_EVENT_TYPES catalogue; we
 * accept the raw string and classify defensively.
 */
export type LifecycleEventCategory =
  | 'success'
  | 'failure'
  | 'in_progress'
  | 'neutral';

export const LIFECYCLE_RESULT_FILTERS = ['all', 'success', 'failure'] as const;
export type LifecycleResultFilter = (typeof LIFECYCLE_RESULT_FILTERS)[number];

export const LIFECYCLE_SINCE_OPTIONS = ['24h', '7d', '30d', 'all'] as const;
export type LifecycleSinceOption = (typeof LIFECYCLE_SINCE_OPTIONS)[number];

// ---------------------------------------------------------------------------
// Event shape — matches the JSON returned by GetLifecycleTimeline.
// ---------------------------------------------------------------------------

export interface LifecycleEvent {
  id: string;
  deployment_id?: string | null;
  release_id?: string | null;
  ci_run_id?: string | null;
  project_id?: string | null;
  service_id?: string | null;
  repo_full_name: string;
  commit_sha: string;
  branch: string;
  ref: string;
  target_env?: string | null;
  /**
   * One of LIFECYCLE_EVENT_TYPES, but kept loose because the API also
   * emits ad-hoc rollback events (`deploy.rolled_back`,
   * `deploy.rollback_failed`) that aren't in the constant catalogue.
   */
  event_type: LifecycleEventType | string;
  source: string;
  message?: string | null;
  metadata?: Record<string, unknown> | null;
  created_at: string;
}

export interface LifecycleTimelineResponse {
  repo: string;
  count: number;
  events: LifecycleEvent[];
}

// ---------------------------------------------------------------------------
// Helpers — pure, easy to unit test.
// ---------------------------------------------------------------------------

const SUCCESS_EVENTS = new Set<string>([
  'build_succeeded',
  'image_pushed',
  'digest_committed',
  'deploy_synced',
  'deploy_healthy',
  'preview_created',
  'deploy.rolled_back',
]);

const FAILURE_EVENTS = new Set<string>([
  'build_failed',
  'deploy_failed',
  'deploy_degraded',
  'deploy.rollback_failed',
]);

const IN_PROGRESS_EVENTS = new Set<string>([
  'build_started',
  'deploy_started',
]);

/**
 * Categorize a lifecycle event by its semantic outcome. Used for
 * color/icon selection in the timeline UI.
 */
export function lifecycleEventCategory(
  eventType: string,
): LifecycleEventCategory {
  if (SUCCESS_EVENTS.has(eventType)) return 'success';
  if (FAILURE_EVENTS.has(eventType)) return 'failure';
  if (IN_PROGRESS_EVENTS.has(eventType)) return 'in_progress';
  return 'neutral';
}

/**
 * Human-readable labels for known event types. Unknown types fall back
 * to a title-cased version of the raw string.
 */
const EVENT_LABELS: Record<string, string> = {
  push_received: 'Push received',
  build_started: 'Build started',
  build_succeeded: 'Build succeeded',
  build_failed: 'Build failed',
  image_pushed: 'Image pushed',
  digest_committed: 'Digest committed',
  deploy_started: 'Deploy started',
  deploy_synced: 'Deploy synced',
  deploy_healthy: 'Deploy healthy',
  deploy_degraded: 'Deploy degraded',
  deploy_failed: 'Deploy failed',
  preview_created: 'Preview created',
  preview_destroyed: 'Preview destroyed',
  'deploy.rolled_back': 'Rolled back',
  'deploy.rollback_failed': 'Rollback failed',
};

export function lifecycleEventLabel(eventType: string): string {
  if (EVENT_LABELS[eventType]) return EVENT_LABELS[eventType];
  // Title-case fallback for unknown event types.
  return eventType
    .replace(/[._]/g, ' ')
    .replace(/\b\w/g, (c) => c.toUpperCase());
}

/**
 * Group a sorted (most-recent-first) list of events into "deployment
 * groups" by contiguous runs of the same git SHA. Returns groups in
 * the same order as input — newest deployment first.
 *
 * If `pr_number` is present in metadata for a contiguous run we prefer
 * grouping by that, but the per-event group key always falls back to
 * git SHA for events that don't carry a PR number.
 */
export interface LifecycleEventGroup {
  /** Stable group key (git SHA fallback). */
  key: string;
  git_sha: string;
  branch: string;
  pr_number?: number;
  pr_title?: string;
  pr_url?: string;
  author?: string;
  /** Newest event first within the group. */
  events: LifecycleEvent[];
  /** First (chronologically earliest) created_at in the group. */
  earliest_at: string;
  /** Latest (chronologically newest) created_at in the group. */
  latest_at: string;
}

function metaString(
  meta: Record<string, unknown> | null | undefined,
  key: string,
): string | undefined {
  if (!meta) return undefined;
  const v = meta[key];
  return typeof v === 'string' && v.length > 0 ? v : undefined;
}

function metaNumber(
  meta: Record<string, unknown> | null | undefined,
  key: string,
): number | undefined {
  if (!meta) return undefined;
  const v = meta[key];
  if (typeof v === 'number') return v;
  if (typeof v === 'string' && /^\d+$/.test(v)) return parseInt(v, 10);
  return undefined;
}

export function groupLifecycleEvents(
  events: LifecycleEvent[],
): LifecycleEventGroup[] {
  if (events.length === 0) return [];

  const groups: LifecycleEventGroup[] = [];
  let current: LifecycleEventGroup | null = null;

  for (const event of events) {
    const sha = event.commit_sha || '(unknown)';
    if (current && current.git_sha === sha) {
      current.events.push(event);
      // events are newest-first, so the group's latest_at stays the
      // first event we saw and earliest_at moves down as we go.
      if (event.created_at < current.earliest_at) {
        current.earliest_at = event.created_at;
      }
      if (event.created_at > current.latest_at) {
        current.latest_at = event.created_at;
      }
    } else {
      current = {
        key: sha + ':' + event.created_at,
        git_sha: sha,
        branch: event.branch,
        pr_number: metaNumber(event.metadata, 'pr_number'),
        pr_title: metaString(event.metadata, 'pr_title'),
        pr_url: metaString(event.metadata, 'pr_url'),
        author: metaString(event.metadata, 'author'),
        events: [event],
        earliest_at: event.created_at,
        latest_at: event.created_at,
      };
      groups.push(current);
    }
    // Backfill PR/author from any event in the group that has it.
    if (current.pr_number == null) {
      current.pr_number = metaNumber(event.metadata, 'pr_number');
    }
    if (!current.pr_title) {
      current.pr_title = metaString(event.metadata, 'pr_title');
    }
    if (!current.pr_url) {
      current.pr_url = metaString(event.metadata, 'pr_url');
    }
    if (!current.author) {
      current.author = metaString(event.metadata, 'author');
    }
  }
  return groups;
}

/** 7-character short SHA, or the full string if shorter. */
export function shortSHA(sha: string): string {
  return sha && sha.length > 7 ? sha.slice(0, 7) : sha || '';
}
