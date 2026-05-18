"use client";

import { Fragment, useState, useCallback } from "react";
import Link from "next/link";
import {
  Archive,
  Check,
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
import { ServiceLink, normalizeEnv } from "./service-link";
import { ProjectCardMenu } from "./project-card-menu";
import { RolloutStateIndicator } from "./rollout-state-indicator";
import {
  ProjectProcessDrawer,
  ProjectProcessRail,
  ServiceProcessIndicator,
} from "./project-process-feed";
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
  health: "healthy" | "unhealthy" | "unknown";
  version?: string;
  replicas?: string;
  environment?: string;
  rolloutState?: "ok" | "progressing" | "blocked";
  rolloutBlockedReason?: string;
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
  updatedAt?: string;
  // Tri-state outcome of resolving the latest deployment from the
  // services fetch. Drives the empty-state copy on Row 3 so we don't
  // claim "no deployments" when the upstream `/v1/projects/:slug/services`
  // call rejected. See lib/project-deploy.ts (audit finding PR-1).
  deployResolution?: "deployed" | "no-deploys" | "unknown";
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

  // Truthy when at least one visible service exposes its own deep-link.
  // Drives whether we de-emphasize the project-level domain link below
  // (the brief asked us to keep it as a fallback entry-point but lower
  // its visual weight when per-service links carry the same destination
  // information with strictly more env context).
  const hasAnyServiceDomain = visibleServices.some((s) => !!s.domain);

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
    // The card itself is a div, not a <Link>. The "click anywhere on the
    // card to open the project" affordance is delivered by the project
    // name's surface-overlay link (see Row 1 below): its ::before
    // pseudo-element with `inset-0` covers the entire card. All inner
    // interactive elements use `relative z-10` so they sit ABOVE the
    // overlay and capture their own clicks first — that's how this card
    // gets ~12 distinct destinations without a single nested-anchor.
    // `group/card` lets inner elements respond to hover on the card surface
    // (e.g. dim the project-level domain when hovering elsewhere).
    <Card
      className={cn(
        "hover:border-primary/50 group/card focus-within:ring-primary/40 relative flex min-h-[240px] flex-col justify-between p-4 transition-all duration-200 focus-within:ring-2 hover:shadow-lg",
        className,
      )}
      data-testid="project-card"
    >
      {/* Row 1: Framework icon + name + framework chip + status + kebab.
          The project name carries the surface-overlay link (`before:absolute
          before:inset-0`) so clicking any non-interactive area of the card
          navigates to the project. Other inner clickables override with
          `relative z-10`. */}
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

        {/* Row 2: Service table.
            Each row is now a granular interaction surface:
            - Service name \u2192 service detail page (/services/:id)
            - Status badge \u2192 service logs (/services/:id/logs)
            - Image digest chip \u2192 copy to clipboard (button, not link)
            - Replicas / env \u2192 service detail page
            All inner clickables sit at z-10 to escape the surface overlay. */}
        {services.length > 0 && (
          <div className="border-border/40 relative z-10 mt-2 overflow-hidden rounded border">
            <table className="w-full text-[11px]">
              <thead>
                <tr className="bg-muted/30 text-muted-foreground">
                  <th className="py-1 pl-2 pr-1 text-left font-medium">Service</th>
                  <th className="px-1 py-1 text-left font-medium">Status</th>
                  <th className="px-1 py-1 text-right font-medium">Replicas</th>
                  <th className="py-1 pl-1 pr-2 text-right font-medium">Env</th>
                </tr>
              </thead>
              <tbody className="divide-border/20 divide-y">
                {visibleServices.map((service) => {
                  const serviceHref = `/projects/${project.slug}/services/${service.id}`;
                  const logsHrefForRow = `${serviceHref}/logs`;
                  const digestKey = `digest-${service.id}`;
                  const digestCopied = copiedKey === digestKey;
                  return (
                    // Fragment per service so we can emit two rows: the main
                    // status row + an optional sub-row with the deep-link
                    // (only when the service has a public domain). React
                    // requires the Fragment itself carry the key, since it
                    // is the immediate child of `.map()`.
                    <Fragment key={service.id}>
                      <tr className="hover:bg-muted/30 transition-colors">
                        <td className="py-1 pl-2 pr-1">
                          <div className="flex min-w-0 items-center gap-1.5">
                            <div
                              className={cn(
                                "h-1.5 w-1.5 shrink-0 rounded-full",
                                serviceStatusColor[service.status] ||
                                  "bg-muted-foreground",
                              )}
                            />
                            <Link
                              href={serviceHref}
                              className="hover:text-primary max-w-[100px] truncate font-medium hover:underline"
                              aria-label={`Open service ${service.name}`}
                            >
                              {service.name}
                            </Link>
                            {service.currentImageUri && (
                              <button
                                type="button"
                                onClick={() =>
                                  copy(digestKey, service.currentImageUri!)
                                }
                                className={cn(
                                  "bg-muted/30 hidden shrink-0 rounded border px-1 py-0.5 font-mono text-[9px] leading-none transition-colors md:inline-flex md:items-center md:gap-1",
                                  digestCopied
                                    ? "border-status-success/40 text-status-success"
                                    : "border-border/40 text-muted-foreground hover:border-border hover:text-foreground",
                                )}
                                title={
                                  digestCopied
                                    ? "Copied!"
                                    : `Click to copy: ${service.currentImageUri}`
                                }
                                aria-label={
                                  digestCopied
                                    ? "Image reference copied"
                                    : `Copy running image reference: ${service.currentImageUri}`
                                }
                              >
                                {shortImageRef(service.currentImageUri)}
                                {digestCopied ? (
                                  <Check className="h-2 w-2" />
                                ) : (
                                  <Copy className="h-2 w-2 opacity-0 transition-opacity group-hover/card:opacity-60" />
                                )}
                              </button>
                            )}
                          </div>
                        </td>
                        <td className="px-1 py-1">
                          <div className="flex min-w-0 items-center gap-1">
                            <Link
                              href={logsHrefForRow}
                              className={cn(
                                "inline-block rounded px-1 py-0.5 text-[10px] font-medium leading-none transition-colors hover:underline",
                                service.status === "running" &&
                                  "bg-status-success/15 text-status-success hover:bg-status-success/25",
                                service.status === "failed" &&
                                  "bg-status-error/15 text-status-error hover:bg-status-error/25",
                                service.status === "pending" &&
                                  "bg-status-warning/15 text-status-warning hover:bg-status-warning/25",
                                service.status === "deploying" &&
                                  "bg-status-info/15 text-status-info hover:bg-status-info/25 animate-pulse",
                                service.status === "unknown" &&
                                  "bg-muted text-muted-foreground hover:bg-muted/80",
                              )}
                              aria-label={`View ${service.name} logs (status: ${serviceStatusLabel[service.status] || "unknown"})`}
                              title={`View logs \u2014 current status: ${serviceStatusLabel[service.status] || "unknown"}`}
                            >
                              {serviceStatusLabel[service.status] || "Unknown"}
                            </Link>
                            <RolloutStateIndicator
                              state={service.rolloutState}
                              reason={service.rolloutBlockedReason}
                            />
                            <ServiceProcessIndicator
                              projectSlug={project.slug}
                              service={service}
                            />
                          </div>
                        </td>
                        <td className="text-muted-foreground px-1 py-1 text-right tabular-nums">
                          {service.replicas ? (
                            <Link
                              href={serviceHref}
                              className="hover:text-foreground hover:underline"
                              aria-label={`${service.name} replicas: ${service.replicas} \u2014 open service`}
                            >
                              {service.replicas}
                            </Link>
                          ) : (
                            "\u2014"
                          )}
                        </td>
                        <td className="text-muted-foreground max-w-[60px] truncate py-1 pl-1 pr-2 text-right">
                          {service.environment || "\u2014"}
                        </td>
                      </tr>
                    {/* Per-service deep-link sub-row.
                        Renders beneath each service that has a public
                        domain so operators can click straight through to
                        a live URL without going through the project
                        detail page. The ServiceLink also surfaces the
                        env (prod/staging/preview/dev) explicitly via a
                        colored badge, eliminating the previous
                        first-service-wins ambiguity at the project
                        level. Services without a domain render no
                        sub-row -- the main row already conveys "service
                        exists" and an extra placeholder per service
                        would overwhelm the card. */}
                      {service.domain && (
                        <tr className="hover:bg-muted/20 transition-colors">
                          <td colSpan={4} className="py-1 pl-4 pr-2">
                            <ServiceLink
                              domain={service.domain}
                              env={normalizeEnv(service.environment)}
                              isHealthy={service.health !== "unhealthy"}
                              ariaLabelService={service.name}
                            />
                          </td>
                        </tr>
                      )}
                    </Fragment>
                  );
                })}
                {hasOverflow && (
                  <tr>
                    <td colSpan={4} className="py-0">
                      <Link
                        href={`/projects/${project.slug}#services`}
                        className="text-muted-foreground hover:bg-muted/30 hover:text-foreground block py-1 text-center text-[10px] transition-colors"
                        aria-label={`View all ${services.length} services for ${project.name}`}
                      >
                        +{overflowCount} more service{overflowCount > 1 ? "s" : ""}
                      </Link>
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        )}

        {/* Row 3: Commit message, description, or status summary.
            Empty-state copy is driven by the tri-state `deployResolution`:
            - "no-deploys": services loaded, none ever deployed -> explicit copy
            - "unknown":    services fetch rejected -> em-dash, don't fabricate
            - undefined:    legacy callers that didn't thread the field through
            See lib/project-deploy.ts (audit finding PR-1).
            Decorative — passes clicks through to the surface link. */}
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

        {/* Row 4: Project-level domain.
            Kept as a fallback entry-point but de-emphasized when at
            least one service already exposes its own deep-link in the
            table -- the brief flagged that per-service links carry
            strictly more information (env context + correct host per
            service), so the project-level link becomes a fallback rather
            than the headline. When no service has a domain, this stays
            at full weight as before. */}
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
            className="text-muted-foreground hover:text-foreground relative z-10 mt-1.5 flex w-fit items-center gap-1.5 truncate text-xs transition-colors"
            aria-label={`Open ${repoSlug || project.gitRepo} on GitHub in a new tab`}
          >
            <Github className="h-3 w-3 shrink-0" />
            <span className="truncate">{project.gitRepo.replace(/^https?:\/\/github\.com\//, '')}</span>
            {project.repoMeta?.visibility === "private" && (
              <Lock
                className="text-status-warning h-3 w-3 shrink-0"
                aria-label="Private repository"
              />
            )}
            {project.repoMeta?.visibility === "public" && (
              <Globe
                className="text-status-success h-3 w-3 shrink-0"
                aria-label="Public repository"
              />
            )}
            {project.repoMeta?.visibility === "internal" && (
              <Lock
                className="text-status-info h-3 w-3 shrink-0"
                aria-label="Internal repository"
              />
            )}
            {project.repoMeta?.archived && (
              <Archive
                className="text-muted-foreground h-3 w-3 shrink-0"
                aria-label="Archived repository"
              />
            )}
            {project.repoMeta?.fork && (
              <GitFork
                className="text-muted-foreground h-3 w-3 shrink-0"
                aria-label="Forked repository"
              />
            )}
            {project.repoMeta?.isTemplate && (
              <Copy
                className="text-muted-foreground h-3 w-3 shrink-0"
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
            <div className="text-muted-foreground mt-1 flex items-center gap-2 text-[10px]">
              {project.repoMeta.language && (
                <span
                  className="flex items-center gap-1"
                  title={`Primary language: ${project.repoMeta.language}`}
                >
                  <span
                    className="h-2 w-2 shrink-0 rounded-full"
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
                  className="border-border/40 bg-muted/30 rounded border px-1 py-px font-mono leading-none"
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
            row degrades gracefully on unconfigured deployments.
            `relative z-10` so the badges' own hover/click targets sit
            above the card-surface overlay. */}
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

        {/* Row 5: Git branch + view deployments + relative timestamp.
            Each cell is now its own destination:
            - Branch → GitHub branch view (when we have repoSlug)
            - Deployments → /projects/:slug/deployments
            - Timestamp → /projects/:slug/deployments (alias — operators
              naturally click the time when they want "what just shipped") */}
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
