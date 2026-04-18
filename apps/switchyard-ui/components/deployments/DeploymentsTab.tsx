'use client';

import { useState, useEffect, useCallback } from 'react';
import { usePolling } from '@/hooks/use-polling';
import { POLLING_SLOW } from '@/lib/constants';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Spinner } from '@/components/ui/spinner';
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
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { Badge } from '@/components/ui/badge';
import { apiGet, apiPost } from '@/lib/api';
import { useRouter } from 'next/navigation';
import { GitBranch, ExternalLink, RefreshCw, RotateCcw, Zap } from 'lucide-react';
import { AuthorAvatar, CommitLink } from '@/components/git';
import { formatDate, formatRelativeTime } from '@/lib/formatting';
import type {
  Deployment,
  DeploymentsListResponse,
  InstantRollbackRequest,
  InstantRollbackResponse,
} from './types';

interface DeploymentsTabProps {
  serviceId: string;
  serviceName: string;
}

/**
 * Deployment history tab with Vercel-parity instant rollback.
 *
 * Clicking "Rollback to here" on any historical deploy row triggers a
 * Service-selector flip via POST /v1/services/{id}/rollback. Traffic
 * shifts in <30s for still-running targets, <90s when the ReplicaSet
 * needs to scale back up. Production envs require a change_ticket_url
 * in the request body (HITL gate) — surfaced via the confirmation modal.
 */
