'use client';

import Link from 'next/link';
import { useMemo, useState } from 'react';
import { ArrowUpDown, ExternalLink, Globe, Network } from 'lucide-react';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@enclii/ui-components/table";
import { Badge } from "@enclii/ui-components/badge";
import { cn } from '@/lib/utils';
import { formatRelativeTime } from '@/lib/formatting';
import {
  CertExpiryIndicator,
  describeCertExpiry,
} from './cert-expiry-indicator';
import {
  DomainStatusBadge,
  deriveDomainHealth,
} from './domain-status-badge';
import type { Domain, DomainHealthStatus, DomainTunnelMode } from '@/types/domain';

export interface DomainsTableFilters {
  search: string;
  status: 'all' | DomainHealthStatus;
  project: string; // 'all' or a project_slug
}

export interface DomainsTableProps {
  domains: Domain[];
  filters: DomainsTableFilters;
  /** Defaults to "expiry-asc" (worst first) per spec. */
  initialSort?: SortKey;
  /** Resend domain verification status keyed by apex domain (admin). */
  resendStatusByDomain?: Record<string, string>;
  /**
   * When true, "Unknown" status badges render as "Stale" — the
   * verification pipeline is wedged and the displayed status is DB
   * state, not live state. Parity-audit gap DM-3.
   */
  verifierStale?: boolean;
}

type SortKey =
  | 'expiry-asc'
  | 'expiry-desc'
  | 'domain-asc'
  | 'domain-desc'
  | 'verified-asc'
  | 'verified-desc';

/** Pure helper: derive tunnel mode from the row. Exported for tests. */
export function deriveTunnelMode(domain: Domain): DomainTunnelMode {
  if (domain.cloudflare_tunnel_id) return 'tunneled';
  // Platform domains without a tunnel ID are typically routed direct via
  // Cloudflare for SaaS — we can't tell without sync data, so report unknown.
  if (domain.is_platform_domain) return 'unknown';
  return 'direct';
}

export interface ExternalEvidenceSummary {
  label: string;
  detail: string;
  toneClass: string;
}

export function describeExternalEvidence(domain: Domain): ExternalEvidenceSummary {
  const evidence = domain.evidence;
  if (!evidence) {
    return {
      label: 'No probe',
      detail: 'No public evidence',
      toneClass: 'text-muted-foreground border-border',
    };
  }

  if (
    evidence.public_dns_status === 'resolved' &&
    evidence.public_tls_status === 'valid' &&
    evidence.public_http_reachable
  ) {
    return {
      label: 'HTTPS valid',
      detail: evidence.public_http_status
        ? `HTTP ${evidence.public_http_status}`
        : 'response received',
      toneClass: 'text-status-success border-status-success',
    };
  }

  if (evidence.public_dns_status === 'missing') {
    return {
      label: 'DNS missing',
      detail: evidence.error ?? 'no public records',
      toneClass: 'text-status-error border-status-error',
    };
  }

  if (evidence.public_tls_status === 'invalid') {
    return {
      label: 'TLS invalid',
      detail: evidence.error ?? 'certificate rejected',
      toneClass: 'text-status-error border-status-error',
    };
  }

  if (evidence.public_dns_status === 'resolved') {
    return {
      label: 'DNS resolved',
      detail: evidence.error ?? 'HTTPS not confirmed',
      toneClass: 'text-status-warning border-status-warning',
    };
  }

  return {
    label: 'Unknown',
    detail: evidence.error ?? 'probe inconclusive',
    toneClass: 'text-muted-foreground border-border',
  };
}

const TUNNEL_LABELS: Record<DomainTunnelMode, string> = {
  tunneled: 'Tunneled',
  direct: 'Direct',
  unknown: 'Unknown',
};

const TUNNEL_CLASSES: Record<DomainTunnelMode, string> = {
  tunneled: 'text-status-info border-status-info',
  direct: 'text-muted-foreground border-border',
  unknown: 'text-muted-foreground border-border',
};

