"use client";

import { useState, useEffect } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import {
  Cpu,
  HardDrive,
  Gauge,
  Globe,
  Hammer,
  TrendingUp
} from "lucide-react";
import { cn } from "@/lib/utils";
import { apiGet } from "@/lib/api";
import { Spinner } from "@/components/ui/spinner";

interface UsageMetric {
  type: string;
  label: string;
  used: number;
  included: number;
  unit: string;
  cost: number;
}

interface UsageMetricDisplay extends UsageMetric {
  icon: React.ReactNode;
}

interface UsageSummary {
  period_start: string;
  period_end: string;
  metrics: UsageMetric[];
  total_cost: number;
  plan_base: number;
  grand_total: number;
  plan_name: string;
}

interface UsageMetersProps {
  projectId: string;
  className?: string;
}

const iconMap: Record<string, React.ReactNode> = {
  compute: <Cpu className="h-4 w-4" />,
  build: <Hammer className="h-4 w-4" />,
  storage: <HardDrive className="h-4 w-4" />,
  bandwidth: <Gauge className="h-4 w-4" />,
  domains: <Globe className="h-4 w-4" />,
};

function getProgressColor(percentage: number): string {
  if (percentage >= 90) return "bg-status-error";
  if (percentage >= 75) return "bg-status-warning";
  return "bg-status-success";
}

function formatNumber(num: number): string {
  if (num >= 1000) {
    return (num / 1000).toFixed(1) + "k";
  }
  return num.toFixed(1);
}

export function UsageMeters({
  projectId,
  className
}: UsageMetersProps) {
  const [usageData, setUsageData] = useState<UsageSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const fetchUsage = async () => {
      try {
        setError(null);
        const data = await apiGet<UsageSummary>('/v1/usage');
        setUsageData(data);
      } catch (err) {
        console.error('Failed to fetch usage:', err);
        setError(err instanceof Error ? err.message : 'Failed to fetch usage');
      } finally {
        setLoading(false);
      }
    };

    fetchUsage();
  }, [projectId]);

  if (loading) {
    return (
      <Card className={cn("", className)}>
        <CardHeader className="flex flex-row items-center justify-between pb-2">
          <CardTitle className="text-sm font-medium">
            Current Period Usage
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-center py-8">
            <Spinner />
            <span className="ml-2 text-sm text-muted-foreground">Loading usage...</span>
          </div>
        </CardContent>
      </Card>
    );
  }

  if (error) {
    return (
      <Card className={cn("border-status-error/30", className)}>
        <CardHeader className="flex flex-row items-center justify-between pb-2">
          <CardTitle className="text-sm font-medium">
            Current Period Usage
          </CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-status-error">{error}</p>
        </CardContent>
      </Card>
    );
  }

  const metrics: UsageMetricDisplay[] = (usageData?.metrics || []).map(m => ({
    ...m,
    icon: iconMap[m.type] || <TrendingUp className="h-4 w-4" />
  }));

  const totalCost = usageData?.total_cost || 0;

  return (
    <Card className={cn("", className)}>
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <CardTitle className="text-sm font-medium">
          Current Period Usage
        </CardTitle>
        <div className="flex items-center gap-1 text-sm text-muted-foreground">
          <TrendingUp className="h-4 w-4" />
          <span>${totalCost.toFixed(2)} overage</span>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        {metrics.map((metric) => {
          const isUnlimited = metric.included === -1;
          const percentage = isUnlimited
            ? 0
            : Math.min((metric.used / metric.included) * 100, 100);
          const overLimit = !isUnlimited && metric.used > metric.included;

          return (
            <div key={metric.type} className="space-y-2">
              <div className="flex items-center justify-between text-sm">
                <div className="flex items-center gap-2">
                  <span className="text-muted-foreground">{metric.icon}</span>
                  <span className="font-medium">{metric.label}</span>
                </div>
                <div className="flex items-center gap-2">
                  <span className={cn(
                    "font-mono",
                    overLimit && "text-status-error"
                  )}>
                    {formatNumber(metric.used)}
                  </span>
                  <span className="text-muted-foreground">/</span>
                  <span className="text-muted-foreground font-mono">
                    {isUnlimited ? "∞" : formatNumber(metric.included)}
                  </span>
                  <span className="text-muted-foreground text-xs">
                    {metric.unit}
                  </span>
                </div>
              </div>

              {!isUnlimited && (
                <div className="relative">
                  <Progress
                    value={percentage}
                    className="h-2"
                  />
                  <div
                    className={cn(
                      "absolute top-0 left-0 h-2 rounded-full transition-all",
                      getProgressColor(percentage)
                    )}
                    style={{ width: `${Math.min(percentage, 100)}%` }}
                  />
                  {overLimit && (
                    <div
                      className="absolute top-0 h-2 bg-status-error/30 rounded-r-full"
                      style={{
                        left: "100%",
                        width: `${((metric.used - metric.included) / metric.included) * 100}%`,
                        maxWidth: "20%"
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
  );
}
