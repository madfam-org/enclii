'use client';

import { useState, useEffect, useCallback } from 'react';
import { usePolling } from '@/hooks/use-polling';
import { POLLING_SLOW } from '@/lib/constants';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from "@enclii/ui-components/button";
import { Spinner } from '@/components/ui/spinner';
import { apiGet } from '@/lib/api';
import { useIsAdminScope } from '@/contexts/ScopeContext';

/**
 * One usage figure. `unavailable` means nothing measured it: `used` is then
 * 0 and means nothing. `included` is -1 (no allowance) and `cost` is 0 for
 * every metric: Enclii has no price list (RFC 0049 Phase 1 truth pass).
 */
interface UsageMetric {
  type: string;
  label: string;
  used: number;
  included: number;
  unit: string;
  cost: number;
  unavailable?: boolean;
  note?: string;
  source?: string;
}

interface UsageSummary {
  period_start: string;
  period_end: string;
  metrics: UsageMetric[];
  total_cost: number;
  priced?: boolean;
  pricing_note?: string;
}

interface ServiceMetrics {
  service_id: string;
  service_name: string;
  namespace: string;
  pod_count: number;
  cpu_usage_millicores: number;
  memory_usage_mb: number;
  status: string;
}

interface RealtimeMetrics {
  total_cpu_millicores: number;
  total_memory_mb: number;
  total_pods: number;
  metrics_enabled: boolean;
  services: ServiceMetrics[];
  collected_at: string;
}

function formatNumber(num: number): string {
  if (num >= 1000) {
    return (num / 1000).toFixed(1) + 'k';
  }
  return num.toFixed(1);
}

const iconMap: Record<string, React.ReactNode> = {
  compute: (
    <svg aria-hidden="true" className="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 3v2m6-2v2M9 19v2m6-2v2M5 9H3m2 6H3m18-6h-2m2 6h-2M7 19h10a2 2 0 002-2V7a2 2 0 00-2-2H7a2 2 0 00-2 2v10a2 2 0 002 2zM9 9h6v6H9V9z" />
    </svg>
  ),
  build: (
    <svg aria-hidden="true" className="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
    </svg>
  ),
  storage: (
    <svg aria-hidden="true" className="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 7v10c0 2.21 3.582 4 8 4s8-1.79 8-4V7M4 7c0 2.21 3.582 4 8 4s8-1.79 8-4M4 7c0-2.21 3.582-4 8-4s8 1.79 8 4m0 5c0 2.21-3.582 4-8 4s-8-1.79-8-4" />
    </svg>
  ),
  bandwidth: (
    <svg aria-hidden="true" className="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 10V3L4 14h7v7l9-11h-7z" />
    </svg>
  ),
  domains: (
    <svg aria-hidden="true" className="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9" />
    </svg>
  ),
};

