'use client';

/**
 * P2.1 in-UI log viewer. Full-bleed, Loki-backed, virtualized.
 *
 * Design notes:
 *   - URL-synced state via next/navigation so links are shareable.
 *   - Virtualization is hand-rolled (no react-window dep) via a small
 *     windowing hook. Rows are fixed-height so the math stays trivial;
 *     wrap on long lines is opt-in per row via the "expand" click.
 *   - Live tail uses a WebSocket that reconnects with exponential
 *     backoff. A server-side heartbeat keeps the connection live.
 *   - Auto-scroll pauses when the user scrolls up. A "N new" pill
 *     appears to let them jump back to bottom.
 *   - Dropped frames from the backend (see backend doc on
 *     backpressure) render as a synthetic row so the user sees the gap.
 */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useRouter, useSearchParams } from 'next/navigation';

import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { API_BASE_URL } from '@/lib/constants';
import { apiGet } from '@/lib/api';

import { parseAnsi } from './ansi';
import {
  ALL_LEVELS,
  type LogEntry,
  type LogLevel,
  type LogQueryResponse,
  type LogRow,
  type TailFrame,
  type TimeRangeKey,
  TIME_RANGE_LABEL,
} from './types';

// ---------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------

export interface LogViewerProps {
  /** Switchyard service UUID. */
  serviceId: string;
  /** Display name, used for download filename + heading. */
  serviceName: string;
  /** Environment name (e.g., "production"). Wires into namespace. */
  env: string;
}

// ---------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------

const ROW_HEIGHT = 22;        // px — monospace + 2px line padding
const OVERSCAN = 10;          // rows rendered above/below viewport
const MAX_ROWS_IN_MEMORY = 5000; // hard cap to keep memory bounded
const HISTORY_PAGE_SIZE = 500;
const WS_BACKOFF_BASE_MS = 1000;
const WS_BACKOFF_MAX_MS = 30000;
const DEBOUNCE_SEARCH_MS = 300;

// ---------------------------------------------------------------------
// URL-sync helpers
// ---------------------------------------------------------------------

function parseTimeRange(v: string | null): TimeRangeKey {
  switch (v) {
    case 'live':
    case '15m':
    case '1h':
    case '6h':
    case '24h':
      return v;
    default:
      return 'live';
  }
}

function parseLevelsParam(sp: URLSearchParams): LogLevel[] {
  const raw = sp.getAll('level');
  const valid = raw.filter((l): l is LogLevel =>
    (ALL_LEVELS as readonly string[]).includes(l),
  );
  return valid;
}

// ---------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------

