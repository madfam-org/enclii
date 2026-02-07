'use client';

import { useState, useEffect, Suspense } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { apiGet, apiPost } from "@/lib/api";
import { Spinner } from "@/components/ui/spinner";
import type { Project, ProjectsResponse, Service } from "@/lib/types";
import { useTier } from "@/hooks/use-tier";
import { PricingModal } from "@/components/modals/PricingModal";

// Icons as SVG components
const GithubIcon = () => (
  <svg aria-hidden="true" className="h-5 w-5" viewBox="0 0 24 24" fill="currentColor">
    <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"/>
  </svg>
);

const ChevronLeftIcon = () => (
  <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
  </svg>
);

const RocketIcon = () => (
  <svg aria-hidden="true" className="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 10V3L4 14h7v7l9-11h-7z" />
  </svg>
);

const CubeIcon = () => (
  <svg aria-hidden="true" className="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4" />
  </svg>
);

function CreateServiceContent() {
  const router = useRouter();
  const searchParams = useSearchParams();

  // Get repo info from URL params (from import page)
  const repoUrl = searchParams.get('repo') || '';
  const suggestedName = searchParams.get('name') || '';
  const defaultBranch = searchParams.get('branch') || 'main';

  // Form state
  const [serviceName, setServiceName] = useState(suggestedName);
  const [selectedProject, setSelectedProject] = useState<string>('');
  const [buildType, setBuildType] = useState<'buildpack' | 'dockerfile'>('buildpack');
  const [port, setPort] = useState('8080');
  const [branch, setBranch] = useState(defaultBranch);

  // UI state
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [serviceCount, setServiceCount] = useState(0);

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

  // Load projects and service count on mount
  useEffect(() => {
    const fetchProjectsAndServices = async () => {
      try {
        const response = await apiGet<ProjectsResponse>('/v1/projects');
        const projectList = response.projects || [];
        setProjects(projectList);
        // Auto-select first project if available
        if (projectList.length > 0 && !selectedProject) {
          setSelectedProject(projectList[0].slug);
        }

        // Count total services across all projects for tier limit check
        let totalServices = 0;
        for (const project of projectList) {
          try {
            const servicesRes = await apiGet<{ services: Service[] }>(`/v1/projects/${project.slug}/services`);
            totalServices += (servicesRes.services || []).length;
          } catch {
            // Ignore errors for individual projects
          }
        }
        setServiceCount(totalServices);
        setLoading(false);
      } catch (err) {
        console.error("Failed to fetch projects:", err);
        setError("Failed to load projects");
        setLoading(false);
      }
    };
    fetchProjectsAndServices();
  }, []);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    // Tier check before creating service
    if (!requireTier('deploy', { currentServiceCount: serviceCount })) {
      return; // Modal will be shown automatically
    }

    if (!serviceName.trim()) {
      setError("Service name is required");
      return;
    }

    if (!selectedProject) {
      setError("Please select a project");
      return;
    }

    setSubmitting(true);
    setError(null);

    try {
      // Create the service
      const service = await apiPost<Service>(`/v1/projects/${selectedProject}/services`, {
        name: serviceName.trim(),
        git_repo: repoUrl,
        git_branch: branch,
        build_config: {
          type: buildType,
          port: parseInt(port, 10),
        },
      });

      // Navigate to the new service page
      router.push(`/services/${service.id}`);
    } catch (err) {
      console.error("Failed to create service:", err);
      setError(err instanceof Error ? err.message : "Failed to create service");
      setSubmitting(false);
    }
  };

  // Extract repo name from URL for display
  const repoDisplayName = repoUrl.split('/').slice(-2).join('/').replace('.git', '');

  return (
    <div className="container mx-auto py-8 max-w-2xl">
      {/* Header */}
      <div className="mb-8">
        <Link href={repoUrl ? "/services/import" : "/services"} className="text-primary hover:text-primary/80 text-sm mb-2 inline-flex items-center gap-1">
          <ChevronLeftIcon />
          {repoUrl ? "Back to Repository Selection" : "Back to Services"}
        </Link>
        <h1 className="text-3xl font-bold flex items-center gap-3 mt-2">
          <RocketIcon />
          Create New Service
        </h1>
        <p className="text-muted-foreground mt-2">
          Configure and deploy your service
        </p>
      </div>

      {/* Repository Info Card (if from import) */}
      {repoUrl && (
        <Card className="mb-6 bg-muted/50 border-border">
          <CardContent className="py-4">
            <div className="flex items-center gap-3">
              <GithubIcon />
              <div>
                <p className="font-medium">{repoDisplayName}</p>
                <p className="text-sm text-muted-foreground">Branch: {branch}</p>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Main Form Card */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <CubeIcon />
            Service Configuration
          </CardTitle>
          <CardDescription>
            Set up your service details and build configuration
          </CardDescription>
        </CardHeader>
        <CardContent>
          {error && (
            <div className="mb-6 p-3 bg-status-error-muted border border-status-error/30 rounded-md text-status-error-foreground text-sm">
              {error}
            </div>
          )}

          <form onSubmit={handleSubmit} className="space-y-6">
            {/* Service Name */}
            <div className="space-y-2">
              <Label htmlFor="serviceName">Service Name</Label>
              <Input
                id="serviceName"
                value={serviceName}
                onChange={(e) => setServiceName(e.target.value)}
                placeholder="my-service"
                required
              />
              <p className="text-xs text-muted-foreground">
                Lowercase letters, numbers, and hyphens only
              </p>
            </div>

            {/* Project Selection */}
            <div className="space-y-2">
              <Label htmlFor="project">Project</Label>
              {loading ? (
                <div className="h-10 bg-muted animate-pulse rounded-md" />
              ) : projects.length === 0 ? (
                <div className="text-sm text-muted-foreground p-3 bg-status-warning-muted border border-status-warning/30 rounded-md">
                  No projects found. <Link href="/projects/new" className="text-primary hover:underline">Create a project first</Link>.
                </div>
              ) : (
                <select
                  id="project"
                  value={selectedProject}
                  onChange={(e) => setSelectedProject(e.target.value)}
                  className="w-full h-10 px-3 rounded-md border border-input bg-background text-sm"
                  required
                >
                  <option value="">Select a project</option>
                  {projects.map((project) => (
                    <option key={project.id} value={project.slug}>
                      {project.name}
                    </option>
                  ))}
                </select>
              )}
            </div>

            {/* Git Repository (if from import) */}
            {repoUrl && (
              <div className="space-y-2">
                <Label htmlFor="gitRepo">Git Repository</Label>
                <Input
                  id="gitRepo"
                  value={repoUrl}
                  disabled
                  className="bg-muted/50"
                />
              </div>
            )}

            {/* Branch */}
            <div className="space-y-2">
              <Label htmlFor="branch">Branch</Label>
              <Input
                id="branch"
                value={branch}
                onChange={(e) => setBranch(e.target.value)}
                placeholder="main"
              />
            </div>

            {/* Build Type */}
            <div className="space-y-2">
              <Label>Build Type</Label>
              <div className="flex gap-4">
                <label className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="radio"
                    name="buildType"
                    value="buildpack"
                    checked={buildType === 'buildpack'}
                    onChange={(e) => setBuildType('buildpack')}
                    className="w-4 h-4"
                  />
                  <span className="text-sm">Buildpack</span>
                  <Badge variant="secondary" className="text-xs">Recommended</Badge>
                </label>
                <label className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="radio"
                    name="buildType"
                    value="dockerfile"
                    checked={buildType === 'dockerfile'}
                    onChange={(e) => setBuildType('dockerfile')}
                    className="w-4 h-4"
                  />
                  <span className="text-sm">Dockerfile</span>
                </label>
              </div>
              <p className="text-xs text-muted-foreground">
                {buildType === 'buildpack'
                  ? "Auto-detect language and build with Cloud Native Buildpacks"
                  : "Build using your Dockerfile in the repository root"}
              </p>
            </div>

            {/* Port */}
            <div className="space-y-2">
              <Label htmlFor="port">Port</Label>
              <Input
                id="port"
                type="number"
                value={port}
                onChange={(e) => setPort(e.target.value)}
                placeholder="8080"
                min="1"
                max="65535"
              />
              <p className="text-xs text-muted-foreground">
                The port your application listens on
              </p>
            </div>

            {/* Submit Button */}
            <div className="flex gap-3 pt-4">
              <Button
                type="submit"
                disabled={submitting || loading || projects.length === 0}
                className="flex-1"
              >
                {submitting ? (
                  <>
                    <Spinner size="sm" className="-ml-1 mr-2" />
                    Creating Service...
                  </>
                ) : (
                  <>
                    <RocketIcon />
                    <span className="ml-2">Create & Deploy</span>
                  </>
                )}
              </Button>
              <Button
                type="button"
                variant="outline"
                onClick={() => router.back()}
              >
                Cancel
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>

      {/* What happens next */}
      <Card className="mt-6">
        <CardHeader>
          <CardTitle className="text-sm font-medium text-muted-foreground">What happens next?</CardTitle>
        </CardHeader>
        <CardContent>
          <ol className="text-sm space-y-2 text-muted-foreground">
            <li className="flex items-start gap-2">
              <span className="flex-shrink-0 w-5 h-5 rounded-full bg-primary/10 text-primary flex items-center justify-center text-xs font-medium">1</span>
              <span>Clone your repository</span>
            </li>
            <li className="flex items-start gap-2">
              <span className="flex-shrink-0 w-5 h-5 rounded-full bg-primary/10 text-primary flex items-center justify-center text-xs font-medium">2</span>
              <span>Build container image using {buildType === 'buildpack' ? 'Cloud Native Buildpacks' : 'your Dockerfile'}</span>
            </li>
            <li className="flex items-start gap-2">
              <span className="flex-shrink-0 w-5 h-5 rounded-full bg-primary/10 text-primary flex items-center justify-center text-xs font-medium">3</span>
              <span>Deploy to Kubernetes cluster</span>
            </li>
            <li className="flex items-start gap-2">
              <span className="flex-shrink-0 w-5 h-5 rounded-full bg-primary/10 text-primary flex items-center justify-center text-xs font-medium">4</span>
              <span>Assign URL and configure routing</span>
            </li>
          </ol>
        </CardContent>
      </Card>

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

// Wrap in Suspense for useSearchParams
export default function CreateServicePage() {
  return (
    <Suspense fallback={
      <div className="container mx-auto py-8 max-w-2xl">
        <div className="animate-pulse">
          <div className="h-8 bg-muted rounded w-1/3 mb-4" />
          <div className="h-4 bg-muted rounded w-1/2 mb-8" />
          <div className="h-64 bg-muted rounded" />
        </div>
      </div>
    }>
      <CreateServiceContent />
    </Suspense>
  );
}
