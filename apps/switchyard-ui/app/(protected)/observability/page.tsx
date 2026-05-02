"use client";

/**
 * Observability Dashboard Page
 * Monitor metrics, health, errors, and alerts across services
 *
 * Each panel (metrics, history, health, errors, alerts) loads
 * INDEPENDENTLY via Promise.allSettled — a 503 from Sentry or a timeout
 * from one upstream must NEVER block the others. This used to be a
 * single Promise.all which produced an indefinite spinner whenever any
 * one of the five endpoints stalled. Now each panel has its own
 * status/error/data triple and an inline retry.
 */

import { useState, useEffect, useCallback } from "react";
import { usePolling } from "@/hooks/use-polling";
import { POLLING_SLOW } from "@/lib/constants";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@enclii/ui-components/button";
import { Badge } from "@enclii/ui-components/badge";
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
import { BarChart2, Activity, AlertTriangle, Bell, Clock, CheckCircle2, RefreshCw } from "lucide-react";

// =============================================================================
// TAB DEFINITIONS
// =============================================================================

const tabs: TabDefinition[] = [
  {
    id: "metrics",
    label: "Metrics",
    icon: <BarChart2 className="h-4 w-4" />,
  },
  {
    id: "health",
    label: "Service Health",
    icon: <Activity className="h-4 w-4" />,
  },
  {
    id: "errors",
    label: "Errors",
    icon: <AlertTriangle className="h-4 w-4" />,
  },
  {
    id: "alerts",
    label: "Alerts",
    icon: <Bell className="h-4 w-4" />,
  },
];

// Per-panel async state. Keeping data + status + error in one object means
// a panel that 503s can render its own retry button without affecting the
// other three panels — which used to be impossible under Promise.all.
interface PanelState<T> {
  data: T | null;
  status: "idle" | "loading" | "ready" | "error";
  error: string | null;
}

const initialPanel = <T,>(): PanelState<T> => ({
  data: null,
  status: "idle",
  error: null,
});

function describeError(err: unknown): string {
  const msg = err instanceof Error ? err.message : String(err);
  if (/timed out/i.test(msg)) {
    return "Request took longer than expected. The upstream may be slow — please retry.";
  }
  return msg;
}

// Inline panel error tile, used by every tab when its endpoint fails.
function PanelError({
  message,
  onRetry,
  label,
}: {
  message: string;
  onRetry: () => void;
  label: string;
}) {
  return (
    <Card className="border-status-error/30 bg-status-error-muted">
      <CardContent className="py-6 flex items-center justify-between gap-4">
        <div>
          <p className="font-medium text-status-error">Failed to load {label}</p>
          <p className="text-xs text-muted-foreground mt-1">{message}</p>
        </div>
        <Button variant="outline" size="sm" onClick={onRetry}>
          <RefreshCw className="h-3 w-3 mr-1" />
          Retry
        </Button>
      </CardContent>
    </Card>
  );
}

function PanelLoading({ label }: { label: string }) {
  return (
    <Card>
      <CardContent className="py-12 flex items-center justify-center text-muted-foreground">
        <Spinner size="lg" />
        <span className="ml-3">Loading {label}...</span>
      </CardContent>
    </Card>
  );
}

// =============================================================================
// MAIN COMPONENT
// =============================================================================

