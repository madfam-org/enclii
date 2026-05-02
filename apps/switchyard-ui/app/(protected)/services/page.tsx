'use client';

import { useState, useEffect, useMemo, useCallback } from "react";
import { usePolling } from "@/hooks/use-polling";
import { POLLING_SLOW } from "@/lib/constants";
import Link from "next/link";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@enclii/ui-components/button";
import { ProjectSearch, FilterState, SortState, ServiceStatus } from "@/components/search/project-search";
import { apiGet } from "@/lib/api";
import { Spinner } from "@/components/ui/spinner";
import { Github, Plus, RefreshCw, Server } from "lucide-react";

interface ServiceOverview {
  id: string;
  name: string;
  project_name: string;
  project_slug?: string;
  environment: string;
  status: ServiceStatus;
  version: string;
  replicas: string;
}

// Subset of fields we need from /v1/projects/:slug/services payload.
// Must match the SDK Service shape (see packages/sdk-go/pkg/types/types.go).
interface ApiProject {
  id: string;
  name: string;
  slug: string;
}

interface ApiService {
  id: string;
  name: string;
  health?: string;             // "healthy" | "unhealthy" | "unknown" | ""
  status?: string;
  desired_replicas?: number;
  ready_replicas?: number;
  auto_deploy_env?: string;
}

function normalizeStatus(health: string | undefined): ServiceStatus {
  if (health === 'healthy') return 'healthy';
  if (health === 'unhealthy') return 'unhealthy';
  return 'unknown';
}

