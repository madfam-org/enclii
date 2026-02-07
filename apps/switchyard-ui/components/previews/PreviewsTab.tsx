'use client';

import { useState, useEffect } from 'react';
import { usePolling } from '@/hooks/use-polling';
import { POLLING_SLOW } from '@/lib/constants';
import { toast } from 'sonner';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';
import { apiGet, apiPost, apiDelete } from '@/lib/api';
import { Spinner } from '@/components/ui/spinner';
import { formatRelativeTime } from '@/lib/formatting';
import { PreviewEnvironment, PreviewEnvironmentListResponse, PreviewEnvironmentStatus } from './types';

interface PreviewsTabProps {
  serviceId: string;
  serviceName: string;
}

const statusConfig: Record<PreviewEnvironmentStatus, { label: string; variant: 'default' | 'secondary' | 'destructive' | 'outline'; className: string }> = {
  pending: { label: 'Pending', variant: 'secondary', className: 'bg-muted text-foreground' },
  building: { label: 'Building', variant: 'default', className: 'bg-status-info-muted text-status-info-foreground animate-pulse' },
  deploying: { label: 'Deploying', variant: 'default', className: 'bg-status-info-muted text-status-info-foreground animate-pulse' },
  active: { label: 'Active', variant: 'default', className: 'bg-status-success-muted text-status-success-foreground' },
  sleeping: { label: 'Sleeping', variant: 'secondary', className: 'bg-status-warning-muted text-status-warning-foreground' },
  failed: { label: 'Failed', variant: 'destructive', className: 'bg-status-error-muted text-status-error-foreground' },
  closed: { label: 'Closed', variant: 'outline', className: 'bg-muted text-muted-foreground' },
};

// Generate GitHub avatar URL from username
function getGitHubAvatarUrl(username: string, size: number = 40): string {
  return `https://github.com/${username}.png?size=${size}`;
}

// Generate commit URL from PR URL and commit SHA
function getCommitUrl(prUrl?: string, commitSha?: string): string | undefined {
  if (!prUrl || !commitSha) return undefined;
  // PR URL format: https://github.com/owner/repo/pull/123
  // Commit URL format: https://github.com/owner/repo/commit/sha
  try {
    const url = new URL(prUrl);
    const pathParts = url.pathname.split('/');
    // pathParts: ['', 'owner', 'repo', 'pull', '123']
    if (pathParts.length >= 4) {
      const owner = pathParts[1];
      const repo = pathParts[2];
      return `https://github.com/${owner}/${repo}/commit/${commitSha}`;
    }
  } catch {
    return undefined;
  }
  return undefined;
}

