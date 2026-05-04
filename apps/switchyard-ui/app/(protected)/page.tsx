"use client";

import { useState, useEffect, useMemo, useCallback } from "react";
import { usePolling } from "@/hooks/use-polling";
import { POLLING_SLOW } from "@/lib/constants";
import { apiGet, apiPost } from "@/lib/api";
import { useScope } from "@/contexts/ScopeContext";
import { useTier } from "@/hooks/use-tier";
import { PricingModal } from "@/components/modals/PricingModal";
import { Button } from "@enclii/ui-components/button";
import { Rocket } from "lucide-react";
import {
  ProjectCardCompact,
  ProjectCardCompactSkeleton,
  ProjectRowCompact,
  type CompactProject,
  type CompactRepoMeta,
  type CompactService,
} from "@/components/dashboard/project-card-compact";
import { resolveLatestDeployment } from "@/lib/project-deploy";
import { inferFrameworkFromContext } from "@/components/dashboard/framework-icon";
import { type SortOption } from "@/components/dashboard/project-search-filter";
import { useViewMode } from "@/components/dashboard/view-toggle";
import { SubNavActionBar } from "@/components/dashboard/sub-nav-action-bar";
import { UsageOverview } from "@/components/dashboard/usage-overview";
import { SidebarAlerts } from "@/components/dashboard/sidebar-alerts";
import { SidebarRecentPreviews } from "@/components/dashboard/sidebar-recent-previews";
import { LastSyncBadge } from "@/components/dashboard/last-sync-badge";
import { SystemHealthSummary } from "@/components/dashboard/system-health-summary";

interface ApiProject {
  id: string;
  name: string;
  slug: string;
  description: string;
  created_at: string;
  updated_at: string;
}

interface ApiService {
  id: string;
  name: string;
  project_id: string;
  git_repo: string;
  status: string;
  health: string;
  last_deployment: string;
  domain?: string;
  framework?: string;
  last_commit_message?: string;
  last_commit_branch?: string;
  desired_replicas?: number;
  ready_replicas?: number;
  auto_deploy_env?: string;
  // Surfaced by ListByProject as of switchyard-api PR #155 — the
  // currently-running release's image URI (digest-pinned in production
  // by Kyverno). Drives the digest chip in project-card-compact.tsx.
  current_image_uri?: string;
  // Rollout truthfulness signals — separate from `health`. Reports whether
  // the *newest* ReplicaSet has actually landed. The legacy `health` field
  // reports "healthy" while a new RS may have been failing readiness for
  // days; rollout_state surfaces that lie.
  // See switchyard-api/internal/k8s/rollout_state.go.
  rollout_state?: string;
  rollout_blocked_reason?: string;
}

const INITIAL_VISIBLE = 10;

