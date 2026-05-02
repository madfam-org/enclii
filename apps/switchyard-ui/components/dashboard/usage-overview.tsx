'use client';

import * as React from 'react';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { CircularGauge, UsageGauge, GaugeGrid } from '@/components/ui/circular-gauge';
import { useUsageMetrics, useRealtimeResources } from '@/hooks/use-usage-metrics';
import { Cpu, HardDrive, Gauge, Activity, Hammer, Globe } from 'lucide-react';
import { cn } from '@/lib/utils';
import { formatBytes } from '@/lib/formatting';
import { Spinner } from '@/components/ui/spinner';
import { useIsAdminScope } from '@/contexts/ScopeContext';

// =============================================================================
// TYPES
// =============================================================================

interface UsageOverviewProps {
  className?: string;
  variant?: 'full' | 'compact';
}

// =============================================================================
// ICON MAP
// =============================================================================

const metricIcons: Record<string, React.ElementType> = {
  compute: Cpu,
  build: Hammer,
  storage: HardDrive,
  bandwidth: Gauge,
  domains: Globe,
};

// =============================================================================
// MAIN COMPONENT
// =============================================================================

export function UsageOverview({ className, variant = 'full' }: UsageOverviewProps) {
  const { usage, isLoading, error } = useUsageMetrics();
  const { cpuUsage, memoryUsage, podCount, isMetricsEnabled } = useRealtimeResources(30000);
  const isAdmin = useIsAdminScope();

  // Master-admin scope: self-hosted cluster has no plan-tier limits or
  // overage charges. Render absolute-value tiles instead of percent-of-limit
  // gauges so we don't fabricate "100% of plan" numbers (audit D-1, US-1..4).
  if (isAdmin) {
    return (
      <AdminUsageOverview
        className={className}
        variant={variant}
        usage={usage}
        isLoading={isLoading}
        error={error}
        cpuUsage={cpuUsage}
        memoryUsage={memoryUsage}
        podCount={podCount}
        isMetricsEnabled={isMetricsEnabled}
      />
    );
  }

  if (isLoading) {
    return (
      <Card className={cn(className)}>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Activity className="h-5 w-5" />
            Usage Overview
          </CardTitle>
          <CardDescription>Loading usage metrics...</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-center py-12">
            <Spinner size="lg" />
          </div>
        </CardContent>
      </Card>
    );
  }

  if (error) {
    return (
      <Card className={cn('border-destructive/50', className)}>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-destructive">
            <Activity className="h-5 w-5" />
            Usage Overview
          </CardTitle>
          <CardDescription className="text-destructive">{error}</CardDescription>
        </CardHeader>
      </Card>
    );
  }

  const metrics = usage?.metrics || [];

  // Compact variant - just the key metrics
  if (variant === 'compact') {
    return (
      <Card className={cn(className)}>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium flex items-center gap-2">
            <Activity className="h-4 w-4" />
            Usage
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex flex-wrap gap-4 justify-center">
            {metrics.slice(0, 4).map((metric) => {
              const percentage = metric.included > 0 ? (metric.used / metric.included) * 100 : 0;
              return (
                <CircularGauge
                  key={metric.type}
                  value={percentage}
                  max={100}
                  size={80}
                  strokeWidth={6}
                  label={metric.label}
                  variant="auto"
                />
              );
            })}
          </div>
        </CardContent>
      </Card>
    );
  }

  // Full variant - detailed view
  return (
    <Card className={cn(className)}>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Activity className="h-5 w-5" />
          Usage Overview
        </CardTitle>
        <CardDescription>
          {usage?.plan_name} plan - {usage?.period_start} to {usage?.period_end}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-8">
        {/* Plan Metrics */}
        <div>
          <h3 className="text-sm font-medium text-muted-foreground mb-4">Plan Limits</h3>
          <GaugeGrid columns={4}>
            {metrics.map((metric) => {
              const isUnlimited = metric.included === -1;
              const Icon = metricIcons[metric.type] || Activity;

              if (isUnlimited) {
                return (
                  <div key={metric.type} className="flex flex-col items-center gap-2">
                    <div className="h-[120px] w-[120px] rounded-full border-8 border-muted flex items-center justify-center">
                      <div className="text-center">
                        <Icon className="h-6 w-6 mx-auto text-muted-foreground mb-1" />
                        <span className="text-xl font-mono font-semibold">∞</span>
                      </div>
                    </div>
                    <span className="text-xs text-muted-foreground font-medium">{metric.label}</span>
                    <span className="text-[10px] text-muted-foreground/70">Unlimited</span>
                  </div>
                );
              }

              return (
                <UsageGauge
                  key={metric.type}
                  used={metric.used}
                  limit={metric.included}
                  label={metric.label}
                  unit={metric.type === 'storage' || metric.type === 'bandwidth' ? 'bytes' : 'number'}
                  size="md"
                  // `cost` from the API represents the metered overage cost
                  // for this billing period when the user is over plan. Pass
                  // it through so the gauge can show "+$X.XX this period"
                  // beneath the percentage when applicable.
                  overageCostUsd={metric.cost}
                />
              );
            })}
          </GaugeGrid>
        </div>

        {/* Realtime Resources */}
        {isMetricsEnabled && (
          <div>
            <h3 className="text-sm font-medium text-muted-foreground mb-4 flex items-center gap-2">
              <span className="relative flex h-2 w-2">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-status-success opacity-75"></span>
                <span className="relative inline-flex rounded-full h-2 w-2 bg-status-success"></span>
              </span>
              Live Resources
            </h3>
            <div className="grid gap-4 sm:grid-cols-3">
              <RealtimeGauge
                value={cpuUsage / 1000}
                max={4}
                label="CPU Usage"
                unit="cores"
                icon={<Cpu className="h-4 w-4" />}
              />
              <RealtimeGauge
                value={memoryUsage}
                max={8192}
                label="Memory"
                unit="MB"
                formatValue={(v) => v >= 1024 ? `${(v / 1024).toFixed(1)} GB` : `${v.toFixed(0)} MB`}
                icon={<HardDrive className="h-4 w-4" />}
              />
              <RealtimeGauge
                value={podCount}
                max={50}
                label="Running Pods"
                unit="pods"
                icon={<Activity className="h-4 w-4" />}
              />
            </div>
          </div>
        )}

        {/* Cost Summary */}
        {usage && (
          <div className="flex items-center justify-between pt-4 border-t">
            <div>
              <p className="text-sm text-muted-foreground">Estimated total this period</p>
              <p className="text-2xl font-bold text-enclii-blue">${usage.grand_total.toFixed(2)}</p>
            </div>
            {usage.total_cost > 0 && (
              <div className="text-right">
                <p className="text-sm text-muted-foreground">Overage charges</p>
                <p className="text-lg font-medium text-status-warning">${usage.total_cost.toFixed(2)}</p>
              </div>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

// =============================================================================
// ADMIN VARIANT (master-admin scope)
// =============================================================================

interface AdminUsageOverviewProps {
  className?: string;
  variant?: 'full' | 'compact';
  usage: ReturnType<typeof useUsageMetrics>['usage'];
  isLoading: boolean;
  error: string | null;
  cpuUsage: number;
  memoryUsage: number;
  podCount: number;
  isMetricsEnabled: boolean;
}

/**
 * Format an absolute metric value into a short, human-readable string.
 * Returns "—" when the value isn't numerically usable (null/undefined/NaN).
 * Storage / bandwidth use binary units; compute is GB-hours; build is minutes;
 * domains is a count.
 */
function formatAbsoluteMetric(
  type: string,
  value: number | null | undefined,
  unit: string,
): string {
  if (value === null || value === undefined || Number.isNaN(value)) return '—';
  if (type === 'storage' || type === 'bandwidth') {
    // API returns bytes for these; show GB-friendly string.
    return formatBytes(value);
  }
  if (type === 'compute') return `${value.toFixed(1)} GB-hours`;
  if (type === 'build') return `${value.toFixed(0)} minutes`;
  if (type === 'domains') return `${value.toFixed(0)} ${value === 1 ? 'domain' : 'domains'}`;
  // Fallback: show value with unit if we have one.
  return unit ? `${value.toFixed(1)} ${unit}` : value.toFixed(1);
}

function AdminUsageOverview({
  className,
  variant = 'full',
  usage,
  isLoading,
  error,
  cpuUsage,
  memoryUsage,
  podCount,
  isMetricsEnabled,
}: AdminUsageOverviewProps) {
  if (isLoading) {
    return (
      <Card className={cn(className)}>
        <CardHeader className={variant === 'compact' ? 'pb-2' : undefined}>
          <CardTitle className={cn('flex items-center gap-2', variant === 'compact' && 'text-sm font-medium')}>
            <Activity className={variant === 'compact' ? 'h-4 w-4' : 'h-5 w-5'} />
            Cluster usage
          </CardTitle>
          {variant === 'full' && <CardDescription>Loading cluster metrics...</CardDescription>}
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-center py-8">
            <Spinner size={variant === 'compact' ? 'md' : 'lg'} />
          </div>
        </CardContent>
      </Card>
    );
  }

  // On error we still render the panel (no fabricated numbers) — each tile
  // becomes "—". The error message is shown only in full variant to keep the
  // sidebar tile compact.
  const metrics = usage?.metrics ?? [];
  // Order tiles consistently regardless of API ordering.
  const tileOrder = ['compute', 'storage', 'bandwidth', 'build'];
  const tilesByType = new Map(metrics.map((m) => [m.type, m]));

  const tiles = tileOrder.map((type) => {
    const m = tilesByType.get(type);
    const Icon = metricIcons[type] ?? Activity;
    const label = m?.label ?? defaultLabel(type);
    const display = formatAbsoluteMetric(type, m?.used, m?.unit ?? '');
    return { type, label, display, Icon };
  });

  const titleText = 'Cluster usage';
  const subtitleText = 'Self-hosted — no plan limits.';

  if (variant === 'compact') {
    return (
      <Card className={cn(className)}>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium flex items-center gap-2">
            <Activity className="h-4 w-4" />
            {titleText}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="grid grid-cols-2 gap-2">
            {tiles.map(({ type, label, display, Icon }) => (
              <div
                key={type}
                className="rounded-md border border-border/40 bg-muted/30 px-2 py-2"
              >
                <div className="flex items-center gap-1 text-[11px] text-muted-foreground">
                  <Icon className="h-3 w-3" />
                  <span className="truncate">{label}</span>
                </div>
                <div className="mt-1 font-mono text-xs font-medium text-foreground">
                  {display}
                </div>
              </div>
            ))}
          </div>
          <p className="text-[10px] text-muted-foreground/80">{subtitleText}</p>
          {error && (
            <p className="text-[10px] text-status-warning">Metrics unavailable</p>
          )}
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className={cn(className)}>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Activity className="h-5 w-5" />
          {titleText}
        </CardTitle>
        <CardDescription>
          Live cluster utilization in absolute units. Plan-tier billing is not applied to master-admin scope.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-8">
        <div>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
            {tiles.map(({ type, label, display, Icon }) => (
              <div
                key={type}
                className="rounded-lg border border-border/50 bg-muted/30 p-4"
              >
                <div className="flex items-center gap-2 text-muted-foreground">
                  <Icon className="h-4 w-4" />
                  <span className="text-xs font-medium">{label} used</span>
                </div>
                <div className="mt-2 font-mono text-lg font-semibold text-foreground">
                  {display}
                </div>
              </div>
            ))}
          </div>
          <p className="mt-3 text-xs text-muted-foreground">{subtitleText}</p>
          {error && (
            <p className="mt-1 text-xs text-status-warning">
              Metrics unavailable — values shown as &quot;—&quot;.
            </p>
          )}
        </div>

        {isMetricsEnabled && (
          <div>
            <h3 className="text-sm font-medium text-muted-foreground mb-4 flex items-center gap-2">
              <span className="relative flex h-2 w-2">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-status-success opacity-75"></span>
                <span className="relative inline-flex rounded-full h-2 w-2 bg-status-success"></span>
              </span>
              Live Resources
            </h3>
            <div className="grid gap-4 sm:grid-cols-3">
              <RealtimeGauge
                value={cpuUsage / 1000}
                max={4}
                label="CPU Usage"
                unit="cores"
                icon={<Cpu className="h-4 w-4" />}
              />
              <RealtimeGauge
                value={memoryUsage}
                max={8192}
                label="Memory"
                unit="MB"
                formatValue={(v) => v >= 1024 ? `${(v / 1024).toFixed(1)} GB` : `${v.toFixed(0)} MB`}
                icon={<HardDrive className="h-4 w-4" />}
              />
              <RealtimeGauge
                value={podCount}
                max={50}
                label="Running Pods"
                unit="pods"
                icon={<Activity className="h-4 w-4" />}
              />
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function defaultLabel(type: string): string {
  switch (type) {
    case 'compute':
      return 'Compute';
    case 'storage':
      return 'Storage';
    case 'bandwidth':
      return 'Bandwidth';
    case 'build':
      return 'Build minutes';
    case 'domains':
      return 'Domains';
    default:
      return type;
  }
}

// =============================================================================
// HELPER COMPONENTS
// =============================================================================

interface RealtimeGaugeProps {
  value: number;
  max: number;
  label: string;
  unit: string;
  formatValue?: (value: number) => string;
  icon?: React.ReactNode;
}

function RealtimeGauge({ value, max, label, unit, formatValue, icon }: RealtimeGaugeProps) {
  const percentage = (value / max) * 100;
  const displayValue = formatValue ? formatValue(value) : `${value.toFixed(1)} ${unit}`;

  return (
    <div className="bg-muted/50 rounded-lg p-4">
      <div className="flex items-center gap-2 text-muted-foreground mb-3">
        {icon}
        <span className="text-sm font-medium">{label}</span>
      </div>
      <div className="flex items-end gap-2">
        <CircularGauge
          value={percentage}
          max={100}
          size={64}
          strokeWidth={5}
          variant="auto"
          showPercentage={false}
          formatValue={() => displayValue}
        />
        <div className="text-xs text-muted-foreground mb-1">
          of {max} {unit}
        </div>
      </div>
    </div>
  );
}

// =============================================================================
// EXPORT MINI VARIANT FOR DASHBOARD
// =============================================================================

export function UsageGauges({ className }: { className?: string }) {
  return <UsageOverview variant="compact" className={className} />;
}
