'use client';

import { useState, useEffect, use } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@enclii/ui-components/button";
import { Badge } from "@enclii/ui-components/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@enclii/ui-components/select";
import { apiGet, apiPost } from "@/lib/api";
import { Spinner } from "@/components/ui/spinner";
import type { AnalysisResult, DetectedService, GitHubBranch, BranchesResponse, Project, ProjectsResponse } from "@/lib/types";

// Icons
const GithubIcon = () => (
  <svg aria-hidden="true" className="h-5 w-5" viewBox="0 0 24 24" fill="currentColor">
    <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"/>
  </svg>
);

const FolderIcon = () => (
  <svg aria-hidden="true" className="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" />
  </svg>
);

const CheckCircleIcon = () => (
  <svg aria-hidden="true" className="h-5 w-5 text-status-success" fill="none" stroke="currentColor" viewBox="0 0 24 24">
    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
  </svg>
);

const CodeIcon = () => (
  <svg aria-hidden="true" className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4" />
  </svg>
);

const ServerIcon = () => (
  <svg aria-hidden="true" className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2" />
  </svg>
);

// Framework and runtime colors
const runtimeColors: Record<string, string> = {
  nodejs: "bg-green-100 text-green-800",
  python: "bg-primary/10 text-primary",
  go: "bg-cyan-100 text-cyan-800",
  rust: "bg-orange-100 text-orange-800",
  docker: "bg-sky-100 text-sky-800",
  static: "bg-muted text-foreground",
};

const frameworkColors: Record<string, string> = {
  nextjs: "bg-black text-white",
  remix: "bg-indigo-100 text-indigo-800",
  express: "bg-muted text-foreground",
  fastapi: "bg-teal-100 text-teal-800",
  flask: "bg-red-100 text-red-800",
  django: "bg-green-100 text-green-800",
  gin: "bg-cyan-100 text-cyan-800",
  fiber: "bg-primary/10 text-primary",
  actix: "bg-orange-100 text-orange-800",
};

interface PageProps {
  params: Promise<{
    owner: string;
    repo: string;
  }>;
}

