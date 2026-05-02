'use client';

import { useState, useEffect, useCallback } from 'react';
import { usePolling } from '@/hooks/use-polling';
import { POLLING_SLOW } from '@/lib/constants';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from "@enclii/ui-components/button";
import { Progress } from '@/components/ui/progress';
import { Spinner } from '@/components/ui/spinner';
import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  PieChart,
  Pie,
  Cell,
  Legend,
} from 'recharts';
import { apiGet } from '@/lib/api';
import { useIsAdminScope } from '@/contexts/ScopeContext';
import { formatBytes } from '@/lib/formatting';

interface UsageMetric {
  type: string;
  label: string;
  used: number;
  included: number;
  unit: string;
  cost: number;
}

interface UsageSummary {
  period_start: string;
  period_end: string;
  metrics: UsageMetric[];
  total_cost: number;
}

interface CostCategory {
  name: string;
  value: number;
  color: string;
}

interface CostBreakdown {
  period_start: string;
  period_end: string;
  categories: CostCategory[];
  total_usage: number;
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

function getProgressColor(percentage: number): string {
  if (percentage >= 90) return 'bg-status-error';
  if (percentage >= 75) return 'bg-status-warning';
  return 'bg-status-success';
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
  const [costs, setCosts] = useState<CostBreakdown | null>(null);
  const [realtime, setRealtime] = useState<RealtimeMetrics | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchData = useCallback(async () => {
    try {
      setError(null);
      setLoading(true);

      const [usageData, costsData, realtimeData] = await Promise.all([
        apiGet<UsageSummary>('/v1/usage'),
        apiGet<CostBreakdown>('/v1/usage/costs'),
        apiGet<RealtimeMetrics>('/v1/usage/realtime').catch(() => null),
      ]);

      setUsage(usageData);
      setCosts(costsData);
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

  // Prepare chart data
  const usageChartData = usage?.metrics.map(m => ({
    name: m.label,
    used: m.used,
    included: m.included === -1 ? m.used : m.included,
    percentage: m.included === -1 ? 0 : Math.min((m.used / m.included) * 100, 150),
  })) || [];

  const costChartData = costs?.categories.filter(c => c.value > 0) || [];

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">
            {isAdmin ? 'Cluster utilization' : 'Infrastructure Usage'}
          </h1>
          <p className="text-muted-foreground">
            {isAdmin
              ? 'Live cluster metrics. Plan-tier billing is not applied to master-admin scope.'
              : 'Monitor your resource usage and infrastructure costs for the current period'}
          </p>
        </div>
        <Button variant="outline" onClick={fetchData}>
          <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          Refresh
        </Button>
      </div>

      {/* Period & Usage Summary. Admin scope hides the Infrastructure Cost
          pill — there's no plan to bill against (audit US-1). */}
      <div className={`grid gap-4 ${isAdmin ? '' : 'md:grid-cols-2'}`}>
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
            <p className="text-xs text-muted-foreground">Metering period</p>
          </CardContent>
        </Card>
        {!isAdmin && (
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Infrastructure Cost</CardTitle>
              <svg aria-hidden="true" className="w-4 h-4 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8c-1.657 0-3 .895-3 2s1.343 2 3 2 3 .895 3 2-1.343 2-3 2m0-8c1.11 0 2.08.402 2.599 1M12 8V7m0 1v8m0 0v1m0-1c-1.11 0-2.08-.402-2.599-1M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold text-primary">${usage?.total_cost.toFixed(2)}</div>
              <p className="text-xs text-muted-foreground">Resource usage this period</p>
            </CardContent>
          </Card>
        )}
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
                  Real-time metrics from your running services
                  {realtime.collected_at && (
                    <span className="ml-2 text-xs">
                      (Updated {new Date(realtime.collected_at).toLocaleTimeString()})
                    </span>
                  )}
                </CardDescription>
              </div>
              {!realtime.metrics_enabled && (
                <span className="text-xs bg-status-warning-muted text-status-warning-foreground px-2 py-1 rounded">
                  Metrics server unavailable
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
                      {realtime.total_cpu_millicores.toFixed(0)} millicores
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
                      Across all services
                    </div>
                  </div>
                  <div className="bg-status-success-muted rounded-lg p-4">
                    <div className="text-sm text-muted-foreground">Running Pods</div>
                    <div className="text-2xl font-bold text-status-success">
                      {realtime.total_pods}
                    </div>
                    <div className="text-xs text-muted-foreground">
                      {realtime.services.length} services
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
                              svc.status === 'unknown' ? 'bg-status-warning' : 'bg-status-error'
                            }`} />
                            <div>
                              <div className="font-medium">{svc.service_name}</div>
                              <div className="text-xs text-muted-foreground">
                                {svc.pod_count} pod{svc.pod_count !== 1 ? 's' : ''}
                              </div>
                            </div>
                          </div>
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
                <p className="font-medium">Metrics collection unavailable</p>
                <p className="text-sm">Install metrics-server in your cluster to enable real-time resource monitoring</p>
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {/* Usage Meters & Cost Breakdown.
          Admin scope: drop the Cost Breakdown donut and render Resource
          Usage in absolute units (no denominator, no progress bar) so we
          don't fabricate plan-limit utilization (audit D-1, US-2). */}
      <div className={`grid gap-6 ${isAdmin ? '' : 'md:grid-cols-2'}`}>
        {/* Usage Meters */}
        <Card>
          <CardHeader>
            <CardTitle>Resource Usage</CardTitle>
            <CardDescription>
              {isAdmin
                ? 'Current cluster usage in absolute units. No plan limits applied.'
                : 'Current usage against included allocations'}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-6">
            {usage?.metrics.map((metric) => {
              const isUnlimited = metric.included === -1;
              const percentage = isUnlimited
                ? 0
                : Math.min((metric.used / metric.included) * 100, 100);
              const overLimit = !isUnlimited && metric.used > metric.included;

              if (isAdmin) {
                // Absolute-value row, no denominator, no progress bar.
                // Storage / bandwidth come from the API as bytes; format
                // accordingly. Render "—" if the value isn't usable.
                const displayUsed =
                  metric.used === null || metric.used === undefined || Number.isNaN(metric.used)
                    ? '—'
                    : metric.type === 'storage' || metric.type === 'bandwidth'
                      ? formatBytes(metric.used)
                      : `${formatNumber(metric.used)} ${metric.unit}`;
                return (
                  <div key={metric.type} className="flex items-center justify-between text-sm">
                    <div className="flex items-center gap-2">
                      <span className="text-muted-foreground">{iconMap[metric.type]}</span>
                      <span className="font-medium">{metric.label}</span>
                    </div>
                    <span className="font-mono text-foreground">{displayUsed}</span>
                  </div>
                );
              }

              return (
                <div key={metric.type} className="space-y-2">
                  <div className="flex items-center justify-between text-sm">
                    <div className="flex items-center gap-2">
                      <span className="text-muted-foreground">{iconMap[metric.type]}</span>
                      <span className="font-medium">{metric.label}</span>
                    </div>
                    <div className="flex items-center gap-2">
                      <span className={`font-mono ${overLimit ? 'text-status-error' : ''}`}>
                        {formatNumber(metric.used)}
                      </span>
                      <span className="text-muted-foreground">/</span>
                      <span className="text-muted-foreground font-mono">
                        {isUnlimited ? '∞' : formatNumber(metric.included)}
                      </span>
                      <span className="text-muted-foreground text-xs">{metric.unit}</span>
                    </div>
                  </div>

                  {!isUnlimited && (
                    <div className="relative">
                      <Progress value={percentage} className="h-2" />
                      <div
                        className={`absolute top-0 left-0 h-2 rounded-full transition-all ${getProgressColor(percentage)}`}
                        style={{ width: `${Math.min(percentage, 100)}%` }}
                      />
                      {overLimit && (
                        <div
                          className="absolute top-0 h-2 bg-status-error/30 rounded-r-full"
                          style={{
                            left: '100%',
                            width: `${((metric.used - metric.included) / metric.included) * 100}%`,
                            maxWidth: '20%',
                          }}
                        />
                      )}
                    </div>
                  )}

                  {metric.cost > 0 && (
                    <p className="text-xs text-muted-foreground">
                      +${metric.cost.toFixed(2)} overage charges
                    </p>
                  )}
                </div>
              );
            })}
          </CardContent>
        </Card>

        {/* Cost Breakdown Pie Chart — hidden for master-admin scope. */}
        {!isAdmin && (
          <Card>
            <CardHeader>
              <CardTitle>Cost Breakdown</CardTitle>
              <CardDescription>Usage costs by category</CardDescription>
            </CardHeader>
            <CardContent>
              {costChartData.length > 0 ? (
                <div className="h-64">
                  <ResponsiveContainer width="100%" height="100%">
                    <PieChart>
                      <Pie
                        data={costChartData}
                        cx="50%"
                        cy="50%"
                        innerRadius={60}
                        outerRadius={80}
                        paddingAngle={2}
                        dataKey="value"
                        label={({ name, value }) => `${name}: $${value.toFixed(2)}`}
                      >
                        {costChartData.map((entry, index) => (
                          <Cell key={`cell-${index}`} fill={entry.color} />
                        ))}
                      </Pie>
                      <Tooltip formatter={(value: number) => [`$${value.toFixed(2)}`, '']} />
                      <Legend />
                    </PieChart>
                  </ResponsiveContainer>
                </div>
              ) : (
                <div className="flex items-center justify-center h-64 text-muted-foreground">
                  <div className="text-center">
                    <svg aria-hidden="true" className="w-12 h-12 mx-auto mb-4 text-status-success" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
                    </svg>
                    <p className="font-medium">No overage charges</p>
                    <p className="text-sm">You&apos;re within your plan limits</p>
                  </div>
                </div>
              )}
            </CardContent>
          </Card>
        )}
      </div>

      {/* Usage Bar Chart — hidden for master-admin (compares used vs.
          included allocation, which doesn't apply to a self-hosted scope). */}
      {!isAdmin && (
        <Card>
          <CardHeader>
            <CardTitle>Usage Overview</CardTitle>
            <CardDescription>Comparing used resources against included allocations</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="h-80">
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={usageChartData} margin={{ top: 20, right: 30, left: 20, bottom: 5 }}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis dataKey="name" />
                  <YAxis />
                  <Tooltip
                    formatter={(value: number, name: string) => [
                      name === 'percentage' ? `${value.toFixed(1)}%` : value.toFixed(1),
                      name === 'used' ? 'Used' : name === 'included' ? 'Included' : 'Usage %',
                    ]}
                  />
                  <Legend />
                  <Bar dataKey="used" fill="#3b82f6" name="Used" />
                  <Bar dataKey="included" fill="#e5e7eb" name="Included" />
                </BarChart>
              </ResponsiveContainer>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Cost Details Table — hidden for master-admin (no overage / billing). */}
      {!isAdmin && (
      <Card>
        <CardHeader>
          <CardTitle>Detailed Cost Summary</CardTitle>
          <CardDescription>Breakdown of infrastructure costs for the current period</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead>
                <tr className="border-b">
                  <th className="text-left py-3 px-4 font-medium text-muted-foreground">Category</th>
                  <th className="text-right py-3 px-4 font-medium text-muted-foreground">Used</th>
                  <th className="text-right py-3 px-4 font-medium text-muted-foreground">Included</th>
                  <th className="text-right py-3 px-4 font-medium text-muted-foreground">Overage</th>
                  <th className="text-right py-3 px-4 font-medium text-muted-foreground">Cost</th>
                </tr>
              </thead>
              <tbody>
                {usage?.metrics.map((metric) => {
                  const overage = metric.included === -1 ? 0 : Math.max(0, metric.used - metric.included);
                  return (
                    <tr key={metric.type} className="border-b">
                      <td className="py-3 px-4">
                        <div className="flex items-center gap-2">
                          <span className="text-muted-foreground">{iconMap[metric.type]}</span>
                          <span>{metric.label}</span>
                        </div>
                      </td>
                      <td className="text-right py-3 px-4 font-mono">
                        {formatNumber(metric.used)} {metric.unit}
                      </td>
                      <td className="text-right py-3 px-4 font-mono text-muted-foreground">
                        {metric.included === -1 ? 'Unlimited' : `${formatNumber(metric.included)} ${metric.unit}`}
                      </td>
                      <td className="text-right py-3 px-4 font-mono">
                        {overage > 0 ? (
                          <span className="text-status-error">+{formatNumber(overage)} {metric.unit}</span>
                        ) : (
                          <span className="text-status-success">-</span>
                        )}
                      </td>
                      <td className="text-right py-3 px-4 font-mono font-medium">
                        {metric.cost > 0 ? (
                          <span className="text-status-error">${metric.cost.toFixed(2)}</span>
                        ) : (
                          <span className="text-status-success">$0.00</span>
                        )}
                      </td>
                    </tr>
                  );
                })}
                <tr className="bg-primary/5">
                  <td colSpan={4} className="py-3 px-4 font-bold">Total Infrastructure Cost</td>
                  <td className="text-right py-3 px-4 font-mono font-bold text-primary">${usage?.total_cost.toFixed(2)}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>
      )}

      {/* Footer Note — admin-scope footer reflects no-billing posture. */}
      <p className="text-sm text-muted-foreground text-center">
        {isAdmin
          ? 'Cluster utilization estimates update hourly. Plan-tier billing does not apply to master-admin scope.'
          : 'Infrastructure usage estimates are updated hourly. Customer billing is handled by Dhanam.'}
      </p>
    </div>
  );
}