export default function UsagePage() {
  const isAdmin = useIsAdminScope();
  const [usage, setUsage] = useState<UsageSummary | null>(null);
  const [realtime, setRealtime] = useState<RealtimeMetrics | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchData = useCallback(async () => {
    try {
      setError(null);
      setLoading(true);

      const [usageData, realtimeData] = await Promise.all([
        apiGet<UsageSummary>('/v1/usage'),
        apiGet<RealtimeMetrics>('/v1/usage/realtime').catch(() => null),
      ]);

      setUsage(usageData);
      setRealtime(realtimeData);
    } catch (err) {
      console.error('Failed to fetch usage data:', err);
      setError(err instanceof Error ? err.message : 'Failed to fetch usage data');
    } finally {
      setLoading(false);
    }
  }, []);

  const fetchRealtimeMetrics = useCallback(() => {
    apiGet<RealtimeMetrics>('/v1/usage/realtime')
      .then(setRealtime)
      .catch(console.error);
  }, []);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  // Auto-refresh realtime metrics every 30 seconds
  usePolling(fetchRealtimeMetrics, POLLING_SLOW);

  if (loading) {
    return (
      <div className="flex items-center justify-center py-24">
        <Spinner size="lg" />
        <span className="ml-3 text-muted-foreground">Loading usage data...</span>
      </div>
    );
  }

  if (error) {
    return (
      <div className="text-center py-24">
        <p className="text-status-error mb-4">{error}</p>
        <Button variant="outline" onClick={fetchData}>
          Try Again
        </Button>
      </div>
    );
  }

  const pricingNote =
    usage?.pricing_note ||
    'Not priced. Customer billing is handled by Dhanam; infrastructure cost comes from the provider invoice.';

  const displayUsed = (metric: UsageMetric): string => {
    if (metric.unavailable) return 'Not measured yet';
    if (metric.used === null || metric.used === undefined || Number.isNaN(metric.used)) return '—';
    return `${formatNumber(metric.used)} ${metric.unit}`;
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">
            {isAdmin ? 'Cluster utilization' : 'Infrastructure Usage'}
          </h1>
          <p className="text-muted-foreground">
            Measured usage for the current period. Nothing on this page is priced or estimated.
          </p>
        </div>
        <Button variant="outline" onClick={fetchData}>
          <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          Refresh
        </Button>
      </div>

      {/* Period and pricing. The "Infrastructure Cost" pill this replaces
          multiplied guessed usage by invented rates ($0.05/GB-hour over a
          500 GB-hour "allowance"); there is no price list to apply. */}
      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Current Period</CardTitle>
            <svg aria-hidden="true" className="w-4 h-4 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" />
            </svg>
          </CardHeader>
          <CardContent>
            <div className="text-lg font-bold">
              {usage?.period_start} - {usage?.period_end}
            </div>
            <p className="text-xs text-muted-foreground">Metering period (calendar month, UTC)</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Cost</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-lg font-bold" data-testid="usage-pricing">Not priced</div>
            <p className="text-xs text-muted-foreground">{pricingNote}</p>
          </CardContent>
        </Card>
      </div>

      {/* Real-time Resource Metrics */}
      {realtime && (
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between">
              <div>
                <CardTitle className="flex items-center gap-2">
                  <span className="relative flex h-3 w-3">
                    <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-status-success opacity-75"></span>
                    <span className="relative inline-flex rounded-full h-3 w-3 bg-status-success"></span>
                  </span>
                  Live Resource Usage
                </CardTitle>
                <CardDescription>
                  Live from metrics-server: cluster totals, then the Enclii services in it
                  {realtime.collected_at && (
                    <span className="ml-2 text-xs">
                      (Updated {new Date(realtime.collected_at).toLocaleTimeString()})
                    </span>
                  )}
                </CardDescription>
              </div>
              {!realtime.metrics_enabled && (
                <span className="text-xs bg-status-warning-muted text-status-warning-foreground px-2 py-1 rounded">
                  Resource metrics unreadable
                </span>
              )}
            </div>
          </CardHeader>
          <CardContent>
            {realtime.metrics_enabled ? (
              <>
                {/* Cluster Summary */}
                <div className="grid gap-4 md:grid-cols-3 mb-6">
                  <div className="bg-primary/5 rounded-lg p-4">
                    <div className="text-sm text-muted-foreground">Total CPU</div>
                    <div className="text-2xl font-bold text-primary">
                      {(realtime.total_cpu_millicores / 1000).toFixed(2)} cores
                    </div>
                    <div className="text-xs text-muted-foreground">
                      In use now, all pods in the cluster
                    </div>
                  </div>
                  <div className="bg-purple-500/5 rounded-lg p-4">
                    <div className="text-sm text-muted-foreground">Total Memory</div>
                    <div className="text-2xl font-bold text-purple-600">
                      {realtime.total_memory_mb >= 1024
                        ? (realtime.total_memory_mb / 1024).toFixed(2) + ' GB'
                        : realtime.total_memory_mb.toFixed(0) + ' MB'}
                    </div>
                    <div className="text-xs text-muted-foreground">
                      Working set now, all pods in the cluster
                    </div>
                  </div>
                  <div className="bg-status-success-muted rounded-lg p-4">
                    <div className="text-sm text-muted-foreground">Running Pods</div>
                    <div className="text-2xl font-bold text-status-success">
                      {realtime.total_pods}
                    </div>
                    <div className="text-xs text-muted-foreground">
                      All namespaces; {realtime.services.length} Enclii services
                    </div>
                  </div>
                </div>

                {/* Service-level Metrics */}
                {realtime.services.length > 0 && (
                  <div className="space-y-3">
                    <h4 className="text-sm font-medium text-muted-foreground">Service Breakdown</h4>
                    <div className="space-y-2">
                      {realtime.services.map((svc) => (
                        <div
                          key={svc.service_id}
                          className="flex items-center justify-between p-3 bg-muted/50 rounded-lg"
                        >
                          <div className="flex items-center gap-3">
                            <div className={`w-2 h-2 rounded-full ${
                              svc.status === 'running' ? 'bg-status-success' :
                              svc.status === 'unknown' ? 'bg-status-warning' : 'bg-muted-foreground'
                            }`} />
                            <div>
                              <div className="font-medium">{svc.service_name}</div>
                              <div className="text-xs text-muted-foreground">
                                {svc.status === 'unknown'
                                  ? 'Not readable'
                                  : `${svc.pod_count} pod${svc.pod_count !== 1 ? 's' : ''}`}
                              </div>
                            </div>
                          </div>
                          {svc.status === 'unknown' ? (
                            <div className="text-sm text-muted-foreground">—</div>
                          ) : (
                            <div className="flex gap-6 text-sm">
                              <div className="text-right">
                                <div className="font-mono text-primary">
                                  {(svc.cpu_usage_millicores / 1000).toFixed(2)} cores
                                </div>
                                <div className="text-xs text-muted-foreground">CPU</div>
                              </div>
                              <div className="text-right">
                                <div className="font-mono text-purple-600">
                                  {svc.memory_usage_mb >= 1024
                                    ? (svc.memory_usage_mb / 1024).toFixed(2) + ' GB'
                                    : svc.memory_usage_mb.toFixed(0) + ' MB'}
                                </div>
                                <div className="text-xs text-muted-foreground">Memory</div>
                              </div>
                            </div>
                          )}
                        </div>
                      ))}
                    </div>
                  </div>
                )}
              </>
            ) : (
              <div className="text-center py-8 text-muted-foreground">
                <svg aria-hidden="true" className="w-12 h-12 mx-auto mb-4 opacity-50" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z" />
                </svg>
                <p className="font-medium">Resource metrics could not be read</p>
                <p className="text-sm">
                  The API could not read metrics.k8s.io, so no live figure is shown rather than an estimate.
                </p>
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {/* Month-to-date usage. Absolute values only: there is no plan
          allowance to compare against, and a metric nobody measures says so
          instead of showing a guess. */}
      <Card>
        <CardHeader>
          <CardTitle>Resource Usage</CardTitle>
          <CardDescription>
            Month to date, in absolute units. No plan allowance is applied.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {usage?.metrics.map((metric) => (
            <div key={metric.type} className="space-y-1" data-testid={`usage-metric-${metric.type}`}>
              <div className="flex items-center justify-between text-sm">
                <div className="flex items-center gap-2">
                  <span className="text-muted-foreground">{iconMap[metric.type]}</span>
                  <span className="font-medium">{metric.label}</span>
                </div>
                <span className={`font-mono ${metric.unavailable ? 'text-muted-foreground' : 'text-foreground'}`}>
                  {displayUsed(metric)}
                </span>
              </div>
              {metric.note && (
                <p className="text-xs text-muted-foreground">{metric.note}</p>
              )}
            </div>
          ))}
        </CardContent>
      </Card>

      <p className="text-sm text-muted-foreground text-center">
        Live figures come from metrics-server; build minutes from Waybill. Month-to-date compute,
        storage and bandwidth are not measured yet. Customer billing is handled by Dhanam.
      </p>
    </div>
  );
}
