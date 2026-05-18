'use client';

import { useState, useEffect, useMemo, useCallback } from 'react';
import { useSearchParams, useRouter } from 'next/navigation';
import { apiGet, apiPost } from '@/lib/api';
import { useTier } from '@/hooks/use-tier';
import { usePolling } from '@/hooks/use-polling';
import { useProjectProcessFeed } from '@/hooks/use-project-process-feed';
import { POLLING_SLOW } from '@/lib/constants';
import { PricingModal } from '@/components/modals/PricingModal';
import { Button } from "@enclii/ui-components/button";
import { cn } from '@/lib/utils';
import {
  Plus,
  Rocket,
  FolderKanban,
  Boxes,
  CheckCircle2,
  AlertTriangle,
  XCircle,
} from 'lucide-react';
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
import { LastSyncBadge } from '@/components/dashboard/last-sync-badge';
import { StatCard } from '@/components/dashboard/stat-card';
import {
  processLiveState,
  serviceSummariesById,
} from '@/lib/project-process-feed';
import {
  buildCompactProject,
  type ApiProjectForCards,
  type ApiServiceForCards,
} from '@/lib/project-card-transform';

// /projects is the dedicated projects-only surface — distinct from the home
// dashboard at /. Home shows the ecosystem context (usage, system health,
// alerts, recent previews) alongside a curated project grid; /projects
// drops that context and gives the full width to project management:
// stats banner + status filters + denser grid. If you find yourself
// re-adding the home sidebar widgets here, you're undoing the split.

type StatusFilter = 'all' | 'healthy' | 'degraded' | 'failing';

interface ApiProject extends ApiProjectForCards {
  created_at: string;
}

