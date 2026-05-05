/**
 * Alert routing — maps a backend Alert payload to its triage destination.
 *
 * The Switchyard API (`apps/switchyard-api/internal/api/observability_handlers.go`)
 * tags every alert with a stable ID prefix that encodes its class. This
 * file is the single source of truth for translating those prefixes into
 * UI deep-links so an operator can click an alert and land directly on
 * the panel that explains it.
 *
 * Design contract:
 *   - Pure: no React, no fetch, no side effects.
 *   - Total: every input returns a string. Unknown prefixes fall through
 *     to `/observability` rather than producing a broken link.
 *   - Defensive: a service-scoped alert without a populated `service_id`
 *     does NOT generate `/services/undefined`; it falls through too.
 *
 * Extending: when the backend introduces a new `alert-<class>-…` prefix,
 * add a row to ROUTING_TABLE plus a test in `alert-routing.test.ts`.
 * That keeps the prefix→href contract documented and exercised together.
 */

/**
 * Minimal shape of an alert relevant to routing. Both the dashboard
 * sidebar and the full observability page use this projection — the
 * `service_id` field is optional because not every alert class is
 * service-scoped.
 */
export interface RoutableAlert {
  id: string;
  service_id?: string;
}

/**
 * Default destination for any alert we don't recognise. The full
 * observability page is the safe landing zone — it shows every alert
 * with full context, so an unknown alert is still actionable from there.
 */
export const FALLBACK_HREF = "/observability";

interface RoutingRule {
  /** Prefix on `alert.id` that triggers this rule. */
  prefix: string;
  /**
   * Whether this rule needs `alert.service_id` populated. If true and
   * the field is empty, we fall through to FALLBACK_HREF rather than
   * minting a broken `/services/undefined` URL.
   */
  requiresServiceId?: boolean;
  /** Build the destination URL given a (verified-shape) alert. */
  buildHref: (alert: RoutableAlert) => string;
  /** Verb-phrase for the action affordance ("Open service", etc.). */
  actionLabel: string;
}

/**
 * Ordered list of routing rules. Order matters because some prefixes are
 * substrings of others (e.g. `alert-service-` could match three rules);
 * the earliest match wins, so list the most specific prefixes first.
 */
const ROUTING_TABLE: ReadonlyArray<RoutingRule> = [
  // Service-scoped rules (most specific first).
  {
    prefix: "alert-service-replicas-",
    requiresServiceId: true,
    buildHref: (a) => `/services/${a.service_id}`,
    actionLabel: "Open service",
  },
  {
    prefix: "alert-service-unhealthy-",
    requiresServiceId: true,
    buildHref: (a) => `/services/${a.service_id}`,
    actionLabel: "Open service",
  },
  {
    prefix: "alert-service-failed-",
    requiresServiceId: true,
    // The service detail page surfaces a Deployments tab; we land on the
    // service and let the operator switch tabs. A dedicated
    // `/services/<id>/deployments` route does not yet exist (see
    // app/(protected)/services/[id]/), so this is the spec-documented
    // fallback rather than a redirect.
    buildHref: (a) => `/services/${a.service_id}`,
    actionLabel: "View deployment history",
  },
  // Plan/usage overage.
  {
    prefix: "alert-usage-overage-",
    buildHref: () => "/usage",
    actionLabel: "View usage",
  },
  // Global metric rules — anchor links open the relevant panel.
  {
    prefix: "alert-cache-hit-low",
    buildHref: () => "/observability#cache",
    actionLabel: "Open observability",
  },
  {
    prefix: "alert-db-conn-high",
    buildHref: () => "/observability#database",
    actionLabel: "Open observability",
  },
  {
    prefix: "alert-build-failures",
    buildHref: () => "/observability#builds",
    actionLabel: "Open observability",
  },
  {
    prefix: "alert-error-rate-high",
    buildHref: () => "/observability",
    actionLabel: "Open observability",
  },
  {
    prefix: "alert-latency-high",
    buildHref: () => "/observability",
    actionLabel: "Open observability",
  },
];

function findRule(alertId: string): RoutingRule | undefined {
  return ROUTING_TABLE.find((rule) => alertId.startsWith(rule.prefix));
}

/**
 * Map an alert to the URL operators should land on when they click it.
 *
 * Falls through to {@link FALLBACK_HREF} (`/observability`) for:
 *   - any unknown ID prefix
 *   - service-scoped alerts whose `service_id` is missing/empty
 *
 * Never throws, never returns an empty string, never embeds `undefined`
 * in a path segment.
 */
export function alertHref(alert: RoutableAlert): string {
  const rule = findRule(alert.id);
  if (!rule) return FALLBACK_HREF;
  if (rule.requiresServiceId && !alert.service_id) {
    return FALLBACK_HREF;
  }
  return rule.buildHref(alert);
}

/**
 * Verb-phrase describing what clicking the alert will do. Used as the
 * accessible label for the row link and (when applicable) the
 * "Acknowledge"/etc. action menu's primary action.
 */
export function alertActionLabel(alert: RoutableAlert): string {
  const rule = findRule(alert.id);
  if (!rule) return "Open observability";
  if (rule.requiresServiceId && !alert.service_id) {
    return "Open observability";
  }
  return rule.actionLabel;
}
