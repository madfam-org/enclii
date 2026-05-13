'use client';

/**
 * Domains page — closes parity audit gap #4 (per-domain visibility).
 *
 * Surfaces every custom domain across the ecosystem with sync state, project
 * ownership, certificate health, and Cloudflare tunnel route. Replaces the
 * single-`domain` field on dashboard cards with a centralized, filterable
 * view.
 *
 * Backend: GET /v1/domains, GET /v1/domains/stats, POST /v1/domains/sync
 * (existing — see apps/switchyard-api/internal/api/global_domains_handlers.go).
 *
 * The `tls_expires_at` and `last_verified_at` columns are populated when the
 * domain_sync service has caught up; missing values render as "unknown" so
 * the UI is forward-compatible with sync-service growth.
 */

import { useMemo, useState } from 'react';
import { RefreshCw, ShieldAlert, Globe, CheckCircle2 } from 'lucide-react';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Button } from "@enclii/ui-components/button";
import { Input } from "@enclii/ui-components/input";
import { Spinner } from '@/components/ui/spinner';
import { LastSyncBadge } from '@/components/dashboard/last-sync-badge';
import { DomainsTable } from '@/components/domains/domains-table';
import { describeCertExpiry } from '@/components/domains/cert-expiry-indicator';
import {
  CoverageBannerCard,
  STALE_VERIFIER_THRESHOLD_SECONDS,
  decideBanners,
} from '@/components/domains/domain-coverage-banner';
import { useDomains } from '@/hooks/use-domains';
import { apiPost } from '@/lib/api';
import type { DomainHealthStatus } from '@/types/domain';
import { deriveDomainHealth } from '@/components/domains/domain-status-badge';

const STATUS_OPTIONS: { value: 'all' | DomainHealthStatus; label: string }[] = [
  { value: 'all', label: 'All status' },
  { value: 'active', label: 'Active' },
  { value: 'provisioning', label: 'Provisioning' },
  { value: 'failed', label: 'Failed' },
  { value: 'orphaned', label: 'Orphaned' },
  { value: 'unknown', label: 'Unknown' },
];

