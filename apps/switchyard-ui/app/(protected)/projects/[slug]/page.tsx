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
} from "@enclii/ui-components/dialog";
import { Switch } from '@/components/ui/switch';
import { Button } from "@enclii/ui-components/button";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { toast } from 'sonner';
import { ChevronRight, Rocket, Webhook, ArrowRightLeft, Play, Github, Loader2, Plus } from 'lucide-react';
import { HealthBadge } from '@/components/dashboard/health-badge';
import { SentryErrorBadge } from '@/components/dashboard/sentry-error-badge';
import { formatRelativeTime } from '@/lib/formatting';

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
  const [isBuilding, setIsBuilding] = useState<Record<string, boolean>>({});
  const [isDeploying, setIsDeploying] = useState<Record<string, boolean>>({});

  const toggleCIRunnerMode = async () => {
    if (!project || ciRunnerToggling) return;
    setCiRunnerToggling(true);
    try {
      const newMode = project.ci_runner_mode === 'self-hosted' ? 'github' : 'self-hosted';
      await apiPut(`/v1/projects/${slug}/ci-runner-config`, { mode: newMode });
      setProject({ ...project, ci_runner_mode: newMode });
      toast.success(`CI runner mode updated to ${newMode}`);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to update CI runner mode');
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
      toast.success("Service created successfully");
      fetchProjectData(); // Refresh the data
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to create service');
    }
  };

  const triggerBuild = async (serviceId: string, gitSha: string) => {
    try {
      setIsBuilding(prev => ({ ...prev, [serviceId]: true }));
      await apiPost(`/v1/services/${serviceId}/build`, { git_sha: gitSha });
      toast.success("Build triggered successfully");
      fetchProjectData(); // Refresh to show new build
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to trigger build');
    } finally {
      setIsBuilding(prev => ({ ...prev, [serviceId]: false }));
    }
  };

  const deployRelease = async (serviceId: string, releaseId: string) => {
    try {
      setIsDeploying(prev => ({ ...prev, [releaseId]: true }));
      await apiPost(`/v1/services/${serviceId}/deploy`, {
        release_id: releaseId,
        environment: {},
        replicas: 1
      });
      toast.success("Deployment triggered successfully");
      fetchProjectData(); // Refresh to show deployment
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to deploy release');
    } finally {
      setIsDeploying(prev => ({ ...prev, [releaseId]: false }));
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
          <ol className="flex items-center space-x-2 text-sm text-muted-foreground">
            <li>
              <Link href="/projects" className="hover:text-foreground transition-colors">
                Projects
              </Link>
            </li>
            <ChevronRight className="h-4 w-4" />
            <li>
              <span className="font-medium text-foreground">{project.name}</span>
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
              {ciRunnerToggling && <Loader2 className="w-3 h-3 animate-spin text-muted-foreground" />}
            </div>
          </div>
          <div className="flex items-center space-x-3">
            <Button variant="outline" asChild>
              <Link href={`/projects/${slug}/webhooks`}>
                <Webhook className="w-4 h-4 mr-2" />
                Webhooks
              </Link>
            </Button>
            <Button variant="outline" asChild title="Signed HTTPS webhooks for deploy/rollback/scale events">
              <Link href={`/projects/${slug}/lifecycle-webhooks`}>
                <ArrowRightLeft className="w-4 h-4 mr-2" />
                Lifecycle
              </Link>
            </Button>
            <Button onClick={() => setShowCreateServiceForm(true)}>
              <Plus className="w-4 h-4 mr-2" />
              Add Service
            </Button>
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
                  className="w-full px-3 py-2 border border-input rounded-md focus:outline-none focus:ring-2 focus:ring-enclii-blue bg-background"
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
                  className="w-full px-3 py-2 border border-input rounded-md focus:outline-none focus:ring-2 focus:ring-enclii-blue bg-background"
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
            <div className="border-border rounded-lg border border-dashed py-16 text-center">
              <Rocket className="text-muted-foreground mx-auto mb-3 h-10 w-10" />
              <h3 className="text-lg font-medium text-foreground">No services found</h3>
              <p className="text-muted-foreground mt-1 mb-4">Add your first service to get started.</p>
              <Button onClick={() => setShowCreateServiceForm(true)}>
                <Plus className="w-4 h-4 mr-2" />
                Add Service
              </Button>
            </div>
          ) : (
            services.map((service) => (
              <Card key={service.id} className="overflow-hidden">
                <CardHeader className="pb-4">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-4">
                      <CardTitle className="text-xl">{service.name}</CardTitle>
                      <div className="flex items-center gap-1.5">
                        <HealthBadge serviceId={service.id} serviceName={service.name} />
                        <SentryErrorBadge serviceId={service.id} serviceName={service.name} />
                      </div>
                    </div>
                    <div className="flex space-x-2">
                      <Button 
                        variant="outline" 
                        size="sm"
                        onClick={() => triggerBuild(service.id, 'main')}
                        disabled={isBuilding[service.id]}
                      >
                        {isBuilding[service.id] ? <Loader2 className="h-4 w-4 mr-2 animate-spin" /> : <Play className="h-4 w-4 mr-2" />}
                        Build
                      </Button>
                      <Button variant="outline" size="sm" asChild>
                        <Link href={`/services/${service.id}`}>
                          View Details
                        </Link>
                      </Button>
                    </div>
                  </div>
                  <div className="mt-1 flex items-center text-sm text-muted-foreground">
                    <Github className="h-3.5 w-3.5 mr-1.5" />
                    <a 
                      href={service.git_repo.startsWith('http') ? service.git_repo : `https://github.com/${service.git_repo}`} 
                      target="_blank" 
                      rel="noopener noreferrer"
                      className="hover:text-foreground transition-colors truncate max-w-sm"
                    >
                      {service.git_repo.replace(/^https?:\/\/github\.com\//, '')}
                    </a>
                  </div>
                </CardHeader>

                {/* Recent Releases */}
                <CardContent>
                  <h4 className="text-sm font-medium text-foreground mb-3">Recent Releases</h4>
                  {releases[service.id] && releases[service.id].length > 0 ? (
                    <div className="overflow-x-auto rounded-md border border-border">
                      <table className="min-w-full divide-y divide-border text-sm">
                        <thead className="bg-muted/50">
                          <tr>
                            <th className="px-4 py-3 text-left font-medium text-muted-foreground">
                              Version
                            </th>
                            <th className="px-4 py-3 text-left font-medium text-muted-foreground">
                              Status
                            </th>
                            <th className="px-4 py-3 text-left font-medium text-muted-foreground">
                              Created
                            </th>
                            <th className="px-4 py-3 text-left font-medium text-muted-foreground">
                              Actions
                            </th>
                          </tr>
                        </thead>
                        <tbody className="bg-card divide-y divide-border">
                          {releases[service.id].slice(0, 3).map((release) => (
                            <tr key={release.id} className="hover:bg-muted/20 transition-colors">
                              <td className="px-4 py-3 whitespace-nowrap text-foreground font-mono text-xs">
                                {release.version}
                              </td>
                              <td className="px-4 py-3 whitespace-nowrap">
                                <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${
                                  release.status === 'ready'
                                    ? 'bg-status-success/15 text-status-success'
                                    : release.status === 'building'
                                    ? 'bg-status-warning/15 text-status-warning'
                                    : 'bg-status-error/15 text-status-error'
                                }`}>
                                  {release.status}
                                </span>
                              </td>
                              <td className="px-4 py-3 whitespace-nowrap text-muted-foreground">
                                {formatRelativeTime(release.created_at)}
                              </td>
                              <td className="px-4 py-3 whitespace-nowrap">
                                {release.status === 'ready' && (
                                  <Button
                                    variant="ghost"
                                    size="sm"
                                    onClick={() => deployRelease(service.id, release.id)}
                                    disabled={isDeploying[release.id]}
                                  >
                                    {isDeploying[release.id] ? <Loader2 className="h-4 w-4 mr-2 animate-spin" /> : null}
                                    Deploy
                                  </Button>
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
                </CardContent>
              </Card>
            ))
          )}
        </div>
      </div>
    </div>
  );
}