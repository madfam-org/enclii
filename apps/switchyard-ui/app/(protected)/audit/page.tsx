'use client';

/**
 * Consolidated audit log view (P1.5).
 *
 * Pulls `/v1/audit` from switchyard-api, which merges:
 *   - Janua session events (auth)
 *   - Switchyard lifecycle + request audit (deploy)
 *   - Selva RFC 0005/0006/0007/0008 ledgers (secret/github/config/webhook)
 *
 * URL-synced filters so an auditor can share a link to a specific slice
 * (e.g. `?category=secret&since=2026-04-01`). Cursor-based pagination via
 * a "Load more" button; we intentionally do NOT poll — this is a forensic
 * tool, not a dashboard.
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { useRouter, useSearchParams } from 'next/navigation';

import { Button } from "@enclii/ui-components/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from "@enclii/ui-components/input";
import { Spinner } from '@/components/ui/spinner';
import { StatusBadge } from '@/components/ui/status-badge';
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@enclii/ui-components/table";
import { useAuth } from '@/contexts/AuthContext';
import { apiFetchResponse, apiGet } from '@/lib/api';

// -----------------------------------------------------------------------
// Types (must stay in sync with switchyard-api/internal/audit.AuditEvent)
// -----------------------------------------------------------------------

type AuditSource =
  | 'janua'
  | 'switchyard'
  | 'selva_secret'
  | 'selva_github'
  | 'selva_config'
  | 'selva_webhook';

type AuditCategory = 'auth' | 'deploy' | 'secret' | 'github' | 'config' | 'webhook';

type AuditOutcome = 'success' | 'failure' | 'denied';

interface AuditEvent {
  timestamp: string;
  actor?: string;
  actor_email?: string;
  source: AuditSource;
  category: AuditCategory;
  action: string;
  target?: string;
  outcome: AuditOutcome;
  request_id?: string;
  details?: Record<string, unknown>;
}

interface AuditResponse {
  events: AuditEvent[];
  next_cursor?: string;
  // Populated when an upstream source failed; surfaced as a warning banner.
  source_errors?: Record<string, string>;
}

// -----------------------------------------------------------------------
// Static filter option maps (kept outside the component for stable refs)
// -----------------------------------------------------------------------

const CATEGORY_OPTIONS: { value: AuditCategory; label: string }[] = [
  { value: 'auth', label: 'Authentication' },
  { value: 'deploy', label: 'Deploy / Lifecycle' },
  { value: 'secret', label: 'Secret (RFC 0005)' },
  { value: 'github', label: 'GitHub admin (RFC 0006)' },
  { value: 'config', label: 'ConfigMap / flags (RFC 0007)' },
  { value: 'webhook', label: 'Webhooks (RFC 0008)' },
];

const SOURCE_OPTIONS: { value: AuditSource; label: string }[] = [
  { value: 'janua', label: 'Janua' },
  { value: 'switchyard', label: 'Switchyard' },
  { value: 'selva_secret', label: 'Selva: Secrets' },
  { value: 'selva_github', label: 'Selva: GitHub' },
  { value: 'selva_config', label: 'Selva: ConfigMaps' },
  { value: 'selva_webhook', label: 'Selva: Webhooks' },
];

const PAGE_LIMIT = 100;
const DEFAULT_LOOKBACK_DAYS = 7;

// -----------------------------------------------------------------------
// URL <→ state helpers
// -----------------------------------------------------------------------

function defaultSinceISO(): string {
  const d = new Date();
  d.setDate(d.getDate() - DEFAULT_LOOKBACK_DAYS);
  return d.toISOString();
}

function buildQuery(state: {
  since: string;
  until: string;
  category: AuditCategory | '';
  source: AuditSource | '';
  actor: string;
  target: string;
  cursor?: string;
}): URLSearchParams {
  const params = new URLSearchParams();
  params.set('limit', String(PAGE_LIMIT));
  if (state.since) params.set('since', state.since);
  if (state.until) params.set('until', state.until);
  if (state.category) params.set('category', state.category);
  if (state.source) params.set('source', state.source);
  if (state.actor) params.set('actor', state.actor);
  if (state.target) params.set('target', state.target);
  if (state.cursor) params.set('cursor', state.cursor);
  return params;
}

function shortSha(s: string | undefined): string {
  if (!s) return '';
  return s.length > 12 ? `${s.slice(0, 12)}…` : s;
}

function formatTimestamp(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleString('en-US', {
    year: 'numeric',
    month: 'short',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  });
}

// Outcome → StatusBadge variant mapping. The project's StatusBadge accepts
// "success" | "error" | "warning" | "info" | "inactive" | "pending".
function outcomeVariant(o: AuditOutcome): 'success' | 'error' | 'warning' {
  if (o === 'success') return 'success';
  if (o === 'denied') return 'warning';
  return 'error';
}

// -----------------------------------------------------------------------
// Page component
// -----------------------------------------------------------------------

export default function AuditPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const { user } = useAuth();
  const isAdmin = (user?.roles || []).includes('admin');

  // URL-synced filter state.
  const [since, setSince] = useState<string>(searchParams.get('since') || defaultSinceISO());
  const [until, setUntil] = useState<string>(searchParams.get('until') || '');
  const [category, setCategory] = useState<AuditCategory | ''>(
    (searchParams.get('category') as AuditCategory) || '',
  );
  const [source, setSource] = useState<AuditSource | ''>(
    (searchParams.get('source') as AuditSource) || '',
  );
  const [actor, setActor] = useState<string>(searchParams.get('actor') || '');
  const [target, setTarget] = useState<string>(searchParams.get('target') || '');

  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [nextCursor, setNextCursor] = useState<string | undefined>();
  const [sourceErrors, setSourceErrors] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<AuditEvent | null>(null);

  // Fetch a page. When `append` is true we concatenate; otherwise replace.
  const fetchPage = useCallback(
    async (append: boolean, cursor?: string) => {
      try {
        if (append) setLoadingMore(true);
        else setLoading(true);
        setError(null);

        const params = buildQuery({ since, until, category, source, actor, target, cursor });
        const data = await apiGet<AuditResponse>(`/v1/audit?${params.toString()}`);

        setEvents((prev) => (append ? [...prev, ...data.events] : data.events));
        setNextCursor(data.next_cursor);
        setSourceErrors(data.source_errors || {});
      } catch (err) {
        console.error('audit fetch failed', err);
        setError(err instanceof Error ? err.message : 'Failed to load audit events');
      } finally {
        setLoading(false);
        setLoadingMore(false);
      }
    },
    [since, until, category, source, actor, target],
  );

  // Re-fetch from scratch whenever a filter changes. We also push the
  // filter state into the URL so links are shareable.
  useEffect(() => {
    const params = buildQuery({ since, until, category, source, actor, target });
    params.delete('limit');
    params.delete('cursor');
    const qs = params.toString();
    router.replace(qs ? `/audit?${qs}` : '/audit', { scroll: false });
    void fetchPage(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [since, until, category, source, actor, target]);

  const related = useMemo(() => {
    // Related events = same request_id, excluding the selected one.
    if (!selected?.request_id) return [];
    return events.filter(
      (e) => e.request_id && e.request_id === selected.request_id && e !== selected,
    );
  }, [selected, events]);

  // CSV export: admin-only. We build the URL with the current filter
  // state and hand it to the browser; the backend streams CSV.
  const handleExport = useCallback(async () => {
    const params = buildQuery({ since, until, category, source, actor, target });
    params.delete('limit');
    try {
      const response = await apiFetchResponse(
        `/v1/audit/export?${params.toString()}`,
      );
      if (!response.ok) {
        throw new Error(`Export failed: ${response.statusText}`);
      }
      const blob = await response.blob();
      const downloadUrl = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = downloadUrl;
      a.download = `audit-export-${new Date().toISOString().split('T')[0]}.csv`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      window.URL.revokeObjectURL(downloadUrl);
    } catch (err) {
      console.error('Failed to export audit CSV:', err);
    }
  }, [since, until, category, source, actor, target]);

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between flex-wrap gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Audit log</h1>
          <p className="text-muted-foreground max-w-2xl">
            Consolidated &quot;who did what when&quot; across Janua (auth), Switchyard (deploys),
            and the four Selva append-only ledgers (secrets, GitHub admin, config maps,
            webhooks). SOC 2 evidence surface.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={() => void fetchPage(false)} disabled={loading}>
            {loading ? 'Refreshing…' : 'Refresh'}
          </Button>
          {isAdmin && (
            <Button onClick={handleExport}>Export CSV</Button>
          )}
        </div>
      </div>

      {/* Source-level degradation banner. A missing source is never fatal,
          but it's a SOC 2 gap the auditor should see. */}
      {Object.keys(sourceErrors).length > 0 && (
        <Card className="border-status-warning bg-status-warning-muted">
          <CardContent className="pt-4 text-sm">
            <strong>Partial results:</strong> some upstream audit sources are unavailable —{' '}
            {Object.entries(sourceErrors)
              .map(([src, msg]) => `${src} (${msg})`)
              .join('; ')}
            .
          </CardContent>
        </Card>
      )}

      {/* Filter card */}
      <Card>
        <CardContent className="pt-6">
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
            <div>
              <label className="block text-xs font-medium mb-1 text-muted-foreground">
                Since
              </label>
              <Input
                type="datetime-local"
                value={since ? since.slice(0, 16) : ''}
                onChange={(e) => setSince(e.target.value ? new Date(e.target.value).toISOString() : '')}
              />
            </div>
            <div>
              <label className="block text-xs font-medium mb-1 text-muted-foreground">
                Until
              </label>
              <Input
                type="datetime-local"
                value={until ? until.slice(0, 16) : ''}
                onChange={(e) => setUntil(e.target.value ? new Date(e.target.value).toISOString() : '')}
              />
            </div>
            <div>
              <label className="block text-xs font-medium mb-1 text-muted-foreground">
                Category
              </label>
              <select
                value={category}
                onChange={(e) => setCategory(e.target.value as AuditCategory | '')}
                className="w-full px-3 py-2 border rounded-md bg-card"
              >
                <option value="">All categories</option>
                {CATEGORY_OPTIONS.map((c) => (
                  <option key={c.value} value={c.value}>
                    {c.label}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className="block text-xs font-medium mb-1 text-muted-foreground">
                Source
              </label>
              <select
                value={source}
                onChange={(e) => setSource(e.target.value as AuditSource | '')}
                className="w-full px-3 py-2 border rounded-md bg-card"
              >
                <option value="">All sources</option>
                {SOURCE_OPTIONS.map((s) => (
                  <option key={s.value} value={s.value}>
                    {s.label}
                  </option>
                ))}
              </select>
            </div>
            {/* Admin-only actor filter. Non-admins are server-forced to their own sub. */}
            {isAdmin && (
              <div>
                <label className="block text-xs font-medium mb-1 text-muted-foreground">
                  Actor (Janua sub or email)
                </label>
                <Input
                  type="text"
                  value={actor}
                  onChange={(e) => setActor(e.target.value)}
                  placeholder="sub-xyz…"
                />
              </div>
            )}
            <div>
              <label className="block text-xs font-medium mb-1 text-muted-foreground">
                Target
              </label>
              <Input
                type="text"
                value={target}
                onChange={(e) => setTarget(e.target.value)}
                placeholder="repo name, secret key, commit SHA…"
              />
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Event table */}
      <Card>
        <CardHeader>
          <CardTitle>Events</CardTitle>
          <CardDescription>
            {events.length} event{events.length === 1 ? '' : 's'} shown
            {nextCursor ? ' (more available)' : ''}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {loading && events.length === 0 ? (
            <div className="flex items-center justify-center py-12">
              <Spinner size="lg" />
              <span className="ml-3 text-muted-foreground">Loading audit events…</span>
            </div>
          ) : error ? (
            <div className="text-center py-12">
              <p className="text-status-error mb-4">{error}</p>
              <Button variant="outline" onClick={() => void fetchPage(false)}>
                Try again
              </Button>
            </div>
          ) : events.length === 0 ? (
            <div className="text-center py-12 text-muted-foreground">
              <p className="text-lg font-medium">No events in this range</p>
              <p className="text-sm mt-1">Widen your date range or clear filters.</p>
            </div>
          ) : (
            <>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Timestamp</TableHead>
                    <TableHead>Actor</TableHead>
                    <TableHead>Source</TableHead>
                    <TableHead>Category</TableHead>
                    <TableHead>Action</TableHead>
                    <TableHead>Target</TableHead>
                    <TableHead>Outcome</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {events.map((ev, idx) => (
                    <TableRow
                      key={`${ev.timestamp}-${ev.source}-${idx}`}
                      className="cursor-pointer hover:bg-accent"
                      onClick={() => setSelected(ev)}
                      data-testid="audit-row"
                    >
                      <TableCell className="font-mono text-xs whitespace-nowrap">
                        {formatTimestamp(ev.timestamp)}
                      </TableCell>
                      <TableCell className="text-sm">
                        {ev.actor_email || ev.actor || <span className="text-muted-foreground">—</span>}
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">{ev.source}</TableCell>
                      <TableCell className="text-sm">{ev.category}</TableCell>
                      <TableCell className="text-sm font-medium">{ev.action}</TableCell>
                      <TableCell className="text-xs font-mono max-w-[240px] truncate">
                        {shortSha(ev.target)}
                      </TableCell>
                      <TableCell>
                        <StatusBadge status={outcomeVariant(ev.outcome)}>{ev.outcome}</StatusBadge>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>

              {nextCursor && (
                <div className="text-center pt-4">
                  <Button
                    variant="outline"
                    onClick={() => void fetchPage(true, nextCursor)}
                    disabled={loadingMore}
                    data-testid="audit-load-more"
                  >
                    {loadingMore ? 'Loading…' : 'Load more'}
                  </Button>
                </div>
              )}
            </>
          )}
        </CardContent>
      </Card>

      {/* Row-detail drawer */}
      <Sheet open={!!selected} onOpenChange={(open) => !open && setSelected(null)}>
        <SheetContent side="right" className="w-full sm:max-w-xl overflow-y-auto">
          {selected && (
            <>
              <SheetHeader>
                <SheetTitle>{selected.action}</SheetTitle>
                <SheetDescription>
                  <span className="font-mono text-xs">{formatTimestamp(selected.timestamp)}</span>
                  {' · '}
                  {selected.source} · {selected.category}
                </SheetDescription>
              </SheetHeader>

              <div className="mt-6 space-y-4 text-sm">
                <DetailRow label="Actor" value={selected.actor_email || selected.actor || '—'} />
                <DetailRow label="Target" value={selected.target || '—'} mono />
                <DetailRow
                  label="Outcome"
                  value={
                    <StatusBadge status={outcomeVariant(selected.outcome)}>
                      {selected.outcome}
                    </StatusBadge>
                  }
                />
                {selected.request_id && (
                  <DetailRow label="Request ID" value={selected.request_id} mono />
                )}

                <div>
                  <div className="text-xs font-medium text-muted-foreground mb-1">Details</div>
                  <pre className="bg-muted rounded-md p-3 text-xs overflow-x-auto whitespace-pre-wrap break-words">
                    {JSON.stringify(selected.details || {}, null, 2)}
                  </pre>
                </div>

                {related.length > 0 && (
                  <div>
                    <div className="text-xs font-medium text-muted-foreground mb-2">
                      Related events (same request_id)
                    </div>
                    <ul className="space-y-1">
                      {related.map((r, i) => (
                        <li
                          key={i}
                          className="border rounded px-2 py-1 text-xs hover:bg-accent cursor-pointer"
                          onClick={() => setSelected(r)}
                        >
                          <span className="font-mono">{formatTimestamp(r.timestamp)}</span>
                          {' · '}
                          {r.source} / {r.action}
                        </li>
                      ))}
                    </ul>
                  </div>
                )}
              </div>
            </>
          )}
        </SheetContent>
      </Sheet>
    </div>
  );
}

function DetailRow({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: React.ReactNode;
  mono?: boolean;
}) {
  return (
    <div>
      <div className="text-xs font-medium text-muted-foreground mb-0.5">{label}</div>
      <div className={mono ? 'font-mono text-xs break-all' : 'text-sm'}>{value}</div>
    </div>
  );
}
