"use client";

/**
 * Pipeline Step Components
 * Sub-components for the unified pipeline progress display
 */

import {
  CheckCircle2,
  Circle,
  Loader2,
  XCircle,
  ExternalLink,
  ChevronDown,
  ChevronRight,
  AlertCircle,
  Shield,
  FileText,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { Badge } from "@enclii/ui-components/badge";
import type {
  StepIconProps,
  WorkflowRunItemProps,
  StageDetailsProps,
} from "./pipeline-types";
import { formatDuration, getOverallStatusLabel } from "./pipeline-utils";
import type { OverallPipelineStatus } from "@/lib/types/pipeline";

// ============================================================================
// StepIcon - Status indicator icon for pipeline steps
// ============================================================================

export function StepIcon({ status }: StepIconProps) {
  switch (status) {
    case "completed":
      return <CheckCircle2 className="h-6 w-6 text-status-success" />;
    case "active":
      return <Loader2 className="h-6 w-6 animate-spin text-status-info" />;
    case "failed":
      return <XCircle className="h-6 w-6 text-status-error" />;
    case "skipped":
      return <Circle className="h-6 w-6 text-status-neutral" />;
    default:
      return <Circle className="h-6 w-6 text-muted-foreground" />;
  }
}

// ============================================================================
// PipelineStatusBadge - Overall pipeline status indicator
// ============================================================================

interface PipelineStatusBadgeProps {
  status: OverallPipelineStatus;
}

export function PipelineStatusBadge({ status }: PipelineStatusBadgeProps) {
  const variants: Record<OverallPipelineStatus, string> = {
    pending: "bg-status-neutral-muted text-status-neutral-muted-foreground",
    building: "bg-status-warning-muted text-status-warning-muted-foreground",
    ready: "bg-status-info-muted text-status-info-muted-foreground",
    deploying: "bg-status-info-muted text-status-info-muted-foreground",
    running: "bg-status-success-muted text-status-success-muted-foreground",
    failed: "bg-status-error-muted text-status-error-muted-foreground",
  };

  return (
    <Badge className={cn("font-medium", variants[status])}>
      {getOverallStatusLabel(status)}
    </Badge>
  );
}

// ============================================================================
// WorkflowRunItem - Individual CI workflow run display
// ============================================================================

export function WorkflowRunItem({ run }: WorkflowRunItemProps) {
  const statusIcon =
    run.status === "completed" ? (
      run.conclusion === "success" ? (
        <CheckCircle2 className="h-4 w-4 text-status-success" />
      ) : (
        <XCircle className="h-4 w-4 text-status-error" />
      )
    ) : run.status === "in_progress" ? (
      <Loader2 className="h-4 w-4 animate-spin text-status-info" />
    ) : (
      <Circle className="h-4 w-4 text-status-neutral" />
    );

  return (
    <div className="flex items-center justify-between rounded-md bg-muted/50 px-3 py-2">
      <div className="flex items-center gap-2">
        {statusIcon}
        <span className="text-sm font-medium">{run.workflow_name}</span>
        {run.run_number && (
          <span className="text-xs text-muted-foreground">#{run.run_number}</span>
        )}
      </div>
      <div className="flex items-center gap-2">
        {run.started_at && (
          <span className="text-xs text-muted-foreground">
            {formatDuration(run.started_at, run.completed_at)}
          </span>
        )}
        {run.html_url && (
          <a
            href={run.html_url}
            target="_blank"
            rel="noopener noreferrer"
            className="text-muted-foreground transition-colors hover:text-foreground"
          >
            <ExternalLink className="h-4 w-4" />
          </a>
        )}
      </div>
    </div>
  );
}

// ============================================================================
// StageDetails - Expandable details for each pipeline stage
// ============================================================================

export function StageDetails({
  status,
  stepId,
  expanded,
  onToggle,
}: StageDetailsProps) {
  const renderCIDetails = () => {
    if (!status.github_actions) return null;
    const { workflows, success_count, failure_count, in_progress, total_runs } =
      status.github_actions;

    return (
      <div className="space-y-2">
        <div className="flex items-center gap-4 text-xs text-muted-foreground">
          <span className="flex items-center gap-1">
            <CheckCircle2 className="h-3 w-3 text-status-success" />
            {success_count} passed
          </span>
          {failure_count > 0 && (
            <span className="flex items-center gap-1">
              <XCircle className="h-3 w-3 text-status-error" />
              {failure_count} failed
            </span>
          )}
          {in_progress > 0 && (
            <span className="flex items-center gap-1">
              <Loader2 className="h-3 w-3 text-status-info" />
              {in_progress} running
            </span>
          )}
          <span>{total_runs} total</span>
        </div>
        {expanded && workflows.length > 0 && (
          <div className="mt-2 space-y-1">
            {workflows.map((run) => (
              <WorkflowRunItem key={run.id} run={run} />
            ))}
          </div>
        )}
      </div>
    );
  };

  const renderBuildDetails = () => {
    if (!status.roundhouse) return null;
    const { release, has_sbom, has_signature, image_uri, error_message } =
      status.roundhouse;

    return (
      <div className="space-y-2">
        <div className="flex items-center gap-4 text-xs text-muted-foreground">
          {image_uri && (
            <span className="max-w-[200px] truncate font-mono" title={image_uri}>
              {image_uri.split("/").pop()?.substring(0, 20)}...
            </span>
          )}
          {has_sbom && (
            <span className="flex items-center gap-1 text-status-success">
              <FileText className="h-3 w-3" />
              SBOM
            </span>
          )}
          {has_signature && (
            <span className="flex items-center gap-1 text-status-success">
              <Shield className="h-3 w-3" />
              Signed
            </span>
          )}
        </div>
        {expanded && release && (
          <div className="space-y-1 rounded-md bg-muted/50 p-3 text-xs">
            <div className="flex justify-between">
              <span className="text-muted-foreground">Version:</span>
              <span className="font-mono">{release.version}</span>
            </div>
            {release.git_sha && (
              <div className="flex justify-between">
                <span className="text-muted-foreground">Commit:</span>
                <span className="font-mono">{release.git_sha.substring(0, 8)}</span>
              </div>
            )}
            {release.created_at && (
              <div className="flex justify-between">
                <span className="text-muted-foreground">Started:</span>
                <span>{new Date(release.created_at).toLocaleTimeString()}</span>
              </div>
            )}
          </div>
        )}
        {error_message && (
          <div className="flex items-start gap-2 rounded-md border border-status-error/20 bg-status-error-muted p-2">
            <AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-status-error" />
            <span className="text-sm text-status-error-muted-foreground">
              {error_message}
            </span>
          </div>
        )}
      </div>
    );
  };

  const renderDeployDetails = () => {
    if (!status.deployment) return null;
    const { deployment, health, error_message } = status.deployment;

    return (
      <div className="space-y-2">
        <div className="flex items-center gap-4 text-xs text-muted-foreground">
          <span
            className={cn(
              "rounded-full px-2 py-0.5 text-xs font-medium",
              health === "healthy"
                ? "bg-status-success-muted text-status-success-muted-foreground"
                : health === "unhealthy"
                  ? "bg-status-error-muted text-status-error-muted-foreground"
                  : "bg-status-neutral-muted text-status-neutral-muted-foreground"
            )}
          >
            {health}
          </span>
          {deployment?.replicas !== undefined && (
            <span>
              {deployment.ready_replicas || 0}/{deployment.replicas} replicas
            </span>
          )}
        </div>
        {expanded && deployment && (
          <div className="space-y-1 rounded-md bg-muted/50 p-3 text-xs">
            <div className="flex justify-between">
              <span className="text-muted-foreground">ID:</span>
              <span className="font-mono">{deployment.id.substring(0, 8)}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Status:</span>
              <span className="capitalize">{deployment.status}</span>
            </div>
            {deployment.created_at && (
              <div className="flex justify-between">
                <span className="text-muted-foreground">Started:</span>
                <span>{new Date(deployment.created_at).toLocaleTimeString()}</span>
              </div>
            )}
          </div>
        )}
        {error_message && (
          <div className="flex items-start gap-2 rounded-md border border-status-error/20 bg-status-error-muted p-2">
            <AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-status-error" />
            <span className="text-sm text-status-error-muted-foreground">
              {error_message}
            </span>
          </div>
        )}
      </div>
    );
  };

  const hasDetails =
    (stepId === "ci" && status.github_actions) ||
    (stepId === "build" && status.roundhouse) ||
    (stepId === "deploy" && status.deployment);

  if (!hasDetails) return null;

  return (
    <div className="ml-8 mt-2">
      <button
        onClick={onToggle}
        className="flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
      >
        {expanded ? (
          <ChevronDown className="h-3 w-3" />
        ) : (
          <ChevronRight className="h-3 w-3" />
        )}
        {expanded ? "Hide details" : "Show details"}
      </button>
      <div className="mt-2">
        {stepId === "ci" && renderCIDetails()}
        {stepId === "build" && renderBuildDetails()}
        {stepId === "deploy" && renderDeployDetails()}
      </div>
    </div>
  );
}
