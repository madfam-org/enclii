'use client';

import { useState, useEffect, useCallback } from 'react';
import { useParams, useRouter } from 'next/navigation';
import Link from 'next/link';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { apiGet, apiPost } from '@/lib/api';
import { ArrowLeft, GitBranch, ExternalLink, RefreshCw, RotateCcw, Clock, Server, Container, Hash } from 'lucide-react';
import { AuthorAvatar, CommitLink } from '@/components/git';
import type { Deployment, RollbackResponse } from '@/components/deployments/types';
import { LogsTab } from '@/components/log-viewer/LogsTab';

export default function DeploymentDetailPage() {
  const params = useParams();
  const router = useRouter();
  const deploymentId = params.id as string;

  const [deployment, setDeployment] = useState<Deployment | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [rollingBack, setRollingBack] = useState(false);
  const [rollbackSuccess, setRollbackSuccess] = useState(false);
  const [confirmDialogOpen, setConfirmDialogOpen] = useState(false);

  const fetchDeployment = useCallback(async () => {
    try {
      setError(null);
      const data = await apiGet<Deployment>(`/v1/deployments/${deploymentId}`);
      setDeployment(data);
    } catch (err) {
      console.error('Failed to fetch deployment:', err);
      setError(err instanceof Error ? err.message : 'Failed to fetch deployment');
    } finally {
      setLoading(false);
    }
  }, [deploymentId]);

  useEffect(() => {
    fetchDeployment();
    const interval = setInterval(fetchDeployment, 30000);
    return () => clearInterval(interval);
  }, [fetchDeployment]);

  const handleConfirmRollback = async () => {
    try {
      setRollingBack(true);
      setRollbackSuccess(false);
      setConfirmDialogOpen(false);
      await apiPost<RollbackResponse>(`/v1/deployments/${deploymentId}/rollback`, {});
      setRollbackSuccess(true);
      await fetchDeployment();
    } catch (err) {
      console.error('Rollback failed:', err);
      setError(err instanceof Error ? err.message : 'Rollback failed');
    } finally {
      setRollingBack(false);
    }
  };

  const getStatusVariant = (status: string): 'default' | 'secondary' | 'destructive' | 'outline' => {
    const variants: Record<string, 'default' | 'secondary' | 'destructive' | 'outline'> = {
      running: 'default',
      pending: 'secondary',
      deploying: 'secondary',
      failed: 'destructive',
      stopped: 'outline',
    };
    return variants[status] || 'outline';
  };

  const getHealthColor = (health: string) => {
    const colors: Record<string, string> = {
      healthy: 'bg-status-success-muted text-status-success-foreground',
      unhealthy: 'bg-status-error-muted text-status-error-foreground',
      unknown: 'bg-gray-100 text-gray-800',
    };
    return colors[health] || colors.unknown;
  };

  const formatDate = (dateString: string) => new Date(dateString).toLocaleString();

  const formatRelativeTime = (dateString: string) => {
    const diff = Date.now() - new Date(dateString).getTime();
    const minutes = Math.floor(diff / 60000);
    const hours = Math.floor(diff / 3600000);
    const days = Math.floor(diff / 86400000);
    if (minutes < 1) return 'just now';
    if (minutes < 60) return `${minutes}m ago`;
    if (hours < 24) return `${hours}h ago`;
    return `${days}d ago`;
  };

  if (loading) {
    return (
      <div className="container mx-auto py-8">
        <div className="flex items-center justify-center py-12">
          <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600" />
          <span className="ml-3 text-muted-foreground">Loading deployment...</span>
        </div>
      </div>
    );
  }

  if (error && !deployment) {
    return (
      <div className="container mx-auto py-8">
        <div className="mb-4">
          <Button variant="ghost" size="sm" onClick={() => router.back()}>
            <ArrowLeft className="w-4 h-4 mr-2" />
            Back
          </Button>
        </div>
        <Card className="border-status-error/30 bg-status-error-muted">
          <CardContent className="py-8">
            <div className="text-center">
              <p className="text-status-error font-medium mb-4">{error}</p>
              <Button variant="outline" onClick={fetchDeployment}>Try Again</Button>
            </div>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (!deployment) return null;

  const canRollback = deployment.status === 'running' || deployment.status === 'failed';

  return (
    <div className="container mx-auto py-8 space-y-6">
      {/* Breadcrumb / Back */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <Button variant="ghost" size="sm" onClick={() => router.back()}>
            <ArrowLeft className="w-4 h-4 mr-2" />
            Back
          </Button>
          <nav className="text-sm text-muted-foreground">
            <Link href="/deployments" className="hover:text-foreground">Deployments</Link>
            <span className="mx-2">/</span>
            <span className="text-foreground font-mono">{deploymentId.slice(0, 8)}</span>
          </nav>
        </div>
        <Button variant="outline" size="sm" onClick={fetchDeployment}>
          <RefreshCw className="w-4 h-4 mr-2" />
          Refresh
        </Button>
      </div>

      {/* Rollback success banner */}
      {rollbackSuccess && (
        <div className="p-4 bg-status-success-muted border border-status-success/30 rounded-md">
          <p className="text-status-success-foreground text-sm">
            Rollback initiated successfully. The previous deployment is being restored.
          </p>
        </div>
      )}

      {/* Error banner */}
      {error && (
        <div className="p-4 bg-status-error-muted border border-status-error/30 rounded-md">
          <p className="text-status-error-foreground text-sm">{error}</p>
        </div>
      )}

      {/* Header Card */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div className="space-y-2">
              <div className="flex items-center gap-3">
                <CardTitle className="text-xl">Deployment</CardTitle>
                <Badge variant={getStatusVariant(deployment.status)}>{deployment.status}</Badge>
                <span className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${getHealthColor(deployment.health)}`}>
                  {deployment.health}
                </span>
              </div>
              <CardDescription className="font-mono text-xs">{deployment.id}</CardDescription>
            </div>
            {canRollback && (
              <Button
                variant="outline"
                disabled={rollingBack}
                onClick={() => setConfirmDialogOpen(true)}
              >
                {rollingBack ? (
                  <><RefreshCw className="animate-spin w-4 h-4 mr-2" />Rolling back...</>
                ) : (
                  <><RotateCcw className="w-4 h-4 mr-2" />Rollback</>
                )}
              </Button>
            )}
          </div>
        </CardHeader>
      </Card>

      {/* Details Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        {/* Deployment Info */}
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Details</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-2 gap-4 text-sm">
              <div>
                <p className="text-muted-foreground mb-1 flex items-center gap-1"><Server className="w-3 h-3" /> Replicas</p>
                <p className="font-medium">{deployment.replicas}</p>
              </div>
              <div>
                <p className="text-muted-foreground mb-1 flex items-center gap-1"><Container className="w-3 h-3" /> Environment</p>
                <p className="font-medium font-mono text-xs">{deployment.environment_id}</p>
              </div>
              <div>
                <p className="text-muted-foreground mb-1 flex items-center gap-1"><Hash className="w-3 h-3" /> Release</p>
                <p className="font-medium font-mono text-xs">{deployment.release_id}</p>
              </div>
              <div>
                <p className="text-muted-foreground mb-1 flex items-center gap-1"><Clock className="w-3 h-3" /> Created</p>
                <p className="font-medium" title={formatDate(deployment.created_at)}>
                  {formatRelativeTime(deployment.created_at)}
                </p>
              </div>
              {deployment.updated_at && deployment.updated_at !== deployment.created_at && (
                <div>
                  <p className="text-muted-foreground mb-1">Updated</p>
                  <p className="font-medium" title={formatDate(deployment.updated_at)}>
                    {formatRelativeTime(deployment.updated_at)}
                  </p>
                </div>
              )}
            </div>
          </CardContent>
        </Card>

        {/* Git Info */}
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Source</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {deployment.pr_number && deployment.pr_url ? (
              <div>
                <a
                  href={deployment.pr_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1 text-sm text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
                >
                  <span className="font-medium">PR #{deployment.pr_number}</span>
                  <ExternalLink className="h-3 w-3" />
                </a>
                {deployment.pr_title && (
                  <p className="text-sm text-muted-foreground mt-1">{deployment.pr_title}</p>
                )}
              </div>
            ) : null}

            <div className="flex flex-col gap-2 text-sm">
              {deployment.git_branch && (
                <div className="flex items-center gap-2 text-muted-foreground">
                  <GitBranch className="h-4 w-4" />
                  <span>{deployment.git_branch}</span>
                </div>
              )}
              {deployment.git_sha && (
                <div className="flex items-center gap-2">
                  <CommitLink
                    sha={deployment.git_sha}
                    repoUrl={deployment.repo_url}
                    message={deployment.commit_message}
                  />
                </div>
              )}
              {deployment.commit_message && (
                <p className="text-muted-foreground text-xs mt-1">{deployment.commit_message}</p>
              )}
            </div>

            {deployment.commit_author && (
              <div className="flex items-center gap-2 pt-2 border-t">
                <span className="text-xs text-muted-foreground">Deployed by</span>
                <AuthorAvatar
                  name={deployment.commit_author}
                  username={deployment.commit_author_username}
                  email={deployment.commit_author_email}
                  avatarUrl={deployment.commit_author_avatar_url}
                  size="sm"
                  showName
                  linkToProfile={!!deployment.commit_author_username}
                />
              </div>
            )}

            {!deployment.git_sha && !deployment.pr_number && !deployment.commit_author && (
              <p className="text-sm text-muted-foreground">Manual deployment — no git metadata available.</p>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Logs Section */}
      <div>
        <h2 className="text-lg font-semibold mb-4">Logs</h2>
        <LogsTab
          serviceId={deployment.release_id}
          serviceName={`Deployment ${deploymentId.slice(0, 8)}`}
          deploymentId={deploymentId}
        />
      </div>

      {/* Rollback Confirmation Dialog */}
      <Dialog open={confirmDialogOpen} onOpenChange={setConfirmDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Confirm Rollback</DialogTitle>
            <DialogDescription>
              Are you sure you want to rollback this deployment? This will restore the previous
              deployment version and may cause a brief service interruption.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmDialogOpen(false)}>Cancel</Button>
            <Button onClick={handleConfirmRollback}>Confirm Rollback</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
