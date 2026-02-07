"use client";

/**
 * Observability Dashboard Page
 * Monitor metrics, health, errors, and alerts across services
 *
 * Split structure:
 * - observability-types.ts: Type definitions
 * - tabs/: Tab components (MetricsTab, HealthTab, ErrorsTab, AlertsTab)
 */

import { useState, useEffect, useCallback } from "react";
import { usePolling } from "@/hooks/use-polling";
import { POLLING_SLOW } from "@/lib/constants";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { apiGet } from "@/lib/api";
import { Spinner } from "@/components/ui/spinner";
import type {
  MetricsSnapshot,
  MetricsHistory,
  ServiceHealthResponse,
  RecentErrorsResponse,
  AlertsResponse,
  Tab,
  TabDefinition,
} from "./observability-types";
import { MetricsTab, HealthTab, ErrorsTab, AlertsTab } from "./tabs";

// =============================================================================
// TAB DEFINITIONS
// =============================================================================

const tabs: TabDefinition[] = [
  {
    id: "metrics",
    label: "Metrics",
    icon: (
      <svg aria-hidden="true" className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeWidth={2}
          d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z"
        />
      </svg>
    ),
  },
  {
    id: "health",
    label: "Service Health",
    icon: (
      <svg aria-hidden="true" className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeWidth={2}
          d="M4.318 6.318a4.5 4.5 0 000 6.364L12 20.364l7.682-7.682a4.5 4.5 0 00-6.364-6.364L12 7.636l-1.318-1.318a4.5 4.5 0 00-6.364 0z"
        />
      </svg>
    ),
  },
  {
    id: "errors",
    label: "Errors",
    icon: (
      <svg aria-hidden="true" className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeWidth={2}
          d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
        />
      </svg>
    ),
  },
  {
    id: "alerts",
    label: "Alerts",
    icon: (
      <svg aria-hidden="true" className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeWidth={2}
          d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9"
        />
      </svg>
    ),
  },
];

// =============================================================================
// MAIN COMPONENT
// =============================================================================