export function LogViewer({ serviceId, serviceName, env }: LogViewerProps) {
  const router = useRouter();
  const searchParams = useSearchParams();

  // --- URL-backed state -------------------------------------------------
  const initialTimeRange = parseTimeRange(searchParams.get('since'));
  const initialLevels = parseLevelsParam(new URLSearchParams(searchParams.toString()));
  const initialSearch = searchParams.get('search') ?? '';

  const [timeRange, setTimeRange] = useState<TimeRangeKey>(initialTimeRange);
  const [levels, setLevels] = useState<LogLevel[]>(initialLevels);
  const [search, setSearch] = useState(initialSearch);
  const [debouncedSearch, setDebouncedSearch] = useState(initialSearch);
  const [liveTail, setLiveTail] = useState(initialTimeRange === 'live');

  // --- Viewport state ---------------------------------------------------
  const [rows, setRows] = useState<LogRow[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<{ message: string; retriable: boolean } | null>(null);
  const [nextCursor, setNextCursor] = useState<string | undefined>(undefined);
  const [autoScroll, setAutoScroll] = useState(true);
  const [newRowsBelow, setNewRowsBelow] = useState(0);
  const [rateLimitedUntil, setRateLimitedUntil] = useState<number | null>(null);

  // --- Refs -------------------------------------------------------------
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const wsBackoffRef = useRef(WS_BACKOFF_BASE_MS);
  const rowIdRef = useRef(0);
  const mountedRef = useRef(true);

  // --- URL sync effect --------------------------------------------------
  // Debounced write-back to URL; avoid thrashing history during typing.
  useEffect(() => {
    const params = new URLSearchParams();
    params.set('since', timeRange);
    for (const l of levels) params.append('level', l);
    if (debouncedSearch) params.set('search', debouncedSearch);
    router.replace(`?${params.toString()}`, { scroll: false });
  }, [timeRange, levels, debouncedSearch, router]);

  // --- Search debounce --------------------------------------------------
  useEffect(() => {
    const t = setTimeout(() => setDebouncedSearch(search), DEBOUNCE_SEARCH_MS);
    return () => clearTimeout(t);
  }, [search]);

  // --- Fetching historical window --------------------------------------
  /**
   * Fetch a window of history. When liveTail is also active, this populates
   * the initial buffer; the WS then appends new entries on top. `reset`
   * means "replace the row buffer", otherwise "append".
   */
  const fetchWindow = useCallback(
    async (opts: { reset: boolean; cursor?: string }) => {
      setLoading(true);
      setError(null);
      try {
        const params = new URLSearchParams();
        if (timeRange !== 'live') params.set('since', timeRange);
        for (const l of levels) params.append('level', l);
        if (debouncedSearch) params.set('search', debouncedSearch);
        params.set('limit', String(HISTORY_PAGE_SIZE));
        params.set('env', env);
        if (opts.cursor) params.set('cursor', opts.cursor);

        const data = await apiGet<LogQueryResponse>(
          `/v1/services/${serviceId}/logs?${params.toString()}`,
        );
        if (!mountedRef.current) return;

        const newRows = data.entries.map(entryToRow);
        setRows((prev) =>
          opts.reset ? newRows : boundRows([...prev, ...newRows]),
        );
        setNextCursor(data.next_cursor);
      } catch (err: unknown) {
        if (!mountedRef.current) return;
        const msg = err instanceof Error ? err.message : 'Failed to load logs';
        // 503 -> retriable; all others are user-visible errors.
        const retriable = /503|unavailable/i.test(msg);
        setError({ message: msg, retriable });
        // 429 -> compute cooldown from Retry-After when the lib surfaces it.
        if (/429|too many|rate/i.test(msg)) {
          setRateLimitedUntil(Date.now() + 15_000);
        }
      } finally {
        if (mountedRef.current) setLoading(false);
      }
    },
    [serviceId, env, timeRange, levels, debouncedSearch],
  );

  // --- Initial + filter-change fetch -----------------------------------
  useEffect(() => {
    mountedRef.current = true;
    fetchWindow({ reset: true });
    return () => {
      mountedRef.current = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serviceId, env, timeRange, levels, debouncedSearch]);

  // --- Live tail WebSocket ---------------------------------------------
  useEffect(() => {
    if (!liveTail) {
      // Stop any running WS when the user toggles off.
      if (wsRef.current) {
        wsRef.current.close();
        wsRef.current = null;
      }
      return;
    }

    let cancelled = false;
    const connect = () => {
      if (cancelled) return;
      const wsProtocol = API_BASE_URL.startsWith('https') ? 'wss' : 'ws';
      const base = API_BASE_URL.replace(/^https?:\/\//, '');
      const params = new URLSearchParams();
      for (const l of levels) params.append('level', l);
      if (debouncedSearch) params.set('search', debouncedSearch);
      params.set('env', env);

      // WebSocket auth: same convention as the existing LogsTab — token
      // in query param since the browser WS API can't set headers.
      try {
        const stored = localStorage.getItem('enclii_tokens');
        if (stored) {
          const tokens = JSON.parse(stored);
          if (tokens.accessToken) params.append('token', tokens.accessToken);
        }
      } catch {
        // ignore
      }

      const url = `${wsProtocol}://${base}/v1/services/${serviceId}/logs/tail?${params.toString()}`;
      const ws = new WebSocket(url);
      wsRef.current = ws;

      ws.onopen = () => {
        wsBackoffRef.current = WS_BACKOFF_BASE_MS;
        setError(null);
      };
      ws.onmessage = (evt) => {
        try {
          const frame = JSON.parse(evt.data) as TailFrame;
          if (frame.type === 'entry') {
            appendLiveRow(entryToRow(frame.entry));
          } else if (frame.type === 'dropped') {
            appendLiveRow(syntheticRow(`… ${frame.dropped} messages dropped (slow client)`));
          } else if (frame.type === 'error') {
            setError({ message: frame.error, retriable: true });
          } else if (frame.type === 'bye') {
            // Server asking us to go away; don't reconnect immediately.
          }
        } catch {
          // Ignore parse failures — the connection will keep flowing.
        }
      };
      ws.onerror = () => {
        // onclose will fire right after; let it handle reconnect.
      };
      ws.onclose = () => {
        if (cancelled || !liveTail) return;
        const backoff = Math.min(wsBackoffRef.current, WS_BACKOFF_MAX_MS);
        wsBackoffRef.current = Math.min(backoff * 2, WS_BACKOFF_MAX_MS);
        setTimeout(connect, backoff);
      };
    };
    connect();

    return () => {
      cancelled = true;
      if (wsRef.current) {
        wsRef.current.close();
        wsRef.current = null;
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [liveTail, serviceId, env, levels, debouncedSearch]);

  // --- Row helpers ------------------------------------------------------
  const appendLiveRow = useCallback((row: LogRow) => {
    setRows((prev) => boundRows([...prev, row]));
    if (!autoScroll) {
      setNewRowsBelow((n) => n + 1);
    }
  }, [autoScroll]);

  const entryToRow = useCallback((e: LogEntry): LogRow => {
    return {
      id: rowIdRef.current++,
      ts: new Date(e.timestamp),
      tsString: e.timestamp,
      level: e.level,
      pod: e.pod,
      message: e.message,
    };
  }, []);

  const syntheticRow = useCallback((text: string): LogRow => ({
    id: rowIdRef.current++,
    ts: new Date(),
    tsString: new Date().toISOString(),
    level: 'warn',
    message: text,
    synthetic: true,
  }), []);

  // --- Auto-scroll effect ----------------------------------------------
  useEffect(() => {
    if (!autoScroll || !scrollRef.current) return;
    const el = scrollRef.current;
    el.scrollTop = el.scrollHeight;
    if (newRowsBelow !== 0) setNewRowsBelow(0);
  }, [rows, autoScroll, newRowsBelow]);

  // User scroll handler — pauses auto-scroll when they move away from bottom.
  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < ROW_HEIGHT;
    if (atBottom && !autoScroll) {
      setAutoScroll(true);
      setNewRowsBelow(0);
    } else if (!atBottom && autoScroll) {
      setAutoScroll(false);
    }
  }, [autoScroll]);

  const jumpToBottom = useCallback(() => {
    setAutoScroll(true);
    setNewRowsBelow(0);
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, []);

  // --- Virtualization --------------------------------------------------
  const [viewportTop, setViewportTop] = useState(0);
  const [viewportHeight, setViewportHeight] = useState(600);

  const updateViewportMetrics = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    setViewportTop(el.scrollTop);
    setViewportHeight(el.clientHeight);
  }, []);

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    updateViewportMetrics();
    const onResize = () => updateViewportMetrics();
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, [updateViewportMetrics]);

  const windowStart = Math.max(0, Math.floor(viewportTop / ROW_HEIGHT) - OVERSCAN);
  const windowEnd = Math.min(
    rows.length,
    Math.ceil((viewportTop + viewportHeight) / ROW_HEIGHT) + OVERSCAN,
  );
  const visibleRows = rows.slice(windowStart, windowEnd);
  const topPad = windowStart * ROW_HEIGHT;
  const bottomPad = (rows.length - windowEnd) * ROW_HEIGHT;

  // --- Download --------------------------------------------------------
  const downloadJSONL = useCallback(() => {
    const lines = rows
      .filter((r) => !r.synthetic)
      .map((r) =>
        JSON.stringify({
          timestamp: r.tsString,
          level: r.level,
          pod: r.pod,
          message: r.message,
        }),
      )
      .join('\n');
    const blob = new Blob([lines], { type: 'application/x-ndjson' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${serviceName}-${env}-${new Date().toISOString().slice(0, 10)}.jsonl`;
    a.click();
    URL.revokeObjectURL(url);
  }, [rows, serviceName, env]);

  // --- Render ----------------------------------------------------------
  return (
    <div className="flex h-full flex-col">
      <Toolbar
        timeRange={timeRange}
        onTimeRange={(v) => {
          setTimeRange(v);
          setLiveTail(v === 'live');
        }}
        levels={levels}
        onLevels={setLevels}
        search={search}
        onSearch={setSearch}
        liveTail={liveTail}
        onLiveTail={setLiveTail}
        onDownload={downloadJSONL}
        rowCount={rows.length}
        rateLimitedUntil={rateLimitedUntil}
      />

      {error && (
        <ErrorBanner
          error={error}
          onRetry={() => fetchWindow({ reset: true })}
        />
      )}

      <div
        ref={(el) => {
          scrollRef.current = el;
          if (el) updateViewportMetrics();
        }}
        onScroll={(e) => {
          handleScroll();
          setViewportTop((e.target as HTMLDivElement).scrollTop);
        }}
        className="relative flex-1 overflow-y-auto overflow-x-hidden bg-gray-950 font-mono text-xs text-gray-100"
        aria-label={`Logs for ${serviceName} in ${env}`}
        role="log"
        aria-live={liveTail ? 'polite' : 'off'}
      >
        {loading && rows.length === 0 ? (
          <LoadingSkeleton />
        ) : rows.length === 0 ? (
          <EmptyState />
        ) : (
          <div style={{ paddingTop: topPad, paddingBottom: bottomPad }}>
            {visibleRows.map((row) => (
              <LogRowItem key={row.id} row={row} />
            ))}
          </div>
        )}

        {!autoScroll && newRowsBelow > 0 && (
          <button
            onClick={jumpToBottom}
            className="fixed bottom-6 left-1/2 z-10 -translate-x-1/2 rounded-full bg-blue-600 px-4 py-2 text-sm text-white shadow-lg hover:bg-blue-500"
            type="button"
          >
            ↓ {newRowsBelow} new
          </button>
        )}
      </div>

      {/* Status bar — always visible so the user knows what they're looking at. */}
      <div className="flex items-center justify-between border-t border-gray-800 bg-gray-900 px-3 py-1.5 text-xs text-gray-400">
        <span>{rows.length.toLocaleString()} entries</span>
        <span>
          {liveTail ? (
            <span className="text-green-400">● Live</span>
          ) : (
            <span>○ Paused</span>
          )}
        </span>
        {nextCursor && !liveTail && (
          <Button
            size="sm"
            variant="ghost"
            onClick={() => fetchWindow({ reset: false, cursor: nextCursor })}
            disabled={loading}
          >
            Load older
          </Button>
        )}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------

function boundRows(rows: LogRow[]): LogRow[] {
  if (rows.length <= MAX_ROWS_IN_MEMORY) return rows;
  return rows.slice(rows.length - MAX_ROWS_IN_MEMORY);
}

function levelClass(level: LogLevel): string {
  switch (level) {
    case 'error':
      return 'text-red-400';
    case 'warn':
      return 'text-yellow-400';
    case 'debug':
      return 'text-gray-500';
    default:
      return 'text-gray-200';
  }
}

function levelLabel(level: LogLevel): string {
  return level.toUpperCase().padEnd(5);
}

function LogRowItem({ row }: { row: LogRow }) {
  const segs = useMemo(() => parseAnsi(row.message), [row.message]);
  return (
    <div
      className={`flex gap-3 px-3 py-0.5 hover:bg-gray-900 ${
        row.synthetic ? 'italic text-gray-500' : ''
      }`}
      style={{ height: ROW_HEIGHT, lineHeight: `${ROW_HEIGHT - 2}px` }}
    >
      <span className="shrink-0 tabular-nums text-gray-500">
        {formatTs(row.ts)}
      </span>
      <span className={`shrink-0 tabular-nums ${levelClass(row.level)}`}>
        {levelLabel(row.level)}
      </span>
      {row.pod && (
        <span className="shrink-0 text-cyan-400">{row.pod}</span>
      )}
      <span className="flex-1 truncate whitespace-pre">
        {segs.map((s, i) => (
          <span key={i} className={s.className} style={s.style}>
            {s.text}
          </span>
        ))}
      </span>
    </div>
  );
}

function formatTs(ts: Date): string {
  const hh = String(ts.getHours()).padStart(2, '0');
  const mm = String(ts.getMinutes()).padStart(2, '0');
  const ss = String(ts.getSeconds()).padStart(2, '0');
  const ms = String(ts.getMilliseconds()).padStart(3, '0');
  return `${hh}:${mm}:${ss}.${ms}`;
}

// Fixed widths avoid Math.random() at render time (non-deterministic
// and flagged by react-hooks/purity). Variety comes from the table, not
// the roll of a die.
const SKELETON_WIDTHS = ['74%', '62%', '88%', '70%', '55%', '81%', '67%', '92%', '64%', '77%'];

function LoadingSkeleton() {
  return (
    <div className="space-y-1 p-4" aria-busy="true">
      {SKELETON_WIDTHS.map((w, i) => (
        <div
          key={i}
          className="bg-muted h-4 animate-pulse rounded"
          style={{ width: w }}
        />
      ))}
    </div>
  );
}

function EmptyState() {
  return (
    <div className="flex h-full items-center justify-center p-8 text-gray-500">
      <div className="text-center">
        <p className="text-sm">No logs in this window</p>
        <p className="mt-2 text-xs">
          Try widening the time range or clearing the search filter.
        </p>
      </div>
    </div>
  );
}

function ErrorBanner({
  error,
  onRetry,
}: {
  error: { message: string; retriable: boolean };
  onRetry: () => void;
}) {
  return (
    <div className="border-b border-red-500/30 bg-red-950/60 px-4 py-2 text-sm text-red-200">
      <div className="flex items-center justify-between gap-4">
        <span>
          {error.retriable
            ? 'Logs are temporarily unavailable.'
            : `Couldn't load logs: ${error.message}`}
        </span>
        <Button size="sm" variant="outline" onClick={onRetry}>
          Retry
        </Button>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------
// Toolbar
// ---------------------------------------------------------------------

interface ToolbarProps {
  timeRange: TimeRangeKey;
  onTimeRange: (v: TimeRangeKey) => void;
  levels: LogLevel[];
  onLevels: (v: LogLevel[]) => void;
  search: string;
  onSearch: (v: string) => void;
  liveTail: boolean;
  onLiveTail: (v: boolean) => void;
  onDownload: () => void;
  rowCount: number;
  rateLimitedUntil: number | null;
}

function Toolbar(props: ToolbarProps) {
  const [rateLimitCountdown, setRateLimitCountdown] = useState(0);

  useEffect(() => {
    if (!props.rateLimitedUntil) return;
    const t = setInterval(() => {
      const remaining = Math.max(0, props.rateLimitedUntil! - Date.now());
      setRateLimitCountdown(Math.ceil(remaining / 1000));
      if (remaining === 0) clearInterval(t);
    }, 500);
    return () => clearInterval(t);
  }, [props.rateLimitedUntil]);

  const toggleLevel = (l: LogLevel) => {
    if (props.levels.includes(l)) {
      props.onLevels(props.levels.filter((x) => x !== l));
    } else {
      props.onLevels([...props.levels, l]);
    }
  };

  return (
    <div className="bg-card sticky top-0 z-20 flex flex-wrap items-center gap-2 border-b px-3 py-2">
      {/* Time range */}
      <select
        value={props.timeRange}
        onChange={(e) => props.onTimeRange(e.target.value as TimeRangeKey)}
        className="bg-background h-8 rounded-md border px-2 text-sm"
        aria-label="Time range"
      >
        {(Object.keys(TIME_RANGE_LABEL) as TimeRangeKey[]).map((k) => (
          <option key={k} value={k}>
            {TIME_RANGE_LABEL[k]}
          </option>
        ))}
      </select>

      {/* Level checkboxes */}
      <div className="flex items-center gap-1 text-xs">
        {ALL_LEVELS.map((lvl) => (
          <label
            key={lvl}
            className="hover:bg-muted flex cursor-pointer items-center gap-1 rounded-md border px-2 py-1"
          >
            <input
              type="checkbox"
              checked={props.levels.includes(lvl)}
              onChange={() => toggleLevel(lvl)}
              aria-label={`${lvl} logs`}
            />
            <span className="capitalize">{lvl}</span>
          </label>
        ))}
      </div>

      {/* Search */}
      <Input
        type="search"
        placeholder="Search… (case-sensitive)"
        value={props.search}
        onChange={(e) => props.onSearch(e.target.value)}
        className="h-8 max-w-xs text-sm"
        aria-label="Search logs"
      />

      {/* Live tail toggle */}
      <Button
        size="sm"
        variant={props.liveTail ? 'default' : 'outline'}
        onClick={() => props.onLiveTail(!props.liveTail)}
        aria-pressed={props.liveTail}
      >
        {props.liveTail ? '● Live' : '○ Paused'}
      </Button>

      {/* Download */}
      <Button
        size="sm"
        variant="outline"
        onClick={props.onDownload}
        disabled={props.rowCount === 0}
      >
        Download .jsonl
      </Button>

      {/* Rate limit notice */}
      {rateLimitCountdown > 0 && (
        <div className="text-xs text-yellow-500">
          Slow down — retry in {rateLimitCountdown}s
        </div>
      )}
    </div>
  );
}
