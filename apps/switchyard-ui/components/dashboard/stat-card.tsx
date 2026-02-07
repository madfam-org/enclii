"use client";

import { LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";

interface StatCardProps {
  title: string;
  value: string | number;
  icon: LucideIcon;
  variant?: "success" | "warning" | "info" | "neutral";
  className?: string;
}

const variantStyles = {
  success: "bg-status-success",
  warning: "bg-status-warning",
  info: "bg-status-info",
  neutral: "bg-muted-foreground",
};

/**
 * StatCard - Reusable dashboard statistics card component
 *
 * @example
 * <StatCard
 *   title="Healthy Services"
 *   value={16}
 *   icon={CheckCircle2}
 *   variant="success"
 * />
 */
export function StatCard({
  title,
  value,
  icon: Icon,
  variant = "neutral",
  className,
}: StatCardProps) {
  return (
    <div
      className={cn(
        "bg-card overflow-hidden shadow rounded-lg border border-border",
        className
      )}
      data-testid="stat-card"
    >
      <div className="p-5">
        <div className="flex items-center">
          <div className="flex-shrink-0">
            <div
              className={cn(
                "w-8 h-8 rounded-full flex items-center justify-center",
                variantStyles[variant]
              )}
            >
              <Icon className="w-4 h-4 text-white" />
            </div>
          </div>
          <div className="ml-5 w-0 flex-1">
            <dl>
              <dt className="text-sm font-medium text-muted-foreground truncate">
                {title}
              </dt>
              <dd className="text-lg font-medium text-foreground">{value}</dd>
            </dl>
          </div>
        </div>
      </div>
    </div>
  );
}

/**
 * StatCardSkeleton - Loading state for StatCard
 */
export function StatCardSkeleton({ className }: { className?: string }) {
  return (
    <div
      className={cn(
        "bg-card overflow-hidden shadow rounded-lg border border-border",
        className
      )}
      data-testid="loading-skeleton"
    >
      <div className="p-5">
        <div className="flex items-center">
          <div className="flex-shrink-0">
            <div className="w-8 h-8 bg-muted rounded-full animate-pulse" />
          </div>
          <div className="ml-5 w-0 flex-1">
            <div className="h-4 bg-muted rounded w-24 mb-2 animate-pulse" />
            <div className="h-6 bg-muted rounded w-16 animate-pulse" />
          </div>
        </div>
      </div>
    </div>
  );
}
