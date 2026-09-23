/**
 * Observability Types
 * Type definitions for observability dashboard data
 */

// =============================================================================
// METRICS TYPES
// =============================================================================

export interface MetricsSnapshot {
  timestamp: string;
  http: {
    requests_per_second: number;
    average_latency: number;
    error_rate: number;
  };
  database: {
    connections_open: number;
    connections_in_use: number;
    average_query_time: number;
    error_rate: number;
  };
  cache: {
    hit_rate: number;
    average_latency: number;
    operations_per_second: number;
  };
  builds: {
    success_rate: number;
    average_duration: number;
    queue_length: number;
  };
  kubernetes: {
    operation_latency: number;
    error_rate: number;
    active_pods: number;
  };
}

export interface MetricsDataPoint {
  timestamp: string;
  requests_per_sec: number;
  average_latency_ms: number;
  error_rate: number;
  cpu_usage: number;
  memory_usage: number;
}

export interface MetricsHistory {
  time_range: string;
  resolution: string;
  data_points: MetricsDataPoint[];
}

// =============================================================================
// HEALTH TYPES
// =============================================================================

export interface ServiceHealth {
  service_id: string;
  service_name: string;
  project_slug: string;
  status: string;
  uptime: number;
  response_time_ms: number;
  error_rate: number;
  last_checked: string;
  pod_count: number;
  ready_pods: number;
  deployment_name?: string;
  observation_reason?: string;
}

export interface ServiceHealthResponse {
  services: ServiceHealth[];
  healthy_count: number;
  degraded_count: number;
  unhealthy_count: number;
  timestamp: string;
}

// =============================================================================
// ERROR TYPES
// =============================================================================

export interface ErrorEntry {
  id: string;
  timestamp: string;
  service_id: string;
  service_name: string;
  level: string;
  message: string;
  stack_trace?: string;
  count: number;
  last_seen: string;
  first_seen: string;
  resolved: boolean;
}

export interface RecentErrorsResponse {
  errors: ErrorEntry[];
  total_count: number;
  time_range: string;
}

// =============================================================================
// ALERT TYPES
// =============================================================================

export interface Alert {
  id: string;
  name: string;
  severity: string;
  status: string;
  message: string;
  service_id?: string;
  service_name?: string;
  value?: number;
  threshold?: number;
  fired_at: string;
  resolved_at?: string;
  labels?: Record<string, string>;
}

export interface AlertsResponse {
  alerts: Alert[];
  critical_count: number;
  warning_count: number;
  info_count: number;
  timestamp: string;
}

// =============================================================================
// UI TYPES
// =============================================================================

export type Tab = "metrics" | "health" | "errors" | "alerts";

export interface TabDefinition {
  id: Tab;
  label: string;
  icon: React.ReactNode;
}
