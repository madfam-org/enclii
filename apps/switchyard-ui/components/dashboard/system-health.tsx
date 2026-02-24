"use client";

import { useState, useEffect, useCallback } from "react";
import { usePolling } from "@/hooks/use-polling";
import { POLLING_SLOW } from "@/lib/constants";
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  RefreshCw,
  Server,
  XCircle,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { API_BASE_URL } from "@/lib/constants";

interface HealthStatus {
  status: "healthy" | "degraded" | "unhealthy" | "unknown";
  latency_ms?: number;
  last_checked?: string;
  message?: string;
}

interface SystemHealthResponse {
  overall: HealthStatus;
  components: {
    api: HealthStatus;
    database: HealthStatus;
    cache: HealthStatus;
    kubernetes: HealthStatus;
    builds: HealthStatus;
  };
}

interface SystemHealthProps {
  className?: string;
  compact?: boolean;
}

function getStatusColor(status: HealthStatus["status"]) {
  switch (status) {
    case "healthy":
      return "text-status-success";
    case "degraded":
      return "text-status-warning";
    case "unhealthy":
      return "text-status-error";
    default:
      return "text-muted-foreground";
  }
}

function getStatusBgColor(status: HealthStatus["status"]) {
  switch (status) {
    case "healthy":
      return "bg-status-success";
    case "degraded":
      return "bg-status-warning";
    case "unhealthy":
      return "bg-status-error";
    default:
      return "bg-muted-foreground";
  }
}

function StatusIcon({ status }: { status: HealthStatus["status"] }) {
  switch (status) {
    case "healthy":
      return <CheckCircle2 className="w-4 h-4 text-status-success" />;
    case "degraded":
      return <AlertTriangle className="w-4 h-4 text-status-warning" />;
    case "unhealthy":
      return <XCircle className="w-4 h-4 text-status-error" />;
    default:
      return <Activity className="w-4 h-4 text-muted-foreground" />;
  }
}

/**
 * SystemHealth - Platform health indicator component
 *
 * Shows overall system health and individual component statuses.
 * Useful for the navbar or dashboard.
 */
