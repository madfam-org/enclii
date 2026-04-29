'use client';

import { useState, useEffect } from "react";
import { usePolling } from "@/hooks/use-polling";
import { POLLING_NORMAL } from "@/lib/constants";
import { toast } from 'sonner';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from '@/components/ui/confirm-dialog';
import { apiGet, apiPost, apiDelete } from "@/lib/api";
import { Spinner } from "@/components/ui/spinner";
import { CreateDatabaseModal } from "@/components/databases/CreateDatabaseModal";
import { DatabaseCard } from "@/components/databases/DatabaseCard";

// Database addon types matching the API
export type DatabaseAddonType = 'postgres' | 'redis' | 'mysql';
export type DatabaseAddonStatus = 'pending' | 'provisioning' | 'ready' | 'failed' | 'deleting' | 'deleted';

export interface DatabaseAddon {
  id: string;
  project_id: string;
  environment_id?: string;
  type: DatabaseAddonType;
  name: string;
  status: DatabaseAddonStatus;
  status_message?: string;
  config: {
    version?: string;
    storage_gb?: number;
    cpu?: string;
    memory?: string;
    replicas?: number;
  };
  host?: string;
  port?: number;
  database_name?: string;
  username?: string;
  k8s_namespace?: string;
  k8s_resource_name?: string;
  created_at: string;
  updated_at: string;
  provisioned_at?: string;
}

export interface Project {
  id: string;
  name: string;
  slug: string;
}

export default function DatabasesPage() {
  const [databases, setDatabases] = useState<DatabaseAddon[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [deleteTargetId, setDeleteTargetId] = useState<string | null>(null);

  const fetchDatabases = async () => {
    try {
      setError(null);

      // Single batched call replaces a per-project loop that fanned out
      // ~25 sequential requests on cold load. /v1/databases is the
      // backend alias for ListAllAddonsForUser (handlers.go:654) — it
      // returns the full set in one query and respects the same RBAC
      // boundary the per-project endpoint enforces. Projects are still
      // fetched in parallel so the Create modal's project picker stays
      // populated, but the addon list no longer waits on it.
      const [databasesResponse, projectsResponse] = await Promise.all([
        apiGet<{addons: DatabaseAddon[]; count: number}>('/v1/databases'),
        apiGet<{projects: Project[]}>('/v1/projects'),
      ]);

      const projectsData = projectsResponse?.projects || [];
      setProjects(projectsData);

      // Decorate each addon with project_name / project_slug so the
      // existing card UI can render the project label without an extra
      // lookup. Falls back to project_id when the project list missed
      // a referenced project (rare — only happens during a project
      // delete-while-rendering race).
      const projectByID = new Map<string, Project>();
      for (const p of projectsData) projectByID.set(p.id, p);
      const decorated: DatabaseAddon[] = (databasesResponse?.addons || []).map((db) => {
        const p = projectByID.get(db.project_id);
        return {
          ...db,
          project_name: p?.name,
          project_slug: p?.slug,
        } as DatabaseAddon & { project_name?: string; project_slug?: string };
      });
      setDatabases(decorated);
      setLoading(false);
    } catch (err) {
      console.error("Failed to fetch databases:", err);
      setError(err instanceof Error ? err.message : "Failed to fetch databases");
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchDatabases();
  }, []);

  usePolling(fetchDatabases, POLLING_NORMAL);

  const handleCreateDatabase = async (data: {
    projectSlug: string;
    type: DatabaseAddonType;
    name: string;
    config: {
      version?: string;
      storage_gb?: number;
      memory?: string;
      replicas?: number;
    };
  }) => {
    try {
      await apiPost(`/v1/projects/${data.projectSlug}/addons`, {
        type: data.type,
        name: data.name,
        config: data.config,
      });
      setIsCreateModalOpen(false);
      fetchDatabases();
    } catch (err) {
      throw err; // Let the modal handle the error
    }
  };

  const handleDeleteDatabase = (addonId: string) => {
    setDeleteTargetId(addonId);
  };

  const confirmDeleteDatabase = async () => {
    if (!deleteTargetId) return;

    const addonId = deleteTargetId;
    setDeleteTargetId(null);
    setDeletingId(addonId);
    try {
      await apiDelete(`/v1/addons/${addonId}`);
      fetchDatabases();
    } catch (err) {
      console.error("Failed to delete database:", err);
      toast.error(err instanceof Error ? err.message : "Failed to delete database");
    } finally {
      setDeletingId(null);
    }
  };

  if (loading) {
    return (
      <div className="container mx-auto py-8">
        <div className="mb-8">
          <h1 className="text-3xl font-bold">Databases</h1>
          <p className="text-muted-foreground mt-2">
            Managed database add-ons for your projects
          </p>
        </div>
        <Card>
          <CardContent className="py-12">
            <div className="flex items-center justify-center">
              <Spinner size="lg" />
              <span className="ml-3 text-muted-foreground">Loading databases...</span>
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
          <h1 className="text-3xl font-bold">Databases</h1>
          <p className="text-muted-foreground mt-2">
            Managed database add-ons for your projects
          </p>
        </div>
        <Card className="border-status-error/30 bg-status-error-muted">
          <CardContent className="py-8">
            <div className="text-center">
              <p className="text-status-error font-medium mb-4">{error}</p>
              <Button variant="outline" onClick={fetchDatabases}>
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
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-3xl font-bold">Databases</h1>
          <p className="text-muted-foreground mt-2">
            Managed database add-ons for your projects
          </p>
        </div>
        <div className="flex items-center gap-3">
          <Button onClick={() => setIsCreateModalOpen(true)}>
            <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
            </svg>
            New Database
          </Button>
          <Button variant="outline" onClick={fetchDatabases}>
            <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
            Refresh
          </Button>
        </div>
      </div>

      {databases.length === 0 ? (
        <Card>
          <CardContent className="py-16">
            <div className="text-center">
              <div className="mx-auto w-16 h-16 mb-4 rounded-full bg-primary/10 flex items-center justify-center">
                <svg aria-hidden="true" className="w-8 h-8 text-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 7v10c0 2.21 3.582 4 8 4s8-1.79 8-4V7M4 7c0 2.21 3.582 4 8 4s8-1.79 8-4M4 7c0-2.21 3.582-4 8-4s8 1.79 8 4m0 5c0 2.21-3.582 4-8 4s-8-1.79-8-4" />
                </svg>
              </div>
              <h3 className="text-lg font-medium mb-2">No databases yet</h3>
              <p className="text-muted-foreground mb-6 max-w-md mx-auto">
                Create a managed PostgreSQL or Redis database and bind it to your services
                for automatic environment variable injection.
              </p>
              <Button onClick={() => setIsCreateModalOpen(true)}>
                <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
                </svg>
                Create Your First Database
              </Button>
            </div>
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {databases.map((db) => (
            <DatabaseCard
              key={db.id}
              database={db}
              onDelete={() => handleDeleteDatabase(db.id)}
              isDeleting={deletingId === db.id}
            />
          ))}
        </div>
      )}

      <CreateDatabaseModal
        isOpen={isCreateModalOpen}
        onClose={() => setIsCreateModalOpen(false)}
        onSubmit={handleCreateDatabase}
        projects={projects}
      />

      <ConfirmDialog
        open={!!deleteTargetId}
        title="Delete Database"
        description="Are you sure you want to delete this database? This action cannot be undone."
        variant="destructive"
        confirmLabel="Delete"
        onConfirm={confirmDeleteDatabase}
        onCancel={() => setDeleteTargetId(null)}
      />
    </div>
  );
}
