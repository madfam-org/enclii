"use client";

import { useState, useEffect } from "react";
import {
  CheckCircle2,
  Circle,
  Loader2,
  XCircle,
  GitCommit,
  ExternalLink,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { apiGet } from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import type { UnifiedBuildStatus, OverallPipelineStatus } from "@/lib/types/pipeline";

// ============================================================================
// Types
// ============================================================================

interface PipelineStatusBadgeProps {
  serviceId: string;
  commitSha: string;
  className?: string;
  showCommit?: boolean;
  pollInterval?: number;
}

interface PipelineStatusIndicatorProps {
  status: OverallPipelineStatus;
  size?: "sm" | "md";
}

// ============================================================================
// Utility Functions
// ============================================================================

function getStatusConfig(status: OverallPipelineStatus) {
  switch (status) {
    case "running":
      return {
        icon: CheckCircle2,
        color: "text-status-success",
        bgColor: "bg-status-success-muted",
        label: "Running",
        animate: false,
      };
    case "ready":
      return {
        icon: CheckCircle2,
        color: "text-status-info",
        bgColor: "bg-status-info-muted",
        label: "Ready",
        animate: false,
      };
    case "building":
      return {
        icon: Loader2,
        color: "text-status-warning",
        bgColor: "bg-status-warning-muted",
        label: "Building",
        animate: true,
      };
    case "deploying":
      return {
        icon: Loader2,
        color: "text-primary",
        bgColor: "bg-primary/10",
        label: "Deploying",
        animate: true,
      };
    case "failed":
      return {
        icon: XCircle,
        color: "text-status-error",
        bgColor: "bg-status-error-muted",
        label: "Failed",
        animate: false,
      };
    default:
      return {
        icon: Circle,
        color: "text-muted-foreground",
        bgColor: "bg-muted",
        label: "Pending",
        animate: false,
      };
  }
}

// ============================================================================
// Sub-components
// ============================================================================

export function PipelineStatusIndicator({
  status,
  size = "md",
}: PipelineStatusIndicatorProps) {
  const config = getStatusConfig(status);
  const Icon = config.icon;
  const iconSize = size === "sm" ? "w-3 h-3" : "w-4 h-4";

  return (
    <div className={cn("flex items-center gap-1.5", config.bgColor, "px-2 py-1 rounded-full")}>
      <Icon className={cn(iconSize, config.color, config.animate && "animate-spin")} />
      <span className={cn("text-xs font-medium", config.color)}>{config.label}</span>
    </div>
  );
}

// ============================================================================
// Main Component
// ============================================================================

/**
 * Compact pipeline status badge for use in lists and tables.
 * Fetches and displays the unified pipeline status with optional polling.
 */
export function PipelineStatusBadge({
  serviceId,
  commitSha,
  className,
  showCommit = true,
  pollInterval = 10000,
}: PipelineStatusBadgeProps) {
  const [status, setStatus] = useState<UnifiedBuildStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let mounted = true;

    const fetchStatus = async () => {
      try {
        const data = await apiGet<UnifiedBuildStatus>(
          `/v1/services/${serviceId}/builds/${commitSha}/status`
        );
        if (mounted) {
          setStatus(data);
          setError(null);
        }
      } catch (err) {
        if (mounted) {
          setError(err instanceof Error ? err.message : "Failed to fetch");
        }
      } finally {
        if (mounted) {
          setLoading(false);
        }
      }
    };

    fetchStatus();

    // Only poll if status is not terminal
    const shouldPoll = () => {
      return (
        !status ||
        (status.overall_status !== "running" && status.overall_status !== "failed")
      );
    };

    const interval = setInterval(() => {
      if (shouldPoll()) {
        fetchStatus();
      }
    }, pollInterval);

    return () => {
      mounted = false;
      clearInterval(interval);
    };
  }, [serviceId, commitSha, pollInterval, status?.overall_status]);

  if (loading) {
    return (
      <div className={cn("flex items-center gap-2", className)}>
        <div className="w-16 h-5 bg-muted rounded animate-pulse" />
        {showCommit && <div className="w-14 h-4 bg-muted rounded animate-pulse" />}
      </div>
    );
  }

  if (error || !status) {
    return (
      <div className={cn("flex items-center gap-2", className)}>
        <Badge variant="outline" className="text-muted-foreground">
          Unknown
        </Badge>
        {showCommit && (
          <span className="text-xs font-mono text-muted-foreground">
            {commitSha.substring(0, 7)}
          </span>
        )}
      </div>
    );
  }

  const stageWithUrl = status.stages.find((s) => s.url);

  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <div className={cn("flex items-center gap-2", className)}>
            <PipelineStatusIndicator status={status.overall_status} size="sm" />
            {showCommit && (
              <span className="flex items-center gap-1 text-xs font-mono text-muted-foreground">
                <GitCommit className="w-3 h-3" />
                {commitSha.substring(0, 7)}
              </span>
            )}
            {stageWithUrl && (
              <a
                href={stageWithUrl.url}
                target="_blank"
                rel="noopener noreferrer"
                className="text-muted-foreground hover:text-foreground transition-colors"
                onClick={(e) => e.stopPropagation()}
              >
                <ExternalLink className="w-3 h-3" />
              </a>
            )}
          </div>
        </TooltipTrigger>
        <TooltipContent side="top" className="max-w-xs">
          <div className="space-y-1">
            <p className="font-medium">{status.service_name}</p>
            <div className="text-xs space-y-0.5">
              {status.stages.map((stage) => (
                <div key={stage.name} className="flex items-center justify-between gap-4">
                  <span>{stage.name}</span>
                  <span
                    className={cn(
                      stage.status === "success" && "text-status-success",
                      stage.status === "failure" && "text-status-error",
                      stage.status === "in_progress" && "text-status-info"
                    )}
                  >
                    {stage.status}
                  </span>
                </div>
              ))}
            </div>
          </div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}

export default PipelineStatusBadge;
