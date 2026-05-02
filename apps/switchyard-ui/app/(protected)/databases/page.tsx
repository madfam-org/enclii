'use client';

import { useState, useEffect } from "react";
import { useSearchParams } from "next/navigation";
import { usePolling } from "@/hooks/use-polling";
import { POLLING_NORMAL } from "@/lib/constants";
import { toast } from 'sonner';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@enclii/ui-components/button";
import { ConfirmDialog } from '@/components/ui/confirm-dialog';
import { apiGet, apiPost, apiDelete } from "@/lib/api";
import { Spinner } from "@/components/ui/spinner";
import { CreateDatabaseModal } from "@/components/databases/CreateDatabaseModal";
import { DatabaseCard } from "@/components/databases/DatabaseCard";
import { Database, Plus, RefreshCw } from "lucide-react";

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
  const searchParams = useSearchParams();
  const [databases, setDatabases] = useState<DatabaseAddon[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [deleteTargetId, setDeleteTargetId] = useState<string | null>(null);

  // Open the create modal when arriving via the dashboard's "+ Add New… >
  // New database" dropdown (audit D-5). Mirrors the same pattern used by
  // /projects?create=true.
  useEffect(() => {
    if (searchParams.get('create') === 'true') {
      setIsCreateModalOpen(true);
    }
  }, [searchParams]);

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
          <h1 className="text-3xl font-bold text-foreground">Databases</h1>
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
          <h1 className="text-3xl font-bold text-foreground">Databases</h1>
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
      <div className="flex flex-col sm:flex-row sm:items-center justify-between mb-8 gap-4">
        <div>
          <h1 className="text-3xl font-bold text-foreground">Databases</h1>
          <p className="text-muted-foreground mt-2">
            Managed database add-ons for your projects
          </p>
        </div>
        <div className="flex items-center gap-3">
          <Button onClick={() => setIsCreateModalOpen(true)}>
            <Plus className="w-4 h-4 mr-2" />
            New Database
          </Button>
          <Button variant="outline" onClick={fetchDatabases}>
            <RefreshCw className="w-4 h-4 mr-2" />
            Refresh
          </Button>
        </div>
      </div>

      {databases.length === 0 ? (
        <Card>
          <CardContent className="py-16">
            <div className="text-center">
              <div className="mx-auto w-16 h-16 mb-4 rounded-full bg-primary/10 flex items-center justify-center border border-primary/20">
                <Database className="w-8 h-8 text-primary" />
              </div>
              <h3 className="text-lg font-medium mb-2 text-foreground">No databases yet</h3>
              <p className="text-muted-foreground mb-6 max-w-md mx-auto">
                Create a managed PostgreSQL or Redis database and bind it to your services
                for automatic environment variable injection.
              </p>
              <Button onClick={() => setIsCreateModalOpen(true)}>
                <Plus className="w-4 h-4 mr-2" />
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