export function DeploymentsTab({ serviceId, serviceName }: DeploymentsTabProps) {
  const router = useRouter();
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Rollback UX state
  const [rollingBack, setRollingBack] = useState<string | null>(null);
  const [rollbackResult, setRollbackResult] = useState<InstantRollbackResponse | null>(null);
  const [rollbackErrorBanner, setRollbackErrorBanner] = useState<string | null>(null);
  const [confirmDialogOpen, setConfirmDialogOpen] = useState(false);
  const [selectedDeployment, setSelectedDeployment] = useState<Deployment | null>(null);
  const [currentDeployment, setCurrentDeployment] = useState<Deployment | null>(null);
  const [reason, setReason] = useState('');
  const [changeTicketURL, setChangeTicketURL] = useState('');
  const [requireChangeTicket, setRequireChangeTicket] = useState(false);

  const fetchDeployments = useCallback(async () => {
    try {
      setError(null);
      const data = await apiGet<DeploymentsListResponse>(`/v1/services/${serviceId}/deployments`);
      const list = data.deployments || [];
      setDeployments(list);
      // Track current (live) deployment separately for the modal display.
      setCurrentDeployment(list[0] ?? null);
    } catch (err) {
      console.error('Failed to fetch deployments:', err);
      setError(err instanceof Error ? err.message : 'Failed to fetch deployments');
    } finally {
      setLoading(false);
    }
  }, [serviceId]);

  useEffect(() => {
    fetchDeployments();
  }, [fetchDeployments]);

  usePolling(fetchDeployments, POLLING_SLOW);

  const openRollbackDialog = (target: Deployment) => {
    setSelectedDeployment(target);
    setReason('');
    setChangeTicketURL('');
    setRequireChangeTicket(false);
    setRollbackErrorBanner(null);
    setConfirmDialogOpen(true);
  };

  const handleConfirmRollback = async () => {
    if (!selectedDeployment) return;

    const body: InstantRollbackRequest = {
      target_deployment_id: selectedDeployment.id,
    };
    if (reason.trim()) body.reason = reason.trim();
    if (changeTicketURL.trim()) body.change_ticket_url = changeTicketURL.trim();

    try {
      setRollingBack(selectedDeployment.id);
      setRollbackResult(null);
      setRollbackErrorBanner(null);

      const resp = await apiPost<InstantRollbackResponse>(
        `/v1/services/${serviceId}/rollback`,
        body,
      );

      setRollbackResult(resp);
      setConfirmDialogOpen(false);
      setSelectedDeployment(null);
      await fetchDeployments();
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Rollback failed';
      // The API returns 403 with a "change_ticket_url is required" message
      // for production envs without a ticket. Surface it in-dialog and
      // focus the change-ticket field rather than dismissing.
      if (/change_ticket_url is required/i.test(message)) {
        setRequireChangeTicket(true);
        setRollbackErrorBanner('A change ticket URL is required for production rollbacks.');
      } else {
        setRollbackErrorBanner(message);
      }
    } finally {
      setRollingBack(null);
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
      unknown: 'bg-muted text-foreground',
    };
    return (
      <span className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${colors[health] || colors.unknown}`}>
        {health}
      </span>
    );
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
            <Spinner size="lg" />
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

  const rollbackInProgress = rollingBack !== null;

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
          {/* Rollback in-progress banner */}
          {rollbackInProgress && (
            <div
              className="mb-4 p-4 bg-muted border border-border rounded-md flex items-center gap-3"
              role="status"
              aria-live="polite"
              data-testid="rollback-in-progress"
            >
              <Spinner size="sm" />
              <span className="text-sm">
                Flipping traffic to <code className="font-mono">{shortId(rollingBack!)}</code>…
              </span>
            </div>
          )}

          {/* Success toast */}
          {rollbackResult && !rollbackInProgress && (
            <div
              className="mb-4 p-4 bg-status-success-muted border border-status-success/30 rounded-md"
              role="status"
              aria-live="polite"
              data-testid="rollback-success"
            >
              <p className="text-status-success-foreground text-sm">
                <svg aria-hidden="true" className="inline w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                </svg>
                Rollback complete in {formatTookMS(rollbackResult.took_ms)}.{' '}
                Traffic now serving <code className="font-mono">{shortId(rollbackResult.to_deployment_id)}</code>.
                {rollbackResult.scaled_up && ' Target ReplicaSet was scaled up.'}
              </p>
            </div>
          )}

          {/* Failure toast */}
          {rollbackErrorBanner && !confirmDialogOpen && (
            <div
              className="mb-4 p-4 bg-status-error-muted border border-status-error/30 rounded-md"
              role="alert"
              data-testid="rollback-error"
            >
              <p className="text-status-error-foreground text-sm">
                Rollback failed: {rollbackErrorBanner}
              </p>
            </div>
          )}

          {deployments.length === 0 ? (
            <div className="text-center py-12 text-muted-foreground">
              <svg aria-hidden="true" className="w-12 h-12 mx-auto mb-4 text-muted-foreground/50" fill="none" stroke="currentColor" viewBox="0 0 24 24">
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
                  <TableRow
                    key={deployment.id}
                    className="cursor-pointer hover:bg-muted"
                    onClick={() => router.push(`/deployments/${deployment.id}`)}
                  >
                    <TableCell>{getStatusBadge(deployment.status)}</TableCell>
                    <TableCell>
                      <div className="flex flex-col gap-1">
                        {deployment.pr_number && deployment.pr_url && (
                          <a
                            href={deployment.pr_url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="inline-flex items-center gap-1 text-sm text-primary hover:text-primary/80"
                            onClick={(e) => e.stopPropagation()}
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
                      {index === 0 && deployment.status === 'running' ? (
                        <Badge variant="outline" className="text-status-success border-status-success">
                          Current
                        </Badge>
                      ) : index > 0 && deployment.status === 'running' ? (
                        <Button
                          variant="outline"
                          size="sm"
                          // Disable all rollback buttons while one is in flight.
                          disabled={rollbackInProgress}
                          aria-label={`Rollback to deployment ${shortId(deployment.id)}`}
                          data-testid={`rollback-to-${deployment.id}`}
                          onClick={(e) => {
                            e.stopPropagation();
                            openRollbackDialog(deployment);
                          }}
                        >
                          {rollingBack === deployment.id ? (
                            <>
                              <RefreshCw className="animate-spin -ml-1 mr-2 h-4 w-4" />
                              Flipping…
                            </>
                          ) : (
                            <>
                              <Zap className="w-4 h-4 mr-1" />
                              Rollback to here
                            </>
                          )}
                        </Button>
                      ) : (
                        <span className="text-xs text-muted-foreground">—</span>
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
      <Dialog open={confirmDialogOpen} onOpenChange={(open) => !rollbackInProgress && setConfirmDialogOpen(open)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <RotateCcw className="h-5 w-5" aria-hidden="true" />
              Instant rollback
            </DialogTitle>
            <DialogDescription>
              {selectedDeployment && currentDeployment ? (
                <>
                  Rollback <code className="font-mono">{serviceName}</code> from{' '}
                  <code className="font-mono">{shortSha(currentDeployment.git_sha) ?? shortId(currentDeployment.id)}</code> to{' '}
                  <code className="font-mono">{shortSha(selectedDeployment.git_sha) ?? shortId(selectedDeployment.id)}</code>?
                  This will take ~30s. ArgoCD reconciles in the background afterwards.
                </>
              ) : (
                'Flip traffic to the selected deployment at the routing layer.'
              )}
            </DialogDescription>
          </DialogHeader>

          <div className="grid gap-4 py-2">
            <div className="grid gap-2">
              <Label htmlFor="rollback-reason">Reason (optional)</Label>
              <Textarea
                id="rollback-reason"
                placeholder="e.g. 500s on /checkout after deploy; regression in commit abc123"
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                rows={2}
                disabled={rollbackInProgress}
              />
            </div>

            <div className="grid gap-2">
              <Label htmlFor="rollback-ticket">
                Change ticket URL{' '}
                {requireChangeTicket && <span className="text-status-error">*</span>}
                <span className="text-xs text-muted-foreground ml-2">(required in production)</span>
              </Label>
              <Input
                id="rollback-ticket"
                type="url"
                placeholder="https://..."
                value={changeTicketURL}
                onChange={(e) => setChangeTicketURL(e.target.value)}
                disabled={rollbackInProgress}
                aria-required={requireChangeTicket}
                aria-invalid={requireChangeTicket && !changeTicketURL.trim()}
              />
            </div>

            {rollbackErrorBanner && (
              <div
                className="text-sm text-status-error-foreground bg-status-error-muted border border-status-error/30 rounded-md p-3"
                role="alert"
              >
                {rollbackErrorBanner}
              </div>
            )}
          </div>

          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setConfirmDialogOpen(false)}
              disabled={rollbackInProgress}
            >
              Cancel
            </Button>
            <Button
              onClick={handleConfirmRollback}
              disabled={rollbackInProgress}
              data-testid="confirm-rollback"
            >
              {rollbackInProgress ? (
                <>
                  <RefreshCw className="animate-spin -ml-1 mr-2 h-4 w-4" />
                  Flipping…
                </>
              ) : (
                'Flip traffic'
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

// ------- helpers

function shortId(id: string | undefined | null): string {
  if (!id) return '';
  return id.length >= 8 ? id.slice(0, 8) : id;
}

function shortSha(sha: string | undefined | null): string | null {
  if (!sha) return null;
  return sha.length >= 7 ? sha.slice(0, 7) : sha;
}

function formatTookMS(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}
