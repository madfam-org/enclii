"use client";

import { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { Activity, AlertTriangle, Clock, Loader2 } from "lucide-react";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { apiRequest } from "@/lib/api";
import { formatRelativeTime } from "@/lib/formatting";
import { cn } from "@/lib/utils";
import {
  groupProcessesByService,
  processHref,
  processStatusLabel,
  processSummaryTitle,
  topProjectProcesses,
  type ProjectLiveState,
  type ProjectProcess,
  type ProjectProcessStatus,
  type ProjectProcessSummary,
  type ProjectProcessTimelineResponse,
} from "@/lib/project-process-feed";

interface ProjectProcessFeedProject {
  name: string;
  slug: string;
  processSummary?: ProjectProcessSummary;
  liveState?: ProjectLiveState;
}

interface ProcessAwareService {
  lastProcess?: ProjectProcess;
}

const processStatusTone: Record<ProjectProcessStatus, string> = {
  blocked:
    "border-status-error/40 bg-status-error/15 text-status-error hover:bg-status-error/25",
  failed:
    "border-status-error/40 bg-status-error/15 text-status-error hover:bg-status-error/25",
  running:
    "border-status-info/40 bg-status-info/15 text-status-info hover:bg-status-info/25",
  waiting:
    "border-status-warning/40 bg-status-warning/15 text-status-warning hover:bg-status-warning/25",
  queued:
    "border-status-warning/40 bg-status-warning/15 text-status-warning hover:bg-status-warning/25",
  succeeded:
    "border-status-success/35 bg-status-success/10 text-status-success hover:bg-status-success/20",
  cancelled:
    "border-border/50 bg-muted/40 text-muted-foreground hover:bg-muted/60",
  unknown:
    "border-border/50 bg-muted/40 text-muted-foreground hover:bg-muted/60",
};

const liveStateLabel: Record<ProjectLiveState, string> = {
  idle: "No live work",
  running: "Live work",
  failed: "Failed work",
  blocked: "Blocked work",
  unknown: "Unknown work",
};

function ProcessIcon({ status }: { status: ProjectProcessStatus }) {
  if (status === "running") {
    return <Loader2 className="h-3 w-3 shrink-0 animate-spin" aria-hidden="true" />;
  }
  if (status === "blocked" || status === "failed") {
    return <AlertTriangle className="h-3 w-3 shrink-0" aria-hidden="true" />;
  }
  if (status === "waiting" || status === "queued") {
    return <Clock className="h-3 w-3 shrink-0" aria-hidden="true" />;
  }
  return <Activity className="h-3 w-3 shrink-0" aria-hidden="true" />;
}

function ProcessChip({
  projectSlug,
  process,
  compact = false,
}: {
  projectSlug: string;
  process: ProjectProcess;
  compact?: boolean;
}) {
  const href = processHref(projectSlug, process);
  const external = /^https?:\/\//.test(href);
  const content = (
    <span
      className={cn(
        "inline-flex max-w-full items-center gap-1 rounded-full border font-medium transition-colors",
        compact ? "px-1.5 py-0.5 text-[10px]" : "px-2 py-1 text-[11px]",
        processStatusTone[process.status] || processStatusTone.unknown,
      )}
      title={processSummaryTitle(process)}
    >
      <ProcessIcon status={process.status} />
      <span className="truncate">
        {compact
          ? processStatusLabel(process.status)
          : process.service_name || process.kind.replace(/_/g, " ")}
      </span>
    </span>
  );

  if (external) {
    return (
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        className="relative z-10 min-w-0"
        aria-label={processSummaryTitle(process)}
      >
        {content}
      </a>
    );
  }

  return (
    <Link
      href={href}
      className="relative z-10 min-w-0"
      aria-label={processSummaryTitle(process)}
    >
      {content}
    </Link>
  );
}

export function ProjectProcessRail({
  project,
  onOpenDetails,
}: {
  project: ProjectProcessFeedProject;
  onOpenDetails: () => void;
}) {
  const processes = topProjectProcesses(project.processSummary, 3);
  if (processes.length === 0) return null;

  const activeCount = project.processSummary?.active_count || 0;
  const failedCount = project.processSummary?.failed_count || 0;
  const blockedCount = project.processSummary?.blocked_count || 0;
  const liveState = project.liveState || "idle";

  return (
    <div
      className="border-border/50 bg-muted/20 relative z-10 mt-2 rounded-md border px-2 py-1.5"
      aria-label={`Project process feed: ${liveStateLabel[liveState]}`}
    >
      <div className="mb-1 flex items-center justify-between gap-2">
        <span className="text-muted-foreground flex items-center gap-1 text-[10px] font-medium uppercase tracking-wide">
          <Activity className="h-3 w-3" aria-hidden="true" />
          Process feed
        </span>
        <button
          type="button"
          onClick={onOpenDetails}
          className="text-muted-foreground hover:text-foreground rounded text-[10px] tabular-nums underline-offset-2 hover:underline focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-primary"
          aria-label={`Open process feed for ${project.name}`}
        >
          {blockedCount > 0
            ? `${blockedCount} blocked`
            : failedCount > 0
              ? `${failedCount} failed`
              : activeCount > 0
                ? `${activeCount} active`
                : "recent"}
        </button>
      </div>
      <div className="flex min-w-0 flex-wrap gap-1">
        {processes.map((process) => (
          <ProcessChip
            key={process.id}
            projectSlug={project.slug}
            process={process}
          />
        ))}
      </div>
    </div>
  );
}

export function ProjectProcessDrawer({
  project,
  open,
  onOpenChange,
}: {
  project: ProjectProcessFeedProject;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const fallbackProcesses = project.processSummary?.processes || [];
  const [historyProcesses, setHistoryProcesses] = useState<ProjectProcess[] | null>(
    null,
  );
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setHistoryProcesses(null);
    setError(null);
  }, [project.slug]);

  useEffect(() => {
    if (!open) return;

    const controller = new AbortController();
    setLoading(true);
    setError(null);

    apiRequest<ProjectProcessTimelineResponse>(
      `/v1/projects/${encodeURIComponent(project.slug)}/processes?limit=50&active_only=true`,
      { method: "GET", signal: controller.signal },
    )
      .then((response) => {
        setHistoryProcesses(response.processes || response.summary?.processes || []);
      })
      .catch((err) => {
        if (err instanceof DOMException && err.name === "AbortError") return;
        setError(
          err instanceof Error ? err.message : "Failed to load process history",
        );
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          setLoading(false);
        }
      });

    return () => controller.abort();
  }, [open, project.slug]);

  const processes = historyProcesses ?? fallbackProcesses;
  const groupedProcesses = useMemo(
    () => groupProcessesByService(processes, project.name),
    [processes, project.name],
  );

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-full overflow-y-auto sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>{project.name} process feed</SheetTitle>
          <SheetDescription>
            Active service processes currently reported for this project.
          </SheetDescription>
        </SheetHeader>

        <div className="mt-6 space-y-4">
          {error && (
            <div className="border-status-warning/40 bg-status-warning/10 text-status-warning rounded-md border p-3 text-xs">
              {error}
            </div>
          )}

          {loading && historyProcesses === null && (
            <div className="border-border/60 bg-muted/30 text-muted-foreground flex items-center gap-2 rounded-md border p-3 text-sm">
              <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />
              Loading active process events...
            </div>
          )}

          {groupedProcesses.length === 0 ? (
            <div className="border-border/60 bg-muted/30 text-muted-foreground rounded-md border p-4 text-sm">
              No active process events are currently reported for this project.
            </div>
          ) : (
            groupedProcesses.map((group) => (
              <section key={group.key} className="space-y-2">
                <div className="flex items-center justify-between gap-2">
                  <h3 className="text-foreground truncate text-sm font-semibold">
                    {group.service_name}
                  </h3>
                  <span className="text-muted-foreground text-xs tabular-nums">
                    {group.processes.length} event
                    {group.processes.length === 1 ? "" : "s"}
                  </span>
                </div>
                <div className="space-y-2">
                  {group.processes.map((process) => (
                    <ProcessFeedRow
                      key={process.id}
                      projectSlug={project.slug}
                      process={process}
                    />
                  ))}
                </div>
              </section>
            ))
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}

function ProcessFeedRow({
  projectSlug,
  process,
}: {
  projectSlug: string;
  process: ProjectProcess;
}) {
  const href = processHref(projectSlug, process);
  const external = /^https?:\/\//.test(href);
  const content = (
    <div className="border-border/60 hover:bg-muted/30 flex gap-3 rounded-md border p-3 transition-colors">
      <div
        className={cn(
          "mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-full border",
          processStatusTone[process.status] || processStatusTone.unknown,
        )}
      >
        <ProcessIcon status={process.status} />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center justify-between gap-3">
          <p className="text-foreground truncate text-sm font-medium">
            {processStatusLabel(process.status)} ·{" "}
            {process.kind.replace(/_/g, " ")}
          </p>
          <span className="text-muted-foreground shrink-0 text-xs">
            {formatRelativeTime(process.updated_at)}
          </span>
        </div>
        <p className="text-muted-foreground mt-0.5 text-xs">
          {process.phase ? process.phase.replace(/_/g, " ") : process.source}
          {process.service_name ? ` · ${process.service_name}` : ""}
        </p>
        {process.message && (
          <p className="text-muted-foreground mt-1 line-clamp-2 text-xs">
            {process.message}
          </p>
        )}
        <div className="text-muted-foreground/80 mt-2 flex flex-wrap gap-2 text-[11px]">
          {process.environment && <span>{process.environment}</span>}
          {process.branch && <span>{process.branch}</span>}
          {process.commit_sha && (
            <span className="font-mono">{process.commit_sha.slice(0, 8)}</span>
          )}
        </div>
      </div>
    </div>
  );

  if (external) {
    return (
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        aria-label={processSummaryTitle(process)}
      >
        {content}
      </a>
    );
  }
  return (
    <Link href={href} aria-label={processSummaryTitle(process)}>
      {content}
    </Link>
  );
}

export function ServiceProcessIndicator({
  projectSlug,
  service,
}: {
  projectSlug: string;
  service: ProcessAwareService;
}) {
  if (!service.lastProcess) return null;
  return (
    <ProcessChip
      projectSlug={projectSlug}
      process={service.lastProcess}
      compact
    />
  );
}
