"use client";

/**
 * UnifiedPipelineProgress
 * Main component for displaying unified CI/CD pipeline progress
 *
 * Split structure:
 * - pipeline-types.ts: Type definitions
 * - pipeline-utils.ts: Utility functions
 * - pipeline-step-components.tsx: Sub-components
 */

import { useState, useEffect, useCallback, useRef } from "react";
import {
  CheckCircle2,
  Loader2,
  XCircle,
  GitCommit,
  Package,
  Server,
  PlayCircle,
  AlertCircle,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { apiGet } from "@/lib/api";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@enclii/ui-components/button";
import type { UnifiedBuildStatus } from "@/lib/types/pipeline";
import type { UnifiedPipelineProgressProps, PipelineStep } from "./pipeline-types";
import { formatDuration, getStepStatusFromBuildStatus } from "./pipeline-utils";
import {
  StepIcon,
  PipelineStatusBadge,
  StageDetails,
} from "./pipeline-step-components";

// ============================================================================
// Constants
// ============================================================================

const PIPELINE_STEPS: PipelineStep[] = [
  {
    id: "ci",
    label: "CI Pipeline",
    icon: <PlayCircle className="h-4 w-4" />,
    description: "GitHub Actions workflows",
  },
  {
    id: "build",
    label: "Container Build",
    icon: <Package className="h-4 w-4" />,
    description: "Roundhouse image build",
  },
  {
    id: "deploy",
    label: "Deployment",
    icon: <Server className="h-4 w-4" />,
    description: "Kubernetes deployment",
  },
];

// ============================================================================
// Main Component
// ============================================================================

export function UnifiedPipelineProgress({
  serviceId,
  commitSha,
  serviceName,
  onComplete,
  onError,
  pollInterval = 5000,
  className,
}: UnifiedPipelineProgressProps) {
  const [status, setStatus] = useState<UnifiedBuildStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [elapsedTime, setElapsedTime] = useState<string>("");
  const [expandedSteps, setExpandedSteps] = useState<Set<string>>(new Set());
  const hasCalledComplete = useRef(false);
  const startTimeRef = useRef<string | null>(null);

  // Fetch status
  const fetchStatus = useCallback(async () => {
    try {
      const data = await apiGet<UnifiedBuildStatus>(
        `/v1/services/${serviceId}/builds/${commitSha}/status`
      );
      setStatus(data);
      setError(null);

      // Track start time from first stage
      if (!startTimeRef.current && data.stages.length > 0) {
        const firstStage = data.stages.find((s) => s.started_at);
        if (firstStage?.started_at) {
          startTimeRef.current = firstStage.started_at;
        }
      }

      // Handle completion
      if (data.overall_status === "running" && !hasCalledComplete.current) {
        hasCalledComplete.current = true;
        onComplete?.();
      }

      // Handle failure
      if (data.overall_status === "failed") {
        const failedStage = data.stages.find((s) => s.status === "failure");
        if (failedStage?.error_message) {
          onError?.(failedStage.error_message);
        }
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : "Failed to fetch status";
      setError(message);
    } finally {
      setLoading(false);
    }
  }, [serviceId, commitSha, onComplete, onError]);

  // Initial fetch and polling
  useEffect(() => {
    fetchStatus();

    const interval = setInterval(() => {
      if (
        status?.overall_status !== "running" &&
        status?.overall_status !== "failed"
      ) {
        fetchStatus();
      }
    }, pollInterval);

    return () => clearInterval(interval);
  }, [fetchStatus, pollInterval, status?.overall_status]);

  // Update elapsed time
  useEffect(() => {
    if (!startTimeRef.current) return;
    if (status?.overall_status === "running" || status?.overall_status === "failed") {
      const lastStage = [...(status.stages || [])].reverse().find((s) => s.completed_at);
      setElapsedTime(formatDuration(startTimeRef.current, lastStage?.completed_at));
      return;
    }

    const interval = setInterval(() => {
      if (startTimeRef.current) {
        setElapsedTime(formatDuration(startTimeRef.current));
      }
    }, 1000);

    return () => clearInterval(interval);
  }, [status?.overall_status, status?.stages]);

  const toggleStep = (stepId: string) => {
    setExpandedSteps((prev) => {
      const next = new Set(prev);
      if (next.has(stepId)) {
        next.delete(stepId);
      } else {
        next.add(stepId);
      }
      return next;
    });
  };

  const isTerminal =
    status?.overall_status === "running" || status?.overall_status === "failed";

  if (loading && !status) {
    return <UnifiedPipelineProgressSkeleton />;
  }

  if (error && !status) {
    return (
      <Card className={cn("border-status-error/50", className)}>
        <CardContent className="pt-6">
          <div className="flex items-center gap-2 text-status-error">
            <AlertCircle className="h-5 w-5" />
            <span>{error}</span>
          </div>
          <Button variant="outline" size="sm" className="mt-4" onClick={fetchStatus}>
            Retry
          </Button>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className={className}>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <div className="space-y-1">
            <CardTitle className="flex items-center gap-2 text-lg">
              {!isTerminal && <Loader2 className="h-4 w-4 animate-spin text-status-info" />}
              {isTerminal && status?.overall_status === "running" && (
                <CheckCircle2 className="h-4 w-4 text-status-success" />
              )}
              {isTerminal && status?.overall_status === "failed" && (
                <XCircle className="h-4 w-4 text-status-error" />
              )}
              Pipeline Progress
            </CardTitle>
            <div className="flex items-center gap-3 text-sm text-muted-foreground">
              {serviceName && <span>{serviceName}</span>}
              <span className="flex items-center gap-1 font-mono">
                <GitCommit className="h-3 w-3" />
                {commitSha.substring(0, 8)}
              </span>
            </div>
          </div>
          <div className="flex items-center gap-3">
            {status && <PipelineStatusBadge status={status.overall_status} />}
            {elapsedTime && (
              <span className="text-sm text-muted-foreground">{elapsedTime}</span>
            )}
          </div>
        </div>
      </CardHeader>

      <CardContent>
        {/* Pipeline Steps */}
        <div className="space-y-4">
          {PIPELINE_STEPS.map((step, index) => {
            const stepStatus = getStepStatusFromBuildStatus(step.id, status);
            const isLast = index === PIPELINE_STEPS.length - 1;

            return (
              <div key={step.id}>
                <div className="flex items-start gap-3">
                  {/* Icon and connector */}
                  <div className="flex flex-col items-center">
                    <StepIcon status={stepStatus} />
                    {!isLast && (
                      <div
                        className={cn(
                          "mt-1 h-8 w-0.5",
                          stepStatus === "completed"
                            ? "bg-status-success"
                            : stepStatus === "active"
                              ? "bg-status-info"
                              : "bg-muted"
                        )}
                      />
                    )}
                  </div>

                  {/* Content */}
                  <div className="min-w-0 flex-1 pb-2">
                    <div className="flex items-center gap-2">
                      <span
                        className={cn(
                          "font-medium",
                          stepStatus === "completed"
                            ? "text-status-success"
                            : stepStatus === "active"
                              ? "text-status-info"
                              : stepStatus === "failed"
                                ? "text-status-error"
                                : "text-muted-foreground"
                        )}
                      >
                        {step.label}
                      </span>
                      <span className="text-muted-foreground">{step.icon}</span>
                    </div>
                    <p className="mt-0.5 text-xs text-muted-foreground">
                      {step.description}
                    </p>

                    {/* Stage details */}
                    {status && (
                      <StageDetails
                        status={status}
                        stepId={step.id}
                        expanded={expandedSteps.has(step.id)}
                        onToggle={() => toggleStep(step.id)}
                      />
                    )}
                  </div>
                </div>
              </div>
            );
          })}
        </div>

        {/* Success Message */}
        {status?.overall_status === "running" && (
          <div className="mt-4 rounded-md border border-status-success/20 bg-status-success-muted p-3">
            <div className="flex items-center gap-2">
              <CheckCircle2 className="h-4 w-4 text-status-success" />
              <span className="text-sm font-medium text-status-success-muted-foreground">
                Deployment successful! Service is now running.
              </span>
            </div>
          </div>
        )}

        {/* Overall Error Message */}
        {status?.overall_status === "failed" && (
          <div className="mt-4 rounded-md border border-status-error/20 bg-status-error-muted p-3">
            <div className="flex items-center gap-2">
              <XCircle className="h-4 w-4 text-status-error" />
              <span className="text-sm font-medium text-status-error-muted-foreground">
                Pipeline failed. Check the failed stage for details.
              </span>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

// ============================================================================
// Skeleton
// ============================================================================

export function UnifiedPipelineProgressSkeleton() {
  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <div className="space-y-2">
            <div className="h-5 w-40 animate-pulse rounded bg-muted" />
            <div className="h-4 w-32 animate-pulse rounded bg-muted" />
          </div>
          <div className="flex items-center gap-3">
            <div className="h-6 w-20 animate-pulse rounded bg-muted" />
            <div className="h-4 w-12 animate-pulse rounded bg-muted" />
          </div>
        </div>
      </CardHeader>
      <CardContent>
        <div className="space-y-4">
          {[1, 2, 3].map((i) => (
            <div key={i} className="flex items-start gap-3">
              <div className="flex flex-col items-center">
                <div className="h-6 w-6 animate-pulse rounded-full bg-muted" />
                {i < 3 && <div className="mt-1 h-8 w-0.5 bg-muted" />}
              </div>
              <div className="flex-1 pb-2">
                <div className="h-4 w-28 animate-pulse rounded bg-muted" />
                <div className="mt-1 h-3 w-40 animate-pulse rounded bg-muted" />
              </div>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  );
}

export default UnifiedPipelineProgress;