export default function ObservabilityPage() {
  const [activeTab, setActiveTab] = useState<Tab>("metrics");
  const [timeRange, setTimeRange] = useState("1h");

  const [snapshot, setSnapshot] = useState<PanelState<MetricsSnapshot>>(initialPanel);
  const [history, setHistory] = useState<PanelState<MetricsHistory>>(initialPanel);
  const [serviceHealth, setServiceHealth] = useState<PanelState<ServiceHealthResponse>>(initialPanel);
  const [errors, setErrors] = useState<PanelState<RecentErrorsResponse>>(initialPanel);
  const [alerts, setAlerts] = useState<PanelState<AlertsResponse>>(initialPanel);

  // Generic single-panel fetcher. Always resolves the panel state; never
  // throws. The optional `silent` flag is used by polling refreshes so a
  // transient hiccup doesn't flash a stale-data panel into an error state.
  const loadPanel = useCallback(
    async <T,>(
      endpoint: string,
      setter: React.Dispatch<React.SetStateAction<PanelState<T>>>,
      opts: { silent?: boolean } = {}
    ) => {
      if (!opts.silent) {
        setter((prev) => ({ ...prev, status: "loading", error: null }));
      }
      try {
        const data = await apiGet<T>(endpoint);
        setter({ data, status: "ready", error: null });
      } catch (err) {
        console.error(`Observability panel failed (${endpoint}):`, err);
        setter((prev) => ({
          // Keep the last good data on a refresh failure so polling errors
          // don't blank out a working panel — only blank when we never had data.
          data: opts.silent ? prev.data : null,
          status: "error",
          error: describeError(err),
        }));
      }
    },
    []
  );

  const loadSnapshot = useCallback(
    (silent = false) => loadPanel<MetricsSnapshot>("/v1/observability/metrics", setSnapshot, { silent }),
    [loadPanel]
  );
  const loadHistory = useCallback(
    (silent = false) =>
      loadPanel<MetricsHistory>(
        `/v1/observability/metrics/history?range=${timeRange}`,
        setHistory,
        { silent }
      ),
    [loadPanel, timeRange]
  );
  const loadHealth = useCallback(
    (silent = false) => loadPanel<ServiceHealthResponse>("/v1/observability/health", setServiceHealth, { silent }),
    [loadPanel]
  );
  const loadErrors = useCallback(
    (silent = false) => loadPanel<RecentErrorsResponse>("/v1/observability/errors?limit=50", setErrors, { silent }),
    [loadPanel]
  );
  const loadAlerts = useCallback(
    (silent = false) => loadPanel<AlertsResponse>("/v1/observability/alerts", setAlerts, { silent }),
    [loadPanel]
  );

  // Refresh fans out via Promise.allSettled so a stuck panel can't hang
  // the rest. Manual refresh = visible spinners; polling refresh = silent.
  const refreshAll = useCallback(
    async (silent = false) => {
      await Promise.allSettled([
        loadSnapshot(silent),
        loadHistory(silent),
        loadHealth(silent),
        loadErrors(silent),
        loadAlerts(silent),
      ]);
    },
    [loadSnapshot, loadHistory, loadHealth, loadErrors, loadAlerts]
  );

  // Initial load
  useEffect(() => {
    refreshAll(false);
  }, [refreshAll]);

  // Background refresh: silent so polling failures don't toggle UI.
  usePolling(() => refreshAll(true), POLLING_SLOW);

  const snapshotData = snapshot.data;
  const healthData = serviceHealth.data;
  const alertsData = alerts.data;

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
          <Button variant="outline" onClick={() => refreshAll(false)}>
            <RefreshCw className="mr-2 h-4 w-4" />
            Refresh
          </Button>
        </div>
      </div>

      {/* Summary Cards — show "—" when underlying panel hasn't resolved
          yet so we never block the page on a single missing dataset. */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Error Rate</CardTitle>
            <AlertTriangle className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {snapshotData
                ? `${(snapshotData.http.error_rate * 100).toFixed(2)}%`
                : snapshot.status === "error"
                  ? "—"
                  : "…"}
            </div>
            <p className="text-xs text-muted-foreground">HTTP request error rate</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Avg Latency</CardTitle>
            <Clock className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {snapshotData
                ? `${(snapshotData.http.average_latency * 1000).toFixed(0)}ms`
                : snapshot.status === "error"
                  ? "—"
                  : "…"}
            </div>
            <p className="text-xs text-muted-foreground">Average response time</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Healthy Services</CardTitle>
            <CheckCircle2 className="h-4 w-4 text-status-success" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-status-success">
              {healthData
                ? `${healthData.healthy_count}/${healthData.services.length}`
                : serviceHealth.status === "error"
                  ? "—"
                  : "…"}
            </div>
            <p className="text-xs text-muted-foreground">Services running healthy</p>
          </CardContent>
        </Card>

        <Card
          className={cn(
            alertsData && alertsData.critical_count > 0 &&
              "border-status-error/30 bg-status-error-muted"
          )}
        >
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Active Alerts</CardTitle>
            <Bell
              className={cn(
                "h-4 w-4",
                alertsData && alertsData.critical_count > 0
                  ? "text-status-error"
                  : "text-muted-foreground"
              )}
            />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {alertsData
                ? alertsData.alerts.length
                : alerts.status === "error"
                  ? "—"
                  : "…"}
            </div>
            <p className="text-xs text-muted-foreground">
              {alertsData?.critical_count ?? 0} critical, {alertsData?.warning_count ?? 0}{" "}
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
              {tab.id === "alerts" && alertsData && alertsData.alerts.length > 0 && (
                <Badge
                  variant={alertsData.critical_count > 0 ? "destructive" : "secondary"}
                  className="ml-1"
                >
                  {alertsData.alerts.length}
                </Badge>
              )}
            </button>
          ))}
        </nav>
      </div>

      {/* Tab Content. Each tab handles its own loading / error / data
          state so a 503 in one upstream never freezes the others. */}
      {activeTab === "metrics" &&
        (snapshot.status === "error" && !snapshot.data ? (
          <PanelError
            label="metrics"
            message={snapshot.error || "Unknown error"}
            onRetry={() => {
              loadSnapshot(false);
              loadHistory(false);
            }}
          />
        ) : snapshot.status === "loading" && !snapshot.data ? (
          <PanelLoading label="metrics" />
        ) : (
          <MetricsTab snapshot={snapshot.data} history={history.data} timeRange={timeRange} />
        ))}

      {activeTab === "health" &&
        (serviceHealth.status === "error" && !serviceHealth.data ? (
          <PanelError
            label="service health"
            message={serviceHealth.error || "Unknown error"}
            onRetry={() => loadHealth(false)}
          />
        ) : serviceHealth.status === "loading" && !serviceHealth.data ? (
          <PanelLoading label="service health" />
        ) : (
          <HealthTab serviceHealth={serviceHealth.data} />
        ))}

      {activeTab === "errors" &&
        (errors.status === "error" && !errors.data ? (
          <PanelError
            label="errors"
            message={errors.error || "Unknown error"}
            onRetry={() => loadErrors(false)}
          />
        ) : errors.status === "loading" && !errors.data ? (
          <PanelLoading label="errors" />
        ) : (
          <ErrorsTab errors={errors.data} />
        ))}

      {activeTab === "alerts" &&
        (alerts.status === "error" && !alerts.data ? (
          <PanelError
            label="alerts"
            message={alerts.error || "Unknown error"}
            onRetry={() => loadAlerts(false)}
          />
        ) : alerts.status === "loading" && !alerts.data ? (
          <PanelLoading label="alerts" />
        ) : (
          <AlertsTab alerts={alerts.data} />
        ))}
    </div>
  );
}
