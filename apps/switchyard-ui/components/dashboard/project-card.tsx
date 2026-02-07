"use client";

import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  MoreVertical,
  ExternalLink,
  Settings,
  Box,
  GitBranch,
  Clock,
  CheckCircle2,
  XCircle,
  Loader2,
  Circle,
  AlertCircle,
  Trash2,
} from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import Link from "next/link";
import { cn } from "@/lib/utils";
import { formatRelativeTime, formatFullTimestamp, formatDuration } from '@/lib/formatting';
import { FrameworkIcon, FrameworkType } from "./framework-icon";

interface Service {
  id: string;
  name: string;
  status: "running" | "stopped" | "deploying" | "failed" | "pending";
  url?: string;
}

interface Project {
  id: string;
  name: string;
  slug: string;
  services: Service[];
  framework?: FrameworkType | string;
  gitRepo?: string;
  lastDeployment?: {
    timestamp: string;
    status: "success" | "failed" | "pending" | "building";
    branch: string;
    commitSha?: string;
    commitMessage?: string;
    duration?: number; // in seconds
  };
  usage?: {
    computePercent: number;
    buildPercent: number;
  };
  createdAt?: string;
}

interface ProjectCardProps {
  project: Project;
  className?: string;
  onDelete?: (projectId: string) => void;
}

// Status configurations with colors and icons
const deploymentStatusConfig = {
  success: {
    icon: CheckCircle2,
    color: "text-status-success",
    bgColor: "bg-status-success-muted",
    borderColor: "border-status-success/30",
    label: "Deployed",
    badgeVariant: "default" as const,
  },
  failed: {
    icon: XCircle,
    color: "text-status-error",
    bgColor: "bg-status-error-muted",
    borderColor: "border-status-error/30",
    label: "Failed",
    badgeVariant: "destructive" as const,
  },
  pending: {
    icon: Clock,
    color: "text-status-warning",
    bgColor: "bg-status-warning-muted",
    borderColor: "border-status-warning/30",
    label: "Pending",
    badgeVariant: "secondary" as const,
  },
  building: {
    icon: Loader2,
    color: "text-status-info",
    bgColor: "bg-status-info-muted",
    borderColor: "border-status-info/30",
    label: "Building",
    badgeVariant: "secondary" as const,
  },
};

const serviceStatusConfig = {
  running: {
    color: "bg-status-success",
    label: "Running",
  },
  stopped: {
    color: "bg-muted-foreground",
    label: "Stopped",
  },
  deploying: {
    color: "bg-status-info animate-pulse",
    label: "Deploying",
  },
  failed: {
    color: "bg-status-error",
    label: "Failed",
  },
  pending: {
    color: "bg-status-warning",
    label: "Pending",
  },
};