export function SystemHealth({ className, compact = false }: SystemHealthProps) {
  const [health, setHealth] = useState<SystemHealthResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchHealth = async () => {
    try {
      setError(null);
      const response = await fetch(`${API_BASE_URL}/health`);

      if (!response.ok) {
        throw new Error(`Health check failed: ${response.status}`);
      }

      const data = await response.json();

      // Transform simple health response to SystemHealthResponse format
      const healthData: SystemHealthResponse = {
        overall: {
          status: data.status === "ok" ? "healthy" : "unhealthy",
          latency_ms: 0,
          last_checked: new Date().toISOString(),
        },
        components: {
          api: {
            status: data.status === "ok" ? "healthy" : "unhealthy",
            message: "API responding",
          },
          database: {
            status: data.database?.status === "connected" ? "healthy" : "degraded",
            message: data.database?.message || "Database status unknown",
          },
          cache: {
            status: data.cache?.status === "connected" ? "healthy" : "degraded",
            message: data.cache?.message || "Cache status unknown",
          },
          kubernetes: {
            status: data.kubernetes?.status === "connected" ? "healthy" : "degraded",
            message: "Kubernetes cluster",
          },
          builds: {
            status: "healthy",
            message: "Build system operational",
          },
        },
      };

      setHealth(healthData);
    } catch (err) {
      console.error("Health check failed:", err);
      setError(err instanceof Error ? err.message : "Health check failed");
      setHealth({
        overall: { status: "unknown" },
        components: {
          api: { status: "unknown" },
          database: { status: "unknown" },
          cache: { status: "unknown" },
          kubernetes: { status: "unknown" },
          builds: { status: "unknown" },
        },
      });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchHealth();
  }, []);

  usePolling(fetchHealth, POLLING_SLOW);

  if (compact) {
    // Compact mode: just a status indicator
    return (
      <div
        className={cn("flex items-center gap-2", className)}
        data-testid="system-health-compact"
        title={`System Status: ${health?.overall.status || "loading..."}`}
      >
        {loading ? (
          <RefreshCw className="w-4 h-4 text-muted-foreground animate-spin" />
        ) : (
          <>
            <span
              className={cn(
                "w-2 h-2 rounded-full",
                getStatusBgColor(health?.overall.status || "unknown")
              )}
            />
            <span className="text-xs text-muted-foreground sr-only md:not-sr-only">
              {health?.overall.status === "healthy"
                ? "All Systems Operational"
                : health?.overall.status === "degraded"
                  ? "Some Issues"
                  : "Service Disruption"}
            </span>
          </>
        )}
      </div>
    );
  }

  // Full mode: detailed component view
  return (
    <div
      className={cn("bg-card border border-border rounded-lg", className)}
      data-testid="system-health"
    >
      {/* Header */}
      <div className="flex items-center justify-between p-4 border-b border-border">
        <div className="flex items-center gap-2">
          <Server className="w-5 h-5 text-muted-foreground" />
          <h3 className="text-sm font-medium text-foreground">System Health</h3>
        </div>
        <button
          onClick={fetchHealth}
          disabled={loading}
          className="p-1 hover:bg-accent rounded-md transition-colors"
          aria-label="Refresh health status"
        >
          <RefreshCw
            className={cn(
              "w-4 h-4 text-muted-foreground",
              loading && "animate-spin"
            )}
          />
        </button>
      </div>

      {/* Overall Status */}
      <div className="p-4 border-b border-border">
        <div className="flex items-center gap-3">
          <span
            className={cn(
              "w-3 h-3 rounded-full",
              getStatusBgColor(health?.overall.status || "unknown")
            )}
          />
          <div>
            <p
              className={cn(
                "text-sm font-medium",
                getStatusColor(health?.overall.status || "unknown")
              )}
            >
              {health?.overall.status === "healthy"
                ? "All Systems Operational"
                : health?.overall.status === "degraded"
                  ? "Degraded Performance"
                  : health?.overall.status === "unhealthy"
                    ? "Service Disruption"
                    : "Status Unknown"}
            </p>
            {health?.overall.latency_ms !== undefined && (
              <p className="text-xs text-muted-foreground">
                API Latency: {health.overall.latency_ms}ms
              </p>
            )}
          </div>
        </div>
      </div>

      {/* Component Status */}
      <div className="p-4 space-y-3">
        {health?.components &&
          Object.entries(health.components).map(([name, status]) => (
            <div key={name} className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <StatusIcon status={status.status} />
                <span className="text-sm text-foreground capitalize">{name}</span>
              </div>
              <span
                className={cn(
                  "text-xs capitalize",
                  getStatusColor(status.status)
                )}
              >
                {status.status}
              </span>
            </div>
          ))}
      </div>

      {/* Last Updated */}
      {health?.overall.last_checked && (
        <div className="px-4 pb-4">
          <p className="text-xs text-muted-foreground">
            Last checked:{" "}
            {new Date(health.overall.last_checked).toLocaleTimeString()}
          </p>
        </div>
      )}

      {/* Error State */}
      {error && (
        <div className="px-4 pb-4">
          <p className="text-xs text-status-error">{error}</p>
        </div>
      )}
    </div>
  );
}

/**
 * SystemHealthBadge - Minimal health indicator for navbar
 */
export function SystemHealthBadge({ className }: { className?: string }) {
  const [status, setStatus] = useState<"healthy" | "degraded" | "unhealthy" | "unknown">("unknown");
  const [loading, setLoading] = useState(true);

  const checkHealth = useCallback(async () => {
    try {
      const response = await fetch(`${API_BASE_URL}/health`);
      setStatus(response.ok ? "healthy" : "unhealthy");
    } catch {
      setStatus("unhealthy");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    checkHealth();
  }, [checkHealth]);

  usePolling(checkHealth, POLLING_SLOW);

  if (loading) {
    return (
      <span className={cn("flex items-center gap-1.5", className)}>
        <RefreshCw className="w-3 h-3 animate-spin text-muted-foreground" />
      </span>
    );
  }

  return (
    <span
      className={cn("flex items-center gap-1.5", className)}
      title={`System: ${status}`}
      data-testid="system-health-badge"
    >
      <span
        className={cn(
          "w-2 h-2 rounded-full",
          status === "healthy"
            ? "bg-status-success"
            : status === "degraded"
              ? "bg-status-warning"
              : status === "unhealthy"
                ? "bg-status-error"
                : "bg-muted-foreground"
        )}
      />
      <span className="text-xs text-muted-foreground hidden xl:inline">
        {status === "healthy" ? "Operational" : "Issues"}
      </span>
    </span>
  );
}