/** Pure helper: filter domains by search/status/project. Exported for tests. */
export function filterDomains(
  domains: Domain[],
  filters: DomainsTableFilters,
): Domain[] {
  const q = filters.search.trim().toLowerCase();
  return domains.filter((d) => {
    if (q) {
      const haystack = [
        d.domain,
        d.service_name ?? '',
        d.project_slug ?? '',
        d.environment_name ?? '',
      ]
        .join(' ')
        .toLowerCase();
      if (!haystack.includes(q)) return false;
    }
    if (filters.status !== 'all') {
      if (deriveDomainHealth(d) !== filters.status) return false;
    }
    if (filters.project !== 'all') {
      if ((d.project_slug ?? '') !== filters.project) return false;
    }
    return true;
  });
}

/** Pure helper: sort by current sort key. Exported for tests. */
export function sortDomains(domains: Domain[], sort: SortKey): Domain[] {
  const copy = [...domains];
  switch (sort) {
    case 'expiry-asc':
    case 'expiry-desc': {
      const sign = sort === 'expiry-asc' ? 1 : -1;
      // Unknown/missing expiries always sort to the bottom (regardless of
      // direction) so the user's eye lands on rows with real expiry data.
      copy.sort((a, b) => {
        const aT = a.tls_expires_at
          ? new Date(a.tls_expires_at).getTime()
          : NaN;
        const bT = b.tls_expires_at
          ? new Date(b.tls_expires_at).getTime()
          : NaN;
        const aMissing = !Number.isFinite(aT);
        const bMissing = !Number.isFinite(bT);
        if (aMissing && bMissing) return 0;
        if (aMissing) return 1;
        if (bMissing) return -1;
        return sign * (aT - bT);
      });
      break;
    }
    case 'domain-asc':
      copy.sort((a, b) => a.domain.localeCompare(b.domain));
      break;
    case 'domain-desc':
      copy.sort((a, b) => b.domain.localeCompare(a.domain));
      break;
    case 'verified-asc':
      copy.sort((a, b) => Number(a.verified) - Number(b.verified));
      break;
    case 'verified-desc':
      copy.sort((a, b) => Number(b.verified) - Number(a.verified));
      break;
  }
  return copy;
}

interface SortableHeaderProps {
  label: string;
  field: 'domain' | 'expiry' | 'verified';
  currentSort: SortKey;
  onSort: (next: SortKey) => void;
  className?: string;
}

function SortableHeader({
  label,
  field,
  currentSort,
  onSort,
  className,
}: SortableHeaderProps) {
  const ascKey = `${field}-asc` as SortKey;
  const descKey = `${field}-desc` as SortKey;
  const active = currentSort === ascKey || currentSort === descKey;
  const next = currentSort === ascKey ? descKey : ascKey;
  const ariaSort = !active
    ? 'none'
    : currentSort === ascKey
      ? 'ascending'
      : 'descending';
  return (
    <TableHead aria-sort={ariaSort} className={className}>
      <button
        type="button"
        onClick={() => onSort(next)}
        className={cn(
          'inline-flex items-center gap-1 font-medium transition-colors',
          active ? 'text-foreground' : 'text-muted-foreground',
          'hover:text-foreground focus-visible:ring-ring rounded focus:outline-none focus-visible:ring-2',
        )}
        aria-label={`Sort by ${label}, currently ${ariaSort}`}
      >
        {label}
        <ArrowUpDown className="h-3 w-3" aria-hidden="true" />
      </button>
    </TableHead>
  );
}

/**
 * Pure rendering of the domains table. The page owns data fetching + filter
 * state; this component only does layout, sort, and click navigation.
 */
