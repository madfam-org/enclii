"use client";

import Link from "next/link";
import {
  Archive,
  Copy,
  ExternalLink,
  GitBranch,
  GitFork,
  GitMerge,
  Github,
  Globe,
  Lock,
  Star,
} from "lucide-react";
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

// Hex colors for the language dot. Source: github-linguist's languages.yml
// (subset — ecosystem-relevant only). Falling back to a neutral grey when
// the language isn't one we've seen, which is fine: the dot is decorative.
const languageColor: Record<string, string> = {
  TypeScript: "#3178c6",
  JavaScript: "#f1e05a",
  Go: "#00ADD8",
  Python: "#3572A5",
  Rust: "#dea584",
  Dart: "#00B4AB",
  Ruby: "#701516",
  Java: "#b07219",
  Kotlin: "#A97BFF",
  Swift: "#F05138",
  Shell: "#89e051",
  HCL: "#844FBA",
  HTML: "#e34c26",
  CSS: "#563d7c",
  Vue: "#41b883",
  Svelte: "#ff3e00",
  Solidity: "#AA6746",
  C: "#555555",
  "C++": "#f34b7d",
  "C#": "#178600",
  PHP: "#4F5D95",
};

// Compact human-readable star/fork counts (1.2k, 3.4k, 12k).
function formatCount(n: number | undefined): string {
  if (n === undefined || n === null) return "";
  if (n < 1000) return String(n);
  if (n < 10000) return (n / 1000).toFixed(1).replace(/\.0$/, "") + "k";
  return Math.round(n / 1000) + "k";
}

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

        {/* Row 4b: GitHub repo + visibility indicator + repo flags.
            The visibility icon (lock/globe) sits inline with the repo path so
            operators can tell at-a-glance whether the underlying source is
            public or private — the most common reason to read this card is
            to confirm "is this safe to share / open externally?". When repo
            metadata hasn't loaded yet, we render no icon (avoids flash of
            wrong indicator); when GitHub returned 404/403, we render a
            neutral question-mark variant via the "unknown" branch. */}
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
            {project.repoMeta?.visibility === "private" && (
              <Lock
                className="h-3 w-3 shrink-0 text-status-warning"
                aria-label="Private repository"
              />
            )}
            {project.repoMeta?.visibility === "public" && (
              <Globe
                className="h-3 w-3 shrink-0 text-status-success"
                aria-label="Public repository"
              />
            )}
            {project.repoMeta?.visibility === "internal" && (
              <Lock
                className="h-3 w-3 shrink-0 text-status-info"
                aria-label="Internal repository"
              />
            )}
            {project.repoMeta?.archived && (
              <Archive
                className="h-3 w-3 shrink-0 text-muted-foreground"
                aria-label="Archived repository"
              />
            )}
            {project.repoMeta?.fork && (
              <GitFork
                className="h-3 w-3 shrink-0 text-muted-foreground"
                aria-label="Forked repository"
              />
            )}
            {project.repoMeta?.isTemplate && (
              <Copy
                className="h-3 w-3 shrink-0 text-muted-foreground"
                aria-label="Template repository"
              />
            )}
          </a>
        )}

        {/* Row 4b-stats: small chip row with language dot + stars + license.
            Only renders when at least one stat is populated, so unconfigured
            (no GH token) deployments show no extra row instead of a blank
            band. The card tolerates partial metadata: each chip is
            individually conditional. */}
        {project.repoMeta &&
          (project.repoMeta.language ||
            (project.repoMeta.stars && project.repoMeta.stars > 0) ||
            project.repoMeta.license) && (
            <div className="mt-1 flex items-center gap-2 text-[10px] text-muted-foreground">
              {project.repoMeta.language && (
                <span
                  className="flex items-center gap-1"
                  title={`Primary language: ${project.repoMeta.language}`}
                >
                  <span
                    className="h-2 w-2 rounded-full shrink-0"
                    style={{
                      backgroundColor:
                        languageColor[project.repoMeta.language] || "#888",
                    }}
                    aria-hidden="true"
                  />
                  {project.repoMeta.language}
                </span>
              )}
              {project.repoMeta.stars !== undefined &&
                project.repoMeta.stars > 0 && (
                  <span
                    className="flex items-center gap-0.5"
                    title={`${project.repoMeta.stars} stars`}
                  >
                    <Star className="h-2.5 w-2.5 shrink-0" />
                    {formatCount(project.repoMeta.stars)}
                  </span>
                )}
              {project.repoMeta.license && (
                <span
                  className="rounded border border-border/40 bg-muted/30 px-1 py-px font-mono leading-none"
                  title={`License: ${project.repoMeta.license}`}
                >
                  {project.repoMeta.license}
                </span>
              )}
            </div>
          )}

        {/* Row 4c: Health + Sentry badges for the lead service.
            SentryErrorBadge renders nothing when the operator hasn't
            provisioned SENTRY_AUTH_TOKEN (parity audit gap #9), so this
            row degrades gracefully on unconfigured deployments. */}
        {services[0]?.id && (
          <div className="mt-1.5 flex items-center gap-1.5">
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

// List-row variant of ProjectCardCompact for the dashboard's "list" view mode.
// Renders the same project as a single horizontal row optimized for scanning
// many projects at once: framework icon + name + status dot + replicas count
// + visibility chip + branch + relative timestamp. Skips the per-service
// table — operators who need that detail click into /projects/{slug}. The
// whole row is a Link so keyboard nav and middle-click "open in new tab"
// work exactly as on the card variant.
export function ProjectRowCompact({
  project,
  className,
}: ProjectCardCompactProps) {
  const aggregateStatus = project.aggregateStatus || "unknown";
  const dotColor = aggregateStatusColor[aggregateStatus] || "bg-muted-foreground";
  const branch = project.lastDeployment?.branch;
  const commitMessage = project.lastDeployment?.commitMessage;
  const timestamp = project.lastDeployment?.timestamp;

  return (
    <Link
      href={`/projects/${project.slug}`}
      className={cn(
        "group relative flex items-center gap-3 px-3 py-2.5 transition-colors hover:bg-muted/40 focus-visible:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 focus-visible:ring-inset sm:gap-4 sm:px-4",
        className,
      )}
      role="listitem"
    >
      {/* Framework icon */}
      <FrameworkIcon
        framework={project.framework || "unknown"}
        size="sm"
      />

      {/* Project name + framework chip */}
      <div className="flex min-w-0 flex-1 items-center gap-2">
        <span className="truncate text-sm font-semibold">{project.name}</span>
        {project.framework && project.framework !== "unknown" && (
          <span
            className="hidden shrink-0 rounded-full border border-border/60 bg-muted/40 px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground md:inline-block"
            aria-label={`Framework: ${getFrameworkLabel(project.framework)}`}
          >
            {getFrameworkLabel(project.framework)}
          </span>
        )}
      </div>

      {/* Replicas summary — hidden on narrow viewports to preserve space */}
      {project.serviceCount !== undefined && (
        <span
          className="hidden shrink-0 text-xs text-muted-foreground tabular-nums sm:inline-block"
          aria-label={`${project.healthyCount ?? 0} of ${project.serviceCount} services healthy`}
        >
          {project.healthyCount ?? 0}/{project.serviceCount}
        </span>
      )}

      {/* Visibility chip — lock/globe so operators can scan public-vs-private */}
      {project.repoMeta?.visibility === "private" && (
        <Lock
          className="hidden h-3.5 w-3.5 shrink-0 text-status-warning sm:inline-block"
          aria-label="Private repository"
        />
      )}
      {project.repoMeta?.visibility === "public" && (
        <Globe
          className="hidden h-3.5 w-3.5 shrink-0 text-status-success sm:inline-block"
          aria-label="Public repository"
        />
      )}
      {project.repoMeta?.visibility === "internal" && (
        <Lock
          className="hidden h-3.5 w-3.5 shrink-0 text-status-info sm:inline-block"
          aria-label="Internal repository"
        />
      )}

      {/* Domain — only on wide viewports */}
      {project.domain && (
        <span className="hidden min-w-0 max-w-[180px] shrink-0 truncate text-xs text-muted-foreground lg:inline-block">
          {project.domain}
        </span>
      )}

      {/* Branch + commit message — flex column, only on wide viewports */}
      {(branch || commitMessage) && (
        <div className="hidden min-w-0 max-w-[280px] shrink-0 items-center gap-1.5 text-xs text-muted-foreground xl:flex">
          {branch && (
            <>
              <GitBranch className="h-3 w-3 shrink-0" aria-hidden="true" />
              <span className="truncate">{branch}</span>
            </>
          )}
          {commitMessage && (
            <span
              className="truncate text-muted-foreground/70"
              title={commitMessage}
            >
              · {commitMessage}
            </span>
          )}
        </div>
      )}

      {/* Relative timestamp */}
      {timestamp && (
        <span className="hidden shrink-0 text-xs text-muted-foreground tabular-nums sm:inline-block">
          {formatRelativeTime(timestamp)}
        </span>
      )}

      {/* Status dot — always visible, rightmost anchor */}
      <div className="flex shrink-0 items-center gap-1.5">
        <div
          className={cn("h-2.5 w-2.5 rounded-full", dotColor)}
          aria-label={`Status: ${aggregateStatus}`}
        />
      </div>
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
