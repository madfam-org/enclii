"use client";

import { useState, useCallback } from "react";
import Link from "next/link";
import { ExternalLink, GitBranch, GitMerge } from "lucide-react";
import { Card } from "@/components/ui/card";
import { cn } from "@/lib/utils";
import { formatRelativeTime } from "@/lib/formatting";
import {
  FrameworkIcon,
  FrameworkType,
  getFrameworkLabel,
} from "./framework-icon";
import { HealthBadge } from "./health-badge";
import { SentryErrorBadge } from "./sentry-error-badge";
import { ProjectCardMenu } from "./project-card-menu";
import { ProjectCardEvidenceChips } from "./project-card-evidence-chips";
import { ProjectCardRepoMetadata } from "./project-card-repo-metadata";
import {
  ProjectProcessDrawer,
  ProjectProcessRail,
} from "./project-process-feed";
import {
  ProjectCardServicesTable,
  ServiceStatusSummary,
} from "./project-card-services-table";
import {
  type ProjectLiveState,
  type ProjectProcess,
  type ProjectProcessSummary,
  type ServiceProcessSummary,
} from "@/lib/project-process-feed";

// Strip protocol + .git suffix from a git repo URL/path so the result is
// always a "{owner}/{repo}" slug suitable for both display and constructing
// downstream GitHub URLs (branch/tree/commit views).
function repoSlugFromGitRepo(gitRepo: string | undefined | null): string {
  if (!gitRepo) return "";
  return gitRepo
    .replace(/^https?:\/\/github\.com\//, "")
    .replace(/\.git$/, "");
}

// Inline-button copy helper used by the digest chip + kebab menu items.
// Falls back silently when the page isn't served over HTTPS / clipboard
// API isn't available — the visible state machine just stays on "idle".
function useCopyToClipboard() {
  const [copiedKey, setCopiedKey] = useState<string | null>(null);
  const copy = useCallback((key: string, value: string) => {
    if (typeof navigator === "undefined" || !navigator.clipboard) return;
    navigator.clipboard.writeText(value).then(
      () => {
        setCopiedKey(key);
        // Reset back to idle after 1.5s so the user gets visual confirmation
        // without the chip permanently showing the "copied" state.
        setTimeout(() => setCopiedKey((k) => (k === key ? null : k)), 1500);
      },
      () => {
        // Clipboard rejected (permission / insecure context). Silent fail —
        // the "Copy" affordance still exists, the operator just won't see
        // the green check. Avoid throwing so it doesn't surface a console
        // error on every dashboard load when /projects is loaded over IP.
      },
    );
  }, []);
  return { copiedKey, copy };
}

export interface CompactService {
  id: string;
  name: string;
  status: "running" | "pending" | "failed" | "deploying" | "unknown";
  health: "healthy" | "unhealthy" | "unknown" | "stale";
  version?: string;
  replicas?: string;
  environment?: string;
  rolloutState?: "ok" | "progressing" | "blocked";
  rolloutBlockedReason?: string;
  healthObservedAt?: string;
  healthStale?: boolean;
  // Image URI of the currently-running release. Digest-pinned in production by
  // the Kyverno require-image-digest policy. Used to render the truncated
  // digest chip so operators can confirm what's running before triggering a
  // rollback. Source: Service.current_image_uri (parity audit gap #5).
  currentImageUri?: string;
  // Per-service public URL (bare host, no protocol) -- drives the new
  // ServiceLink deep-link sub-row. Source: ApiService.domain. Previously
  // the home/projects pages collapsed this into a single project-level
  // `domain` by picking the FIRST service that had one, which was lossy
  // for projects with both `api.example.com` and `example.com`. Operators
  // now click straight through to the service of their choice.
  domain?: string;
  processSummary?: ServiceProcessSummary;
  activeProcessCount?: number;
  lastProcess?: ProjectProcess;
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

// At-a-glance repo metadata surfaced on the card. Populated by a single batch
// call to /v1/integrations/github/repos/metadata after the project list loads.
// `visibility === undefined` means "not fetched yet" (loading). `visibility ===
// "unknown"` means "fetched but inaccessible" (404/403 from GitHub) — render
// a neutral indicator and tooltip rather than guessing public/private.
export interface CompactRepoMeta {
  visibility?: "public" | "private" | "internal" | "unknown";
  language?: string;
  license?: string;
  stars?: number;
  forks?: number;
  archived?: boolean;
  fork?: boolean;
  isTemplate?: boolean;
  defaultBranch?: string;
  pushedAt?: string;
  description?: string;
}

export interface CompactProject {
  id: string;
  name: string;
  slug: string;
  description?: string;
  framework?: FrameworkType | string;
  gitRepo?: string;
  repoMeta?: CompactRepoMeta;
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
  processSummary?: ProjectProcessSummary;
  liveState?: ProjectLiveState;
  evidence?: ProjectCardEvidence;
  updatedAt?: string;
  // Tri-state outcome of resolving the latest deployment from the
  // services fetch. Drives the empty-state copy on Row 3 so we don't
  // claim "no deployments" when the upstream `/v1/projects/:slug/services`
  // call rejected. See lib/project-deploy.ts (audit finding PR-1).
  deployResolution?: "deployed" | "no-deploys" | "unknown";
}

export interface ProjectCardEvidence {
  serviceRows: {
    status: string;
    count: number;
    healthyCount: number;
    staleCount: number;
    lastObservedAt?: string;
    staleAfterSeconds: number;
  };
  argoApplication?: {
    name: string;
    syncStatus: string;
    healthStatus: string;
    revision?: string;
    destinationNamespace?: string;
    observedAt: string;
  };
  jobs?: {
    status: string;
    namespaceCount: number;
    cronJobCount: number;
    failedCount: number;
    activeCount: number;
    stuckCount: number;
    pendingCount?: number;
    succeededCount: number;
    lastObservedAt: string;
    items?: {
      namespace: string;
      name: string;
      status: string;
      latestJobName?: string;
      recentFailedJobs?: number;
      activeJobs?: number;
      stuckJobs?: number;
      succeededJobs?: number;
      lastScheduleTime?: string;
      lastFailureTime?: string;
    }[];
  };
}

interface ProjectCardCompactProps {
  project: CompactProject;
  className?: string;
}

const aggregateStatusColor: Record<string, string> = {
  healthy: "bg-status-success",
  degraded: "bg-status-warning",
  failing: "bg-status-error",
  unknown: "bg-muted-foreground",
};

export function ProjectCardCompact({
  project,
  className,
}: ProjectCardCompactProps) {
  const services = project.services || [];
  const aggregateStatus = project.aggregateStatus || "unknown";
  const dotColor = aggregateStatusColor[aggregateStatus] || "bg-muted-foreground";

  // Truthy when at least one visible service exposes its own deep-link.
  // Drives whether we de-emphasize the project-level domain link below
  // (the brief asked us to keep it as a fallback entry-point but lower
  // its visual weight when per-service links carry the same destination
  // information with strictly more env context).
  const hasAnyServiceDomain = services.some((s) => !!s.domain);

  const repoSlug = repoSlugFromGitRepo(project.gitRepo);
  const githubRepoUrl = repoSlug ? `https://github.com/${repoSlug}` : "";
  const branchUrl =
    repoSlug && project.lastDeployment?.branch
      ? `https://github.com/${repoSlug}/tree/${encodeURIComponent(
          project.lastDeployment.branch,
        )}`
      : "";

  // First service drives the "Logs" quick-action target inside the menu;
  // the menu itself computes the href.
  const firstServiceId = services[0]?.id;

  const { copiedKey, copy } = useCopyToClipboard();
  const [feedOpen, setFeedOpen] = useState(false);

  return (
    <Card
      className={cn(
        "hover:border-primary/50 group/card focus-within:ring-primary/40 relative flex min-h-[240px] flex-col justify-between p-4 transition-all duration-200 focus-within:ring-2 hover:shadow-lg",
        className,
      )}
      data-testid="project-card"
    >
      <div className="flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2.5">
          <FrameworkIcon
            framework={project.framework || "unknown"}
            size="md"
          />
          <Link
            href={`/projects/${project.slug}`}
            className="hover:text-primary min-w-0 truncate text-sm font-semibold transition-colors before:absolute before:inset-0 before:z-0 before:content-[''] focus-visible:outline-none"
            aria-label={`Open project ${project.name}`}
          >
            {project.name}
          </Link>
          {project.framework && project.framework !== "unknown" && (
            <span
              className="border-border/60 bg-muted/40 text-muted-foreground hidden shrink-0 rounded-full border px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide sm:inline-block"
              aria-label={`Framework: ${getFrameworkLabel(project.framework)}`}
            >
              {getFrameworkLabel(project.framework)}
            </span>
          )}
        </div>
        <div className="relative z-10 flex shrink-0 items-center gap-1.5">
          {project.serviceCount !== undefined && (
            <Link
              href={`/projects/${project.slug}#health`}
              className={cn(
                "hover:bg-muted rounded px-1 py-0.5 text-xs tabular-nums transition-colors",
                aggregateStatus === "failing" && "text-status-error",
                aggregateStatus === "degraded" && "text-status-warning",
                aggregateStatus === "healthy" && "text-muted-foreground",
                aggregateStatus === "unknown" && "text-muted-foreground",
              )}
              aria-label={`${project.healthyCount ?? 0} of ${project.serviceCount} services healthy — view health`}
              title={`${project.healthyCount ?? 0}/${project.serviceCount} healthy — click for details`}
            >
              {project.healthyCount ?? 0}/{project.serviceCount}
            </Link>
          )}
          <div
            className={cn("h-2.5 w-2.5 rounded-full", dotColor)}
            aria-label={`Aggregate status: ${aggregateStatus}`}
            title={`Status: ${aggregateStatus}`}
          />
          <ProjectCardMenu
            projectId={project.id}
            projectName={project.name}
            projectSlug={project.slug}
            firstServiceId={firstServiceId}
            githubRepoUrl={githubRepoUrl || undefined}
            copiedKey={copiedKey}
            onCopy={copy}
          />
        </div>
      </div>

      <ProjectProcessRail
        project={project}
        onOpenDetails={() => setFeedOpen(true)}
      />
      <ProjectProcessDrawer
        project={project}
        open={feedOpen}
        onOpenChange={setFeedOpen}
      />

        <ProjectCardEvidenceChips
          evidence={project.evidence}
          services={services}
        />

        <ProjectCardServicesTable
          copiedKey={copiedKey}
          onCopy={copy}
          projectName={project.name}
          projectSlug={project.slug}
          services={services}
          shortImageRef={shortImageRef}
        />

        <p
          className="text-muted-foreground mt-2 truncate text-xs"
          title={project.lastDeployment?.commitMessage || project.description}
        >
          {project.lastDeployment?.commitMessage ||
            project.description ||
            (services.length > 0 ? (
              <ServiceStatusSummary services={services} />
            ) : project.deployResolution === "unknown" ? (
              "—"
            ) : project.deployResolution === "no-deploys" ? (
              "No deployments yet"
            ) : (
              "No deployments yet"
            ))}
        </p>

        {project.domain ? (
          <a
            href={`https://${project.domain}`}
            target="_blank"
            rel="noopener noreferrer"
            className={cn(
              "relative z-10 mt-2 flex w-fit items-center gap-1.5 truncate text-xs transition-colors",
              hasAnyServiceDomain
                ? "text-muted-foreground/70 hover:text-muted-foreground"
                : "text-muted-foreground hover:text-foreground",
            )}
            aria-label={`Open ${project.domain} in a new tab`}
          >
            <ExternalLink className="h-3 w-3 shrink-0" />
            <span className="truncate">{project.domain}</span>
          </a>
        ) : (
          <div className="mt-2 h-4" />
        )}

        <ProjectCardRepoMetadata project={project} repoSlug={repoSlug} />
        {services[0]?.id && (
          <div className="relative z-10 mt-1.5 flex w-fit items-center gap-1.5">
            <HealthBadge
              serviceId={services[0].id}
              serviceName={services[0].name}
            />
            <SentryErrorBadge
              serviceId={services[0].id}
              serviceName={services[0].name}
            />
          </div>
        )}

        <div className="text-muted-foreground border-border/50 relative z-10 mt-auto flex items-center justify-between gap-2 border-t pt-2 text-xs">
          {project.lastDeployment?.branch ? (
            branchUrl ? (
              <a
                href={branchUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="hover:text-foreground flex min-w-0 items-center gap-1 hover:underline"
                aria-label={`Open ${project.lastDeployment.branch} branch on GitHub`}
              >
                <GitBranch className="h-3 w-3 shrink-0" />
                <span className="truncate">{project.lastDeployment.branch}</span>
              </a>
            ) : (
              <div
                className="flex min-w-0 items-center gap-1"
                title={`Branch: ${project.lastDeployment.branch}`}
              >
                <GitBranch className="h-3 w-3 shrink-0" />
                <span className="truncate">{project.lastDeployment.branch}</span>
              </div>
            )
          ) : (
            <span className="text-muted-foreground/50">-</span>
          )}
          <div className="flex shrink-0 items-center gap-2">
            <Link
              href={`/projects/${project.slug}/deployments`}
              className="hover:text-foreground inline-flex items-center gap-1"
              aria-label={`View deployments for ${project.name}`}
            >
              <GitMerge className="h-3 w-3 shrink-0" />
              <span>Deployments</span>
            </Link>
            {project.lastDeployment?.timestamp && (
              <Link
                href={`/projects/${project.slug}/deployments`}
                className="hover:text-foreground hover:underline"
                aria-label={`Last deployment ${formatRelativeTime(project.lastDeployment.timestamp)} — view deployments`}
                title={new Date(project.lastDeployment.timestamp).toLocaleString()}
              >
                {formatRelativeTime(project.lastDeployment.timestamp)}
              </Link>
            )}
          </div>
        </div>
      </Card>
  );
}

// ProjectRowCompact (list-view variant) lives in ./project-row-compact.tsx.
// Re-exported here so existing call sites that import from this module
// continue to work unchanged.
export { ProjectRowCompact } from "./project-row-compact";
export { ProjectCardCompactSkeleton } from "./project-card-compact-skeleton";
