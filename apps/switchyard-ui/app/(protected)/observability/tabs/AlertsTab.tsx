"use client";

/**
 * AlertsTab
 * Displays the active-alerts list on the full observability dashboard.
 *
 * Phase 1 makes every row clickable (deep-link via `alertHref`) and
 * actionable (per-row action menu). Mute state is local-only via
 * `useMutedAlerts`; muted rows are dimmed but kept in the list so
 * operators can still see and unmute them. Backend ack/mute endpoints
 * are stubbed in Phase 1 and will be wired in Phase 2.
 *
 * Layout: each row is an anchor (Next.js <Link>) wrapping the existing
 * card so the entire surface is keyboard-focusable. The action menu
 * sits in a sibling div so its trigger stops the click bubbling rather
 * than navigating.
 */

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Badge } from "@enclii/ui-components/badge";
import { ChevronRight } from "lucide-react";
import Link from "next/link";
import { cn } from "@/lib/utils";
import type { AlertsResponse, Alert } from "../observability-types";
import { alertHref, alertActionLabel } from "@/lib/alert-routing";
import { useMutedAlerts } from "@/hooks/use-muted-alerts";
import { AlertActionMenu } from "@/components/dashboard/alert-action-menu";

interface AlertsTabProps {
  alerts: AlertsResponse | null;
}

function getSeverityColor(severity: string): string {
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
}

function formatMutedUntil(mutedUntil: number): string {
  return new Date(mutedUntil).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
  });
}

interface AlertRowProps {
  alert: Alert;
}

function AlertRow({ alert }: AlertRowProps) {
  const { isMuted, mutedUntil, mute } = useMutedAlerts();
  const muted = isMuted(alert.id);
  const muteEnd = muted ? mutedUntil(alert.id) : null;
  const href = alertHref({ id: alert.id, service_id: alert.service_id });
  const action = alertActionLabel({
    id: alert.id,
    service_id: alert.service_id,
  });

  return (
    <div
      className={cn(
        "relative rounded-lg border transition-colors",
        getSeverityColor(alert.severity),
        muted && "opacity-60",
      )}
      data-alert-id={alert.id}
      data-muted={muted ? "true" : "false"}
    >
      {/* The link covers the row's content but stops short of the
          action menu so the menu trigger remains independently
          clickable. We use absolute positioning so the entire card
          surface is the click target without nesting interactive
          elements (anchors inside menu buttons would be invalid). */}
      <Link
        href={href}
        aria-label={`${action}: ${alert.name}`}
        data-testid="alerts-tab-row-link"
        className={cn(
          "absolute inset-0 rounded-lg",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
          "hover:bg-accent/30",
        )}
      >
        <span className="sr-only">{action}: {alert.name}</span>
      </Link>

      <div className="relative flex items-start justify-between gap-4 p-4">
        <div className="flex-1 min-w-0">
          <div className="mb-1 flex items-center gap-2">
            <Badge
              variant={
                alert.severity === "critical" ? "destructive" : "secondary"
              }
            >
              {alert.severity}
            </Badge>
            <span className="font-medium">{alert.name}</span>
            {muted && muteEnd && (
              <span
                className="rounded bg-muted px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground"
                data-testid="muted-badge"
              >
                muted until {formatMutedUntil(muteEnd)}
              </span>
            )}
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
        <div className="flex items-start gap-1 ml-4">
          <div className="text-right text-xs text-muted-foreground">
            <div>Fired {new Date(alert.fired_at).toLocaleTimeString()}</div>
            <Badge variant="outline" className="mt-1">
              {alert.status}
            </Badge>
          </div>
          {/* Action menu sits in a relative container so the absolute
              <Link> overlay above doesn't swallow its clicks. */}
          <div className="relative z-10">
            <AlertActionMenu
              alert={{
                id: alert.id,
                name: alert.name,
                service_id: alert.service_id,
              }}
              onMute={mute}
              isMuted={muted}
            />
          </div>
          <ChevronRight
            className="mt-1 h-4 w-4 text-muted-foreground"
            aria-hidden="true"
            data-testid="alerts-tab-chevron"
          />
        </div>
      </div>
    </div>
  );
}

export function AlertsTab({ alerts }: AlertsTabProps) {
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
          <div className="space-y-3">
            {alerts?.alerts.map((alert) => (
              <AlertRow key={alert.id} alert={alert} />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
