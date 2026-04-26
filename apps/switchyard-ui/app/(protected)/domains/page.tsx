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
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Spinner } from '@/components/ui/spinner';
import { LastSyncBadge } from '@/components/dashboard/last-sync-badge';
import { DomainsTable } from '@/components/domains/domains-table';
import { describeCertExpiry } from '@/components/domains/cert-expiry-indicator';
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
    loading,
    refreshing,
    error,
    endpointMissing,
    lastSyncedAt,
    refresh,
  } = useDomains();

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
      </header>

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
            />
          )}
        </CardContent>
      </Card>
    </div>
  );
}