export default function Dashboard() {
  useScope();
  const {
    requireTier,
    showUpgradeModal,
    closeUpgradeModal,
    blockedAction,
    upgradeMessage,
    checkoutUrl,
    tier,
  } = useTier();

  const [projects, setProjects] = useState<CompactProject[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [lastSyncedAt, setLastSyncedAt] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [sort, setSort] = useState<SortOption>("updated");
  const [visibleCount, setVisibleCount] = useState(INITIAL_VISIBLE);
  const [viewMode, setViewMode] = useViewMode({
    storageKey: "enclii-dashboard-view",
    defaultMode: "grid",
  });

  const fetchProjects = useCallback(async () => {
    try {
      setError(null);
      setRefreshing(true);
      const data = await apiGet<{ projects: ApiProject[] }>("/v1/projects");
      const apiProjects = data.projects || [];

      // Fetch services per project in parallel
      const serviceResults = await Promise.allSettled(
        apiProjects.map((p) =>
          apiGet<{ services: ApiService[] }>(`/v1/projects/${p.slug}/services`),
        ),
      );

      const compactProjects: CompactProject[] = apiProjects.map(
        (project, i) => {
          const result = serviceResults[i];
          const apiServices =
            result.status === "fulfilled" ? result.value.services || [] : [];

          const healthyCount = apiServices.filter(
            (s) => s.health === "healthy",
          ).length;

          const domain =
            apiServices.find((s) => s.domain)?.domain || undefined;

          const gitRepo = apiServices.find((s) => s.git_repo)?.git_repo;

          // Framework: API value → heuristic from name/repo
          const framework =
            apiServices.find((s) => s.framework)?.framework ||
            inferFrameworkFromContext(
              apiServices[0]?.name || project.name,
              gitRepo,
            );

          // Map to CompactService[]
          const compactServices: CompactService[] = apiServices.map((s) => ({
            id: s.id,
            name: s.name,
            status: (["running", "pending", "failed", "deploying"].includes(s.status)
              ? s.status
              : "unknown") as CompactService["status"],
            health: (["healthy", "unhealthy"].includes(s.health)
              ? s.health
              : "unknown") as CompactService["health"],
            replicas:
              s.ready_replicas !== undefined && s.desired_replicas !== undefined
                ? `${s.ready_replicas}/${s.desired_replicas}`
                : undefined,
            environment: s.auto_deploy_env || undefined,
            currentImageUri: s.current_image_uri || undefined,
          }));

          // Compute aggregate status
          const hasAny = compactServices.length > 0;
          const hasFailed = compactServices.some((s) => s.status === "failed");
          const allHealthy = compactServices.every(
            (s) => s.status === "running" && s.health === "healthy",
          );
          const aggregateStatus: CompactProject["aggregateStatus"] = !hasAny
            ? "unknown"
            : hasFailed
              ? "failing"
              : allHealthy
                ? "healthy"
                : "degraded";

          // Single source of truth for "latest deployment" — see
          // lib/project-deploy.ts. Both /dashboard and /projects must
          // route through this helper or PR-1 (the empty-state drift)
          // will regress.
          const resolution = resolveLatestDeployment(
            apiServices,
            result.status === "fulfilled",
          );

          return {
            id: project.id,
            name: project.name,
            slug: project.slug,
            description: project.description,
            framework,
            gitRepo,
            domain,
            lastDeployment: resolution.latest,
            deployResolution: resolution.status,
            serviceCount: apiServices.length,
            healthyCount,
            services: compactServices,
            aggregateStatus,
            updatedAt: project.updated_at,
          };
        },
      );

      setProjects(compactProjects);
      setLastSyncedAt(new Date().toISOString());
      setLoading(false);

      // Fan out one batch call to /v1/integrations/github/repos/metadata for
      // the at-a-glance public/private indicator + key repo stats. Server
      // caches results for 5 min; failures degrade gracefully (the card
      // simply renders without the icon/chips). Done after the cards have
      // mounted so the dashboard shows quickly and the metadata fills in.
      const repoSlugs = Array.from(
        new Set(
          compactProjects
            .map((p) => p.gitRepo)
            .filter((r): r is string => !!r)
            .map((r) =>
              r
                .replace(/^https?:\/\/github\.com\//, "")
                .replace(/\.git$/, ""),
            ),
        ),
      );
      if (repoSlugs.length > 0) {
        try {
          const meta = await apiPost<{
            repos: Record<string, {
              visibility?: string;
              language?: string;
              license?: string;
              stars?: number;
              forks?: number;
              archived?: boolean;
              fork?: boolean;
              is_template?: boolean;
              default_branch?: string;
              pushed_at?: string;
              description?: string;
            }>;
            errors?: Record<string, string>;
          }>("/v1/integrations/github/repos/metadata", {
            repos: repoSlugs,
          });
          setProjects((prev) =>
            prev.map((p) => {
              if (!p.gitRepo) return p;
              const key = p.gitRepo
                .replace(/^https?:\/\/github\.com\//, "")
                .replace(/\.git$/, "");
              const m = meta.repos?.[key];
              const errored = meta.errors?.[key];
              if (!m && !errored) return p;
              const repoMeta: CompactRepoMeta = m
                ? {
                    visibility:
                      m.visibility === "public" ||
                      m.visibility === "private" ||
                      m.visibility === "internal"
                        ? m.visibility
                        : "unknown",
                    language: m.language || undefined,
                    license: m.license || undefined,
                    stars: m.stars,
                    forks: m.forks,
                    archived: m.archived,
                    fork: m.fork,
                    isTemplate: m.is_template,
                    defaultBranch: m.default_branch,
                    pushedAt: m.pushed_at,
                    description: m.description || undefined,
                  }
                : { visibility: "unknown" };
              return { ...p, repoMeta };
            }),
          );
        } catch (e) {
          // Metadata is non-critical — dashboard already rendered. Log and
          // move on. Card simply omits visibility chip when meta is absent.
          console.warn("Failed to load repo metadata batch:", e);
        }
      }
    } catch (err) {
      console.error("Failed to fetch projects:", err);
      setError(err instanceof Error ? err.message : "Failed to load projects");
      setLoading(false);
    } finally {
      setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    fetchProjects();
  }, [fetchProjects]);

  usePolling(fetchProjects, POLLING_SLOW);

  // Filter and sort
  const filteredProjects = useMemo(() => {
    let result = projects;

    if (search) {
      const q = search.toLowerCase();
      result = result.filter(
        (p) =>
          p.name.toLowerCase().includes(q) ||
          p.slug.toLowerCase().includes(q) ||
          p.description?.toLowerCase().includes(q),
      );
    }

    switch (sort) {
      case "name-asc":
        result = [...result].sort((a, b) => a.name.localeCompare(b.name));
        break;
      case "name-desc":
        result = [...result].sort((a, b) => b.name.localeCompare(a.name));
        break;
      case "newest":
        break;
      case "updated":
      default:
        result = [...result].sort((a, b) => {
          const aTime = a.lastDeployment?.timestamp
            ? new Date(a.lastDeployment.timestamp).getTime()
            : a.updatedAt ? new Date(a.updatedAt).getTime() : 0;
          const bTime = b.lastDeployment?.timestamp
            ? new Date(b.lastDeployment.timestamp).getTime()
            : b.updatedAt ? new Date(b.updatedAt).getTime() : 0;
          return bTime - aTime;
        });
        break;
    }

    return result;
  }, [projects, search, sort]);

  const visibleProjects = filteredProjects.slice(0, visibleCount);
  const hasMore = filteredProjects.length > visibleCount;

  const handleCreateProjectClick = () => {
    if (!requireTier("project", { currentProjectCount: projects.length })) {
      return;
    }
    window.location.href = "/projects?create=true";
  };

  if (loading) {
    return (
      <div className="mx-auto max-w-7xl py-6 sm:px-6 lg:px-8">
        <div className="px-4 py-6 sm:px-0">
          <div className="mb-6 flex items-center gap-3">
            <div className="bg-muted h-10 w-64 animate-pulse rounded" />
            <div className="bg-muted h-10 w-40 animate-pulse rounded" />
          </div>
          <div className="grid grid-cols-1 gap-6 lg:grid-cols-12">
            <div className="lg:col-span-3 space-y-4">
              <div className="bg-muted h-40 animate-pulse rounded-lg" />
              <div className="bg-muted h-28 animate-pulse rounded-lg" />
            </div>
            <div className="lg:col-span-9">
              <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                {Array.from({ length: 6 }).map((_, i) => (
                  <ProjectCardCompactSkeleton key={i} />
                ))}
              </div>
            </div>
          </div>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="mx-auto max-w-7xl py-6 sm:px-6 lg:px-8">
        <div className="bg-destructive/10 border-destructive/20 rounded-lg border p-6">
          <h2 className="text-destructive mb-2 text-lg font-semibold">
            Error Loading Dashboard
          </h2>
          <p className="text-destructive/80 mb-4">{error}</p>
          <Button variant="outline" onClick={fetchProjects}>
            Try Again
          </Button>
        </div>
      </div>
    );
  }

  return (
    // max-w-screen-2xl + reduced horizontal padding so 1920px+ displays don't
    // bleed gutters into wasted whitespace. Below lg the layout collapses to a
    // single column with the sidebar moving below the project grid (mobile-first
    // priority: see your projects first, then ecosystem state).
    <div className="mx-auto max-w-screen-2xl px-4 py-4 sm:px-6 sm:py-6 lg:px-8">
      <div>
        {/* Sub-Nav Action Bar (sticky-positioned with the page header to keep
            search + view toggle reachable while scrolling project lists). */}
        <SubNavActionBar
          search={search}
          onSearchChange={setSearch}
          sort={sort}
          onSortChange={setSort}
          viewMode={viewMode}
          onViewModeChange={setViewMode}
          onCreateProject={handleCreateProjectClick}
        />

        {/* Freshness indicator: last sync + manual refresh */}
        <div className="mt-2 flex justify-end">
          <LastSyncBadge
            lastSyncedAt={lastSyncedAt}
            onRefresh={fetchProjects}
            refreshing={refreshing}
          />
        </div>

        {/* 3-column layout. On lg+ the left sidebar is sticky inside the
            scroll container so usage / system health / alerts stay visible
            as the user scrolls deep project lists — the most common UX
            complaint was watching them disappear after 3 cards. Below lg
            the sidebar order moves AFTER the projects grid (lg:order-1),
            so mobile users hit projects first. */}
        <div className="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-12 lg:gap-6">
          {/* Left sidebar (sticky on lg+) */}
          <aside
            className="space-y-4 lg:sticky lg:top-20 lg:col-span-3 lg:self-start lg:max-h-[calc(100vh-6rem)] lg:overflow-y-auto lg:pr-1 lg:order-1 order-2"
            aria-label="Ecosystem snapshot"
          >
            <UsageOverview variant="compact" />
            <SystemHealthSummary />
            <SidebarAlerts />
            <SidebarRecentPreviews projects={projects} />
          </aside>

          {/* Main content (projects). Below lg this comes first; on lg+ the
              grid implicit order places it after the sidebar (col-span-9). */}
          <div className="lg:col-span-9 lg:order-2 order-1">
            {filteredProjects.length === 0 ? (
              projects.length === 0 ? (
                <div className="border-border rounded-lg border border-dashed py-16 text-center">
                  <Rocket className="text-muted-foreground mx-auto mb-3 h-10 w-10" />
                  <h3 className="text-foreground text-lg font-medium">
                    No projects yet
                  </h3>
                  <p className="text-muted-foreground mx-auto mb-4 mt-1 max-w-sm">
                    Get started by creating your first project to deploy and
                    manage your services.
                  </p>
                  <Button onClick={handleCreateProjectClick}>
                    Create Project
                  </Button>
                </div>
              ) : (
                <div className="border-border rounded-lg border border-dashed py-16 text-center">
                  <p className="text-muted-foreground">
                    No projects match &quot;{search}&quot;
                  </p>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="mt-2"
                    onClick={() => setSearch("")}
                  >
                    Clear search
                  </Button>
                </div>
              )
            ) : (
              <>
                {viewMode === "list" ? (
                  // List view: full-width rows. Each row keeps the most
                  // operationally useful fields visible (status, replicas,
                  // domain, last commit, age) and skips the per-service
                  // table so the row stays scannable. Click → project page.
                  <div
                    className="divide-y divide-border/40 rounded-lg border border-border/60 bg-card overflow-hidden transition-opacity duration-150"
                    role="list"
                  >
                    {visibleProjects.map((project) => (
                      <ProjectRowCompact
                        key={project.id}
                        project={project}
                      />
                    ))}
                  </div>
                ) : (
                  // Grid view: cards. xl breakpoint adds a 3rd column so
                  // 1440px+ widescreen monitors don't waste edge space.
                  <div className="grid grid-cols-1 gap-3 transition-opacity duration-150 sm:grid-cols-2 xl:grid-cols-3">
                    {visibleProjects.map((project) => (
                      <ProjectCardCompact key={project.id} project={project} />
                    ))}
                  </div>
                )}

                {hasMore && (
                  <div className="mt-6 flex justify-center">
                    <Button
                      variant="outline"
                      onClick={() =>
                        setVisibleCount((c) => c + INITIAL_VISIBLE)
                      }
                    >
                      Show More ({filteredProjects.length - visibleCount}{" "}
                      remaining)
                    </Button>
                  </div>
                )}
              </>
            )}
          </div>
        </div>
      </div>

      {/* Pricing/Upgrade Modal */}
      <PricingModal
        isOpen={showUpgradeModal}
        onClose={closeUpgradeModal}
        blockedAction={blockedAction}
        upgradeMessage={upgradeMessage}
        checkoutUrl={checkoutUrl}
        currentTier={tier}
      />
    </div>
  );
}
