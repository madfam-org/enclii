'use client';

import { useState, useEffect } from "react";
import { usePolling } from "@/hooks/use-polling";
import { POLLING_NORMAL } from "@/lib/constants";
import { toast } from 'sonner';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { ConfirmDialog } from '@/components/ui/confirm-dialog';
import { apiGet, apiPost, apiDelete } from "@/lib/api";
import { Spinner } from "@/components/ui/spinner";
import { FunctionCard, CreateFunctionModal } from "@/components/functions";
import type { Function, FunctionConfig } from "@/components/functions";

interface Project {
  id: string;
  name: string;
  slug: string;
}

export default function FunctionsPage() {
  const [functions, setFunctions] = useState<(Function & { project_name?: string; project_slug?: string })[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [deleteTargetId, setDeleteTargetId] = useState<string | null>(null);

  const fetchFunctions = async () => {
    try {
      setError(null);
      // Fetch all projects first
      const projectsResponse = await apiGet<{projects: Project[]}>('/v1/projects');
      const projectsData = projectsResponse?.projects || [];
      setProjects(projectsData);

      // Fetch functions for each project
      const allFunctions: (Function & { project_name?: string; project_slug?: string })[] = [];
      for (const project of projectsData) {
        try {
          const functionsResponse = await apiGet<{functions: Function[], count: number}>(`/v1/projects/${project.slug}/functions`);
          const fns = functionsResponse?.functions || [];
          if (fns.length > 0) {
            allFunctions.push(...fns.map(fn => ({
              ...fn,
              project_name: project.name,
              project_slug: project.slug
            })));
          }
        } catch {
          // Project might not have any functions, continue
        }
      }
      setFunctions(allFunctions);
      setLoading(false);
    } catch (err) {
      console.error("Failed to fetch functions:", err);
      setError(err instanceof Error ? err.message : "Failed to fetch functions");
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchFunctions();
  }, []);

  usePolling(fetchFunctions, POLLING_NORMAL);

  const handleCreateFunction = async (data: {
    projectSlug: string;
    name: string;
    config: Partial<FunctionConfig>;
  }) => {
    await apiPost(`/v1/projects/${data.projectSlug}/functions`, {
      name: data.name,
      config: data.config,
    });
    fetchFunctions();
  };

  const handleDeleteFunction = (id: string, _projectSlug?: string) => {
    setDeleteTargetId(id);
  };

  const confirmDeleteFunction = async () => {
    if (!deleteTargetId) return;

    const id = deleteTargetId;
    setDeleteTargetId(null);
    setDeletingId(id);
    try {
      await apiDelete(`/v1/functions/${id}`);
      setFunctions(functions.filter(fn => fn.id !== id));
    } catch (err) {
      console.error("Failed to delete function:", err);
      toast.error(err instanceof Error ? err.message : 'Failed to delete function');
    } finally {
      setDeletingId(null);
    }
  };

  // Calculate summary metrics
  const totalInvocations = functions.reduce((sum, fn) => sum + fn.invocation_count, 0);
  const readyCount = functions.filter(fn => fn.status === 'ready').length;
  const scaledToZeroCount = functions.filter(fn => fn.status === 'ready' && fn.available_replicas === 0).length;

  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Functions</h1>
          <p className="text-muted-foreground">
            Serverless functions with automatic scale-to-zero
          </p>
        </div>
        <Button onClick={() => setIsCreateModalOpen(true)}>
          <svg
            aria-hidden="true"
            className="mr-2 h-4 w-4"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M12 4v16m8-8H4"
            />
          </svg>
          Create Function
        </Button>
      </div>

      {/* Runtime Notice */}
      <Card className="border-blue-200 bg-blue-50 dark:border-blue-800 dark:bg-blue-950">
        <CardContent className="pt-4">
          <p className="text-sm text-blue-800 dark:text-blue-200">
            Functions runtime (KEDA) is not yet deployed. Function definitions can be
            created and managed, but will not execute until the runtime is provisioned.
          </p>
        </CardContent>
      </Card>

      {/* Summary Cards */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">
              Total Functions
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-bold">{functions.length}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">
              Ready
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-bold text-status-success">{readyCount}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">
              Scaled to Zero
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-bold text-primary">{scaledToZeroCount}</div>
            <p className="text-xs text-muted-foreground mt-1">Saving resources</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">
              Total Invocations
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-bold">{totalInvocations.toLocaleString()}</div>
          </CardContent>
        </Card>
      </div>

      {/* Error State */}
      {error && (
        <Card className="border-destructive">
          <CardContent className="pt-6">
            <div className="flex items-center gap-2 text-destructive">
              <svg aria-hidden="true" className="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
              <span>{error}</span>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Loading State */}
      {loading && (
        <div className="flex items-center justify-center py-12">
          <Spinner size="lg" />
          <span className="ml-3 text-muted-foreground">Loading functions...</span>
        </div>
      )}

      {/* Empty State */}
      {!loading && functions.length === 0 && (
        <Card>
          <CardContent className="flex flex-col items-center justify-center py-12">
            <div className="rounded-full bg-muted p-4 mb-4">
              <svg aria-hidden="true" className="h-8 w-8 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 10V3L4 14h7v7l9-11h-7z" />
              </svg>
            </div>
            <h3 className="text-lg font-semibold mb-1">No functions yet</h3>
            <p className="text-muted-foreground text-center mb-4 max-w-sm">
              Create your first serverless function to get started with scale-to-zero compute.
            </p>
            <Button onClick={() => setIsCreateModalOpen(true)}>
              Create Your First Function
            </Button>
          </CardContent>
        </Card>
      )}

      {/* Functions Grid */}
      {!loading && functions.length > 0 && (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {functions.map((fn) => (
            <FunctionCard
              key={fn.id}
              fn={fn}
              onDelete={() => handleDeleteFunction(fn.id, fn.project_slug)}
              isDeleting={deletingId === fn.id}
            />
          ))}
        </div>
      )}

      {/* Runtime Info */}
      {!loading && functions.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm">Supported Runtimes</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex flex-wrap gap-2">
              <Badge variant="outline" className="bg-cyan-50">
                Go (&lt;500ms cold start)
              </Badge>
              <Badge variant="outline" className="bg-orange-50">
                Rust (&lt;500ms cold start)
              </Badge>
              <Badge variant="outline" className="bg-green-50">
                Node.js (&lt;2s cold start)
              </Badge>
              <Badge variant="outline" className="bg-yellow-50">
                Python (&lt;3s cold start)
              </Badge>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Create Modal */}
      <CreateFunctionModal
        open={isCreateModalOpen}
        onOpenChange={setIsCreateModalOpen}
        projects={projects}
        onSubmit={handleCreateFunction}
      />

      <ConfirmDialog
        open={!!deleteTargetId}
        title="Delete Function"
        description="Are you sure you want to delete this function? This action cannot be undone."
        variant="destructive"
        confirmLabel="Delete"
        onConfirm={confirmDeleteFunction}
        onCancel={() => setDeleteTargetId(null)}
      />
    </div>
  );
}
