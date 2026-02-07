"use client";

import { useState, useEffect } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  PieChart,
  Pie,
  Cell,
  ResponsiveContainer,
  Tooltip
} from "recharts";
import { cn } from "@/lib/utils";
import { apiGet } from "@/lib/api";
import { Spinner } from "@/components/ui/spinner";

interface CostCategory {
  name: string;
  value: number;
  color: string;
}

interface CostBreakdownResponse {
  period_start: string;
  period_end: string;
  plan_base: number;
  plan_name: string;
  categories: CostCategory[];
  total_usage: number;
  grand_total: number;
}

interface CostBreakdownProps {
  projectId: string;
  periodStart?: Date;
  periodEnd?: Date;
  className?: string;
}

export function CostBreakdown({
  projectId,
  periodStart,
  periodEnd,
  className
}: CostBreakdownProps) {
  const [costData, setCostData] = useState<CostBreakdownResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const fetchCosts = async () => {
      try {
        setError(null);
        const data = await apiGet<CostBreakdownResponse>('/v1/usage/costs');
        setCostData(data);
      } catch (err) {
        console.error('Failed to fetch costs:', err);
        setError(err instanceof Error ? err.message : 'Failed to fetch costs');
      } finally {
        setLoading(false);
      }
    };

    fetchCosts();
  }, [projectId]);

  if (loading) {
    return (
      <Card className={cn("", className)}>
        <CardHeader>
          <CardTitle className="text-sm font-medium">Cost Breakdown</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-center py-8">
            <Spinner />
            <span className="ml-2 text-sm text-muted-foreground">Loading costs...</span>
          </div>
        </CardContent>
      </Card>
    );
  }

  if (error) {
    return (
      <Card className={cn("border-destructive/30", className)}>
        <CardHeader>
          <CardTitle className="text-sm font-medium">Cost Breakdown</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-destructive">{error}</p>
        </CardContent>
      </Card>
    );
  }

  const costs = costData?.categories || [];
  const totalCost = costData?.total_usage || 0;
  const planBase = costData?.plan_base || 20.00;
  const grandTotal = costData?.grand_total || planBase + totalCost;

  return (
    <Card className={cn("", className)}>
      <CardHeader>
        <CardTitle className="text-sm font-medium">
          Cost Breakdown
        </CardTitle>
        <p className="text-xs text-muted-foreground">
          Current billing period
        </p>
      </CardHeader>
      <CardContent>
        <div className="flex items-center justify-between mb-6">
          <div>
            <p className="text-3xl font-bold">${grandTotal.toFixed(2)}</p>
            <p className="text-sm text-muted-foreground">
              ${planBase.toFixed(2)} base + ${totalCost.toFixed(2)} usage
            </p>
          </div>

          <div className="h-32 w-32">
            <ResponsiveContainer width="100%" height="100%">
              <PieChart>
                <Pie
                  data={costs}
                  cx="50%"
                  cy="50%"
                  innerRadius={35}
                  outerRadius={50}
                  paddingAngle={2}
                  dataKey="value"
                >
                  {costs.map((entry, index) => (
                    <Cell key={`cell-${index}`} fill={entry.color} />
                  ))}
                </Pie>
                <Tooltip
                  formatter={(value: number) => [`$${value.toFixed(2)}`, ""]}
                />
              </PieChart>
            </ResponsiveContainer>
          </div>
        </div>

        <div className="space-y-3">
          <div className="flex items-center justify-between py-2 border-b">
            <span className="text-sm font-medium">Pro Plan (base)</span>
            <span className="font-mono">${planBase.toFixed(2)}</span>
          </div>

          {costs.map((cost) => (
            <div
              key={cost.name}
              className="flex items-center justify-between py-1"
            >
              <div className="flex items-center gap-2">
                <div
                  className="w-3 h-3 rounded-full"
                  style={{ backgroundColor: cost.color }}
                />
                <span className="text-sm">{cost.name}</span>
              </div>
              <span className="font-mono text-sm">
                ${cost.value.toFixed(2)}
              </span>
            </div>
          ))}

          <div className="flex items-center justify-between pt-3 border-t">
            <span className="font-medium">Estimated Total</span>
            <span className="font-mono font-bold">
              ${grandTotal.toFixed(2)}
            </span>
          </div>
        </div>

        <p className="text-xs text-muted-foreground mt-4">
          Final invoice generated on the 1st of each month
        </p>
      </CardContent>
    </Card>
  );
}
