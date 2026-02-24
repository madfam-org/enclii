"use client";

import { useState, useEffect, useMemo, useCallback } from "react";
import { usePolling } from "@/hooks/use-polling";
import { POLLING_SLOW } from "@/lib/constants";
import { apiGet } from "@/lib/api";
import { useScope } from "@/contexts/ScopeContext";
import { useTier } from "@/hooks/use-tier";
import { PricingModal } from "@/components/modals/PricingModal";
import { Button } from "@/components/ui/button";
import { Plus, Package, CheckCircle2, Rocket } from "lucide-react";
import Link from "next/link";
import {
  ProjectCardCompact,
  ProjectCardCompactSkeleton,
  type CompactProject,
} from "@/components/dashboard/project-card-compact";
import {
  ProjectSearchFilter,
  type SortOption,
} from "@/components/dashboard/project-search-filter";

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

const INITIAL_VISIBLE = 9;

export default function Dashboard() {
  const { currentScope } = useScope();
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
          const services =
            result.status === "fulfilled" ? result.value.services || [] : [];

          const healthyCount = services.filter(
            (s) => s.health === "healthy",
          ).length;

          // Derive domain from first service with a domain, or from project slug
          const domain =
            services.find((s) => s.domain)?.domain || undefined;

          // Derive framework from first service
          const framework = services.find((s) => s.framework)?.framework;

          // Derive git repo from first service
          const gitRepo = services.find((s) => s.git_repo)?.git_repo;

          // Find the most recent deployment info
          const latestService = services
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
            serviceCount: services.length,
            healthyCount,
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

    // Search
    if (search) {
      const q = search.toLowerCase();
      result = result.filter(
        (p) =>
          p.name.toLowerCase().includes(q) ||
          p.slug.toLowerCase().includes(q) ||
          p.description?.toLowerCase().includes(q),
      );
    }

    // Sort
    switch (sort) {
      case "name-asc":
        result = [...result].sort((a, b) => a.name.localeCompare(b.name));
        break;
      case "name-desc":
        result = [...result].sort((a, b) => b.name.localeCompare(a.name));
        break;
      case "newest":
        // Projects are already sorted by created_at desc from API, but we can reverse for "updated"
        break;
      case "updated":
      default:
        // Sort by last deployment timestamp desc, projects without deployments last
        result = [...result].sort((a, b) => {
          const aTime = a.lastDeployment?.timestamp
            ? new Date(a.lastDeployment.timestamp).getTime()
            : 0;
          const bTime = b.lastDeployment?.timestamp
            ? new Date(b.lastDeployment.timestamp).getTime()
            : 0;
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
    // Navigate to projects page with create intent
    window.location.href = "/projects?create=true";
  };

  // Inline stats
  const totalServices = projects.reduce(
    (sum, p) => sum + (p.serviceCount || 0),
    0,
  );
  const totalHealthy = projects.reduce(
    (sum, p) => sum + (p.healthyCount || 0),
    0,
  );

  const scopeName = currentScope?.name || "Your";

  if (loading) {
    return (
      <div className="mx-auto max-w-7xl py-6 sm:px-6 lg:px-8">
        <div className="px-4 py-6 sm:px-0">
          <div className="bg-muted mb-2 h-8 w-1/3 animate-pulse rounded" />
          <div className="bg-muted mb-6 h-4 w-1/4 animate-pulse rounded" />
          <div className="mb-6 flex items-center gap-3">
            <div className="bg-muted h-10 w-64 animate-pulse rounded" />
            <div className="bg-muted h-10 w-40 animate-pulse rounded" />
          </div>
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
            {Array.from({ length: 6 }).map((_, i) => (
              <ProjectCardCompactSkeleton key={i} />
            ))}
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
        {/* Header */}
        <div className="mb-1 flex items-start justify-between">
          <div>
            <h1 className="text-foreground text-2xl font-bold">
              {scopeName}&apos;s Projects
            </h1>
            {projects.length > 0 && (
              <p className="text-muted-foreground mt-1 flex items-center gap-3 text-sm">
                <span className="flex items-center gap-1">
                  <Package className="h-3.5 w-3.5" />
                  {projects.length} project{projects.length !== 1 ? "s" : ""}
                </span>
                <span className="flex items-center gap-1">
                  <CheckCircle2 className="h-3.5 w-3.5" />
                  {totalHealthy}/{totalServices} services healthy
                </span>
              </p>
            )}
          </div>
          <Button
            size="sm"
            onClick={handleCreateProjectClick}
            data-tour="create-project"
          >
            <Plus className="mr-1.5 h-4 w-4" />
            Add New...
          </Button>
        </div>

        {/* Search & Filter */}
        <div className="mt-6">
          <ProjectSearchFilter
            search={search}
            onSearchChange={setSearch}
            sort={sort}
            onSortChange={setSort}
          />
        </div>

        {/* Project Grid */}
        {filteredProjects.length === 0 ? (
          projects.length === 0 ? (
            // No projects at all
            <div className="border-border rounded-lg border border-dashed py-16 text-center">
              <Rocket className="text-muted-foreground mx-auto mb-3 h-10 w-10" />
              <h3 className="text-foreground text-lg font-medium">
                No projects yet
              </h3>
              <p className="text-muted-foreground mx-auto mb-4 mt-1 max-w-sm">
                Get started by creating your first project to deploy and manage
                your services.
              </p>
              <Button onClick={handleCreateProjectClick}>
                <Plus className="mr-1.5 h-4 w-4" />
                Create Project
              </Button>
            </div>
          ) : (
            // Search returned no results
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
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
              {visibleProjects.map((project) => (
                <ProjectCardCompact key={project.id} project={project} />
              ))}
            </div>

            {/* Show More */}
            {hasMore && (
              <div className="mt-6 flex justify-center">
                <Button
                  variant="outline"
                  onClick={() =>
                    setVisibleCount((c) => c + INITIAL_VISIBLE)
                  }
                >
                  Show More ({filteredProjects.length - visibleCount} remaining)
                </Button>
              </div>
            )}
          </>
        )}
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
