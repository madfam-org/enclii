"use client";

import Link from "next/link";
import { ExternalLink, GitBranch, Github } from "lucide-react";
import { Card } from "@/components/ui/card";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { formatRelativeTime } from "@/lib/formatting";
import { FrameworkIcon, FrameworkType } from "./framework-icon";

export interface CompactService {
  id: string;
  name: string;
  status: "running" | "pending" | "failed" | "deploying" | "unknown";
  health: "healthy" | "unhealthy" | "unknown";
}

export interface CompactProject {
  id: string;
  name: string;
  slug: string;
  description?: string;
  framework?: FrameworkType | string;
  gitRepo?: string;
  domain?: string;
  lastDeployment?: {
    timestamp: string;
    status: "success" | "failed" | "pending" | "building";
    branch: string;
    commitMessage?: string;
  };
  serviceCount?: number;
  healthyCount?: number;
  services?: CompactService[];
  aggregateStatus?: "healthy" | "degraded" | "failing" | "unknown";
  updatedAt?: string;
}

interface ProjectCardCompactProps {
  project: CompactProject;
  className?: string;
}

const serviceStatusColor: Record<string, string> = {
  running: "bg-status-success",
  failed: "bg-status-error",
  pending: "bg-status-warning",
  deploying: "bg-status-info animate-pulse",
  unknown: "bg-muted-foreground",
};

const serviceStatusLabel: Record<string, string> = {
  running: "Running",
  failed: "Failed",
  pending: "Pending",
  deploying: "Deploying",
  unknown: "Unknown",
};

const aggregateStatusColor: Record<string, string> = {
  healthy: "bg-status-success",
  degraded: "bg-status-warning",
  failing: "bg-status-error",
  unknown: "bg-muted-foreground",
};

const MAX_VISIBLE_PILLS = 4;

function ServiceStatusSummary({ services }: { services: CompactService[] }) {
  const counts: Record<string, number> = {};
  for (const s of services) {
    counts[s.status] = (counts[s.status] || 0) + 1;
  }
  const parts: string[] = [];
  if (counts.running) parts.push(`${counts.running} running`);
  if (counts.pending) parts.push(`${counts.pending} pending`);
  if (counts.deploying) parts.push(`${counts.deploying} deploying`);
  if (counts.failed) parts.push(`${counts.failed} failed`);
  if (counts.unknown) parts.push(`${counts.unknown} unknown`);
  return <>{parts.join(", ") || "No services"}</>;
}

