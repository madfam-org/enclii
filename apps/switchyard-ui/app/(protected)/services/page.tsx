'use client';

import { useState, useEffect, useMemo } from "react";
import { usePolling } from "@/hooks/use-polling";
import { POLLING_SLOW } from "@/lib/constants";
import Link from "next/link";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { ProjectSearch, FilterState, SortState, ServiceStatus } from "@/components/search/project-search";
import { apiGet } from "@/lib/api";
import { Spinner } from "@/components/ui/spinner";

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

interface DashboardResponse {
  stats: any;
  activities: any[];
  services: ServiceOverview[];
}

export default function ServicesPage() {
  const [services, setServices] = useState<ServiceOverview[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

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

  const fetchServices = async () => {
    try {
      setError(null);
      const data = await apiGet<DashboardResponse>(`/v1/dashboard/stats`);
      setServices(data.services || []);
      setLoading(false);
    } catch (err) {
      console.error("Failed to fetch services:", err);
      setError(err instanceof Error ? err.message : "Failed to fetch services");
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchServices();
  }, []);

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
              <button
                onClick={fetchServices}
                className="inline-flex items-center px-4 py-2 border border-status-error/30 rounded-md shadow-sm text-sm font-medium text-status-error-foreground bg-card hover:bg-status-error-muted"
              >
                Try Again
              </button>
            </div>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="container mx-auto py-8">
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-3xl font-bold">Services</h1>
          <p className="text-muted-foreground mt-2">
            Manage and monitor your deployed services
          </p>
        </div>
        <div className="flex items-center gap-3">
          <Link
            href="/services/import"
            className="inline-flex items-center px-4 py-2 border border-transparent rounded-md shadow-sm text-sm font-medium text-white bg-primary hover:bg-primary/90"
          >
            <svg aria-hidden="true" className="w-4 h-4 mr-2" viewBox="0 0 24 24" fill="currentColor">
              <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"/>
            </svg>
            Import from GitHub
          </Link>
          <Link
            href="/services/new"
            className="inline-flex items-center px-4 py-2 border border-input rounded-md shadow-sm text-sm font-medium text-foreground bg-card hover:bg-accent"
          >
            <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
            </svg>
            New Service
          </Link>
          <button
            onClick={fetchServices}
            className="inline-flex items-center px-4 py-2 border border-input rounded-md shadow-sm text-sm font-medium text-foreground bg-card hover:bg-accent"
          >
            <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
            Refresh
          </button>
        </div>
      </div>

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
            <div className="text-center py-12 text-muted-foreground">
              <p className="text-lg">No services deployed yet</p>
              <p className="text-sm mt-2">
                Create a project and deploy services to see them here
              </p>
            </div>
          ) : filteredServices.length === 0 ? (
            <div className="text-center py-12 text-muted-foreground">
              <p className="text-lg">No services match your filters</p>
              <p className="text-sm mt-2">
                Try adjusting your search or filter criteria
              </p>
              <button
                onClick={() => setFilters({ search: '', statuses: [], environments: [] })}
                className="mt-4 inline-flex items-center px-4 py-2 border border-input rounded-md shadow-sm text-sm font-medium text-foreground bg-card hover:bg-accent"
              >
                Clear Filters
              </button>
            </div>
          ) : (
            <div className="overflow-x-auto">
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
                    <tr key={service.id} className="hover:bg-muted/50">
                      <td className="px-6 py-4 whitespace-nowrap">
                        <Link
                          href={`/services/${service.id}`}
                          className="text-sm font-medium text-foreground hover:text-primary"
                        >
                          {service.name}
                        </Link>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <Link
                          href={`/projects/${service.project_slug || service.project_name.toLowerCase().replace(/\s+/g, '-')}`}
                          className="text-sm text-muted-foreground hover:text-primary"
                        >
                          {service.project_name}
                        </Link>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <span
                          className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${
                            service.environment === "production"
                              ? "bg-status-success-muted text-status-success-foreground"
                              : service.environment === "staging"
                                ? "bg-status-warning-muted text-status-warning-foreground"
                                : "bg-status-info-muted text-status-info-foreground"
                          }`}
                        >
                          {service.environment}
                        </span>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <span
                          className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${
                            service.status === "healthy"
                              ? "bg-status-success-muted text-status-success-foreground"
                              : service.status === "unhealthy"
                                ? "bg-status-error-muted text-status-error-foreground"
                                : "bg-muted text-muted-foreground"
                          }`}
                        >
                          {service.status}
                        </span>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap text-sm text-foreground">
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