export function ProjectCard({ project, className, onDelete }: ProjectCardProps) {
  const activeServices = project.services.filter(
    (s) => s.status === "running"
  ).length;
  const deploymentConfig = project.lastDeployment
    ? deploymentStatusConfig[project.lastDeployment.status]
    : null;
  const DeploymentIcon = deploymentConfig?.icon;

  return (
    <TooltipProvider>
      <Card
        className={cn(
          "group relative hover:shadow-lg transition-all duration-200 hover:border-primary/50",
          className
        )}
      >
        {/* Deployment Status Indicator Bar */}
        {project.lastDeployment && (
          <div
            className={cn(
              "absolute top-0 left-0 right-0 h-1 rounded-t-lg",
              deploymentConfig?.color.replace("text-", "bg-")
            )}
          />
        )}

        <CardHeader className="flex flex-row items-start justify-between pb-2 pt-4">
          <div className="flex items-start gap-3">
            {/* Framework Icon */}
            <div className="mt-0.5">
              <FrameworkIcon
                framework={project.framework || "unknown"}
                size="lg"
              />
            </div>

            <div className="space-y-1">
              <Link
                href={`/projects/${project.slug}`}
                className="font-semibold text-lg hover:text-primary transition-colors"
              >
                {project.name}
              </Link>
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                {project.gitRepo && (
                  <span className="truncate max-w-[150px]">
                    {project.gitRepo.replace(/^https?:\/\/github\.com\//, "")}
                  </span>
                )}
              </div>
            </div>
          </div>

          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                className="h-8 w-8 opacity-0 group-hover:opacity-100 transition-opacity"
              >
                <MoreVertical className="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem asChild>
                <Link href={`/projects/${project.slug}`}>
                  <Box className="h-4 w-4 mr-2" />
                  View Project
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link href={`/projects/${project.slug}/deployments`}>
                  <GitBranch className="h-4 w-4 mr-2" />
                  Deployments
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link href={`/projects/${project.slug}/settings`}>
                  <Settings className="h-4 w-4 mr-2" />
                  Settings
                </Link>
              </DropdownMenuItem>
              {onDelete && (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    className="text-destructive focus:text-destructive"
                    onClick={() => onDelete(project.id)}
                  >
                    <Trash2 className="h-4 w-4 mr-2" />
                    Delete
                  </DropdownMenuItem>
                </>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        </CardHeader>

        <CardContent className="space-y-4">
          {/* Service Status Pills */}
          <div className="flex flex-wrap gap-1.5">
            {project.services.map((service) => {
              const statusConfig = serviceStatusConfig[service.status];
              return (
                <Tooltip key={service.id}>
                  <TooltipTrigger asChild>
                    <div className="flex items-center gap-1.5 bg-muted/50 px-2 py-1 rounded-full hover:bg-muted transition-colors cursor-default">
                      <div
                        className={cn(
                          "w-2 h-2 rounded-full",
                          statusConfig.color
                        )}
                      />
                      <span className="text-xs font-medium">{service.name}</span>
                    </div>
                  </TooltipTrigger>
                  <TooltipContent>
                    <div className="text-xs">
                      <div className="font-medium">{service.name}</div>
                      <div className="text-muted-foreground">
                        {statusConfig.label}
                      </div>
                      {service.url && (
                        <div className="text-primary mt-1">{service.url}</div>
                      )}
                    </div>
                  </TooltipContent>
                </Tooltip>
              );
            })}
          </div>

          {/* Services Summary */}
          <div className="flex items-center justify-between text-sm text-muted-foreground">
            <div className="flex items-center gap-1.5">
              <Circle className="h-3 w-3 fill-current text-status-success" />
              <span>
                {activeServices}/{project.services.length} service{project.services.length !== 1 ? 's' : ''}
              </span>
            </div>
            {project.usage && (
              <div className="flex items-center gap-3 text-xs">
                <span className="text-muted-foreground">
                  CPU: {project.usage.computePercent}%
                </span>
              </div>
            )}
          </div>

          {/* Last Deployment Section */}
          {project.lastDeployment && (
            <div
              className={cn(
                "flex items-center justify-between p-2.5 rounded-lg border",
                deploymentConfig?.bgColor,
                deploymentConfig?.borderColor
              )}
            >
              <div className="flex items-center gap-2">
                {DeploymentIcon && (
                  <DeploymentIcon
                    className={cn(
                      "h-4 w-4",
                      deploymentConfig?.color,
                      project.lastDeployment.status === "building" && "animate-spin"
                    )}
                  />
                )}
                <div className="flex flex-col">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium">
                      {deploymentConfig?.label}
                    </span>
                    <Badge variant="outline" className="text-xs px-1.5 py-0">
                      <GitBranch className="h-3 w-3 mr-1" />
                      {project.lastDeployment.branch}
                    </Badge>
                  </div>
                  {project.lastDeployment.commitMessage && (
                    <span className="text-xs text-muted-foreground truncate max-w-[200px]">
                      {project.lastDeployment.commitMessage}
                    </span>
                  )}
                </div>
              </div>

              <Tooltip>
                <TooltipTrigger asChild>
                  <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                    <Clock className="h-3 w-3" />
                    <span>{formatRelativeTime(project.lastDeployment.timestamp)}</span>
                  </div>
                </TooltipTrigger>
                <TooltipContent>
                  <div className="text-xs space-y-1">
                    <div>{formatFullTimestamp(project.lastDeployment.timestamp)}</div>
                    {project.lastDeployment.duration && (
                      <div className="text-muted-foreground">
                        Build time: {formatDuration(project.lastDeployment.duration)}
                      </div>
                    )}
                    {project.lastDeployment.commitSha && (
                      <div className="font-mono text-muted-foreground">
                        {project.lastDeployment.commitSha.slice(0, 7)}
                      </div>
                    )}
                  </div>
                </TooltipContent>
              </Tooltip>
            </div>
          )}

          {/* Quick Actions */}
          <div className="flex gap-2 pt-1">
            <Button
              variant="secondary"
              size="sm"
              className="flex-1"
              asChild
            >
              <Link href={`/projects/${project.slug}`}>
                View Details
              </Link>
            </Button>
            {project.services.some((s) => s.url) && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button variant="outline" size="sm" asChild>
                    <a
                      href={project.services.find((s) => s.url)?.url}
                      target="_blank"
                      rel="noopener noreferrer"
                    >
                      <ExternalLink className="h-4 w-4" />
                    </a>
                  </Button>
                </TooltipTrigger>
                <TooltipContent>
                  <span>Open in new tab</span>
                </TooltipContent>
              </Tooltip>
            )}
          </div>
        </CardContent>
      </Card>
    </TooltipProvider>
  );
}

// Export a skeleton version for loading states
export function ProjectCardSkeleton() {
  return (
    <Card className="animate-pulse">
      <CardHeader className="flex flex-row items-start justify-between pb-2 pt-4">
        <div className="flex items-start gap-3">
          <div className="w-8 h-8 rounded bg-muted" />
          <div className="space-y-2">
            <div className="h-5 w-32 rounded bg-muted" />
            <div className="h-4 w-24 rounded bg-muted" />
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex gap-2">
          <div className="h-6 w-20 rounded-full bg-muted" />
          <div className="h-6 w-24 rounded-full bg-muted" />
        </div>
        <div className="h-4 w-32 rounded bg-muted" />
        <div className="h-16 rounded-lg bg-muted" />
        <div className="flex gap-2">
          <div className="h-9 flex-1 rounded bg-muted" />
          <div className="h-9 w-9 rounded bg-muted" />
        </div>
      </CardContent>
    </Card>
  );
}
