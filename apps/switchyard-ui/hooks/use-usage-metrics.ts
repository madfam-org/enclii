'use client';

import { useState, useEffect, useCallback } from 'react';
import { usePolling } from '@/hooks/use-polling';
import { apiGet } from '@/lib/api';

// =============================================================================
// TYPES
// =============================================================================

export interface UsageMetric {
  type: string;
  label: string;
  used: number;
  included: number;
  unit: string;
  cost: number;
}

export interface UsageSummary {
  period_start: string;
  period_end: string;
  metrics: UsageMetric[];
  total_cost: number;
  plan_base: number;
  grand_total: number;
  plan_name: string;
}

export interface RealtimeMetrics {
  total_cpu_millicores: number;
  total_memory_mb: number;
  total_pods: number;
  metrics_enabled: boolean;
  services: ServiceMetrics[];
  collected_at: string;
}

export interface ServiceMetrics {
  service_id: string;
  service_name: string;
  namespace: string;
  pod_count: number;
  cpu_usage_millicores: number;
  memory_usage_mb: number;
  status: string;
}

interface UseUsageMetricsReturn {
  usage: UsageSummary | null;
  realtime: RealtimeMetrics | null;
  isLoading: boolean;
  error: string | null;
  refetch: () => Promise<void>;
}

interface UseUsageMetricsOptions {
  /** Auto-refresh interval in milliseconds (0 to disable) */
  refreshInterval?: number;
  /** Include realtime metrics */
  includeRealtime?: boolean;
}

// =============================================================================
// MAIN HOOK
// =============================================================================

/**
 * Hook to fetch usage metrics from the API
 * Designed to integrate with CircularGauge components
 */
export function useUsageMetrics(options: UseUsageMetricsOptions = {}): UseUsageMetricsReturn {
  const { refreshInterval = 0, includeRealtime = false } = options;

  const [usage, setUsage] = useState<UsageSummary | null>(null);
  const [realtime, setRealtime] = useState<RealtimeMetrics | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchData = useCallback(async () => {
    try {
      setError(null);

      const promises: Promise<unknown>[] = [apiGet<UsageSummary>('/v1/usage')];

      if (includeRealtime) {
        promises.push(apiGet<RealtimeMetrics>('/v1/usage/realtime').catch(() => null));
      }

      const [usageData, realtimeData] = await Promise.all(promises);

      setUsage(usageData as UsageSummary);
      if (includeRealtime) {
        setRealtime(realtimeData as RealtimeMetrics | null);
      }
    } catch (err) {
      console.error('Failed to fetch usage metrics:', err);
      setError(err instanceof Error ? err.message : 'Failed to fetch usage data');
    } finally {
      setIsLoading(false);
    }
  }, [includeRealtime]);

  // Initial fetch
  useEffect(() => {
    fetchData();
  }, [fetchData]);

  // Auto-refresh
  usePolling(fetchData, refreshInterval, { enabled: refreshInterval > 0 });

  return {
    usage,
    realtime,
    isLoading,
    error,
    refetch: fetchData,
  };
}

// =============================================================================
// SPECIALIZED HOOKS
// =============================================================================

interface MetricData {
  used: number;
  limit: number;
  percentage: number;
  cost: number;
  unit: string;
}

/**
 * Hook to get a specific metric by type (compute, build, storage, bandwidth, domains)
 */
export function useMetricByType(type: string): MetricData & { isLoading: boolean; error: string | null } {
  const { usage, isLoading, error } = useUsageMetrics();

  const metric = usage?.metrics.find(m => m.type === type);

  return {
    used: metric?.used || 0,
    limit: metric?.included || 0,
    percentage: metric && metric.included > 0 ? (metric.used / metric.included) * 100 : 0,
    cost: metric?.cost || 0,
    unit: metric?.unit || '',
    isLoading,
    error,
  };
}

/**
 * Hook for compute hours metric
 */
export function useComputeUsage() {
  return useMetricByType('compute');
}

/**
 * Hook for build minutes metric
 */
export function useBuildUsage() {
  return useMetricByType('build');
}

/**
 * Hook for storage metric
 */
export function useStorageUsage() {
  return useMetricByType('storage');
}

/**
 * Hook for bandwidth metric
 */
export function useBandwidthUsage() {
  return useMetricByType('bandwidth');
}

/**
 * Hook for realtime resource metrics with auto-refresh
 */
export function useRealtimeResources(refreshIntervalMs: number = 30000) {
  const [metrics, setMetrics] = useState<RealtimeMetrics | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchData = useCallback(async () => {
    try {
      const data = await apiGet<RealtimeMetrics>('/v1/usage/realtime');
      setMetrics(data);
      setError(null);
    } catch (err) {
      console.error('Failed to fetch realtime metrics:', err);
      setError(err instanceof Error ? err.message : 'Failed to fetch realtime metrics');
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  usePolling(fetchData, refreshIntervalMs, { enabled: refreshIntervalMs > 0 });

  return {
    metrics,
    isLoading,
    error,
    refetch: fetchData,
    // Convenience accessors
    cpuUsage: metrics?.total_cpu_millicores || 0,
    memoryUsage: metrics?.total_memory_mb || 0,
    podCount: metrics?.total_pods || 0,
    services: metrics?.services || [],
    isMetricsEnabled: metrics?.metrics_enabled || false,
  };
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// Re-export formatting helpers for backward compatibility
export { formatBytes, formatNumber } from '@/lib/formatting';

/**
 * Get color based on usage percentage
 */
export function getUsageColor(percentage: number): 'success' | 'warning' | 'danger' {
  if (percentage >= 90) return 'danger';
  if (percentage >= 75) return 'warning';
  return 'success';
}