export default function ServicesPage() {
  const [services, setServices] = useState<ServiceOverview[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [partialFailures, setPartialFailures] = useState<string[]>([]);

  // Filter and sort state
  const [filters, setFilters] = useState<FilterState>({
    search: '',
    statuses: [],
    environments: [],
  });
  const [sort, setSort] = useState<SortState>({
    field: 'name',
    order: 'asc',
  });

  // Aggregate services across visible projects.
  //
  // Why not /v1/dashboard/stats? That endpoint bundles services with
  // dashboard stats + activities; if any sibling query stalls, services
  // get held hostage by the parent errgroup. Fanning out per-project
  // here lets one slow project fail without blocking the whole page.
  const fetchServices = useCallback(async () => {
    try {
      setError(null);
      const projectsResp = await apiGet<{ projects: ApiProject[] }>('/v1/projects');
      const projects = projectsResp.projects || [];

      if (projects.length === 0) {
        setServices([]);
        setPartialFailures([]);
        setLoading(false);
        return;
      }

      // Promise.allSettled — never let a single project's 5xx black out
      // the whole list. Surface failed project slugs so the user knows
      // the data is incomplete instead of silently truncating it.
      const results = await Promise.allSettled(
        projects.map((p) =>
          apiGet<{ services: ApiService[] }>(`/v1/projects/${p.slug}/services`).then(
            (resp) => ({ project: p, services: resp.services || [] })
          )
        )
      );

      const aggregated: ServiceOverview[] = [];
      const failed: string[] = [];
      results.forEach((r, idx) => {
        const project = projects[idx];
        if (r.status === 'fulfilled') {
          for (const svc of r.value.services) {
            const desired = svc.desired_replicas ?? 0;
            const ready = svc.ready_replicas ?? 0;
            aggregated.push({
              id: svc.id,
              name: svc.name,
              project_name: project.name,
              project_slug: project.slug,
              environment: svc.auto_deploy_env || 'production',
              status: normalizeStatus(svc.health),
              version: svc.status || 'N/A',
              replicas: `${ready}/${desired}`,
            });
          }
        } else {
          failed.push(project.slug);
          console.error(`Failed to load services for project ${project.slug}:`, r.reason);
        }
      });

      setServices(aggregated);
      setPartialFailures(failed);
      setLoading(false);
    } catch (err) {
      console.error('Failed to fetch services:', err);
      const msg = err instanceof Error ? err.message : 'Failed to fetch services';
      // Distinguish timeouts so users know to retry vs investigate.
      const friendly = /timed out/i.test(msg)
        ? 'Request took longer than expected. The API may be slow — please retry.'
        : msg;
      setError(friendly);
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchServices();
  }, [fetchServices]);

  usePolling(fetchServices, POLLING_SLOW);

  // Get unique environments for filter options
  const availableEnvironments = useMemo(() => {
    const envSet = new Set(services.map((s) => s.environment));
    return Array.from(envSet).sort();
  }, [services]);

  // Filter and sort services
  const filteredServices = useMemo(() => {
    let result = [...services];

    // Apply search filter
    if (filters.search) {
      const searchLower = filters.search.toLowerCase();
      result = result.filter(
        (s) =>
          s.name.toLowerCase().includes(searchLower) ||
          s.project_name.toLowerCase().includes(searchLower)
      );
    }

    // Apply status filter
    if (filters.statuses.length > 0) {
      result = result.filter((s) => filters.statuses.includes(s.status));
    }

    // Apply environment filter
    if (filters.environments.length > 0) {
      result = result.filter((s) => filters.environments.includes(s.environment));
    }

    // Apply sorting
    result.sort((a, b) => {
      let comparison = 0;
      switch (sort.field) {
        case 'name':
          comparison = a.name.localeCompare(b.name);
          break;
        case 'status':
          comparison = a.status.localeCompare(b.status);
          break;
        case 'environment':
          comparison = a.environment.localeCompare(b.environment);
          break;
        case 'project':
          comparison = a.project_name.localeCompare(b.project_name);
          break;
      }
      return sort.order === 'asc' ? comparison : -comparison;
    });

    return result;
  }, [services, filters, sort]);

  if (loading) {
    return (
      <div className="container mx-auto py-8">
        <div className="mb-8">
          <h1 className="text-3xl font-bold">Services</h1>
          <p className="text-muted-foreground mt-2">
            Manage and monitor your deployed services
          </p>
        </div>
        <Card>
          <CardContent className="py-12">
            <div className="flex items-center justify-center">
              <Spinner size="lg" />
              <span className="ml-3 text-muted-foreground">Loading services...</span>
            </div>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (error) {
    return (
      <div className="container mx-auto py-8">
        <div className="mb-8">
          <h1 className="text-3xl font-bold">Services</h1>
          <p className="text-muted-foreground mt-2">
            Manage and monitor your deployed services
          </p>
        </div>
        <Card className="border-status-error/30 bg-status-error-muted">
          <CardContent className="py-8">
            <div className="text-center">
              <p className="text-status-error font-medium mb-4">{error}</p>
              <Button variant="outline" onClick={fetchServices}>
                <RefreshCw className="w-4 h-4 mr-2" />
                Try Again
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="container mx-auto py-8">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between mb-8 gap-4">
        <div>
          <h1 className="text-3xl font-bold text-foreground">Services</h1>
          <p className="text-muted-foreground mt-2">
            Manage and monitor your deployed services
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <Button asChild>
            <Link href="/services/import">
              <Github className="w-4 h-4 mr-2" />
              Import from GitHub
            </Link>
          </Button>
          <Button variant="outline" asChild>
            <Link href="/services/new">
              <Plus className="w-4 h-4 mr-2" />
              New Service
            </Link>
          </Button>
          <Button variant="outline" onClick={fetchServices}>
            <RefreshCw className="w-4 h-4 mr-2" />
            Refresh
          </Button>
        </div>
      </div>

      {partialFailures.length > 0 && (
        <Card className="border-status-warning/30 bg-status-warning-muted mb-4">
          <CardContent className="py-3">
            <div className="flex items-center justify-between gap-3 text-sm">
              <span className="text-status-warning">
                Could not load services for {partialFailures.length} project(s):{' '}
                <span className="font-mono">{partialFailures.join(', ')}</span>
              </span>
              <Button size="sm" variant="outline" onClick={fetchServices}>
                <RefreshCw className="w-3 h-3 mr-1" />
                Retry
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Search and Filter */}
      <ProjectSearch
        filters={filters}
        sort={sort}
        onFilterChange={setFilters}
        onSortChange={setSort}
        availableEnvironments={availableEnvironments}
      />

      <Card>
        <CardHeader>
          <CardTitle>Services Overview</CardTitle>
          <CardDescription>
            {filteredServices.length === services.length ? (
              <>View all services across your projects ({services.length} total)</>
            ) : (
              <>Showing {filteredServices.length} of {services.length} services</>
            )}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {services.length === 0 ? (
            <div className="text-center py-16 text-muted-foreground border border-dashed border-border rounded-lg">
              <Server className="w-10 h-10 mx-auto text-muted-foreground mb-3" />
              <p className="text-lg font-medium text-foreground">No services deployed yet</p>
              <p className="text-sm mt-1 mb-4">
                Create a project and deploy services to see them here
              </p>
              <Button asChild>
                <Link href="/services/new">
                  <Plus className="w-4 h-4 mr-2" />
                  New Service
                </Link>
              </Button>
            </div>
          ) : filteredServices.length === 0 ? (
            <div className="text-center py-12 text-muted-foreground border border-dashed border-border rounded-lg">
              <p className="text-lg text-foreground font-medium">No services match your filters</p>
              <p className="text-sm mt-2 mb-4">
                Try adjusting your search or filter criteria
              </p>
              <Button
                variant="outline"
                onClick={() => setFilters({ search: '', statuses: [], environments: [] })}
              >
                Clear Filters
              </Button>
            </div>
          ) : (
            <div className="overflow-x-auto rounded-md border border-border">
              <table className="min-w-full divide-y divide-border">
                <thead className="bg-muted/50">
                  <tr>
                    <th className="px-6 py-3 text-left text-xs font-medium text-muted-foreground uppercase tracking-wider">
                      Service
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium text-muted-foreground uppercase tracking-wider">
                      Project
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium text-muted-foreground uppercase tracking-wider">
                      Environment
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium text-muted-foreground uppercase tracking-wider">
                      Status
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium text-muted-foreground uppercase tracking-wider">
                      Version
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium text-muted-foreground uppercase tracking-wider">
                      Replicas
                    </th>
                  </tr>
                </thead>
                <tbody className="bg-card divide-y divide-border">
                  {filteredServices.map((service) => (
                    <tr key={service.id} className="hover:bg-muted/20 transition-colors">
                      <td className="px-6 py-4 whitespace-nowrap">
                        <Link
                          href={`/services/${service.id}`}
                          className="text-sm font-medium text-foreground hover:text-enclii-blue"
                        >
                          {service.name}
                        </Link>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <Link
                          href={`/projects/${service.project_slug || service.project_name.toLowerCase().replace(/\s+/g, '-')}`}
                          className="text-sm text-muted-foreground hover:text-enclii-blue"
                        >
                          {service.project_name}
                        </Link>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <span
                          className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${
                            service.environment === "production"
                              ? "bg-status-success/15 text-status-success"
                              : service.environment === "staging"
                                ? "bg-status-warning/15 text-status-warning"
                                : "bg-status-info/15 text-status-info"
                          }`}
                        >
                          {service.environment}
                        </span>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <span
                          className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${
                            service.status === "healthy"
                              ? "bg-status-success/15 text-status-success"
                              : service.status === "unhealthy"
                                ? "bg-status-error/15 text-status-error"
                                : "bg-muted text-muted-foreground"
                          }`}
                        >
                          {service.status}
                        </span>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap text-foreground font-mono text-xs">
                        {service.version || "N/A"}
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap text-sm text-foreground">
                        {service.replicas || "0/0"}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
