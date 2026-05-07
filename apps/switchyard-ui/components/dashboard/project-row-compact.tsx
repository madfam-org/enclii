"use client";

import Link from "next/link";
import {
  Activity,
  AlertTriangle,
  GitBranch,
  Globe,
  Loader2,
  Lock,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { formatRelativeTime } from "@/lib/formatting";
import {
  FrameworkIcon,
  getFrameworkLabel,
} from "./framework-icon";
import type { CompactProject } from "./project-card-compact";
import { processStatusLabel } from "@/lib/project-process-feed";

const aggregateStatusColor: Record<string, string> = {
  healthy: "bg-status-success",
  degraded: "bg-status-warning",
  failing: "bg-status-error",
  unknown: "bg-muted-foreground",
};

const liveStateColor: Record<string, string> = {
  blocked: "border-status-error/40 bg-status-error/15 text-status-error",
  failed: "border-status-error/40 bg-status-error/15 text-status-error",
  running: "border-status-info/40 bg-status-info/15 text-status-info",
  idle: "border-border/50 bg-muted/40 text-muted-foreground",
  unknown: "border-border/50 bg-muted/40 text-muted-foreground",
};

interface ProjectRowCompactProps {
  project: CompactProject;
  className?: string;
}

// List-row variant of ProjectCardCompact for the dashboard's "list" view mode.
// Renders the same project as a single horizontal row optimized for scanning
// many projects at once: framework icon + name + status dot + replicas count
// + visibility chip + branch + relative timestamp. Skips the per-service
// table — operators who need that detail click into /projects/{slug}. The
// whole row is a Link so keyboard nav and middle-click "open in new tab"
// work exactly as on the card variant. Granular interactivity is the
// card's job; this is the dense-scanning view.
export function ProjectRowCompact({
  project,
  className,
}: ProjectRowCompactProps) {
  const aggregateStatus = project.aggregateStatus || "unknown";
  const dotColor =
    aggregateStatusColor[aggregateStatus] || "bg-muted-foreground";
  const branch = project.lastDeployment?.branch;
  const commitMessage = project.lastDeployment?.commitMessage;
  const timestamp = project.lastDeployment?.timestamp;
  const latestProcess = project.processSummary?.latest;
  const liveState = project.liveState || "idle";
  const activeProcessCount =
    (project.processSummary?.active_count || 0) +
    (project.processSummary?.failed_count || 0) +
    (project.processSummary?.blocked_count || 0);

  return (
    <Link
      href={`/projects/${project.slug}`}
      className={cn(
        "hover:bg-muted/40 focus-visible:bg-muted/40 focus-visible:ring-primary/40 group relative flex items-center gap-3 px-3 py-2.5 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset sm:gap-4 sm:px-4",
        className,
      )}
      role="listitem"
    >
      <FrameworkIcon framework={project.framework || "unknown"} size="sm" />

      <div className="flex min-w-0 flex-1 items-center gap-2">
        <span className="truncate text-sm font-semibold">{project.name}</span>
        {project.framework && project.framework !== "unknown" && (
          <span
            className="border-border/60 bg-muted/40 text-muted-foreground hidden shrink-0 rounded-full border px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide md:inline-block"
            aria-label={`Framework: ${getFrameworkLabel(project.framework)}`}
          >
            {getFrameworkLabel(project.framework)}
          </span>
        )}
      </div>

      {project.serviceCount !== undefined && (
        <span
          className="text-muted-foreground hidden shrink-0 text-xs tabular-nums sm:inline-block"
          aria-label={`${project.healthyCount ?? 0} of ${project.serviceCount} services healthy`}
        >
          {project.healthyCount ?? 0}/{project.serviceCount}
        </span>
      )}

      {project.repoMeta?.visibility === "private" && (
        <Lock
          className="text-status-warning hidden h-3.5 w-3.5 shrink-0 sm:inline-block"
          aria-label="Private repository"
        />
      )}
      {project.repoMeta?.visibility === "public" && (
        <Globe
          className="text-status-success hidden h-3.5 w-3.5 shrink-0 sm:inline-block"
          aria-label="Public repository"
        />
      )}
      {project.repoMeta?.visibility === "internal" && (
        <Lock
          className="text-status-info hidden h-3.5 w-3.5 shrink-0 sm:inline-block"
          aria-label="Internal repository"
        />
      )}

      {project.domain && (
        <span className="text-muted-foreground hidden min-w-0 max-w-[180px] shrink-0 truncate text-xs lg:inline-block">
          {project.domain}
        </span>
      )}

      {latestProcess && (
        <span
          className={cn(
            "hidden shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-[11px] font-medium lg:inline-flex",
            liveStateColor[liveState] || liveStateColor.unknown,
          )}
          title={`${latestProcess.service_name ? `${latestProcess.service_name}: ` : ""}${processStatusLabel(latestProcess.status)} ${latestProcess.kind.replace(/_/g, " ")}`}
          aria-label={`Project process state: ${processStatusLabel(latestProcess.status)}`}
        >
          {liveState === "running" ? (
            <Loader2 className="h-3 w-3 animate-spin" aria-hidden="true" />
          ) : liveState === "failed" || liveState === "blocked" ? (
            <AlertTriangle className="h-3 w-3" aria-hidden="true" />
          ) : (
            <Activity className="h-3 w-3" aria-hidden="true" />
          )}
          <span className="tabular-nums">
            {activeProcessCount > 0 ? activeProcessCount : "recent"}
          </span>
        </span>
      )}

      {(branch || commitMessage) && (
        <div className="text-muted-foreground hidden min-w-0 max-w-[280px] shrink-0 items-center gap-1.5 text-xs xl:flex">
          {branch && (
            <>
              <GitBranch className="h-3 w-3 shrink-0" aria-hidden="true" />
              <span className="truncate">{branch}</span>
            </>
          )}
          {commitMessage && (
            <span
              className="text-muted-foreground/70 truncate"
              title={commitMessage}
            >
              · {commitMessage}
            </span>
          )}
        </div>
      )}

      {timestamp && (
        <span className="text-muted-foreground hidden shrink-0 text-xs tabular-nums sm:inline-block">
          {formatRelativeTime(timestamp)}
        </span>
      )}

      <div className="flex shrink-0 items-center gap-1.5">
        <div
          className={cn("h-2.5 w-2.5 rounded-full", dotColor)}
          aria-label={`Status: ${aggregateStatus}`}
        />
      </div>
    </Link>
  );
}
