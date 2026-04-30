"use client";

import { useState, useEffect, useCallback } from "react";
import { CheckCircle2, AlertTriangle, AlertOctagon, Info } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@enclii/ui-components/badge";
import { apiGet } from "@/lib/api";
import { usePolling } from "@/hooks/use-polling";
import { POLLING_SLOW } from "@/lib/constants";
import { formatRelativeTime } from "@/lib/formatting";
import Link from "next/link";
import { cn } from "@/lib/utils";

interface Alert {
  id: string;
  name: string;
  severity: "critical" | "warning" | "info";
  fired_at: string;
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

export function SidebarAlerts({ className }: { className?: string }) {
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);

  const fetchAlerts = useCallback(async () => {
    try {
      const data = await apiGet<AlertsResponse>(
        "/v1/observability/alerts?limit=5",
      );
      setAlerts(data.alerts || []);
      setTotal(data.total || 0);
    } catch {
      // Silently fail — alerts are supplementary
      setAlerts([]);
      setTotal(0);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchAlerts();
  }, [fetchAlerts]);

  usePolling(fetchAlerts, POLLING_SLOW);

  return (
    <Card className={cn(className)}>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center justify-between text-sm font-medium">
          <span>Alerts</span>
          {total > 0 && (
            <Badge variant="destructive" className="h-5 px-1.5 text-xs">
              {total}
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
        ) : alerts.length === 0 ? (
          <div className="flex items-center gap-2 py-2 text-sm text-muted-foreground">
            <CheckCircle2 className="h-4 w-4 text-status-success" />
            <span>No active alerts</span>
          </div>
        ) : (
          <div className="space-y-2">
            {alerts.map((alert) => {
              const config = severityConfig[alert.severity] || severityConfig.info;
              return (
                <div
                  key={alert.id}
                  className="flex items-start gap-2 text-xs"
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
                    </p>
                  </div>
                </div>
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
