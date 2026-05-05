"use client";

import { useState, useEffect, useCallback, useMemo } from "react";
import {
  CheckCircle2,
  AlertTriangle,
  AlertOctagon,
  Info,
  ChevronRight,
} from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@enclii/ui-components/badge";
import { apiGet } from "@/lib/api";
import { usePolling } from "@/hooks/use-polling";
import { POLLING_SLOW } from "@/lib/constants";
import { formatRelativeTime } from "@/lib/formatting";
import Link from "next/link";
import { cn } from "@/lib/utils";
import { useIsAdminScope } from "@/contexts/ScopeContext";
import { alertHref, alertActionLabel } from "@/lib/alert-routing";
import { useMutedAlerts } from "@/hooks/use-muted-alerts";

interface Alert {
  id: string;
  name: string;
  severity: "critical" | "warning" | "info";
  fired_at: string;
  /**
   * Service-scoped alerts (replicas, unhealthy, deployment-failed)
   * carry the service UUID so the row can deep-link to /services/<id>.
   * Optional because global metric / overage alerts don't populate it.
   */
  service_id?: string;
}

/**
 * Plan-tier overage alerts emitted by switchyard-api
 * (observability_handlers.go ~line 602) all use the
 * `alert-usage-overage-<metric>` ID prefix. Master-admin scope is
 * self-hosted — there is no plan to be over — so these are suppressed.
 * The alert name "<Metric> Over Plan Limit" is checked as a defensive
 * fallback in case the ID scheme changes.
 */
function isPlanOverageAlert(a: Alert): boolean {
  return (
    a.id.startsWith("alert-usage-overage-") ||
    /Over Plan Limit$/i.test(a.name)
  );
}

interface AlertsResponse {
  alerts: Alert[];
  total: number;
}

const severityConfig = {
  critical: {
    icon: AlertOctagon,
    badge: "destructive" as const,
    dot: "bg-status-error",
  },
  warning: {
    icon: AlertTriangle,
    badge: "secondary" as const,
    dot: "bg-status-warning",
  },
  info: {
    icon: Info,
    badge: "outline" as const,
    dot: "bg-status-info",
  },
};

/**
 * Format an absolute mute deadline ("muted until 14:32") for the badge
 * shown on a dimmed alert row. Uses 24-hour locale-respecting format.
 */
function formatMutedUntil(mutedUntil: number): string {
  return new Date(mutedUntil).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function SidebarAlerts({ className }: { className?: string }) {
  const isAdmin = useIsAdminScope();
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const { isMuted, mutedUntil } = useMutedAlerts();

  const fetchAlerts = useCallback(async () => {
    try {
      const data = await apiGet<AlertsResponse>(
        "/v1/observability/alerts?limit=5",
      );
      const raw = data.alerts || [];
      const rawTotal = data.total || 0;
      // For master-admin scope, drop plan-tier overage alerts entirely —
      // they're fabricated for a self-hosted cluster (audit D-4).
      // Adjust `total` so the badge reflects only real alerts.
      if (isAdmin) {
        const dropped = raw.filter(isPlanOverageAlert).length;
        setAlerts(raw.filter((a) => !isPlanOverageAlert(a)));
        setTotal(Math.max(0, rawTotal - dropped));
      } else {
        setAlerts(raw);
        setTotal(rawTotal);
      }
    } catch {
      // Silently fail — alerts are supplementary
      setAlerts([]);
      setTotal(0);
    } finally {
      setLoading(false);
    }
  }, [isAdmin]);

  useEffect(() => {
    fetchAlerts();
  }, [fetchAlerts]);

  usePolling(fetchAlerts, POLLING_SLOW);

  // Filter muted alerts client-side. We keep them in the array but
  // render them dimmed so the operator can see they're suppressed
  // rather than wondering if the alert resolved itself.
  // The `total` badge counts unmuted active alerts only — that's what
  // the operator actually needs to triage.
  const visibleAlerts = useMemo(() => alerts, [alerts]);
  const unmutedCount = useMemo(
    () => alerts.filter((a) => !isMuted(a.id)).length,
    [alerts, isMuted],
  );
  // Adjust the displayed total: subtract muted alerts from the server
  // total. We can't perfectly know the server's muted count beyond
  // what we've seen in this page, but reducing by the locally-muted
  // count matches operator intent.
  const adjustedTotal = useMemo(() => {
    const mutedHere = alerts.length - unmutedCount;
    return Math.max(0, total - mutedHere);
  }, [alerts.length, unmutedCount, total]);

  return (
    <Card className={cn(className)}>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center justify-between text-sm font-medium">
          <span>Alerts</span>
          {adjustedTotal > 0 && (
            <Badge variant="destructive" className="h-5 px-1.5 text-xs">
              {adjustedTotal}
            </Badge>
          )}
        </CardTitle>
      </CardHeader>
      <CardContent>
        {loading ? (
          <div className="space-y-2">
            {[1, 2].map((i) => (
              <div key={i} className="flex items-center gap-2">
                <div className="bg-muted h-2 w-2 animate-pulse rounded-full" />
                <div className="bg-muted h-3 flex-1 animate-pulse rounded" />
              </div>
            ))}
          </div>
        ) : visibleAlerts.length === 0 ? (
          <div className="flex items-center gap-2 py-2 text-sm text-muted-foreground">
            <CheckCircle2 className="h-4 w-4 text-status-success" />
            <span>No active alerts</span>
          </div>
        ) : (
          <div className="space-y-1">
            {visibleAlerts.map((alert) => {
              const config = severityConfig[alert.severity] || severityConfig.info;
              const muted = isMuted(alert.id);
              const muteEnd = muted ? mutedUntil(alert.id) : null;
              const href = alertHref(alert);
              const action = alertActionLabel(alert);
              return (
                <Link
                  key={alert.id}
                  href={href}
                  aria-label={`${action}: ${alert.name}`}
                  data-testid="sidebar-alert-link"
                  data-alert-id={alert.id}
                  data-muted={muted ? "true" : "false"}
                  className={cn(
                    "group flex items-start gap-2 rounded-md px-1.5 py-1 text-xs transition-colors",
                    "hover:bg-accent/60 focus-visible:bg-accent/60",
                    "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                    muted && "opacity-60",
                  )}
                >
                  <span
                    className={cn(
                      "mt-1 h-2 w-2 shrink-0 rounded-full",
                      config.dot,
                    )}
                  />
                  <div className="min-w-0 flex-1">
                    <p className="truncate font-medium text-foreground">
                      {alert.name}
                    </p>
                    <p className="text-muted-foreground">
                      {formatRelativeTime(alert.fired_at)}
                      {muted && muteEnd && (
                        <span
                          className="ml-1 rounded bg-muted px-1 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground"
                          data-testid="muted-badge"
                        >
                          muted until {formatMutedUntil(muteEnd)}
                        </span>
                      )}
                    </p>
                  </div>
                  <ChevronRight
                    className="mt-1 h-3.5 w-3.5 shrink-0 text-muted-foreground opacity-60 transition-opacity group-hover:opacity-100"
                    aria-hidden="true"
                    data-testid="alert-chevron"
                  />
                </Link>
              );
            })}
            {total > 5 && (
              <Link
                href="/observability"
                className="block pt-1 text-xs font-medium text-enclii-blue hover:underline"
              >
                View all {total} alerts
              </Link>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

