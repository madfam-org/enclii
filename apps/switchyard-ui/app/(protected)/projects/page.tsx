'use client';

import { useState, useEffect } from 'react';
import Link from 'next/link';
import { apiGet, apiPost } from '@/lib/api';
import { useTier } from '@/hooks/use-tier';
import { PricingModal } from '@/components/modals/PricingModal';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';

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
  status: string;
  health: string;
  last_deployment: string;
}

export default function ProjectsPage() {
  const [projects, setProjects] = useState<Project[]>([]);
  const [services, setServices] = useState<{ [key: string]: Service[] }>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showCreateForm, setShowCreateForm] = useState(false);
  const [newProject, setNewProject] = useState({
    name: '',
    slug: '',
    description: ''
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

  // Handler for Create Project button with tier check
  const handleCreateProjectClick = () => {
    if (!requireTier('project', { currentProjectCount: projects.length })) {
      return; // Modal will be shown automatically
    }
    setShowCreateForm(true);
  };

  const fetchProjects = async () => {
    try {
      const data = await apiGet<{ projects: Project[] }>('/v1/projects');
      setProjects(data.projects || []);

      // Fetch services for each project
      const servicesData: { [key: string]: Service[] } = {};
      for (const project of data.projects || []) {
        try {
          const servicesResult = await apiGet<{ services: Service[] }>(
            `/v1/projects/${project.slug}/services`
          );
          servicesData[project.id] = servicesResult.services || [];
        } catch (err) {
          console.error(`Failed to fetch services for project ${project.slug}:`, err);
        }
      }

      setServices(servicesData);
      setLoading(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'An error occurred');
      setLoading(false);
    }
  };

  const createProject = async (e: React.FormEvent) => {
    e.preventDefault();

    try {
      await apiPost('/v1/projects', newProject);

      setNewProject({ name: '', slug: '', description: '' });
      setShowCreateForm(false);
      fetchProjects(); // Refresh the list
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create project');
    }
  };

  useEffect(() => {
    fetchProjects();
  }, []);

  if (loading) {
    return (
      <div className="max-w-7xl mx-auto py-6 sm:px-6 lg:px-8">
        <div className="animate-pulse">
          <div className="h-8 bg-muted rounded w-1/4 mb-6"></div>
          <div className="space-y-4">
            {[1, 2, 3].map((i) => (
              <div key={i} className="h-24 bg-muted rounded"></div>
            ))}
          </div>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="max-w-7xl mx-auto py-6 sm:px-6 lg:px-8">
        <div className="bg-status-error-muted border border-status-error/30 rounded-md p-4">
          <div className="flex">
            <div className="text-status-error-foreground">
              <h3 className="text-sm font-medium">Error loading projects</h3>
              <div className="mt-2 text-sm">{error}</div>
            </div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-7xl mx-auto py-6 sm:px-6 lg:px-8">
      <div className="px-4 py-6 sm:px-0">
        <div className="flex items-center justify-between mb-8">
          <h1 className="text-3xl font-bold text-foreground">Projects</h1>
          <button
            onClick={handleCreateProjectClick}
            data-tour="create-project"
            className="inline-flex items-center px-4 py-2 border border-transparent text-sm font-medium rounded-md shadow-sm text-white bg-enclii-blue hover:bg-enclii-blue-dark focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-enclii-blue"
          >
            Create Project
          </button>
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
                <label className="block text-sm font-medium text-foreground mb-2">
                  Project Name
                </label>
                <input
                  type="text"
                  required
                  className="w-full px-3 py-2 border border-input rounded-md focus:outline-none focus:ring-2 focus:ring-enclii-blue"
                  value={newProject.name}
                  onChange={(e) => setNewProject({ ...newProject, name: e.target.value })}
                />
              </div>
              <div className="mb-4">
                <label className="block text-sm font-medium text-foreground mb-2">
                  Slug
                </label>
                <input
                  type="text"
                  required
                  className="w-full px-3 py-2 border border-input rounded-md focus:outline-none focus:ring-2 focus:ring-enclii-blue"
                  value={newProject.slug}
                  onChange={(e) => setNewProject({ ...newProject, slug: e.target.value })}
                />
              </div>
              <div className="mb-4">
                <label className="block text-sm font-medium text-foreground mb-2">
                  Description
                </label>
                <textarea
                  className="w-full px-3 py-2 border border-input rounded-md focus:outline-none focus:ring-2 focus:ring-enclii-blue"
                  rows={3}
                  value={newProject.description}
                  onChange={(e) => setNewProject({ ...newProject, description: e.target.value })}
                />
              </div>
              <DialogFooter>
                <button
                  type="button"
                  onClick={() => setShowCreateForm(false)}
                  className="px-4 py-2 text-sm font-medium bg-secondary text-secondary-foreground rounded-md hover:bg-secondary/80"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="px-4 py-2 text-sm font-medium text-white bg-enclii-blue rounded-md hover:bg-enclii-blue-dark"
                >
                  Create
                </button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>

        {/* Projects List */}
        <div className="space-y-6">
          {projects.length === 0 ? (
            <div className="text-center py-12">
              <div className="text-muted-foreground mb-4">
                <svg aria-hidden="true" className="mx-auto h-12 w-12" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
                </svg>
              </div>
              <h3 className="text-lg font-medium text-foreground">No projects found</h3>
              <p className="text-muted-foreground mt-1">Get started by creating your first project.</p>
            </div>
          ) : (
            projects.map((project) => (
              <div key={project.id} className="bg-card shadow overflow-hidden sm:rounded-lg">
                <div className="px-4 py-5 sm:p-6">
                  <div className="flex items-center justify-between">
                    <div className="flex-1">
                      <Link 
                        href={`/projects/${project.slug}`}
                        className="block hover:bg-accent transition-colors duration-150 -m-2 p-2 rounded"
                      >
                        <h3 className="text-lg font-medium text-foreground hover:text-enclii-blue">
                          {project.name}
                        </h3>
                        <p className="text-sm text-muted-foreground mt-1">{project.description}</p>
                        <div className="flex items-center mt-2 space-x-4 text-xs text-muted-foreground">
                          <span>Slug: {project.slug}</span>
                          <span>Created: {new Date(project.created_at).toLocaleDateString()}</span>
                        </div>
                      </Link>
                    </div>
                    <div className="flex-shrink-0 ml-4">
                      <div className="text-right">
                        <div className="text-sm font-medium text-foreground">
                          {services[project.id]?.length || 0} service{(services[project.id]?.length || 0) !== 1 ? 's' : ''}
                        </div>
                        <div className="flex space-x-1 mt-1">
                          {services[project.id]?.slice(0, 3).map((service) => (
                            <span
                              key={service.id}
                              className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${
                                service.health === 'healthy'
                                  ? 'bg-status-success-muted text-status-success-foreground'
                                  : service.health === 'unhealthy'
                                  ? 'bg-status-error-muted text-status-error-foreground'
                                  : 'bg-muted text-muted-foreground'
                              }`}
                            >
                              {service.name}
                            </span>
                          ))}
                          {services[project.id]?.length > 3 && (
                            <span className="text-xs text-muted-foreground">
                              +{services[project.id].length - 3} more
                            </span>
                          )}
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            ))
          )}
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