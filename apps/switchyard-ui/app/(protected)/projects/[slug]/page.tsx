'use client';

import { useState, useEffect } from 'react';
import Link from 'next/link';
import { useParams } from 'next/navigation';
import { apiGet, apiPost } from '@/lib/api';

interface Project {
  id: string;
  name: string;
  slug: string;
  description: string;
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
          <div className="h-8 bg-gray-200 rounded w-1/3"></div>
          <div className="h-24 bg-gray-200 rounded"></div>
          <div className="space-y-4">
            {[1, 2].map((i) => (
              <div key={i} className="h-32 bg-gray-200 rounded"></div>
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
              <Link href="/projects" className="text-gray-400 hover:text-gray-500">
                Projects
              </Link>
            </li>
            <li>
              <div className="flex items-center">
                <svg className="flex-shrink-0 h-5 w-5 text-gray-300" fill="currentColor" viewBox="0 0 20 20">
                  <path fillRule="evenodd" d="M7.293 14.707a1 1 0 010-1.414L10.586 10 7.293 6.707a1 1 0 011.414-1.414l4 4a1 1 0 010 1.414l-4 4a1 1 0 01-1.414 0z" clipRule="evenodd" />
                </svg>
                <span className="ml-4 text-sm font-medium text-gray-500">{project.name}</span>
              </div>
            </li>
          </ol>
        </nav>

        {/* Project Header */}
        <div className="flex items-center justify-between mb-8">
          <div>
            <h1 className="text-3xl font-bold text-gray-900">{project.name}</h1>
            <p className="text-gray-600 mt-2">{project.description}</p>
            <div className="flex items-center mt-2 space-x-4 text-sm text-gray-500">
              <span>Slug: {project.slug}</span>
              <span>Created: {new Date(project.created_at).toLocaleDateString()}</span>
            </div>
          </div>
          <div className="flex items-center space-x-3">
            <Link
              href={`/projects/${slug}/webhooks`}
              className="inline-flex items-center px-4 py-2 border border-gray-300 text-sm font-medium rounded-md text-gray-700 bg-white hover:bg-gray-50"
            >
              <svg className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9" />
              </svg>
              Webhooks
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
        {showCreateServiceForm && (
          <div className="fixed inset-0 bg-gray-600 bg-opacity-50 overflow-y-auto h-full w-full z-50">
            <div className="relative top-20 mx-auto p-5 border w-96 shadow-lg rounded-md bg-white">
              <div className="mt-3">
                <h3 className="text-lg font-medium text-gray-900 mb-4">Add Service</h3>
                <form onSubmit={createService}>
                  <div className="mb-4">
                    <label className="block text-sm font-medium text-gray-700 mb-2">
                      Service Name
                    </label>
                    <input
                      type="text"
                      required
                      className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-enclii-blue"
                      value={newService.name}
                      onChange={(e) => setNewService({ ...newService, name: e.target.value })}
                    />
                  </div>
                  <div className="mb-4">
                    <label className="block text-sm font-medium text-gray-700 mb-2">
                      Git Repository
                    </label>
                    <input
                      type="url"
                      required
                      className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-enclii-blue"
                      value={newService.git_repo}
                      onChange={(e) => setNewService({ ...newService, git_repo: e.target.value })}
                      placeholder="https://github.com/user/repo"
                    />
                  </div>
                  <div className="flex justify-end space-x-2">
                    <button
                      type="button"
                      onClick={() => setShowCreateServiceForm(false)}
                      className="px-4 py-2 text-sm font-medium text-gray-700 bg-gray-200 rounded-md hover:bg-gray-300"
                    >
                      Cancel
                    </button>
                    <button
                      type="submit"
                      className="px-4 py-2 text-sm font-medium text-white bg-enclii-blue rounded-md hover:bg-enclii-blue-dark"
                    >
                      Add Service
                    </button>
                  </div>
                </form>
              </div>
            </div>
          </div>
        )}

        {/* Services */}
        <div className="space-y-6">
          {services.length === 0 ? (
            <div className="text-center py-12 bg-white rounded-lg shadow">
              <div className="text-gray-500 mb-4">
                <svg className="mx-auto h-12 w-12" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
                </svg>
              </div>
              <h3 className="text-lg font-medium text-gray-900">No services found</h3>
              <p className="text-gray-500 mt-1">Add your first service to get started.</p>
            </div>
          ) : (
            services.map((service) => (
              <div key={service.id} className="bg-white shadow overflow-hidden sm:rounded-lg">
                <div className="px-4 py-5 sm:p-6">
                  <div className="flex items-center justify-between mb-4">
                    <div>
                      <h3 className="text-lg font-medium text-gray-900">{service.name}</h3>
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
                        className="inline-flex items-center px-3 py-2 border border-gray-300 text-sm font-medium rounded-md text-gray-700 bg-white hover:bg-gray-50"
                      >
                        Build
                      </button>
                      <Link
                        href={`/services/${service.id}`}
                        className="inline-flex items-center px-3 py-2 border border-gray-300 text-sm font-medium rounded-md text-gray-700 bg-white hover:bg-gray-50"
                      >
                        View Details
                      </Link>
                    </div>
                  </div>

                  {/* Recent Releases */}
                  <div className="mt-4">
                    <h4 className="text-sm font-medium text-gray-900 mb-2">Recent Releases</h4>
                    {releases[service.id] && releases[service.id].length > 0 ? (
                      <div className="overflow-x-auto">
                        <table className="min-w-full divide-y divide-gray-200">
                          <thead className="bg-gray-50">
                            <tr>
                              <th className="px-3 py-2 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                                Version
                              </th>
                              <th className="px-3 py-2 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                                Status
                              </th>
                              <th className="px-3 py-2 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                                Created
                              </th>
                              <th className="px-3 py-2 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                                Actions
                              </th>
                            </tr>
                          </thead>
                          <tbody className="bg-white divide-y divide-gray-200">
                            {releases[service.id].slice(0, 3).map((release) => (
                              <tr key={release.id}>
                                <td className="px-3 py-2 whitespace-nowrap text-sm text-gray-900">
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
                                <td className="px-3 py-2 whitespace-nowrap text-sm text-gray-500">
                                  {new Date(release.created_at).toLocaleDateString()}
                                </td>
                                <td className="px-3 py-2 whitespace-nowrap text-sm text-gray-500">
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
                      <p className="text-sm text-gray-500">No releases yet. Build the service to create your first release.</p>
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