export default function AnalyzeRepositoryPage({ params }: PageProps) {
  const { owner, repo } = use(params);
  const router = useRouter();
  const searchParams = useSearchParams();
  const initialBranch = searchParams.get("branch") || "main";

  const [analysis, setAnalysis] = useState<AnalysisResult | null>(null);
  const [branches, setBranches] = useState<GitHubBranch[]>([]);
  const [selectedBranch, setSelectedBranch] = useState(initialBranch);
  const [selectedServices, setSelectedServices] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(true);
  const [analyzing, setAnalyzing] = useState(false);
  const [importing, setImporting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedProject, setSelectedProject] = useState<string>("");

  // Define analyzeRepository before useEffect to avoid TDZ
  const analyzeRepository = async (branch: string) => {
    setAnalyzing(true);
    setError(null);
    try {
      const result = await apiPost<AnalysisResult>(
        `/v1/integrations/github/repos/${owner}/${repo}/analyze`,
        { branch }
      );
      setAnalysis(result);

      // Auto-select all services with high confidence
      const highConfidenceServices = result.services
        .filter(s => s.confidence >= 0.7)
        .map(s => s.app_path);
      setSelectedServices(new Set(highConfidenceServices));

      setLoading(false);
      setAnalyzing(false);
    } catch (err) {
      console.error("Analysis failed:", err);
      setError(err instanceof Error ? err.message : "Failed to analyze repository");
      setLoading(false);
      setAnalyzing(false);
    }
  };

  // Fetch branches and projects on mount
  useEffect(() => {
    const fetchInitialData = async () => {
      try {
        const [branchesResp, projectsResp] = await Promise.all([
          apiGet<BranchesResponse>(`/v1/integrations/github/repos/${owner}/${repo}/branches`),
          apiGet<ProjectsResponse>('/v1/projects'),
        ]);
        setBranches(branchesResp.branches || []);
        setProjects(projectsResp.projects || []);

        // Auto-analyze with initial branch
        analyzeRepository(initialBranch);
      } catch (err) {
        console.error("Failed to fetch initial data:", err);
        setError("Failed to load repository data");
        setLoading(false);
      }
    };
    fetchInitialData();
  }, [owner, repo, initialBranch, analyzeRepository]);

  const handleBranchChange = (branch: string) => {
    setSelectedBranch(branch);
    analyzeRepository(branch);
  };

  const toggleService = (appPath: string) => {
    const newSelected = new Set(selectedServices);
    if (newSelected.has(appPath)) {
      newSelected.delete(appPath);
    } else {
      newSelected.add(appPath);
    }
    setSelectedServices(newSelected);
  };

  const selectAll = () => {
    if (analysis) {
      setSelectedServices(new Set(analysis.services.map(s => s.app_path)));
    }
  };

  const selectNone = () => {
    setSelectedServices(new Set());
  };

  const handleImport = async () => {
    if (!selectedProject || selectedServices.size === 0) return;

    setImporting(true);
    setError(null);

    try {
      // Get selected service details
      const servicesToImport = analysis?.services.filter(s => selectedServices.has(s.app_path)) || [];

      // If only one service, go to the normal new service page with pre-filled data
      if (servicesToImport.length === 1) {
        const svc = servicesToImport[0];
        const params = new URLSearchParams({
          repo: `https://github.com/${owner}/${repo}`,
          name: svc.name,
          branch: selectedBranch,
          app_path: svc.app_path,
          port: svc.port.toString(),
          build_command: svc.build_command,
          start_command: svc.start_command,
          project_id: selectedProject,
        });
        router.push(`/services/new?${params.toString()}`);
        return;
      }

      // For multiple services, use bulk import API
      const response = await apiPost<{ services: { id: string; name: string }[] }>(
        `/v1/projects/${selectedProject}/services/bulk`,
        {
          git_repo: `https://github.com/${owner}/${repo}`,
          git_branch: selectedBranch,
          services: servicesToImport.map(svc => ({
            name: svc.name,
            app_path: svc.app_path,
            port: svc.port,
            build_command: svc.build_command,
            start_command: svc.start_command,
          })),
        }
      );

      // Redirect to project page
      router.push(`/projects/${selectedProject}`);
    } catch (err) {
      console.error("Import failed:", err);
      setError(err instanceof Error ? err.message : "Failed to import services");
      setImporting(false);
    }
  };

  const getConfidenceBadge = (confidence: number) => {
    if (confidence >= 0.9) return <Badge className="bg-status-success-muted text-status-success-foreground">High confidence</Badge>;
    if (confidence >= 0.7) return <Badge className="bg-status-warning-muted text-status-warning-foreground">Good confidence</Badge>;
    return <Badge className="bg-orange-100 text-orange-800">Low confidence</Badge>;
  };

  if (loading || analyzing) {
    return (
      <div className="container mx-auto py-8 max-w-4xl">
        <div className="mb-6">
          <Link href="/services/import" className="text-primary hover:text-primary/80 text-sm flex items-center gap-1">
            <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
            </svg>
            Back to Repositories
          </Link>
        </div>
        <Card>
          <CardContent className="py-12">
            <div className="flex flex-col items-center gap-4">
              <Spinner size="lg" />
              <div className="text-center">
                <h2 className="text-lg font-semibold">Analyzing Repository</h2>
                <p className="text-muted-foreground">
                  Scanning {owner}/{repo} for deployable services...
                </p>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (error) {
    return (
      <div className="container mx-auto py-8 max-w-4xl">
        <div className="mb-6">
          <Link href="/services/import" className="text-primary hover:text-primary/80 text-sm flex items-center gap-1">
            <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
            </svg>
            Back to Repositories
          </Link>
        </div>
        <Card className="border-status-error/30 bg-status-error-muted">
          <CardContent className="py-8 text-center">
            <p className="text-status-error mb-4">{error}</p>
            <Button onClick={() => analyzeRepository(selectedBranch)}>
              Retry Analysis
            </Button>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="container mx-auto py-8 max-w-4xl">
      {/* Header */}
      <div className="mb-6">
        <Link href="/services/import" className="text-primary hover:text-primary/80 text-sm flex items-center gap-1">
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
          </svg>
          Back to Repositories
        </Link>
      </div>

      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold flex items-center gap-3">
            <GithubIcon />
            {owner}/{repo}
          </h1>
          <p className="text-muted-foreground mt-1">
            Select services to import from this repository
          </p>
        </div>

        {/* Branch selector */}
        <Select value={selectedBranch} onValueChange={handleBranchChange}>
          <SelectTrigger className="w-[180px]">
            <SelectValue placeholder="Select branch" />
          </SelectTrigger>
          <SelectContent>
            {branches.map(branch => (
              <SelectItem key={branch.name} value={branch.name}>
                {branch.name}
                {branch.protected && <Badge variant="outline" className="ml-2 text-xs">protected</Badge>}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {/* Analysis Summary */}
      {analysis && (
        <Card className="mb-6">
          <CardContent className="py-4">
            <div className="flex items-center gap-4 text-sm">
              <div className="flex items-center gap-2">
                <CheckCircleIcon />
                <span>
                  {analysis.monorepo_detected ? (
                    <>Monorepo detected ({analysis.monorepo_tool})</>
                  ) : (
                    <>Single service repository</>
                  )}
                </span>
              </div>
              <div className="text-muted-foreground">
                {analysis.services.length} service{analysis.services.length !== 1 ? 's' : ''} found
              </div>
              {analysis.shared_paths.length > 0 && (
                <div className="text-muted-foreground">
                  Shared: {analysis.shared_paths.join(', ')}
                </div>
              )}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Service Selection */}
      {analysis && analysis.services.length > 0 && (
        <>
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-lg font-semibold">Detected Services</h2>
            <div className="flex gap-2 text-sm">
              <Button variant="ghost" size="sm" onClick={selectAll}>
                Select all
              </Button>
              <Button variant="ghost" size="sm" onClick={selectNone}>
                Clear
              </Button>
            </div>
          </div>

          <div className="space-y-3 mb-8">
            {analysis.services.map((service) => (
              <Card
                key={service.app_path}
                className={`cursor-pointer transition-all ${
                  selectedServices.has(service.app_path)
                    ? 'border-primary bg-primary/5'
                    : 'hover:border-border'
                }`}
                onClick={() => toggleService(service.app_path)}
              >
                <CardContent className="py-4">
                  <div className="flex items-start gap-4">
                    <Checkbox
                      checked={selectedServices.has(service.app_path)}
                      onCheckedChange={() => toggleService(service.app_path)}
                      className="mt-1"
                    />
                    <div className="flex-1">
                      <div className="flex items-center gap-3 mb-2">
                        <h3 className="font-medium">{service.name}</h3>
                        <Badge variant="outline" className="flex items-center gap-1">
                          <FolderIcon />
                          {service.app_path}
                        </Badge>
                        {getConfidenceBadge(service.confidence)}
                      </div>

                      <div className="flex flex-wrap gap-2 mb-2">
                        <Badge className={runtimeColors[service.runtime] || 'bg-muted'}>
                          {service.runtime}
                        </Badge>
                        {service.framework && (
                          <Badge className={frameworkColors[service.framework] || 'bg-muted'}>
                            {service.framework}
                          </Badge>
                        )}
                        {service.has_dockerfile && (
                          <Badge variant="outline">Dockerfile</Badge>
                        )}
                        <Badge variant="outline" className="flex items-center gap-1">
                          <ServerIcon />
                          Port {service.port}
                        </Badge>
                      </div>

                      {service.detection_notes.length > 0 && (
                        <p className="text-sm text-muted-foreground">
                          {service.detection_notes[0]}
                        </p>
                      )}

                      {service.dependencies && service.dependencies.length > 0 && (
                        <p className="text-sm text-muted-foreground mt-1">
                          Dependencies: {service.dependencies.join(', ')}
                        </p>
                      )}
                    </div>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>

          {/* Import Controls */}
          <Card className="sticky bottom-4">
            <CardContent className="py-4">
              <div className="flex items-center gap-4">
                <div className="flex-1">
                  <label className="text-sm font-medium mb-1 block">Import to project</label>
                  <Select value={selectedProject} onValueChange={setSelectedProject}>
                    <SelectTrigger>
                      <SelectValue placeholder="Select a project" />
                    </SelectTrigger>
                    <SelectContent>
                      {projects.map(project => (
                        <SelectItem key={project.id} value={project.slug}>
                          {project.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="flex items-center gap-3">
                  <span className="text-sm text-muted-foreground">
                    {selectedServices.size} service{selectedServices.size !== 1 ? 's' : ''} selected
                  </span>
                  <Button
                    onClick={handleImport}
                    disabled={!selectedProject || selectedServices.size === 0 || importing}
                    className="min-w-[120px]"
                  >
                    {importing ? (
                      <>
                        <Spinner size="sm" className="mr-2" />
                        Importing...
                      </>
                    ) : (
                      <>Import Services</>
                    )}
                  </Button>
                </div>
              </div>
            </CardContent>
          </Card>
        </>
      )}

      {/* No Services Found */}
      {analysis && analysis.services.length === 0 && (
        <Card>
          <CardContent className="py-12 text-center">
            <div className="mx-auto mb-4 h-12 w-12 text-muted-foreground">
              <CodeIcon />
            </div>
            <h2 className="text-lg font-semibold mb-2">No deployable services found</h2>
            <p className="text-muted-foreground mb-4">
              We couldn't automatically detect any services in this repository.
              You can still import it manually.
            </p>
            <Button
              onClick={() => {
                const params = new URLSearchParams({
                  repo: `https://github.com/${owner}/${repo}`,
                  branch: selectedBranch,
                  name: repo,
                });
                router.push(`/services/new?${params.toString()}`);
              }}
            >
              Import Manually
            </Button>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
