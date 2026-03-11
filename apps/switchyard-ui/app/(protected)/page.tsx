"use client";

import { useState, useEffect, useMemo, useCallback } from "react";
import { usePolling } from "@/hooks/use-polling";
import { POLLING_SLOW } from "@/lib/constants";
import { apiGet } from "@/lib/api";
import { useScope } from "@/contexts/ScopeContext";
import { useTier } from "@/hooks/use-tier";
import { PricingModal } from "@/components/modals/PricingModal";
import { Button } from "@/components/ui/button";
import { Rocket } from "lucide-react";
import {
  ProjectCardCompact,
  ProjectCardCompactSkeleton,
  type CompactProject,
  type CompactService,
} from "@/components/dashboard/project-card-compact";
import { inferFrameworkFromContext } from "@/components/dashboard/framework-icon";
import { type SortOption } from "@/components/dashboard/project-search-filter";
import { useViewMode } from "@/components/dashboard/view-toggle";
import { SubNavActionBar } from "@/components/dashboard/sub-nav-action-bar";
import { UsageOverview } from "@/components/dashboard/usage-overview";
import { SidebarAlerts } from "@/components/dashboard/sidebar-alerts";
import { SidebarRecentPreviews } from "@/components/dashboard/sidebar-recent-previews";

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

          const latestService = apiServices
            .filter((s) => s.last_deployment)
            .sort(
              (a, b) =>
                new Date(b.last_deployment).getTime() -
                new Date(a.last_deployment).getTime(),
            )[0];

          const lastDeployment = latestService
            ? {
                timestamp: latestService.last_deployment,
                status: (latestService.status === "running"
                  ? "success"
                  : latestService.status === "failed"
                    ? "failed"
                    : latestService.status === "deploying"
                      ? "building"
                      : "pending") as
                  | "success"
                  | "failed"
                  | "pending"
                  | "building",
                branch: latestService.last_commit_branch || "main",
                commitMessage:
                  latestService.last_commit_message || undefined,
              }
            : undefined;

          return {
            id: project.id,
            name: project.name,
            slug: project.slug,
            description: project.description,
            framework,
            gitRepo,
            domain,
            lastDeployment,
            serviceCount: apiServices.length,
            healthyCount,
            services: compactServices,
            aggregateStatus,
            updatedAt: project.updated_at,
          };
        },
      );

      setProjects(compactProjects);
      setLoading(false);
    } catch (err) {
      console.error("Failed to fetch projects:", err);
      setError(err instanceof Error ? err.message : "Failed to load projects");
      setLoading(false);
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
    <div className="mx-auto max-w-7xl py-6 sm:px-6 lg:px-8">
      <div className="px-4 py-6 sm:px-0">
        {/* Sub-Nav Action Bar */}
        <SubNavActionBar
          search={search}
          onSearchChange={setSearch}
          sort={sort}
          onSortChange={setSort}
          viewMode={viewMode}
          onViewModeChange={setViewMode}
          onCreateProject={handleCreateProjectClick}
        />

        {/* 3-Column Layout */}
        <div className="grid grid-cols-1 gap-6 mt-6 lg:grid-cols-12">
          {/* Left sidebar */}
          <aside className="lg:col-span-3 space-y-4">
            <UsageOverview variant="compact" />
            <SidebarAlerts />
            <SidebarRecentPreviews projects={projects} />
          </aside>

          {/* Main content */}
          <div className="lg:col-span-9">
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
                <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                  {visibleProjects.map((project) => (
                    <ProjectCardCompact key={project.id} project={project} />
                  ))}
                </div>

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