export default function ProjectsPage() {
  const searchParams = useSearchParams();

  const [projects, setProjects] = useState<CompactProject[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [lastSyncedAt, setLastSyncedAt] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [showCreateForm, setShowCreateForm] = useState(false);
  const [newProject, setNewProject] = useState({
    name: '',
    slug: '',
    description: ''
  });
  const [search, setSearch] = useState('');
  const [sort, setSort] = useState<SortOption>('updated');
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all');
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
  const router = useRouter();
  useEffect(() => {
    if (searchParams.get('create') === 'true') {
      setShowCreateForm(true);
      // Clear the param so a refresh doesn't re-open the modal
      router.replace('/projects', { scroll: false });
    }
  }, [searchParams, router]);

  const handleCreateProjectClick = () => {
    if (!requireTier('project', { currentProjectCount: projects.length })) {
      return;
    }
    setShowCreateForm(true);
  };

  const fetchProjects = useCallback(async () => {
    try {
      setError(null);
      setRefreshing(true);
      const data = await apiGet<{ projects: ApiProject[] }>('/v1/projects');
      const apiProjects = data.projects || [];

      // Fetch services per project in parallel
      const serviceResults = await Promise.allSettled(
        apiProjects.map((p) =>
          apiGet<{ services: ApiServiceForCards[] }>(`/v1/projects/${p.slug}/services`),
        )
      );

      const compactProjects: CompactProject[] = apiProjects.map((project, i) => {
        const result = serviceResults[i];
        const services =
          result.status === 'fulfilled' ? result.value.services || [] : [];

        return buildCompactProject({
          project,
          services,
          servicesResolved: result.status === 'fulfilled',
        });
      });

      setProjects(compactProjects);
      setLastSyncedAt(new Date().toISOString());
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
    } finally {
      setRefreshing(false);
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

  const { summaries: processSummaries } = useProjectProcessFeed(projects);

  const projectsWithProcesses = useMemo(() => {
    return projects.map((project) => {
      const processSummary = processSummaries[project.id];
      const serviceProcessIndex = serviceSummariesById(processSummary);
      return {
        ...project,
        processSummary,
        liveState: processLiveState(processSummary),
        services: project.services?.map((service) => {
          const serviceSummary = serviceProcessIndex[service.id];
          return serviceSummary
            ? {
                ...service,
                processSummary: serviceSummary,
                activeProcessCount: serviceSummary.active_count,
                lastProcess: serviceSummary.latest,
              }
            : service;
        }),
      };
    });
  }, [processSummaries, projects]);

  // Aggregate stats for the projects-page banner. Computed off the full
  // unfiltered set so the totals don't shift when the user narrows by
  // status — that matches the mental model "show me the whole picture,
  // then let me drill in".
  const stats = useMemo(() => {
    let totalServices = 0;
    let healthy = 0;
    let degraded = 0;
    let failing = 0;
    for (const p of projectsWithProcesses) {
      totalServices += p.serviceCount ?? 0;
      if (p.aggregateStatus === 'healthy') healthy += 1;
      else if (p.aggregateStatus === 'degraded') degraded += 1;
      else if (p.aggregateStatus === 'failing') failing += 1;
    }
    return {
      totalProjects: projectsWithProcesses.length,
      totalServices,
      healthy,
      degraded,
      failing,
    };
  }, [projectsWithProcesses]);

  // Filter and sort
  const filteredProjects = useMemo(() => {
    let result = projectsWithProcesses;

    if (statusFilter !== 'all') {
      result = result.filter((p) => p.aggregateStatus === statusFilter);
    }

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
  }, [projectsWithProcesses, search, sort, statusFilter]);

  if (loading) {
    // Loading skeleton mirrors the projects-only layout: title, stats banner
    // placeholder (5 cards), action bar, status pills, then the wide
    // project grid. No sidebar — that's home's job.
    return (
      <div className="mx-auto max-w-screen-2xl px-4 py-4 sm:px-6 sm:py-6 lg:px-8">
        <div className="mb-6 flex items-center gap-3">
          <div className="bg-muted h-8 w-40 animate-pulse rounded" />
        </div>
        <div className="mb-4 grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
          {Array.from({ length: 5 }).map((_, i) => (
            <div key={i} className="bg-muted h-20 animate-pulse rounded-lg" />
          ))}
        </div>
        <div className="mb-3 flex items-center gap-3">
          <div className="bg-muted h-10 w-64 animate-pulse rounded" />
          <div className="bg-muted h-10 w-40 animate-pulse rounded" />
        </div>
        <div className="mt-4 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4">
          {Array.from({ length: 8 }).map((_, i) => (
            <ProjectCardCompactSkeleton key={i} />
          ))}
        </div>
      </div>
    );
  }

  if (error && projects.length === 0) {
    return (
      <div className="mx-auto max-w-screen-2xl px-4 py-4 sm:px-6 sm:py-6 lg:px-8">
        <div className="bg-status-error-muted border-status-error/30 rounded-md border p-4">
          <div className="text-status-error-foreground">
            <h3 className="text-sm font-medium">Error loading projects</h3>
            <div className="mt-2 text-sm">{error}</div>
          </div>
        </div>
      </div>
    );
  }

  const statusPills: Array<{ key: StatusFilter; label: string; count: number }> = [
    { key: 'all', label: 'All', count: stats.totalProjects },
    { key: 'healthy', label: 'Healthy', count: stats.healthy },
    { key: 'degraded', label: 'Degraded', count: stats.degraded },
    { key: 'failing', label: 'Failing', count: stats.failing },
  ];

  return (
    // Projects-only surface — full width for the project list, no
    // ecosystem-context sidebar. Home (/) is the place for usage /
    // system-health / alerts / recent-previews context.
    <div className="mx-auto max-w-screen-2xl px-4 py-4 sm:px-6 sm:py-6 lg:px-8">
      {/* Header — title + count + sync badge */}
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <h1 className="text-foreground text-2xl font-bold">Projects</h1>
          <span className="text-muted-foreground text-sm">
            {stats.totalProjects} total
          </span>
        </div>
        <LastSyncBadge
          lastSyncedAt={lastSyncedAt}
          onRefresh={fetchProjects}
          refreshing={refreshing}
        />
      </div>

      {/* Stats banner — projects-page identity. Counts are over the full
          set, not the filtered subset, so the totals stay anchored as the
          user narrows by status. */}
      <div
        className="mb-4 grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5"
        aria-label="Projects summary"
      >
        <StatCard
          title="Projects"
          value={stats.totalProjects}
          icon={FolderKanban}
          variant="info"
        />
        <StatCard
          title="Services"
          value={stats.totalServices}
          icon={Boxes}
          variant="info"
        />
        <StatCard
          title="Healthy"
          value={stats.healthy}
          icon={CheckCircle2}
          variant="success"
        />
        <StatCard
          title="Degraded"
          value={stats.degraded}
          icon={AlertTriangle}
          variant="warning"
        />
        <StatCard
          title="Failing"
          value={stats.failing}
          icon={XCircle}
          variant="warning"
        />
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
          <form onSubmit={createProject} data-testid="create-project-form">
            <div className="mb-4">
              <label className="text-foreground mb-2 block text-sm font-medium" htmlFor="project-name">
                Project Name
              </label>
              <input
                id="project-name"
                data-testid="project-name-input"
                type="text"
                required
                autoFocus
                className="border-input focus:ring-enclii-blue bg-background w-full rounded-md border px-3 py-2 focus:outline-none focus:ring-2"
                value={newProject.name}
                onChange={(e) => setNewProject({ ...newProject, name: e.target.value })}
              />
            </div>
            <div className="mb-4">
              <label className="text-foreground mb-2 block text-sm font-medium" htmlFor="project-slug">
                Slug
              </label>
              <input
                id="project-slug"
                data-testid="project-slug-input"
                type="text"
                required
                className="border-input focus:ring-enclii-blue bg-background w-full rounded-md border px-3 py-2 focus:outline-none focus:ring-2"
                value={newProject.slug}
                onChange={(e) => setNewProject({ ...newProject, slug: e.target.value })}
              />
            </div>
            <div className="mb-4">
              <label className="text-foreground mb-2 block text-sm font-medium" htmlFor="project-description">
                Description
              </label>
              <textarea
                id="project-description"
                data-testid="project-description-input"
                className="border-input focus:ring-enclii-blue bg-background w-full rounded-md border px-3 py-2 focus:outline-none focus:ring-2"
                rows={3}
                value={newProject.description}
                onChange={(e) => setNewProject({ ...newProject, description: e.target.value })}
              />
            </div>
            <DialogFooter>
              <button
                type="button"
                data-testid="cancel-project-btn"
                onClick={() => setShowCreateForm(false)}
                className="bg-secondary text-secondary-foreground hover:bg-secondary/80 rounded-md px-4 py-2 text-sm font-medium"
              >
                Cancel
              </button>
              <button
                type="submit"
                data-testid="submit-project-btn"
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

      {/* Status filter pills — projects-page-only narrowing primitive.
          Home doesn't need this because it shows a curated subset. */}
      <div
        className="mt-3 flex flex-wrap gap-2"
        role="group"
        aria-label="Filter projects by status"
      >
        {statusPills.map((pill) => {
          const active = statusFilter === pill.key;
          return (
            <button
              key={pill.key}
              type="button"
              onClick={() => setStatusFilter(pill.key)}
              aria-pressed={active}
              className={cn(
                "inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs font-medium transition-colors",
                active
                  ? "border-enclii-blue bg-enclii-blue text-white"
                  : "border-border bg-card text-muted-foreground hover:bg-muted hover:text-foreground"
              )}
            >
              <span>{pill.label}</span>
              <span
                className={cn(
                  "rounded-full px-1.5 py-0.5 text-[10px] tabular-nums",
                  active ? "bg-white/20" : "bg-muted/60"
                )}
              >
                {pill.count}
              </span>
            </button>
          );
        })}
      </div>

      {/* Full-width project grid — no sidebar. Wider grid (lg:3, 2xl:4)
          since we have the entire viewport. */}
      <div className="mt-4">
        {filteredProjects.length === 0 ? (
          projects.length === 0 ? (
            <div className="border-border rounded-lg border border-dashed py-16 text-center">
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
            <div className="border-border rounded-lg border border-dashed py-16 text-center">
              <p className="text-muted-foreground">
                {search
                  ? `No projects match "${search}"`
                  : `No ${statusFilter} projects`}
              </p>
              <Button
                variant="ghost"
                size="sm"
                className="mt-2"
                onClick={() => {
                  setSearch('');
                  setStatusFilter('all');
                }}
              >
                Clear filters
              </Button>
            </div>
          )
        ) : (
          <>
            {viewMode === "list" ? (
              <div
                className="divide-border/40 border-border/60 bg-card divide-y overflow-hidden rounded-lg border transition-opacity duration-150"
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
              <div className="grid grid-cols-1 gap-3 transition-opacity duration-150 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4">
                {filteredProjects.map((project) => (
                  <ProjectCardCompact key={project.id} project={project} />
                ))}
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
