"use client";

/**
 * HealthTab
 * Displays service health status grid
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
import type { ServiceHealthResponse } from "../observability-types";

interface HealthTabProps {
  serviceHealth: ServiceHealthResponse | null;
}

export function HealthTab({ serviceHealth }: HealthTabProps) {
  const getStatusColor = (status: string) => {
    switch (status) {
      case "healthy":
        return "bg-status-success-muted text-status-success-muted-foreground";
      case "degraded":
        return "bg-status-warning-muted text-status-warning-muted-foreground";
      case "unhealthy":
        return "bg-status-error-muted text-status-error-muted-foreground";
      default:
        return "bg-status-neutral-muted text-status-neutral-muted-foreground";
    }
  };

  const getCardBorder = (status: string) => {
    switch (status) {
      case "healthy":
        return "border-status-success/30 bg-status-success-muted/50";
      case "degraded":
        return "border-status-warning/30 bg-status-warning-muted/50";
      case "unhealthy":
        return "border-status-error/30 bg-status-error-muted/50";
      default:
        return "border-muted";
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>Service Health</CardTitle>
        <CardDescription>
          {serviceHealth?.healthy_count} healthy, {serviceHealth?.degraded_count}{" "}
          degraded, {serviceHealth?.unhealthy_count} unhealthy
        </CardDescription>
      </CardHeader>
      <CardContent>
        {serviceHealth?.services.length === 0 ? (
          <div className="py-12 text-center text-muted-foreground">
            <svg
              aria-hidden="true"
              className="mx-auto mb-4 h-12 w-12 text-muted-foreground/50"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={1.5}
                d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2"
              />
            </svg>
            <p>No services found</p>
          </div>
        ) : (
          <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
            {serviceHealth?.services.map((service) => (
              <div
                key={service.service_id}
                className={cn("rounded-lg border p-4", getCardBorder(service.status))}
              >
                <div className="mb-2 flex items-center justify-between">
                  <span className="font-medium">{service.service_name}</span>
                  <Badge className={getStatusColor(service.status)}>
                    {service.status}
                  </Badge>
                </div>
                {service.project_slug && (
                  <p className="mb-2 text-xs text-muted-foreground">
                    {service.project_slug}
                  </p>
                )}
                <div className="grid grid-cols-2 gap-2 text-sm">
                  <div>
                    <span className="text-muted-foreground">Uptime:</span>
                    <span className="ml-1 font-mono">
                      {service.uptime.toFixed(1)}%
                    </span>
                  </div>
                  <div>
                    <span className="text-muted-foreground">Pods:</span>
                    <span className="ml-1 font-mono">
                      {service.ready_pods}/{service.pod_count}
                    </span>
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
