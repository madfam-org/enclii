/**
 * Wire types for the P2.1 Loki-backed log endpoints. Must stay in sync
 * with apps/switchyard-api/internal/logstream/types.go — if the server
 * shape changes, update both.
 */

export type LogLevel = 'error' | 'warn' | 'info' | 'debug';

export const ALL_LEVELS: readonly LogLevel[] = ['error', 'warn', 'info', 'debug'];

/**
 * Matches logstream.Entry. `timestamp` is RFC3339Nano string — convert
 * to Date at the render edge, not in the transport layer, so sorting
 * and cursor comparisons stay string-precise.
 */
export interface LogEntry {
  timestamp: string;
  level: LogLevel;
  pod?: string;
  container?: string;
  message: string;
  labels?: Record<string, string>;
}

/** Matches logstream.Response. */
export interface LogQueryResponse {
  entries: LogEntry[];
  next_cursor?: string;
  reached_live_tail: boolean;
}

/** Matches logstream.TailFrame. `type` is the discriminator. */
export type TailFrame =
  | { type: 'entry'; entry: LogEntry }
  | { type: 'dropped'; dropped: number }
  | { type: 'error'; error: string }
  | { type: 'ping' }
  | { type: 'bye'; error?: string };

/** Canonical time-range buckets surfaced in the toolbar. */
export type TimeRangeKey = 'live' | '15m' | '1h' | '6h' | '24h';

export const TIME_RANGE_LABEL: Record<TimeRangeKey, string> = {
  live: 'Live',
  '15m': 'Last 15 min',
  '1h': 'Last hour',
  '6h': 'Last 6 hours',
  '24h': 'Last 24 hours',
};

/** Client-side row for rendering — enriches wire entry with a stable
 * key and an already-parsed Date. Using separate types keeps the
 * render loop hot-path allocation-free. */
export interface LogRow {
  id: number;
  ts: Date;
  tsString: string;
  level: LogLevel;
  pod?: string;
  message: string;
  // Set when the row was synthesized (e.g., "N messages dropped") rather
  // than coming from Loki. Styled differently in the viewer.
  synthetic?: boolean;
}