export function PreviewsTab({ serviceId, serviceName }: PreviewsTabProps) {
  const [previews, setPreviews] = useState<PreviewEnvironment[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [confirmAction, setConfirmAction] = useState<{ type: 'close' | 'delete'; preview: PreviewEnvironment } | null>(null);

  const fetchPreviews = async () => {
    try {
      setError(null);
      const data = await apiGet<PreviewEnvironmentListResponse>(`/v1/services/${serviceId}/previews`);
      setPreviews(data.previews || []);
    } catch (err) {
      console.error('Failed to fetch previews:', err);
      setError(err instanceof Error ? err.message : 'Failed to load preview environments');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchPreviews();
  }, [serviceId]);

  usePolling(fetchPreviews, POLLING_SLOW);

  const handleWake = async (preview: PreviewEnvironment) => {
    setActionLoading(preview.id);
    try {
      await apiPost(`/v1/previews/${preview.id}/wake`, {});
      await fetchPreviews();
    } catch (err) {
      console.error('Failed to wake preview:', err);
      toast.error('Failed to wake preview: ' + (err instanceof Error ? err.message : 'Unknown error'));
    } finally {
      setActionLoading(null);
    }
  };

  const handleClose = (preview: PreviewEnvironment) => {
    setConfirmAction({ type: 'close', preview });
  };

  const handleDelete = (preview: PreviewEnvironment) => {
    setConfirmAction({ type: 'delete', preview });
  };

  const handleConfirmAction = async () => {
    if (!confirmAction) return;
    const { type, preview } = confirmAction;
    setConfirmAction(null);
    setActionLoading(preview.id);
    try {
      if (type === 'close') {
        await apiPost(`/v1/previews/${preview.id}/close`, {});
      } else {
        await apiDelete(`/v1/previews/${preview.id}`);
      }
      await fetchPreviews();
    } catch (err) {
      const action = type === 'close' ? 'close' : 'delete';
      console.error(`Failed to ${action} preview:`, err);
      toast.error(`Failed to ${action} preview: ` + (err instanceof Error ? err.message : 'Unknown error'));
    } finally {
      setActionLoading(null);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Spinner size="lg" />
        <span className="ml-3 text-muted-foreground">Loading preview environments...</span>
      </div>
    );
  }

  if (error) {
    return (
      <Card className="border-status-error/30 bg-status-error-muted">
        <CardContent className="py-8">
          <div className="text-center">
            <p className="text-status-error font-medium mb-4">{error}</p>
            <Button variant="outline" onClick={fetchPreviews}>
              Try Again
            </Button>
          </div>
        </CardContent>
      </Card>
    );
  }

  const activePreviews = previews.filter(p => !['closed', 'failed'].includes(p.status));
  const closedPreviews = previews.filter(p => ['closed', 'failed'].includes(p.status));

  return (
    <div className="space-y-6">
      {/* Header */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <svg aria-hidden="true" className="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4M10 17l5-5-5-5M15 12H3" />
            </svg>
            Preview Environments
          </CardTitle>
          <CardDescription>
            Automatic PR-based preview deployments. Create a pull request to get a preview URL.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex items-center gap-4 text-sm text-muted-foreground">
            <div className="flex items-center gap-1">
              <span className="inline-block w-2 h-2 rounded-full bg-status-success"></span>
              <span>{activePreviews.filter(p => p.status === 'active').length} Active</span>
            </div>
            <div className="flex items-center gap-1">
              <span className="inline-block w-2 h-2 rounded-full bg-status-info animate-pulse"></span>
              <span>{activePreviews.filter(p => ['building', 'deploying', 'pending'].includes(p.status)).length} In Progress</span>
            </div>
            <div className="flex items-center gap-1">
              <span className="inline-block w-2 h-2 rounded-full bg-status-warning"></span>
              <span>{activePreviews.filter(p => p.status === 'sleeping').length} Sleeping</span>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Active Previews */}
      {activePreviews.length > 0 && (
        <div className="space-y-4">
          <h3 className="text-lg font-semibold">Active Previews</h3>
          {activePreviews.map((preview) => (
            <PreviewCard
              key={preview.id}
              preview={preview}
              onWake={handleWake}
              onClose={handleClose}
              onDelete={handleDelete}
              actionLoading={actionLoading === preview.id}
            />
          ))}
        </div>
      )}

      {/* Empty State */}
      {activePreviews.length === 0 && (
        <Card>
          <CardContent className="py-12">
            <div className="text-center">
              <svg aria-hidden="true" className="mx-auto h-12 w-12 text-muted-foreground" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" />
              </svg>
              <h3 className="mt-4 text-lg font-medium text-foreground">No active previews</h3>
              <p className="mt-2 text-sm text-muted-foreground">
                Preview environments are automatically created when you open a pull request.
              </p>
              <p className="mt-1 text-sm text-muted-foreground">
                Each PR gets a unique URL like <code className="bg-muted px-1 rounded">pr-123-{serviceName.toLowerCase()}.preview.enclii.app</code>
              </p>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Closed Previews */}
      {closedPreviews.length > 0 && (
        <div className="space-y-4">
          <h3 className="text-lg font-semibold text-muted-foreground">Closed Previews</h3>
          {closedPreviews.slice(0, 5).map((preview) => (
            <PreviewCard
              key={preview.id}
              preview={preview}
              onWake={handleWake}
              onClose={handleClose}
              onDelete={handleDelete}
              actionLoading={actionLoading === preview.id}
              isHistorical
            />
          ))}
          {closedPreviews.length > 5 && (
            <p className="text-sm text-muted-foreground text-center">
              + {closedPreviews.length - 5} more closed previews
            </p>
          )}
        </div>
      )}

      <ConfirmDialog
        open={!!confirmAction}
        title={confirmAction?.type === 'close' ? 'Close Preview' : 'Delete Preview'}
        description={
          confirmAction?.type === 'close'
            ? `Close preview for PR #${confirmAction.preview.pr_number}? This will stop the preview deployment.`
            : `Permanently delete preview for PR #${confirmAction?.preview.pr_number}? This cannot be undone.`
        }
        variant="destructive"
        confirmLabel={confirmAction?.type === 'close' ? 'Close' : 'Delete'}
        onConfirm={handleConfirmAction}
        onCancel={() => setConfirmAction(null)}
      />
    </div>
  );
}

interface PreviewCardProps {
  preview: PreviewEnvironment;
  onWake: (preview: PreviewEnvironment) => void;
  onClose: (preview: PreviewEnvironment) => void;
  onDelete: (preview: PreviewEnvironment) => void;
  actionLoading: boolean;
  isHistorical?: boolean;
}

function PreviewCard({ preview, onWake, onClose, onDelete, actionLoading, isHistorical }: PreviewCardProps) {
  const config = statusConfig[preview.status];

  return (
    <Card className={isHistorical ? 'opacity-60' : ''}>
      <CardContent className="py-4">
        <div className="flex items-start justify-between">
          <div className="flex-1 min-w-0">
            {/* PR Info */}
            <div className="flex items-center gap-3">
              <Badge className={config.className}>
                {config.label}
              </Badge>
              <a
                href={preview.pr_url}
                target="_blank"
                rel="noopener noreferrer"
                className="text-lg font-medium hover:underline truncate"
              >
                PR #{preview.pr_number}: {preview.pr_title || 'Untitled'}
              </a>
            </div>

            {/* Author Avatar and Branch/Commit */}
            <div className="mt-2 flex items-center gap-3 text-sm text-muted-foreground">
              {/* Author Avatar */}
              {preview.pr_author && (
                <a
                  href={`https://github.com/${preview.pr_author}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex items-center gap-2 hover:text-foreground transition-colors"
                  title={`View ${preview.pr_author} on GitHub`}
                >
                  <img
                    src={preview.pr_author_avatar_url || getGitHubAvatarUrl(preview.pr_author)}
                    alt={preview.pr_author}
                    className="h-5 w-5 rounded-full border border-border"
                  />
                  <span className="font-medium">{preview.pr_author}</span>
                </a>
              )}

              <span className="text-muted-foreground/50">•</span>

              {/* Branch */}
              <span className="flex items-center gap-1">
                <svg aria-hidden="true" className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                  <line x1="6" y1="3" x2="6" y2="15" />
                  <circle cx="18" cy="6" r="3" />
                  <circle cx="6" cy="18" r="3" />
                  <path d="M18 9a9 9 0 0 1-9 9" />
                </svg>
                <span className="font-mono text-xs">{preview.pr_branch}</span>
              </span>

              <span className="text-muted-foreground/50">•</span>

              {/* Commit SHA with deep link */}
              {(() => {
                const commitUrl = preview.commit_url || getCommitUrl(preview.pr_url, preview.commit_sha);
                return commitUrl ? (
                  <a
                    href={commitUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="flex items-center gap-1 hover:text-foreground transition-colors font-mono text-xs"
                    title="View commit on GitHub"
                  >
                    <svg aria-hidden="true" className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                      <circle cx="12" cy="12" r="4" />
                      <line x1="1.05" y1="12" x2="7" y2="12" />
                      <line x1="17.01" y1="12" x2="22.96" y2="12" />
                    </svg>
                    {preview.commit_sha.substring(0, 7)}
                  </a>
                ) : (
                  <span className="flex items-center gap-1 font-mono text-xs">
                    <svg aria-hidden="true" className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                      <circle cx="12" cy="12" r="4" />
                      <line x1="1.05" y1="12" x2="7" y2="12" />
                      <line x1="17.01" y1="12" x2="22.96" y2="12" />
                    </svg>
                    {preview.commit_sha.substring(0, 7)}
                  </span>
                );
              })()}

              <span className="text-muted-foreground/50">•</span>

              {/* Time ago */}
              <span>{formatRelativeTime(preview.created_at)}</span>
            </div>

            {/* Preview URL */}
            {preview.status === 'active' && (
              <div className="mt-3">
                <a
                  href={preview.preview_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-2 text-sm text-primary hover:text-primary/80"
                >
                  <svg aria-hidden="true" className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                    <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />
                    <polyline points="15 3 21 3 21 9" />
                    <line x1="10" y1="14" x2="21" y2="3" />
                  </svg>
                  {preview.preview_url}
                </a>
              </div>
            )}

            {/* Status Message */}
            {preview.status_message && ['building', 'deploying', 'failed'].includes(preview.status) && (
              <p className="mt-2 text-sm text-muted-foreground">
                {preview.status_message}
              </p>
            )}

            {/* Sleeping Info */}
            {preview.status === 'sleeping' && preview.sleeping_since && (
              <p className="mt-2 text-sm text-status-warning">
                Sleeping since {formatRelativeTime(preview.sleeping_since)} (auto-sleep after {preview.auto_sleep_after} minutes of inactivity)
              </p>
            )}
          </div>

          {/* Actions */}
          <div className="flex items-center gap-2 ml-4">
            {preview.status === 'sleeping' && (
              <Button
                size="sm"
                variant="outline"
                onClick={() => onWake(preview)}
                disabled={actionLoading}
              >
                {actionLoading ? 'Waking...' : 'Wake Up'}
              </Button>
            )}
            {preview.status === 'active' && (
              <Button
                size="sm"
                variant="outline"
                onClick={() => window.open(preview.preview_url, '_blank')}
              >
                Open
              </Button>
            )}
            {!['closed', 'failed'].includes(preview.status) && (
              <Button
                size="sm"
                variant="ghost"
                onClick={() => onClose(preview)}
                disabled={actionLoading}
              >
                Close
              </Button>
            )}
            {['closed', 'failed'].includes(preview.status) && (
              <Button
                size="sm"
                variant="ghost"
                className="text-status-error hover:text-status-error/80"
                onClick={() => onDelete(preview)}
                disabled={actionLoading}
              >
                Delete
              </Button>
            )}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