export function ProjectCardCompact({
  project,
  className,
}: ProjectCardCompactProps) {
  const services = project.services || [];
  const aggregateStatus = project.aggregateStatus || "unknown";
  const dotColor = aggregateStatusColor[aggregateStatus] || "bg-muted-foreground";

  const visibleServices = services.slice(0, MAX_VISIBLE_PILLS);
  const overflowServices = services.slice(MAX_VISIBLE_PILLS);

  return (
    <Link href={`/projects/${project.slug}`} className="block">
      <Card
        className={cn(
          "hover:border-primary/50 group relative flex min-h-[220px] flex-col justify-between p-4 transition-all duration-200 hover:shadow-lg",
          className,
        )}
      >
        {/* Row 1: Framework icon + name + status dot */}
        <div className="flex items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-2.5">
            <FrameworkIcon
              framework={project.framework || "unknown"}
              size="md"
            />
            <span className="truncate text-sm font-semibold">
              {project.name}
            </span>
          </div>
          <div className="flex shrink-0 items-center gap-1.5">
            {project.serviceCount !== undefined && (
              <span className="text-muted-foreground text-xs">
                {project.healthyCount ?? 0}/{project.serviceCount}
              </span>
            )}
            <div className={cn("h-2.5 w-2.5 rounded-full", dotColor)} />
          </div>
        </div>

        {/* Row 2: Service status pills */}
        {services.length > 0 && (
          <TooltipProvider delayDuration={200}>
            <div className="mt-2 flex flex-wrap gap-1">
              {visibleServices.map((service) => (
                <Tooltip key={service.id}>
                  <TooltipTrigger asChild>
                    <div className="bg-muted/50 hover:bg-muted flex items-center gap-1 rounded-full px-2 py-0.5 transition-colors cursor-default">
                      <div
                        className={cn(
                          "h-1.5 w-1.5 rounded-full",
                          serviceStatusColor[service.status] || "bg-muted-foreground",
                        )}
                      />
                      <span className="max-w-[80px] truncate text-[10px] font-medium">
                        {service.name}
                      </span>
                    </div>
                  </TooltipTrigger>
                  <TooltipContent>
                    <div className="text-xs">
                      <div className="font-medium">{service.name}</div>
                      <div className="text-muted-foreground">
                        {serviceStatusLabel[service.status] || "Unknown"} &middot;{" "}
                        {service.health}
                      </div>
                    </div>
                  </TooltipContent>
                </Tooltip>
              ))}
              {overflowServices.length > 0 && (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <div className="bg-muted/50 hover:bg-muted flex items-center rounded-full px-2 py-0.5 transition-colors cursor-default">
                      <span className="text-muted-foreground text-[10px] font-medium">
                        +{overflowServices.length}
                      </span>
                    </div>
                  </TooltipTrigger>
                  <TooltipContent>
                    <div className="text-xs space-y-0.5">
                      {overflowServices.map((s) => (
                        <div key={s.id} className="flex items-center gap-1.5">
                          <div
                            className={cn(
                              "h-1.5 w-1.5 rounded-full",
                              serviceStatusColor[s.status] || "bg-muted-foreground",
                            )}
                          />
                          <span>{s.name}</span>
                          <span className="text-muted-foreground">
                            {serviceStatusLabel[s.status] || "Unknown"}
                          </span>
                        </div>
                      ))}
                    </div>
                  </TooltipContent>
                </Tooltip>
              )}
            </div>
          </TooltipProvider>
        )}

        {/* Row 3: Commit message, description, or status summary */}
        <p className="text-muted-foreground mt-2 truncate text-xs">
          {project.lastDeployment?.commitMessage ||
            project.description ||
            (services.length > 0 ? (
              <ServiceStatusSummary services={services} />
            ) : (
              "No recent deployments"
            ))}
        </p>

        {/* Row 4: Domain URL */}
        {project.domain ? (
          <a
            href={`https://${project.domain}`}
            target="_blank"
            rel="noopener noreferrer"
            className="text-muted-foreground hover:text-foreground mt-2 flex items-center gap-1.5 truncate text-xs transition-colors"
            onClick={(e) => e.stopPropagation()}
          >
            <ExternalLink className="h-3 w-3 shrink-0" />
            <span className="truncate">{project.domain}</span>
          </a>
        ) : (
          <div className="mt-2 h-4" />
        )}

        {/* Row 4b: GitHub repo */}
        {project.gitRepo && (
          <a
            href={project.gitRepo.startsWith('http') ? project.gitRepo : `https://github.com/${project.gitRepo}`}
            target="_blank"
            rel="noopener noreferrer"
            className="text-muted-foreground hover:text-foreground mt-1.5 flex items-center gap-1.5 truncate text-xs transition-colors"
            onClick={(e) => e.stopPropagation()}
          >
            <Github className="h-3 w-3 shrink-0" />
            <span className="truncate">{project.gitRepo.replace(/^https?:\/\/github\.com\//, '')}</span>
          </a>
        )}

        {/* Row 5: Git branch + relative time */}
        <div className="text-muted-foreground border-border/50 mt-auto flex items-center justify-between border-t pt-2 text-xs">
          {project.lastDeployment?.branch ? (
            <div className="flex min-w-0 items-center gap-1">
              <GitBranch className="h-3 w-3 shrink-0" />
              <span className="truncate">
                {project.lastDeployment.branch}
              </span>
            </div>
          ) : (
            <span className="text-muted-foreground/50">-</span>
          )}
          {project.lastDeployment?.timestamp && (
            <span className="ml-2 shrink-0">
              {formatRelativeTime(project.lastDeployment.timestamp)}
            </span>
          )}
        </div>
      </Card>
    </Link>
  );
}

export function ProjectCardCompactSkeleton() {
  return (
    <Card className="flex h-[220px] animate-pulse flex-col justify-between p-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2.5">
          <div className="bg-muted h-6 w-6 rounded" />
          <div className="bg-muted h-4 w-28 rounded" />
        </div>
        <div className="bg-muted h-2.5 w-2.5 rounded-full" />
      </div>
      {/* Service pills skeleton */}
      <div className="mt-2 flex gap-1">
        <div className="bg-muted h-5 w-16 rounded-full" />
        <div className="bg-muted h-5 w-20 rounded-full" />
        <div className="bg-muted h-5 w-14 rounded-full" />
      </div>
      <div className="bg-muted mt-2 h-3 w-3/4 rounded" />
      <div className="bg-muted mt-2 h-3 w-1/2 rounded" />
      <div className="border-border/50 mt-auto flex items-center justify-between border-t pt-2">
        <div className="bg-muted h-3 w-16 rounded" />
        <div className="bg-muted h-3 w-10 rounded" />
      </div>
    </Card>
  );
}
