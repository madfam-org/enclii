'use client';

import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';
import type { Domain, DomainHealthStatus } from '@/types/domain';

/**
 * Maps a backend Domain row → a UI health bucket.
 *
 * The backend `status` field is the source of truth, with one extra rule we
 * compute client-side: a domain that has no service join (or whose service
 * couldn't be resolved) is "orphaned" — surfaced visibly so operators can
 * clean it up. This mirrors the parity-audit gap: we want to *see* orphans.
 *
 * Pure helper, exported for tests.
 */
export function deriveDomainHealth(domain: Domain): DomainHealthStatus {
  // Orphan detection: no resolved service or env name = dangling row.
  // Backend always populates service_id; the join produces names. Missing
  // names = the underlying service or env was deleted but the domain was
  // never reaped.
  if (!domain.service_name || !domain.environment_name) {
    return 'orphaned';
  }

  switch (domain.status) {
    case 'active':
      return domain.verified ? 'active' : 'provisioning';
    case 'verifying':
    case 'pending':
      return 'provisioning';
    case 'error':
      return 'failed';
    default:
      // Unknown future status — fall back to verified flag rather than
      // surfacing nothing.
      if (domain.verified) return 'active';
      return 'unknown';
  }
}

const HEALTH_LABELS: Record<DomainHealthStatus, string> = {
  active: 'Active',
  provisioning: 'Provisioning',
  failed: 'Failed',
  orphaned: 'Orphaned',
  unknown: 'Unknown',
};

const HEALTH_CLASSES: Record<DomainHealthStatus, string> = {
  active:
    'bg-status-success-muted text-status-success-foreground border-transparent',
  provisioning:
    'bg-status-warning-muted text-status-warning-foreground border-transparent',
  failed:
    'bg-status-error-muted text-status-error-foreground border-transparent',
  orphaned:
    'bg-status-error-muted text-status-error-foreground border-status-error',
  unknown: 'bg-muted text-muted-foreground border-transparent',
};

interface DomainStatusBadgeProps {
  domain: Domain;
  className?: string;
}

export function DomainStatusBadge({
  domain,
  className,
}: DomainStatusBadgeProps) {
  const health = deriveDomainHealth(domain);
  const label = HEALTH_LABELS[health];

  const ariaLabel = `Domain status: ${label}`;

  return (
    <Badge
      className={cn(HEALTH_CLASSES[health], className)}
      aria-label={ariaLabel}
      data-testid="domain-status-badge"
      data-health={health}
    >
      {label}
    </Badge>
  );
}

export { HEALTH_LABELS, HEALTH_CLASSES };