export function DomainsTable({
  domains,
  filters,
  initialSort = 'expiry-asc',
  verifierStale = false,
  resendStatusByDomain,
}: DomainsTableProps) {
  const [sort, setSort] = useState<SortKey>(initialSort);

  const visible = useMemo(
    () => sortDomains(filterDomains(domains, filters), sort),
    [domains, filters, sort],
  );

  if (visible.length === 0) {
    return (
      <div
        className="border-border bg-card flex flex-col items-center justify-center rounded-lg border border-dashed py-12 text-center"
        role="status"
      >
        <Globe className="text-muted-foreground/50 mb-3 h-10 w-10" aria-hidden="true" />
        <p className="text-sm font-medium">No domains match these filters</p>
        <p className="text-muted-foreground mt-1 text-xs">
          Try clearing the search or changing the status filter.
        </p>
      </div>
    );
  }

  return (
    <>
      {/* Desktop / tablet table */}
      <div className="hidden md:block">
        <Table>
          <TableHeader>
            <TableRow>
              <SortableHeader
                label="Domain"
                field="domain"
                currentSort={sort}
                onSort={setSort}
              />
              <TableHead>Project</TableHead>
              <TableHead>Service</TableHead>
              <TableHead>Status</TableHead>
              {resendStatusByDomain && <TableHead>Resend</TableHead>}
              <SortableHeader
                label="Cert expiry"
                field="expiry"
                currentSort={sort}
                onSort={setSort}
              />
              <TableHead>Tunnel</TableHead>
              <TableHead>External proof</TableHead>
              <TableHead>Last verified</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {visible.map((d) => {
              const tunnel = deriveTunnelMode(d);
              const lastVerified = d.verified_at ?? d.last_verified_at ?? null;
              const projectHref = d.project_slug
                ? `/projects/${d.project_slug}`
                : null;
              return (
                <TableRow
                  key={d.id}
                  className={cn(
                    projectHref && 'cursor-pointer',
                  )}
                  onClick={() => {
                    if (projectHref && typeof window !== 'undefined') {
                      window.location.href = projectHref;
                    }
                  }}
                >
                  <TableCell>
                    <div className="flex items-center gap-2">
                      <a
                        href={`https://${d.domain}`}
                        target="_blank"
                        rel="noopener noreferrer"
                        onClick={(e) => e.stopPropagation()}
                        className="text-primary font-mono text-sm hover:underline"
                      >
                        {d.domain}
                      </a>
                      <ExternalLink
                        className="text-muted-foreground h-3 w-3"
                        aria-hidden="true"
                      />
                      {d.is_platform_domain && (
                        <Badge variant="outline" className="text-xs">
                          Platform
                        </Badge>
                      )}
                    </div>
                  </TableCell>
                  <TableCell>
                    {projectHref ? (
                      <Link
                        href={projectHref}
                        onClick={(e) => e.stopPropagation()}
                        className="text-sm hover:underline"
                      >
                        {d.project_slug}
                      </Link>
                    ) : (
                      <span className="text-muted-foreground text-sm">
                        &mdash;
                      </span>
                    )}
                  </TableCell>
                  <TableCell>
                    {d.service_id ? (
                      <Link
                        href={`/services/${d.service_id}`}
                        onClick={(e) => e.stopPropagation()}
                        className="text-sm hover:underline"
                      >
                        {d.service_name ?? 'Unknown'}
                      </Link>
                    ) : (
                      <span className="text-muted-foreground text-sm">
                        Unknown
                      </span>
                    )}
                    {d.environment_name && (
                      <span className="text-muted-foreground ml-2 text-xs">
                        ({d.environment_name})
                      </span>
                    )}
                  </TableCell>
                  <TableCell>
                    <DomainStatusBadge domain={d} verifierStale={verifierStale} />
                  </TableCell>
                  {resendStatusByDomain && (
                    <TableCell>
                      {(() => {
                        const apex = d.domain.split('.').slice(-2).join('.');
                        const rs =
                          resendStatusByDomain[d.domain.toLowerCase()] ??
                          resendStatusByDomain[apex.toLowerCase()];
                        if (!rs) {
                          return (
                            <span className="text-muted-foreground text-xs">—</span>
                          );
                        }
                        const verified = rs.toLowerCase() === 'verified';
                        return (
                          <Badge
                            variant="outline"
                            className={cn(
                              'text-xs',
                              verified
                                ? 'text-status-success border-status-success'
                                : 'text-status-warning border-status-warning',
                            )}
                          >
                            {rs}
                          </Badge>
                        );
                      })()}
                    </TableCell>
                  )}
                  <TableCell>
                    <CertExpiryIndicator expiresAt={d.tls_expires_at} />
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant="outline"
                      className={cn('text-xs', TUNNEL_CLASSES[tunnel])}
                      aria-label={`Cloudflare route: ${TUNNEL_LABELS[tunnel]}`}
                    >
                      <Network
                        className="mr-1 h-3 w-3"
                        aria-hidden="true"
                      />
                      {TUNNEL_LABELS[tunnel]}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    {(() => {
                      const external = describeExternalEvidence(d);
                      return (
                        <div className="flex flex-col gap-1">
                          <Badge
                            variant="outline"
                            className={cn('w-fit text-xs', external.toneClass)}
                            aria-label={`External evidence: ${external.label}`}
                          >
                            {external.label}
                          </Badge>
                          <span className="text-muted-foreground max-w-[14rem] truncate text-xs">
                            {external.detail}
                          </span>
                        </div>
                      );
                    })()}
                  </TableCell>
                  <TableCell className="text-muted-foreground text-sm">
                    {lastVerified ? formatRelativeTime(lastVerified) : 'never'}
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </div>

      {/* Mobile cards */}
      <ul className="space-y-3 md:hidden" aria-label="Domains">
        {visible.map((d) => {
          const tunnel = deriveTunnelMode(d);
          const lastVerified = d.verified_at ?? d.last_verified_at ?? null;
          const certDesc = describeCertExpiry(d.tls_expires_at);
          const external = describeExternalEvidence(d);
          return (
            <li
              key={d.id}
              className="border-border bg-card rounded-lg border p-4"
            >
              <div className="flex items-start justify-between gap-2">
                <a
                  href={`https://${d.domain}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-primary font-mono text-sm hover:underline"
                >
                  {d.domain}
                </a>
                <DomainStatusBadge domain={d} verifierStale={verifierStale} />
              </div>
              <dl className="mt-3 grid grid-cols-2 gap-x-3 gap-y-2 text-xs">
                <dt className="text-muted-foreground">Project</dt>
                <dd>
                  {d.project_slug ? (
                    <Link
                      href={`/projects/${d.project_slug}`}
                      className="hover:underline"
                    >
                      {d.project_slug}
                    </Link>
                  ) : (
                    <span className="text-muted-foreground">&mdash;</span>
                  )}
                </dd>
                <dt className="text-muted-foreground">Service</dt>
                <dd>
                  {d.service_name ?? 'Unknown'}
                  {d.environment_name ? ` (${d.environment_name})` : ''}
                </dd>
                <dt className="text-muted-foreground">Cert expiry</dt>
                <dd className={certDesc.toneClass}>{certDesc.label}</dd>
                <dt className="text-muted-foreground">Tunnel</dt>
                <dd>{TUNNEL_LABELS[tunnel]}</dd>
                <dt className="text-muted-foreground">External proof</dt>
                <dd>
                  {external.label}
                  <span className="text-muted-foreground">
                    {' '}
                    ({external.detail})
                  </span>
                </dd>
                <dt className="text-muted-foreground">Last verified</dt>
                <dd>
                  {lastVerified ? formatRelativeTime(lastVerified) : 'never'}
                </dd>
              </dl>
            </li>
          );
        })}
      </ul>
    </>
  );
}
