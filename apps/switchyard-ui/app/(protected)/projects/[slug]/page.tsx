'use client';

import { useState, useEffect } from 'react';
import Link from 'next/link';
import { useParams } from 'next/navigation';
import { apiGet, apiPost, apiPut } from '@/lib/api';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Switch } from '@/components/ui/switch';

interface Project {
  id: string;
  name: string;
  slug: string;
  description: string;
  ci_runner_mode: 'github' | 'self-hosted';
  created_at: string;
  updated_at: string;
}

interface Service {
  id: string;
  name: string;
  project_id: string;
  git_repo: string;
  build_config: any;
  created_at: string;
  updated_at: string;
}

interface Release {
  id: string;
  service_id: string;
  version: string;
  image_url: string;
  git_sha: string;
  status: string;
  build_id: string;
  created_at: string;
}

interface Deployment {
  id: string;
  service_id: string;
  release_id: string;
  status: string;
  environment: { [key: string]: string };
  replicas: number;
  created_at: string;
  updated_at: string;
}

export default function ProjectDetailPage() {
  const params = useParams();
  const slug = params?.slug as string;
  
  const [project, setProject] = useState<Project | null>(null);
  const [services, setServices] = useState<Service[]>([]);
  const [releases, setReleases] = useState<{ [key: string]: Release[] }>({});
  const [deployments, setDeployments] = useState<{ [key: string]: Deployment[] }>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showCreateServiceForm, setShowCreateServiceForm] = useState(false);
  const [newService, setNewService] = useState({
    name: '',
    git_repo: '',
    build_config: {}
  });
  const [ciRunnerToggling, setCiRunnerToggling] = useState(false);

  const toggleCIRunnerMode = async () => {
    if (!project || ciRunnerToggling) return;
    setCiRunnerToggling(true);
    try {
      const newMode = project.ci_runner_mode === 'self-hosted' ? 'github' : 'self-hosted';
      await apiPut(`/v1/projects/${slug}/ci-runner-config`, { mode: newMode });
      setProject({ ...project, ci_runner_mode: newMode });
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update CI runner mode');
    } finally {
      setCiRunnerToggling(false);
    }
  };

  const fetchProjectData = async () => {
    try {
      // Fetch project details
      const projectData = await apiGet<Project>(`/v1/projects/${slug}`);
      setProject(projectData);

      // Fetch services using the canonical slug from the API response
      const servicesData = await apiGet<{ services: Service[] }>(
        `/v1/projects/${projectData.slug}/services`
      );
      setServices(servicesData.services || []);

      // Fetch releases for each service
      const releasesData: { [key: string]: Release[] } = {};
      const deploymentsData: { [key: string]: Deployment[] } = {};

      for (const service of servicesData.services || []) {
        // Fetch releases
        try {
          const releasesResult = await apiGet<{ releases: Release[] }>(
            `/v1/services/${service.id}/releases`
          );
          releasesData[service.id] = releasesResult.releases || [];
        } catch (err) {
          console.error(`Failed to fetch releases for service ${service.name}:`, err);
        }
      }

      setReleases(releasesData);
      setDeployments(deploymentsData);
      setLoading(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'An error occurred');
      setLoading(false);
    }
  };

  const createService = async (e: React.FormEvent) => {
    e.preventDefault();

    try {
      await apiPost(`/v1/projects/${project?.slug || slug}/services`, newService);

      setNewService({ name: '', git_repo: '', build_config: {} });
      setShowCreateServiceForm(false);
      fetchProjectData(); // Refresh the data
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create service');
    }
  };

  const triggerBuild = async (serviceId: string, gitSha: string) => {
    try {
      await apiPost(`/v1/services/${serviceId}/build`, { git_sha: gitSha });

      fetchProjectData(); // Refresh to show new build
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to trigger build');
    }
  };

  const deployRelease = async (serviceId: string, releaseId: string) => {
    try {
      await apiPost(`/v1/services/${serviceId}/deploy`, {
        release_id: releaseId,
        environment: {},
        replicas: 1
      });

      fetchProjectData(); // Refresh to show deployment
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to deploy release');
    }
  };

  useEffect(() => {
    if (slug) {
      fetchProjectData();
    }
  }, [slug]);

  if (loading) {
    return (
      <div className="max-w-7xl mx-auto py-6 sm:px-6 lg:px-8">
        <div className="animate-pulse space-y-6">
          <div className="h-8 bg-muted rounded w-1/3"></div>
          <div className="h-24 bg-muted rounded"></div>
          <div className="space-y-4">
            {[1, 2].map((i) => (
              <div key={i} className="h-32 bg-muted rounded"></div>
            ))}
          </div>
        </div>
      </div>
    );
  }

  if (error || !project) {
    return (
      <div className="max-w-7xl mx-auto py-6 sm:px-6 lg:px-8">
        <div className="bg-status-error-muted border border-status-error/30 rounded-md p-4">
          <div className="flex">
            <div className="text-status-error-foreground">
              <h3 className="text-sm font-medium">Error loading project</h3>
              <div className="mt-2 text-sm">{error || 'Project not found'}</div>
            </div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-7xl mx-auto py-6 sm:px-6 lg:px-8">
      <div className="px-4 py-6 sm:px-0">
        {/* Breadcrumb */}
        <nav className="flex mb-4" aria-label="Breadcrumb">
          <ol className="flex items-center space-x-4">
            <li>
              <Link href="/projects" className="text-muted-foreground hover:text-foreground">
                Projects
              </Link>
            </li>
            <li>
              <div className="flex items-center">
                <svg aria-hidden="true" className="flex-shrink-0 h-5 w-5 text-muted-foreground/50" fill="currentColor" viewBox="0 0 20 20">
                  <path fillRule="evenodd" d="M7.293 14.707a1 1 0 010-1.414L10.586 10 7.293 6.707a1 1 0 011.414-1.414l4 4a1 1 0 010 1.414l-4 4a1 1 0 01-1.414 0z" clipRule="evenodd" />
                </svg>
                <span className="ml-4 text-sm font-medium text-muted-foreground">{project.name}</span>
              </div>
            </li>
          </ol>
        </nav>

        {/* Project Header */}
        <div className="flex items-center justify-between mb-8">
          <div>
            <h1 className="text-3xl font-bold text-foreground">{project.name}</h1>
            <p className="text-muted-foreground mt-2">{project.description}</p>
            <div className="flex items-center mt-2 space-x-4 text-sm text-muted-foreground">
              <span>Slug: {project.slug}</span>
              <span>Created: {new Date(project.created_at).toLocaleDateString()}</span>
            </div>
            <div className="flex items-center mt-3 space-x-3">
              <Switch
                checked={project.ci_runner_mode === 'self-hosted'}
                onCheckedChange={toggleCIRunnerMode}
                disabled={ciRunnerToggling}
              />
              <span className="text-sm text-muted-foreground">
                CI on {project.ci_runner_mode === 'self-hosted' ? 'Enclii (self-hosted)' : 'GitHub Actions'}
              </span>
              {project.ci_runner_mode === 'self-hosted' && (
                <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400">
                  VPS
                </span>
              )}
            </div>
          </div>
          <div className="flex items-center space-x-3">
            <Link
              href={`/projects/${slug}/webhooks`}
              className="inline-flex items-center px-4 py-2 border border-input text-sm font-medium rounded-md text-foreground bg-card hover:bg-accent"
            >
              <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9" />
              </svg>
              Webhooks
            </Link>
            <Link
              href={`/projects/${slug}/lifecycle-webhooks`}
              className="inline-flex items-center px-4 py-2 border border-input text-sm font-medium rounded-md text-foreground bg-card hover:bg-accent"
              title="Signed HTTPS webhooks for deploy/rollback/scale events"
            >
              <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 10V3L4 14h7v7l9-11h-7z" />
              </svg>
              Lifecycle
            </Link>
            <button
              onClick={() => setShowCreateServiceForm(true)}
              className="inline-flex items-center px-4 py-2 border border-transparent text-sm font-medium rounded-md shadow-sm text-white bg-enclii-blue hover:bg-enclii-blue-dark focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-enclii-blue"
            >
              Add Service
            </button>
          </div>
        </div>

        {/* Create Service Modal */}
        <Dialog open={showCreateServiceForm} onOpenChange={setShowCreateServiceForm}>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>Add Service</DialogTitle>
              <DialogDescription>
                Add a new service to this project.
              </DialogDescription>
            </DialogHeader>
            <form onSubmit={createService}>
              <div className="mb-4">
                <label className="block text-sm font-medium text-foreground mb-2">
                  Service Name
                </label>
                <input
                  type="text"
                  required
                  className="w-full px-3 py-2 border border-input rounded-md focus:outline-none focus:ring-2 focus:ring-enclii-blue"
                  value={newService.name}
                  onChange={(e) => setNewService({ ...newService, name: e.target.value })}
                />
              </div>
              <div className="mb-4">
                <label className="block text-sm font-medium text-foreground mb-2">
                  Git Repository
                </label>
                <input
                  type="url"
                  required
                  className="w-full px-3 py-2 border border-input rounded-md focus:outline-none focus:ring-2 focus:ring-enclii-blue"
                  value={newService.git_repo}
                  onChange={(e) => setNewService({ ...newService, git_repo: e.target.value })}
                  placeholder="https://github.com/user/repo"
                />
              </div>
              <DialogFooter>
                <button
                  type="button"
                  onClick={() => setShowCreateServiceForm(false)}
                  className="px-4 py-2 text-sm font-medium bg-secondary text-secondary-foreground rounded-md hover:bg-secondary/80"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="px-4 py-2 text-sm font-medium text-white bg-enclii-blue rounded-md hover:bg-enclii-blue-dark"
                >
                  Add Service
                </button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>

        {/* Services */}
        <div className="space-y-6">
          {services.length === 0 ? (
            <div className="text-center py-12 bg-card rounded-lg shadow">
              <div className="text-muted-foreground mb-4">
                <svg aria-hidden="true" className="mx-auto h-12 w-12" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
                </svg>
              </div>
              <h3 className="text-lg font-medium text-foreground">No services found</h3>
              <p className="text-muted-foreground mt-1">Add your first service to get started.</p>
            </div>
          ) : (
            services.map((service) => (
              <div key={service.id} className="bg-card shadow overflow-hidden sm:rounded-lg">
                <div className="px-4 py-5 sm:p-6">
                  <div className="flex items-center justify-between mb-4">
                    <div>
                      <h3 className="text-lg font-medium text-foreground">{service.name}</h3>
                      <a 
                        href={service.git_repo} 
                        target="_blank" 
                        rel="noopener noreferrer"
                        className="text-sm text-enclii-blue hover:text-enclii-blue-dark"
                      >
                        {service.git_repo}
                      </a>
                    </div>
                    <div className="flex space-x-2">
                      <button
                        onClick={() => triggerBuild(service.id, 'main')}
                        className="inline-flex items-center px-3 py-2 border border-input text-sm font-medium rounded-md text-foreground bg-card hover:bg-accent"
                      >
                        Build
                      </button>
                      <Link
                        href={`/services/${service.id}`}
                        className="inline-flex items-center px-3 py-2 border border-input text-sm font-medium rounded-md text-foreground bg-card hover:bg-accent"
                      >
                        View Details
                      </Link>
                    </div>
                  </div>

                  {/* Recent Releases */}
                  <div className="mt-4">
                    <h4 className="text-sm font-medium text-foreground mb-2">Recent Releases</h4>
                    {releases[service.id] && releases[service.id].length > 0 ? (
                      <div className="overflow-x-auto">
                        <table className="min-w-full divide-y divide-border">
                          <thead className="bg-muted/50">
                            <tr>
                              <th className="px-3 py-2 text-left text-xs font-medium text-muted-foreground uppercase tracking-wider">
                                Version
                              </th>
                              <th className="px-3 py-2 text-left text-xs font-medium text-muted-foreground uppercase tracking-wider">
                                Status
                              </th>
                              <th className="px-3 py-2 text-left text-xs font-medium text-muted-foreground uppercase tracking-wider">
                                Created
                              </th>
                              <th className="px-3 py-2 text-left text-xs font-medium text-muted-foreground uppercase tracking-wider">
                                Actions
                              </th>
                            </tr>
                          </thead>
                          <tbody className="bg-card divide-y divide-border">
                            {releases[service.id].slice(0, 3).map((release) => (
                              <tr key={release.id}>
                                <td className="px-3 py-2 whitespace-nowrap text-sm text-foreground">
                                  {release.version}
                                </td>
                                <td className="px-3 py-2 whitespace-nowrap">
                                  <span className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${
                                    release.status === 'ready'
                                      ? 'bg-status-success-muted text-status-success-foreground'
                                      : release.status === 'building'
                                      ? 'bg-status-warning-muted text-status-warning-foreground'
                                      : 'bg-status-error-muted text-status-error-foreground'
                                  }`}>
                                    {release.status}
                                  </span>
                                </td>
                                <td className="px-3 py-2 whitespace-nowrap text-sm text-muted-foreground">
                                  {new Date(release.created_at).toLocaleDateString()}
                                </td>
                                <td className="px-3 py-2 whitespace-nowrap text-sm text-muted-foreground">
                                  {release.status === 'ready' && (
                                    <button
                                      onClick={() => deployRelease(service.id, release.id)}
                                      className="text-enclii-blue hover:text-enclii-blue-dark font-medium"
                                    >
                                      Deploy
                                    </button>
                                  )}
                                </td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                    ) : (
                      <p className="text-sm text-muted-foreground">No releases yet. Build the service to create your first release.</p>
                    )}
                  </div>
                </div>
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  );
}