export default function ObservabilityPage() {
  const [activeTab, setActiveTab] = useState<Tab>("metrics");
  const [timeRange, setTimeRange] = useState("1h");
  const [snapshot, setSnapshot] = useState<MetricsSnapshot | null>(null);
  const [history, setHistory] = useState<MetricsHistory | null>(null);
  const [serviceHealth, setServiceHealth] = useState<ServiceHealthResponse | null>(
    null
  );
  const [errors, setErrors] = useState<RecentErrorsResponse | null>(null);
  const [alerts, setAlerts] = useState<AlertsResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchData = useCallback(async () => {
    try {
      setError(null);
      setLoading(true);

      const [snapshotData, historyData, healthData, errorsData, alertsData] =
        await Promise.all([
          apiGet<MetricsSnapshot>("/v1/observability/metrics"),
          apiGet<MetricsHistory>(`/v1/observability/metrics/history?range=${timeRange}`),
          apiGet<ServiceHealthResponse>("/v1/observability/health"),
          apiGet<RecentErrorsResponse>("/v1/observability/errors?limit=50"),
          apiGet<AlertsResponse>("/v1/observability/alerts"),
        ]);

      setSnapshot(snapshotData);
      setHistory(historyData);
      setServiceHealth(healthData);
      setErrors(errorsData);
      setAlerts(alertsData);
    } catch (err) {
      console.error("Failed to fetch observability data:", err);
      setError(
        err instanceof Error ? err.message : "Failed to fetch observability data"
      );
    } finally {
      setLoading(false);
    }
  }, [timeRange]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  usePolling(fetchData, POLLING_SLOW);

  if (loading && !snapshot) {
    return (
      <div className="flex items-center justify-center py-24">
        <Spinner size="lg" />
        <span className="ml-3 text-muted-foreground">
          Loading observability data...
        </span>
      </div>
    );
  }

  if (error) {
    return (
      <div className="py-24 text-center">
        <p className="mb-4 text-status-error">{error}</p>
        <Button variant="outline" onClick={fetchData}>
          Try Again
        </Button>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Observability</h1>
          <p className="text-muted-foreground">
            Monitor metrics, health, errors, and alerts across your services
          </p>
        </div>
        <div className="flex items-center gap-4">
          <select
            value={timeRange}
            onChange={(e) => setTimeRange(e.target.value)}
            className="rounded-md border bg-background px-3 py-2 text-sm"
          >
            <option value="1h">Last 1 hour</option>
            <option value="6h">Last 6 hours</option>
            <option value="24h">Last 24 hours</option>
            <option value="7d">Last 7 days</option>
          </select>
          <Button variant="outline" onClick={fetchData}>
            <svg
              aria-hidden="true"
              className="mr-2 h-4 w-4"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
              />
            </svg>
            Refresh
          </Button>
        </div>
      </div>

      {/* Summary Cards */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Error Rate</CardTitle>
            <svg
              aria-hidden="true"
              className="h-4 w-4 text-muted-foreground"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
              />
            </svg>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {((snapshot?.http.error_rate || 0) * 100).toFixed(2)}%
            </div>
            <p className="text-xs text-muted-foreground">HTTP request error rate</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Avg Latency</CardTitle>
            <svg
              aria-hidden="true"
              className="h-4 w-4 text-muted-foreground"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"
              />
            </svg>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {((snapshot?.http.average_latency || 0) * 1000).toFixed(0)}ms
            </div>
            <p className="text-xs text-muted-foreground">Average response time</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Healthy Services</CardTitle>
            <svg
              aria-hidden="true"
              className="h-4 w-4 text-status-success"
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
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-status-success">
              {serviceHealth?.healthy_count || 0}/{serviceHealth?.services.length || 0}
            </div>
            <p className="text-xs text-muted-foreground">Services running healthy</p>
          </CardContent>
        </Card>

        <Card
          className={cn(
            alerts && alerts.critical_count > 0 &&
              "border-status-error/30 bg-status-error-muted"
          )}
        >
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Active Alerts</CardTitle>
            <svg
              aria-hidden="true"
              className={cn(
                "h-4 w-4",
                alerts && alerts.critical_count > 0
                  ? "text-status-error"
                  : "text-muted-foreground"
              )}
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9"
              />
            </svg>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{alerts?.alerts.length || 0}</div>
            <p className="text-xs text-muted-foreground">
              {alerts?.critical_count || 0} critical, {alerts?.warning_count || 0}{" "}
              warning
            </p>
          </CardContent>
        </Card>
      </div>

      {/* Tab Navigation */}
      <div className="border-b">
        <nav className="flex space-x-8" aria-label="Tabs">
          {tabs.map((tab) => (
            <button
              key={tab.id}
              onClick={() => setActiveTab(tab.id)}
              className={cn(
                "flex items-center gap-2 border-b-2 px-1 py-4 text-sm font-medium",
                activeTab === tab.id
                  ? "border-primary text-primary"
                  : "border-transparent text-muted-foreground hover:border-muted-foreground/50 hover:text-foreground"
              )}
            >
              {tab.icon}
              {tab.label}
              {tab.id === "alerts" && alerts && alerts.alerts.length > 0 && (
                <Badge
                  variant={alerts.critical_count > 0 ? "destructive" : "secondary"}
                  className="ml-1"
                >
                  {alerts.alerts.length}
                </Badge>
              )}
            </button>
          ))}
        </nav>
      </div>

      {/* Tab Content */}
      {activeTab === "metrics" && (
        <MetricsTab snapshot={snapshot} history={history} timeRange={timeRange} />
      )}
      {activeTab === "health" && <HealthTab serviceHealth={serviceHealth} />}
      {activeTab === "errors" && <ErrorsTab errors={errors} />}
      {activeTab === "alerts" && <AlertsTab alerts={alerts} />}
    </div>
  );
}
