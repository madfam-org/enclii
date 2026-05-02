'use client';

import { useState, useEffect, useMemo, useCallback } from 'react';
import { useSearchParams } from 'next/navigation';
import { apiGet, apiPost } from '@/lib/api';
import { useTier } from '@/hooks/use-tier';
import { usePolling } from '@/hooks/use-polling';
import { POLLING_SLOW } from '@/lib/constants';
import { PricingModal } from '@/components/modals/PricingModal';
import { Button } from "@enclii/ui-components/button";
import { Plus, Rocket } from 'lucide-react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@enclii/ui-components/dialog";
import {
  ProjectCardCompact,
  ProjectRowCompact,
  ProjectCardCompactSkeleton,
  type CompactProject,
  type CompactRepoMeta,
} from '@/components/dashboard/project-card-compact';
import { type SortOption } from '@/components/dashboard/project-search-filter';
import { SubNavActionBar } from '@/components/dashboard/sub-nav-action-bar';
import { useViewMode } from '@/components/dashboard/view-toggle';

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
  current_image_uri?: string;
}

export default function ProjectsPage() {
  const searchParams = useSearchParams();

  const [projects, setProjects] = useState<CompactProject[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showCreateForm, setShowCreateForm] = useState(false);
  const [newProject, setNewProject] = useState({
    name: '',
    slug: '',
    description: ''
  });
  const [search, setSearch] = useState('');
  const [sort, setSort] = useState<SortOption>('updated');
  const [viewMode, setViewMode] = useViewMode({
    storageKey: "enclii-projects-view",
    defaultMode: "grid",
  });

  // Tier-based RBAC
  const {
    requireTier,
    showUpgradeModal,
    closeUpgradeModal,
    blockedAction,
    upgradeMessage,
    checkoutUrl,
    tier,
  } = useTier();

  // Open create modal if redirected with ?create=true
  useEffect(() => {
    if (searchParams.get('create') === 'true') {
      setShowCreateForm(true);
    }
  }, [searchParams]);

  const handleCreateProjectClick = () => {
    if (!requireTier('project', { currentProjectCount: projects.length })) {
      return;
    }
    setShowCreateForm(true);
  };

  const fetchProjects = useCallback(async () => {
    try {
      setError(null);
      const data = await apiGet<{ projects: ApiProject[] }>('/v1/projects');
      const apiProjects = data.projects || [];

      // Fetch services per project in parallel
      const serviceResults = await Promise.allSettled(
        apiProjects.map((p) =>
          apiGet<{ services: ApiService[] }>(`/v1/projects/${p.slug}/services`)
        )
      );

      const compactProjects: CompactProject[] = apiProjects.map(
        (project, i) => {
          const result = serviceResults[i];
          const services =
            result.status === 'fulfilled' ? result.value.services || [] : [];

          const healthyCount = services.filter(
            (s) => s.health === 'healthy'
          ).length;

          const domain = services.find((s) => s.domain)?.domain || undefined;
          const framework = services.find((s) => s.framework)?.framework;
          const gitRepo = services.find((s) => s.git_repo)?.git_repo;

          // Map services
          const compactServices = services.map((s) => ({
            id: s.id,
            name: s.name,
            status: (["running", "pending", "failed", "deploying"].includes(s.status) ? s.status : "unknown") as any,
            health: (["healthy", "unhealthy"].includes(s.health) ? s.health : "unknown") as any,
            replicas: s.ready_replicas !== undefined && s.desired_replicas !== undefined ? `${s.ready_replicas}/${s.desired_replicas}` : undefined,
            environment: s.auto_deploy_env || undefined,
            currentImageUri: s.current_image_uri || undefined,
          }));

          const hasAny = compactServices.length > 0;
          const hasFailed = compactServices.some((s) => s.status === "failed");
          const allHealthy = compactServices.every(
            (s) => s.status === "running" && s.health === "healthy",
          );
          const aggregateStatus = !hasAny
            ? "unknown"
            : hasFailed
              ? "failing"
              : allHealthy
                ? "healthy"
                : "degraded";

          const latestService = services
            .filter((s) => s.last_deployment)
            .sort(
              (a, b) =>
                new Date(b.last_deployment).getTime() -
                new Date(a.last_deployment).getTime()
            )[0];

          const lastDeployment = latestService
            ? {
                timestamp: latestService.last_deployment,
                status: (latestService.status === 'running'
                  ? 'success'
                  : latestService.status === 'failed'
                    ? 'failed'
                    : latestService.status === 'deploying'
                      ? 'building'
                      : 'pending') as
                  | 'success'
                  | 'failed'
                  | 'pending'
                  | 'building',
                branch: latestService.last_commit_branch || 'main',
                commitMessage: latestService.last_commit_message || undefined,
              }
            : undefined;

          return {
            id: project.id,
            name: project.name,
            slug: project.slug,
            description: project.description,
            framework,
            domain,
            gitRepo,
            lastDeployment,
            serviceCount: services.length,
            healthyCount,
            services: compactServices,
            aggregateStatus,
            updatedAt: project.updated_at,
          };
        }
      );

      setProjects(compactProjects);
      setLoading(false);

      // Fetch Repo Meta
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
                        ? m.visibility as "public"|"private"|"internal"
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
          console.warn("Failed to load repo metadata batch:", e);
        }
      }

    } catch (err) {
      setError(err instanceof Error ? err.message : 'An error occurred');
      setLoading(false);
    }
  }, []);

  const createProject = async (e: React.FormEvent) => {
    e.preventDefault();

    try {
      await apiPost('/v1/projects', newProject);

      setNewProject({ name: '', slug: '', description: '' });
      setShowCreateForm(false);
      fetchProjects();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create project');
    }
  };

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
          p.description?.toLowerCase().includes(q)
      );
    }

    switch (sort) {
      case 'name-asc':
        result = [...result].sort((a, b) => a.name.localeCompare(b.name));
        break;
      case 'name-desc':
        result = [...result].sort((a, b) => b.name.localeCompare(a.name));
        break;
      case 'newest':
        break;
      case 'updated':
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

  if (loading) {
    return (
      <div className="mx-auto max-w-7xl py-6 sm:px-6 lg:px-8">
        <div className="px-4 py-6 sm:px-0">
          <div className="bg-muted mb-6 h-8 w-1/4 animate-pulse rounded" />
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

  if (error && projects.length === 0) {
    return (
      <div className="mx-auto max-w-7xl py-6 sm:px-6 lg:px-8">
        <div className="bg-status-error-muted border-status-error/30 rounded-md border p-4">
          <div className="text-status-error-foreground">
            <h3 className="text-sm font-medium">Error loading projects</h3>
            <div className="mt-2 text-sm">{error}</div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-7xl py-6 sm:px-6 lg:px-8">
      <div className="px-4 py-6 sm:px-0">
        {/* Header */}
        <div className="mb-6 flex items-center justify-between">
          <h1 className="text-foreground text-2xl font-bold">Projects</h1>
        </div>

        {/* Create Project Modal */}
        <Dialog open={showCreateForm} onOpenChange={setShowCreateForm}>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>Create New Project</DialogTitle>
              <DialogDescription>
                Add a new project to organize your services.
              </DialogDescription>
            </DialogHeader>
            <form onSubmit={createProject}>
              <div className="mb-4">
                <label className="text-foreground mb-2 block text-sm font-medium">
                  Project Name
                </label>
                <input
                  type="text"
                  required
                  className="border-input focus:ring-enclii-blue w-full rounded-md border px-3 py-2 focus:outline-none focus:ring-2 bg-background"
                  value={newProject.name}
                  onChange={(e) => setNewProject({ ...newProject, name: e.target.value })}
                />
              </div>
              <div className="mb-4">
                <label className="text-foreground mb-2 block text-sm font-medium">
                  Slug
                </label>
                <input
                  type="text"
                  required
                  className="border-input focus:ring-enclii-blue w-full rounded-md border px-3 py-2 focus:outline-none focus:ring-2 bg-background"
                  value={newProject.slug}
                  onChange={(e) => setNewProject({ ...newProject, slug: e.target.value })}
                />
              </div>
              <div className="mb-4">
                <label className="text-foreground mb-2 block text-sm font-medium">
                  Description
                </label>
                <textarea
                  className="border-input focus:ring-enclii-blue w-full rounded-md border px-3 py-2 focus:outline-none focus:ring-2 bg-background"
                  rows={3}
                  value={newProject.description}
                  onChange={(e) => setNewProject({ ...newProject, description: e.target.value })}
                />
              </div>
              <DialogFooter>
                <button
                  type="button"
                  onClick={() => setShowCreateForm(false)}
                  className="bg-secondary text-secondary-foreground hover:bg-secondary/80 rounded-md px-4 py-2 text-sm font-medium"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="bg-enclii-blue hover:bg-enclii-blue-dark rounded-md px-4 py-2 text-sm font-medium text-white"
                >
                  Create
                </button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>

        {/* Search & Filter & Views */}
        <SubNavActionBar
          search={search}
          onSearchChange={setSearch}
          sort={sort}
          onSortChange={setSort}
          viewMode={viewMode}
          onViewModeChange={setViewMode}
          onCreateProject={handleCreateProjectClick}
        />

        {/* Project Grid */}
        {filteredProjects.length === 0 ? (
          projects.length === 0 ? (
            <div className="border-border mt-4 rounded-lg border border-dashed py-16 text-center">
              <Rocket className="text-muted-foreground mx-auto mb-3 h-10 w-10" />
              <h3 className="text-foreground text-lg font-medium">No projects found</h3>
              <p className="text-muted-foreground mb-4 mt-1">
                Get started by creating your first project.
              </p>
              <Button onClick={handleCreateProjectClick}>
                <Plus className="mr-1.5 h-4 w-4" />
                Create Project
              </Button>
            </div>
          ) : (
            <div className="border-border mt-4 rounded-lg border border-dashed py-16 text-center">
              <p className="text-muted-foreground">
                No projects match &quot;{search}&quot;
              </p>
              <Button
                variant="ghost"
                size="sm"
                className="mt-2"
                onClick={() => setSearch('')}
              >
                Clear search
              </Button>
            </div>
          )
        ) : (
          <div className="mt-4">
            {viewMode === "list" ? (
              <div
                className="divide-y divide-border/40 rounded-lg border border-border/60 bg-card overflow-hidden transition-opacity duration-150"
                role="list"
              >
                {filteredProjects.map((project) => (
                  <ProjectRowCompact
                    key={project.id}
                    project={project}
                  />
                ))}
              </div>
            ) : (
              <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
                {filteredProjects.map((project) => (
                  <ProjectCardCompact key={project.id} project={project} />
                ))}
              </div>
            )}
          </div>
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
