'use client';

import { useState, useEffect, useCallback } from 'react';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Badge } from '@/components/ui/badge';
import { apiGet, apiPost } from '@/lib/api';
import { useRouter } from 'next/navigation';
import { GitBranch, ExternalLink, RefreshCw, RotateCcw } from 'lucide-react';
import { AuthorAvatar, CommitLink } from '@/components/git';
import type { Deployment, DeploymentsListResponse, RollbackResponse } from './types';

interface DeploymentsTabProps {
  serviceId: string;
  serviceName: string;
}

export function DeploymentsTab({ serviceId, serviceName }: DeploymentsTabProps) {
  const router = useRouter();
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [rollingBack, setRollingBack] = useState<string | null>(null);
  const [rollbackSuccess, setRollbackSuccess] = useState<string | null>(null);
  const [confirmDialogOpen, setConfirmDialogOpen] = useState(false);
  const [selectedDeploymentId, setSelectedDeploymentId] = useState<string | null>(null);

  const fetchDeployments = useCallback(async () => {
    try {
      setError(null);
      const data = await apiGet<DeploymentsListResponse>(`/v1/services/${serviceId}/deployments`);
      setDeployments(data.deployments || []);
    } catch (err) {
      console.error('Failed to fetch deployments:', err);
      setError(err instanceof Error ? err.message : 'Failed to fetch deployments');
    } finally {
      setLoading(false);
    }
  }, [serviceId]);

  useEffect(() => {
    fetchDeployments();
    // Refresh every 30 seconds
    const interval = setInterval(fetchDeployments, 30000);
    return () => clearInterval(interval);
  }, [fetchDeployments]);

  const handleRollbackClick = (deploymentId: string) => {
    setSelectedDeploymentId(deploymentId);
    setConfirmDialogOpen(true);
  };

  const handleConfirmRollback = async () => {
    if (!selectedDeploymentId) return;

    try {
      setRollingBack(selectedDeploymentId);
      setRollbackSuccess(null);
      setError(null);
      setConfirmDialogOpen(false);

      await apiPost<RollbackResponse>(`/v1/deployments/${selectedDeploymentId}/rollback`, {});

      setRollbackSuccess(selectedDeploymentId);
      // Refresh the deployments list
      await fetchDeployments();
    } catch (err) {
      console.error('Rollback failed:', err);
      setError(err instanceof Error ? err.message : 'Rollback failed');
    } finally {
      setRollingBack(null);
      setSelectedDeploymentId(null);
    }
  };

  const getStatusBadge = (status: string) => {
    const variants: Record<string, 'default' | 'secondary' | 'destructive' | 'outline'> = {
      running: 'default',
      pending: 'secondary',
      deploying: 'secondary',
      failed: 'destructive',
      stopped: 'outline',
    };
    return <Badge variant={variants[status] || 'outline'}>{status}</Badge>;
  };

  const getHealthBadge = (health: string) => {
    const colors: Record<string, string> = {
      healthy: 'bg-status-success-muted text-status-success-foreground',
      unhealthy: 'bg-status-error-muted text-status-error-foreground',
      unknown: 'bg-gray-100 text-gray-800',
    };
    return (
      <span className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${colors[health] || colors.unknown}`}>
        {health}
      </span>
    );
  };

  const formatDate = (dateString: string) => {
    const date = new Date(dateString);
    return date.toLocaleString();
  };

  const formatRelativeTime = (dateString: string) => {
    const date = new Date(dateString);
    const now = new Date();
    const diff = now.getTime() - date.getTime();

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
      <Card>
        <CardHeader>
          <CardTitle>Deployments</CardTitle>
          <CardDescription>Deployment history for {serviceName}</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-center py-8">
            <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
            <span className="ml-3 text-muted-foreground">Loading deployments...</span>
          </div>
        </CardContent>
      </Card>
    );
  }

  if (error) {
    return (
      <Card className="border-status-error/30">
        <CardHeader>
          <CardTitle>Deployments</CardTitle>
          <CardDescription>Deployment history for {serviceName}</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="text-center py-8">
            <p className="text-status-error mb-4">{error}</p>
            <Button variant="outline" onClick={fetchDeployments}>
              Try Again
            </Button>
          </div>
        </CardContent>
      </Card>
    );
  }

  return (
    <>
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <div>
            <CardTitle>Deployments</CardTitle>
            <CardDescription>Deployment history for {serviceName}</CardDescription>
          </div>
          <Button variant="outline" size="sm" onClick={fetchDeployments}>
            <RefreshCw className="w-4 h-4 mr-2" />
            Refresh
          </Button>
        </CardHeader>
        <CardContent>
          {rollbackSuccess && (
            <div className="mb-4 p-4 bg-status-success-muted border border-status-success/30 rounded-md">
              <p className="text-status-success-foreground text-sm">
                <svg className="inline w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                </svg>
                Rollback initiated successfully. The previous deployment is being restored.
              </p>
            </div>
          )}

          {deployments.length === 0 ? (
            <div className="text-center py-12 text-muted-foreground">
              <svg className="w-12 h-12 mx-auto mb-4 text-gray-300" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M7 16a4 4 0 01-.88-7.903A5 5 0 1115.9 6L16 6a5 5 0 011 9.9M15 13l-3-3m0 0l-3 3m3-3v12" />
              </svg>
              <p className="text-lg font-medium">No deployments yet</p>
              <p className="text-sm mt-1">Deploy your first release to see deployment history here.</p>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Status</TableHead>
                  <TableHead>Source</TableHead>
                  <TableHead>Health</TableHead>
                  <TableHead>Replicas</TableHead>
                  <TableHead>Deployed</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {deployments.map((deployment, index) => (
                  <TableRow key={deployment.id} className="cursor-pointer hover:bg-muted" onClick={() => router.push(`/deployments/${deployment.id}`)}>
                    <TableCell>{getStatusBadge(deployment.status)}</TableCell>
                    <TableCell>
                      <div className="flex flex-col gap-1">
                        {/* PR Link */}
                        {deployment.pr_number && deployment.pr_url && (
                          <a
                            href={deployment.pr_url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="inline-flex items-center gap-1 text-sm text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
                          >
                            <span className="font-medium">PR #{deployment.pr_number}</span>
                            <ExternalLink className="h-3 w-3" />
                          </a>
                        )}
                        {deployment.pr_title && (
                          <span className="text-xs text-muted-foreground truncate max-w-[200px]" title={deployment.pr_title}>
                            {deployment.pr_title}
                          </span>
                        )}
                        {/* Git info with clickable commit link */}
                        <div className="flex items-center gap-2 text-xs text-muted-foreground">
                          {deployment.git_branch && (
                            <span className="inline-flex items-center gap-1">
                              <GitBranch className="h-3 w-3" />
                              <span className="truncate max-w-[100px]" title={deployment.git_branch}>
                                {deployment.git_branch}
                              </span>
                            </span>
                          )}
                          {deployment.git_sha && (
                            <CommitLink
                              sha={deployment.git_sha}
                              repoUrl={deployment.repo_url}
                              message={deployment.commit_message}
                            />
                          )}
                        </div>
                        {/* Fallback if no git info */}
                        {!deployment.git_sha && !deployment.pr_number && (
                          <span className="text-xs text-muted-foreground">Manual deploy</span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>{getHealthBadge(deployment.health)}</TableCell>
                    <TableCell>{deployment.replicas}</TableCell>
                    <TableCell>
                      <div className="flex flex-col gap-1">
                        <span title={formatDate(deployment.created_at)}>
                          {formatRelativeTime(deployment.created_at)}
                        </span>
                        {deployment.commit_author && (
                          <div className="flex items-center gap-1.5">
                            <span className="text-xs text-muted-foreground">by</span>
                            <AuthorAvatar
                              name={deployment.commit_author}
                              username={deployment.commit_author_username}
                              email={deployment.commit_author_email}
                              avatarUrl={deployment.commit_author_avatar_url}
                              size="xs"
                              showName
                              linkToProfile={!!deployment.commit_author_username}
                            />
                          </div>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="text-right">
                      {/* Only show rollback for non-first deployments that are running or failed */}
                      {index > 0 && (deployment.status === 'running' || deployment.status === 'failed') && (
                        <Button
                          variant="outline"
                          size="sm"
                          disabled={rollingBack === deployment.id}
                          onClick={() => handleRollbackClick(deployment.id)}
                        >
                          {rollingBack === deployment.id ? (
                            <>
                              <RefreshCw className="animate-spin -ml-1 mr-2 h-4 w-4" />
                              Rolling back...
                            </>
                          ) : (
                            <>
                              <RotateCcw className="w-4 h-4 mr-1" />
                              Rollback
                            </>
                          )}
                        </Button>
                      )}
                      {index === 0 && deployment.status === 'running' && (
                        <Badge variant="outline" className="text-status-success border-status-success">
                          Current
                        </Badge>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {/* Confirmation Dialog */}
      <Dialog open={confirmDialogOpen} onOpenChange={setConfirmDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Confirm Rollback</DialogTitle>
            <DialogDescription>
              Are you sure you want to rollback this deployment? This will restore the previous deployment version and may cause a brief service interruption.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmDialogOpen(false)}>
              Cancel
            </Button>
            <Button onClick={handleConfirmRollback}>
              Confirm Rollback
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