export default function DomainsPage() {
  const {
    domains,
    stats,
    reconcile,
    exclusions,
    coverage,
    loading,
    refreshing,
    error,
    endpointMissing,
    lastSyncedAt,
    refresh,
  } = useDomains();

  // Banners + verifier-stale decision are derived from `coverage` so the
  // logic is pure and testable in `domain-coverage-banner.test.ts`.
  const banners = useMemo(() => decideBanners(coverage), [coverage]);
  const verifierStale =
    coverage !== null &&
    coverage.oldest_unverified_age_seconds > STALE_VERIFIER_THRESHOLD_SECONDS;
  const reconcileSummary = reconcile?.summary ?? null;
  const actionableRouteOnly =
    reconcile?.actionable_route_only ?? reconcile?.route_only ?? [];
  const excludedRouteOnly = reconcile?.excluded_route_only ?? [];

  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState<'all' | DomainHealthStatus>(
    'all',
  );
  const [projectFilter, setProjectFilter] = useState<string>('all');
  const [syncing, setSyncing] = useState(false);
  const [syncError, setSyncError] = useState<string | null>(null);

  // Derived: project list for the filter dropdown
  const projectOptions = useMemo(() => {
    const slugs = new Set<string>();
    domains.forEach((d) => {
      if (d.project_slug) slugs.add(d.project_slug);
    });
    return Array.from(slugs).sort((a, b) => a.localeCompare(b));
  }, [domains]);

  // Derived: summary stats — what % of domains are active, how many certs
  // are within 30 days of expiry?
  const summary = useMemo(() => {
    const total = domains.length;
    let healthy = 0;
    let expiringSoon = 0;
    let expired = 0;
    domains.forEach((d) => {
      if (deriveDomainHealth(d) === 'active') healthy += 1;
      const cert = describeCertExpiry(d.tls_expires_at);
      if (cert.tone === 'critical') {
        if (cert.label === 'expired') expired += 1;
        else expiringSoon += 1; // <7d also counts as "expiring soon"
      } else if (cert.tone === 'warning') {
        expiringSoon += 1; // 7-30d
      }
    });
    return {
      total,
      healthy,
      healthyPct: total > 0 ? Math.round((healthy / total) * 100) : 0,
      expiringSoon,
      expired,
    };
  }, [domains]);

  const handleSync = async () => {
    setSyncing(true);
    setSyncError(null);
    try {
      await apiPost('/v1/domains/sync', {});
      await refresh();
    } catch (e) {
      setSyncError(
        e instanceof Error ? e.message : 'Failed to sync from Cloudflare',
      );
    } finally {
      setSyncing(false);
    }
  };

  // Endpoint-missing state — graceful placeholder per the spec.
  if (endpointMissing) {
    return (
      <div className="mx-auto max-w-7xl space-y-6 p-4 sm:p-6 lg:p-8">
        <header>
          <h1 className="text-2xl font-bold tracking-tight">Domains</h1>
          <p className="text-muted-foreground">
            Centralized view of every custom domain across the ecosystem.
          </p>
        </header>
        <Card>
          <CardContent className="flex flex-col items-center justify-center py-12 text-center">
            <ShieldAlert
              className="text-status-warning mb-3 h-10 w-10"
              aria-hidden="true"
            />
            <p className="text-sm font-medium">
              Backend endpoint pending
            </p>
            <p className="text-muted-foreground mt-1 max-w-md text-xs">
              The <code className="font-mono">GET /v1/domains</code> endpoint is
              not yet available on this control plane. The page is wired and
              will populate automatically once the backend is deployed.
            </p>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-7xl space-y-6 p-4 sm:p-6 lg:p-8">
      {/* Header */}
      <header className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Domains</h1>
          <p className="text-muted-foreground">
            Centralized view of every custom domain across the ecosystem.
          </p>
        </div>
        <div className="flex flex-col items-end gap-1">
          <div className="flex flex-wrap items-center gap-2">
            <LastSyncBadge
              lastSyncedAt={lastSyncedAt}
              onRefresh={refresh}
              refreshing={refreshing}
            />
            <Button
              onClick={handleSync}
              disabled={syncing}
              aria-label="Sync domains from Cloudflare"
            >
              {syncing ? (
                <>
                  <Spinner size="sm" className="mr-2" />
                  Syncing...
                </>
              ) : (
                <>
                  <RefreshCw className="mr-2 h-4 w-4" aria-hidden="true" />
                  Sync from Cloudflare
                </>
              )}
            </Button>
          </div>
          {/* Sub-header that disambiguates the "synced just now" badge.
              The badge tracks /v1/domains FETCH freshness — NOT verifier
              freshness. Without this caption operators inferred (correctly,
              given the badge wording) that a green "synced just now"
              meant Cloudflare verification just succeeded. It does not.
              See parity-audit gap DM-4. */}
          <p className="text-muted-foreground text-xs">
            Tracks API fetch freshness, not Cloudflare verification.
            {summary.total > 0 && (
              <>
                {' '}
                Verified {summary.healthy} of {summary.total} row
                {summary.total === 1 ? '' : 's'}.
              </>
            )}
          </p>
        </div>
      </header>

      {/* Coverage banners (sync-not-configured, inventory-incomplete,
          verifier-stale). Rendered above the summary cards so operators
          see the actionable state before drilling into per-row data. */}
      {banners.length > 0 && (
        <div className="space-y-2" role="region" aria-label="Domain inventory advisories">
          {banners.map((b) => (
            <CoverageBannerCard key={b.kind} banner={b} />
          ))}
        </div>
      )}

      {reconcileSummary?.drift_detected && (
        <Card className="border-status-warning/40 bg-status-warning-muted/20">
          <CardContent className="flex items-start gap-3 py-3 text-sm">
            <ShieldAlert
              className="text-status-warning mt-0.5 h-5 w-5 flex-shrink-0"
              aria-hidden="true"
            />
            <div className="flex-1">
              <p className="text-status-warning font-medium">
                Domain route inventory drift detected
              </p>
              <p className="text-muted-foreground mt-1 text-xs leading-relaxed">
                Reconciliation found {reconcileSummary.matched} matched domain
                {reconcileSummary.matched === 1 ? '' : 's'}, {' '}
                {reconcileSummary.db_only} DB-only row
                {reconcileSummary.db_only === 1 ? '' : 's'}, and {' '}
                {reconcileSummary.actionable_route_only ??
                  reconcileSummary.route_only} actionable routed hostname
                {(reconcileSummary.actionable_route_only ??
                  reconcileSummary.route_only) === 1
                  ? ''
                  : 's'} missing from Enclii DB. The table is operational,
                but inventory is not yet closed.
              </p>
            </div>
          </CardContent>
        </Card>
      )}

      {reconcile && (actionableRouteOnly.length > 0 || excludedRouteOnly.length > 0) && (
        <Card>
          <CardHeader>
            <CardTitle>Route inventory reconciliation</CardTitle>
            <CardDescription>
              {actionableRouteOnly.length} actionable route-only hostname
              {actionableRouteOnly.length === 1 ? '' : 's'} ·{' '}
              {excludedRouteOnly.length} explicitly excluded catalog entr
              {excludedRouteOnly.length === 1 ? 'y' : 'ies'}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4 text-sm">
            {actionableRouteOnly.length > 0 ? (
              <div className="space-y-2">
                <p className="font-medium">Action required</p>
                <div className="divide-y rounded-md border">
                  {actionableRouteOnly.slice(0, 10).map((item) => (
                    <div key={item.domain} className="grid gap-1 p-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
                      <span className="font-mono text-xs">{item.domain}</span>
                      <span className="text-muted-foreground text-xs">
                        {(item.sources ?? []).join(', ') || 'unknown source'}
                        {(item.route_targets ?? []).length > 0
                          ? ` · ${(item.route_targets ?? []).join(', ')}`
                          : ''}
                      </span>
                    </div>
                  ))}
                </div>
                {actionableRouteOnly.length > 10 && (
                  <p className="text-muted-foreground text-xs">
                    Showing first 10 of {actionableRouteOnly.length}. Use the
                    reconcile API for the full list.
                  </p>
                )}
              </div>
            ) : (
              <p className="text-muted-foreground text-xs">
                No actionable route-only hostnames remain in the current
                reconciliation response.
              </p>
            )}

            {excludedRouteOnly.length > 0 && (
              <div className="space-y-2">
                <p className="font-medium">Excluded from drift</p>
                <div className="divide-y rounded-md border bg-muted/20">
                  {excludedRouteOnly.slice(0, 5).map((item) => (
                    <div key={item.domain} className="grid gap-1 p-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
                      <span className="font-mono text-xs">{item.domain}</span>
                      <span className="text-muted-foreground text-xs">
                        {item.classification ?? 'excluded'} ·{' '}
                        {item.exclusion_reason ?? 'explicitly excluded'}
                      </span>
                    </div>
                  ))}
                </div>
                {excludedRouteOnly.length > 5 && (
                  <p className="text-muted-foreground text-xs">
                    Showing first 5 of {excludedRouteOnly.length} excluded
                    hostnames.
                  </p>
                )}
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {exclusions && exclusions.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Domain inventory exclusion registry</CardTitle>
            <CardDescription>
              {exclusions.length} active exclusion rule
              {exclusions.length === 1 ? '' : 's'} used by reconciliation.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <div className="divide-y rounded-md border">
              {exclusions.slice(0, 5).map((rule, index) => (
                <div
                  key={`${rule.hostname_pattern}-${rule.source}-${rule.route_target}-${index}`}
                  className="grid gap-1 p-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]"
                >
                  <span className="font-mono text-xs">
                    {rule.hostname_pattern} · {rule.source || 'any source'} ·{' '}
                    {rule.route_target || 'any target'}
                  </span>
                  <span className="text-muted-foreground text-xs">
                    {rule.classification} · {rule.reason}
                  </span>
                </div>
              ))}
            </div>
            {exclusions.length > 5 && (
              <p className="text-muted-foreground text-xs">
                Showing first 5 of {exclusions.length} active exclusion rules.
              </p>
            )}
          </CardContent>
        </Card>
      )}

      {syncError && (
        <Card className="border-status-error/40 bg-status-error-muted/20">
          <CardContent className="text-status-error py-3 text-sm">
            {syncError}
          </CardContent>
        </Card>
      )}

      {/* Summary cards */}
      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total domains</CardTitle>
            <Globe
              className="text-muted-foreground h-4 w-4"
              aria-hidden="true"
            />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{summary.total}</div>
            <p className="text-muted-foreground text-xs">
              {stats
                ? `${stats.platform_domains} platform · ${stats.custom_domains} custom`
                : 'across all projects'}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">% healthy</CardTitle>
            <CheckCircle2
              className="text-status-success h-4 w-4"
              aria-hidden="true"
            />
          </CardHeader>
          <CardContent>
            <div className="text-status-success text-2xl font-bold">
              {summary.healthyPct}%
            </div>
            <p className="text-muted-foreground text-xs">
              {summary.healthy} of {summary.total} active and verified
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">
              Certs expiring soon
            </CardTitle>
            <ShieldAlert
              className={
                summary.expired > 0 || summary.expiringSoon > 0
                  ? 'text-status-warning h-4 w-4'
                  : 'text-muted-foreground h-4 w-4'
              }
              aria-hidden="true"
            />
          </CardHeader>
          <CardContent>
            <div
              className={
                summary.expired > 0
                  ? 'text-status-error text-2xl font-bold'
                  : summary.expiringSoon > 0
                    ? 'text-status-warning text-2xl font-bold'
                    : 'text-2xl font-bold'
              }
            >
              {summary.expiringSoon + summary.expired}
            </div>
            <p className="text-muted-foreground text-xs">
              Within 30 days
              {summary.expired > 0
                ? ` · ${summary.expired} already expired`
                : ''}
            </p>
          </CardContent>
        </Card>
      </div>

      {/* Filters */}
      <Card>
        <CardContent className="flex flex-wrap items-center gap-3 pt-6">
          <div className="min-w-[200px] flex-1">
            <label htmlFor="domain-search" className="sr-only">
              Search domains
            </label>
            <Input
              id="domain-search"
              type="search"
              placeholder="Search domain, project, service..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </div>
          <div>
            <label htmlFor="domain-status-filter" className="sr-only">
              Filter by status
            </label>
            <select
              id="domain-status-filter"
              value={statusFilter}
              onChange={(e) =>
                setStatusFilter(e.target.value as 'all' | DomainHealthStatus)
              }
              className="bg-card rounded-md border px-3 py-2 text-sm"
            >
              {STATUS_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value}>
                  {opt.label}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label htmlFor="domain-project-filter" className="sr-only">
              Filter by project
            </label>
            <select
              id="domain-project-filter"
              value={projectFilter}
              onChange={(e) => setProjectFilter(e.target.value)}
              className="bg-card rounded-md border px-3 py-2 text-sm"
            >
              <option value="all">All projects</option>
              {projectOptions.map((slug) => (
                <option key={slug} value={slug}>
                  {slug}
                </option>
              ))}
            </select>
          </div>
        </CardContent>
      </Card>

      {/* Body */}
      <Card>
        <CardHeader>
          <CardTitle>All domains</CardTitle>
          <CardDescription>
            {loading
              ? 'Loading...'
              : `${domains.length} domain${domains.length === 1 ? '' : 's'} tracked across the platform`}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {loading && domains.length === 0 ? (
            <div className="flex items-center justify-center py-12">
              <Spinner size="lg" />
              <span className="text-muted-foreground ml-3">
                Loading domains...
              </span>
            </div>
          ) : error ? (
            <div className="py-12 text-center">
              <p className="text-status-error mb-4">{error}</p>
              <Button variant="outline" onClick={refresh}>
                Try again
              </Button>
            </div>
          ) : (
            <DomainsTable
              domains={domains}
              filters={{
                search,
                status: statusFilter,
                project: projectFilter,
              }}
              verifierStale={verifierStale}
            />
          )}
        </CardContent>
      </Card>
    </div>
  );
}
