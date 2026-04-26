"use client";

import Link from "next/link";
import { ExternalLink, GitBranch, GitMerge, Github } from "lucide-react";
import { Card } from "@/components/ui/card";
import { cn } from "@/lib/utils";
import { formatRelativeTime } from "@/lib/formatting";
import {
  FrameworkIcon,
  FrameworkType,
  getFrameworkLabel,
} from "./framework-icon";
import { HealthBadge } from "./health-badge";

export interface CompactService {
  id: string;
  name: string;
  status: "running" | "pending" | "failed" | "deploying" | "unknown";
  health: "healthy" | "unhealthy" | "unknown";
  version?: string;
  replicas?: string;
  environment?: string;
  // Image URI of the currently-running release. Digest-pinned in production by
  // the Kyverno require-image-digest policy. Used to render the truncated
  // digest chip so operators can confirm what's running before triggering a
  // rollback. Source: Service.current_image_uri (parity audit gap #5).
  currentImageUri?: string;
}

// Extracts the trailing digest fragment from a full image URI for display.
// Inputs and outputs:
//   "ghcr.io/madfam-org/svc@sha256:abc123def4567890" -> "sha256:abc123def4567"
//   "ghcr.io/madfam-org/svc:v1.2.3"                  -> "v1.2.3"
//   ""                                               -> ""
// Tests cover both styles + the empty case.
export function shortImageRef(uri: string | undefined | null): string {
  if (!uri) return "";
  const atIdx = uri.lastIndexOf("@");
  if (atIdx >= 0) {
    const digest = uri.slice(atIdx + 1);
    // sha256:HEX — show the algo + first 12 chars of the hash
    const colonIdx = digest.indexOf(":");
    if (colonIdx >= 0) {
      return `${digest.slice(0, colonIdx + 1)}${digest.slice(colonIdx + 1, colonIdx + 13)}`;
    }
    return digest.slice(0, 19);
  }
  const colonIdx = uri.lastIndexOf(":");
  if (colonIdx >= 0 && colonIdx > uri.lastIndexOf("/")) {
    return uri.slice(colonIdx + 1);
  }
  return "";
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

const MAX_VISIBLE_TABLE_ROWS = 5;

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

  const hasOverflow = services.length > MAX_VISIBLE_TABLE_ROWS;
  const visibleServices = hasOverflow
    ? services.slice(0, MAX_VISIBLE_TABLE_ROWS)
    : services;
  const overflowCount = services.length - MAX_VISIBLE_TABLE_ROWS;

  return (
    <Link href={`/projects/${project.slug}`} className="block">
      <Card
        className={cn(
          "hover:border-primary/50 group relative flex min-h-[240px] flex-col justify-between p-4 transition-all duration-200 hover:shadow-lg",
          className,
        )}
      >
        {/* Row 1: Framework icon + name + framework chip + status dot */}
        <div className="flex items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-2.5">
            <FrameworkIcon
              framework={project.framework || "unknown"}
              size="md"
            />
            <span className="truncate text-sm font-semibold">
              {project.name}
            </span>
            {project.framework && project.framework !== "unknown" && (
              <span
                className="hidden shrink-0 rounded-full border border-border/60 bg-muted/40 px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground sm:inline-block"
                aria-label={`Framework: ${getFrameworkLabel(project.framework)}`}
              >
                {getFrameworkLabel(project.framework)}
              </span>
            )}
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

        {/* Row 2: Service table */}
        {services.length > 0 && (
          <div className="mt-2 overflow-hidden rounded border border-border/40">
            <table className="w-full text-[11px]">
              <thead>
                <tr className="bg-muted/30 text-muted-foreground">
                  <th className="py-1 pl-2 pr-1 text-left font-medium">Service</th>
                  <th className="px-1 py-1 text-left font-medium">Status</th>
                  <th className="px-1 py-1 text-right font-medium">Replicas</th>
                  <th className="py-1 pl-1 pr-2 text-right font-medium">Env</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border/20">
                {visibleServices.map((service) => (
                  <tr
                    key={service.id}
                    className="hover:bg-muted/20 transition-colors"
                  >
                    <td className="py-1 pl-2 pr-1">
                      <div className="flex items-center gap-1.5 min-w-0">
                        <div
                          className={cn(
                            "h-1.5 w-1.5 shrink-0 rounded-full",
                            serviceStatusColor[service.status] ||
                              "bg-muted-foreground",
                          )}
                        />
                        <span className="truncate font-medium max-w-[100px]">
                          {service.name}
                        </span>
                        {service.currentImageUri && (
                          <span
                            className="hidden shrink-0 rounded border border-border/40 bg-muted/30 px-1 py-0.5 font-mono text-[9px] leading-none text-muted-foreground md:inline-block"
                            title={service.currentImageUri}
                            aria-label={`Running image: ${service.currentImageUri}`}
                          >
                            {shortImageRef(service.currentImageUri)}
                          </span>
                        )}
                      </div>
                    </td>
                    <td className="px-1 py-1">
                      <span
                        className={cn(
                          "inline-block rounded px-1 py-0.5 text-[10px] font-medium leading-none",
                          service.status === "running" &&
                            "bg-status-success/15 text-status-success",
                          service.status === "failed" &&
                            "bg-status-error/15 text-status-error",
                          service.status === "pending" &&
                            "bg-status-warning/15 text-status-warning",
                          service.status === "deploying" &&
                            "bg-status-info/15 text-status-info animate-pulse",
                          service.status === "unknown" &&
                            "bg-muted text-muted-foreground",
                        )}
                      >
                        {serviceStatusLabel[service.status] || "Unknown"}
                      </span>
                    </td>
                    <td className="px-1 py-1 text-right text-muted-foreground tabular-nums">
                      {service.replicas || "\u2014"}
                    </td>
                    <td className="py-1 pl-1 pr-2 text-right text-muted-foreground truncate max-w-[60px]">
                      {service.environment || "\u2014"}
                    </td>
                  </tr>
                ))}
                {hasOverflow && (
                  <tr>
                    <td
                      colSpan={4}
                      className="py-1 text-center text-[10px] text-muted-foreground"
                    >
                      +{overflowCount} more service{overflowCount > 1 ? "s" : ""}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
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

        {/* Row 4c: Health badge for the lead service. */}
        {services[0]?.id && (
          <div className="mt-1.5 flex items-center">
            <HealthBadge
              serviceId={services[0].id}
              serviceName={services[0].name}
            />
          </div>
        )}

        {/* Row 5: Git branch + relative time + view deployments */}
        <div className="text-muted-foreground border-border/50 mt-auto flex items-center justify-between gap-2 border-t pt-2 text-xs">
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
          <div className="flex shrink-0 items-center gap-2">
            {/*
              Plain anchor (not <Link>) — the parent card is already a
              <Link>, so we follow the same nesting pattern used by the
              GitHub/domain links on this card. stopPropagation prevents
              the parent Link from intercepting the click.
            */}
            <a
              href={`/projects/${project.slug}/deployments`}
              className="inline-flex items-center gap-1 hover:text-foreground"
              onClick={(e) => e.stopPropagation()}
              aria-label={`View deployments for ${project.name}`}
            >
              <GitMerge className="h-3 w-3 shrink-0" />
              <span>Deployments</span>
            </a>
            {project.lastDeployment?.timestamp && (
              <span>
                {formatRelativeTime(project.lastDeployment.timestamp)}
              </span>
            )}
          </div>
        </div>
      </Card>
    </Link>
  );
}

export function ProjectCardCompactSkeleton() {
  return (
    <Card className="flex h-[240px] animate-pulse flex-col justify-between p-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2.5">
          <div className="bg-muted h-6 w-6 rounded" />
          <div className="bg-muted h-4 w-28 rounded" />
        </div>
        <div className="bg-muted h-2.5 w-2.5 rounded-full" />
      </div>
      {/* Service table skeleton */}
      <div className="mt-2 space-y-1 rounded border border-border/40 p-2">
        <div className="bg-muted h-3 w-full rounded" />
        <div className="bg-muted h-3 w-5/6 rounded" />
        <div className="bg-muted h-3 w-4/6 rounded" />
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
