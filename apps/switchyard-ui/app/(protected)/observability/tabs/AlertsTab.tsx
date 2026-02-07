"use client";

/**
 * AlertsTab
 * Displays active alerts list
 */

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { AlertsResponse } from "../observability-types";

interface AlertsTabProps {
  alerts: AlertsResponse | null;
}

export function AlertsTab({ alerts }: AlertsTabProps) {
  const getSeverityColor = (severity: string) => {
    switch (severity) {
      case "critical":
        return "border-status-error/30 bg-status-error-muted";
      case "warning":
        return "border-status-warning/30 bg-status-warning-muted";
      case "info":
        return "border-status-info/30 bg-status-info-muted";
      default:
        return "border-muted bg-muted/50";
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>Active Alerts</CardTitle>
        <CardDescription>
          {alerts?.critical_count} critical, {alerts?.warning_count} warning,{" "}
          {alerts?.info_count} info
        </CardDescription>
      </CardHeader>
      <CardContent>
        {alerts?.alerts.length === 0 ? (
          <div className="py-12 text-center text-muted-foreground">
            <svg
              aria-hidden="true"
              className="mx-auto mb-4 h-12 w-12 text-status-success"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"
              />
            </svg>
            <p className="font-medium">No active alerts</p>
            <p className="mt-1 text-sm">All systems operating normally</p>
          </div>
        ) : (
          <div className="space-y-4">
            {alerts?.alerts.map((alert) => (
              <div
                key={alert.id}
                className={cn("rounded-lg border p-4", getSeverityColor(alert.severity))}
              >
                <div className="flex items-start justify-between">
                  <div className="flex-1">
                    <div className="mb-1 flex items-center gap-2">
                      <Badge
                        variant={
                          alert.severity === "critical" ? "destructive" : "secondary"
                        }
                      >
                        {alert.severity}
                      </Badge>
                      <span className="font-medium">{alert.name}</span>
                    </div>
                    <p className="text-sm text-muted-foreground">{alert.message}</p>
                    {alert.value !== undefined && alert.threshold !== undefined && (
                      <p className="mt-1 text-sm">
                        <span className="font-mono">
                          Current: {alert.value.toFixed(2)}
                        </span>
                        <span className="mx-2">|</span>
                        <span className="font-mono">
                          Threshold: {alert.threshold.toFixed(2)}
                        </span>
                      </p>
                    )}
                    {alert.service_name && (
                      <p className="mt-1 text-xs text-muted-foreground">
                        Service: {alert.service_name}
                      </p>
                    )}
                  </div>
                  <div className="ml-4 text-right text-xs text-muted-foreground">
                    <div>Fired {new Date(alert.fired_at).toLocaleTimeString()}</div>
                    <Badge variant="outline" className="mt-1">
                      {alert.status}
                    </Badge>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
