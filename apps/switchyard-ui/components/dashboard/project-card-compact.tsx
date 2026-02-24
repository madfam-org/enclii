"use client";

import Link from "next/link";
import { ExternalLink, GitBranch, Github } from "lucide-react";
import { Card } from "@/components/ui/card";
import { cn } from "@/lib/utils";
import { formatRelativeTime } from "@/lib/formatting";
import { FrameworkIcon, FrameworkType } from "./framework-icon";

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
}

interface ProjectCardCompactProps {
  project: CompactProject;
  className?: string;
}

const statusDotColor: Record<string, string> = {
  success: "bg-status-success",
  failed: "bg-status-error",
  pending: "bg-status-warning",
  building: "bg-status-info animate-pulse",
};

export function ProjectCardCompact({
  project,
  className,
}: ProjectCardCompactProps) {
  const status = project.lastDeployment?.status ?? "pending";
  const dotColor = statusDotColor[status] || "bg-muted-foreground";

  return (
    <Link href={`/projects/${project.slug}`} className="block">
      <Card
        className={cn(
          "hover:border-primary/50 group relative flex min-h-[180px] flex-col justify-between p-4 transition-all duration-200 hover:shadow-lg",
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

        {/* Row 2: Commit message or description */}
        <p className="text-muted-foreground mt-2 truncate text-xs">
          {project.lastDeployment?.commitMessage ||
            project.description ||
            "No recent deployments"}
        </p>

        {/* Row 3: Domain URL */}
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

        {/* Row 3b: GitHub repo */}
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

        {/* Row 4: Git branch + relative time */}
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
    <Card className="flex h-[180px] animate-pulse flex-col justify-between p-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2.5">
          <div className="bg-muted h-6 w-6 rounded" />
          <div className="bg-muted h-4 w-28 rounded" />
        </div>
        <div className="bg-muted h-2.5 w-2.5 rounded-full" />
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
