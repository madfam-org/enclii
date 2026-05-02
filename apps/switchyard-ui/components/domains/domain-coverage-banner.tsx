'use client';

/**
 * Surfaces operator-clarity banners above the domains table:
 *
 *  - "Cloudflare integration not configured" when sync_configured=false.
 *    Rows will never auto-verify against Cloudflare without it.
 *  - "Domain inventory may be incomplete" when there are projects on the
 *    control plane that contribute zero rows to the table — typical
 *    cause is the project's prod domain was never registered with
 *    `enclii domains add`.
 *  - "Domain verification is stale" when the oldest unverified row is
 *    older than 24h. Indicates the verifier loop is wedged or dead.
 *
 * These banners replace the false-positive "synced just now" header
 * (parity-audit gap DM-1..4): the previous behavior treated a successful
 * /v1/domains FETCH as evidence of a successful VERIFICATION, which it is
 * not. The fetch + verifier are independent loops.
 *
 * The "decideBanners" pure helper is exported for unit tests.
 */

import { AlertTriangle, ShieldAlert, Info } from 'lucide-react';
import { Card, CardContent } from '@/components/ui/card';
import type { DomainCoverage } from '@/types/domain';

export type CoverageBannerKind =
  | 'sync-not-configured'
  | 'inventory-incomplete'
  | 'verifier-stale';

export interface CoverageBanner {
  kind: CoverageBannerKind;
  /** Severity drives color + icon. "warning" is amber, "error" is red. */
  severity: 'warning' | 'error';
  title: string;
  body: string;
}

/** Threshold above which the verifier is considered "stuck", in seconds. */
export const STALE_VERIFIER_THRESHOLD_SECONDS = 24 * 60 * 60;

/**
 * Decide which banners (if any) to show given a coverage snapshot.
 * Pure, side-effect-free; exported for tests.
 *
 * Returns banners in display order (most-actionable first):
 *   sync-not-configured → inventory-incomplete → verifier-stale
 *
 * Returns [] when:
 *   - coverage is null (older API build that doesn't return the field)
 *   - everything looks healthy
 */
export function decideBanners(
  coverage: DomainCoverage | null,
): CoverageBanner[] {
  if (coverage === null) return [];

  const banners: CoverageBanner[] = [];

  // 1. Sync-not-configured is the most actionable: until Cloudflare is
  // wired up, no verification will ever happen. This is an *error* not a
  // warning because the page is fundamentally non-functional.
  if (!coverage.sync_configured) {
    banners.push({
      kind: 'sync-not-configured',
      severity: 'error',
      title: 'Cloudflare verification is not configured',
      body:
        'Domain rows below show DB state only. The verifier (Cloudflare API integration) is not wired into this control plane, so status, TLS health, and "last verified" timestamps will never auto-update. Set ENCLII_CLOUDFLARE_API_TOKEN, ENCLII_CLOUDFLARE_ACCOUNT_ID, ENCLII_CLOUDFLARE_ZONE_ID on switchyard-api and redeploy to enable.',
    });
  }

  // 2. Inventory may be incomplete: there are projects on the control
  // plane that contribute no rows to the table.
  if (
    coverage.projects_total > 0 &&
    coverage.projects_with_domains < coverage.projects_total
  ) {
    const missing =
      coverage.projects_total - coverage.projects_with_domains;
    banners.push({
      kind: 'inventory-incomplete',
      severity: 'warning',
      title: 'Domain inventory may be incomplete',
      body: `${missing} project${missing === 1 ? '' : 's'} on this control plane do not appear in the table below. The Cloudflare sync only covers domains explicitly registered with \`enclii domains add\`. Production domains served via Cloudflare Tunnel ingress (e.g. *.dhan.am, *.madfam.io) but never registered with the platform will not appear here. Run \`enclii domains add <project> <domain>\` for each missing project to backfill.`,
    });
  }

  // 3. Verifier stale: the oldest unverified row is older than 24h. This
  // means either the background sync is wedged, or the cluster is
  // missing Cloudflare credentials but somebody added domains anyway.
  // Sentinel -1 means there are no unverified rows, so skip.
  if (
    coverage.oldest_unverified_age_seconds > STALE_VERIFIER_THRESHOLD_SECONDS
  ) {
    const hours = Math.floor(
      coverage.oldest_unverified_age_seconds / 3600,
    );
    banners.push({
      kind: 'verifier-stale',
      severity: 'error',
      title: 'Domain verification is stale',
      body: `At least one domain has been waiting for verification for ${hours}h. The verifier may be wedged. "Unknown" rows below are shown as "Stale" — their status reflects the database, not live Cloudflare data.`,
    });
  }

  return banners;
}

const SEVERITY_CARD_CLASSES: Record<CoverageBanner['severity'], string> = {
  warning: 'border-status-warning/40 bg-status-warning-muted/20',
  error: 'border-status-error/40 bg-status-error-muted/20',
};

const SEVERITY_ICON_CLASSES: Record<CoverageBanner['severity'], string> = {
  warning: 'text-status-warning',
  error: 'text-status-error',
};

const SEVERITY_TITLE_CLASSES: Record<CoverageBanner['severity'], string> = {
  warning: 'text-status-warning',
  error: 'text-status-error',
};

interface CoverageBannerProps {
  banner: CoverageBanner;
}

export function CoverageBannerCard({ banner }: CoverageBannerProps) {
  const Icon =
    banner.kind === 'inventory-incomplete'
      ? Info
      : banner.kind === 'sync-not-configured'
        ? ShieldAlert
        : AlertTriangle;
  return (
    <Card
      className={SEVERITY_CARD_CLASSES[banner.severity]}
      data-testid={`coverage-banner-${banner.kind}`}
      role="alert"
    >
      <CardContent className="flex items-start gap-3 py-3 text-sm">
        <Icon
          className={`mt-0.5 h-5 w-5 flex-shrink-0 ${SEVERITY_ICON_CLASSES[banner.severity]}`}
          aria-hidden="true"
        />
        <div className="flex-1">
          <p
            className={`font-medium ${SEVERITY_TITLE_CLASSES[banner.severity]}`}
          >
            {banner.title}
          </p>
          <p className="text-muted-foreground mt-1 text-xs leading-relaxed">
            {banner.body}
          </p>
        </div>
      </CardContent>
    </Card>
  );
}
