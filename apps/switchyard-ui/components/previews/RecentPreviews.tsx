'use client';

import { useState, useEffect, useCallback } from 'react';
import { usePolling } from '@/hooks/use-polling';
import { POLLING_IDLE } from '@/lib/constants';
import Link from 'next/link';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from "@enclii/ui-components/badge";
import { Button } from "@enclii/ui-components/button";
import { apiGet } from '@/lib/api';
import { Spinner } from '@/components/ui/spinner';
import { GitBranch, GitCommit, ExternalLink, Clock, Eye } from 'lucide-react';

interface RecentPreview {
  id: string;
  pr_number: number;
  pr_title: string;
  pr_url: string;
  pr_branch: string;
  pr_author?: string;
  commit_sha: string;
  preview_url: string;
  status: 'pending' | 'building' | 'deploying' | 'active' | 'sleeping' | 'failed' | 'closed';
  created_at: string;
  updated_at: string;
}

interface RecentPreviewsProps {
  serviceId: string;
  limit?: number;
}

const statusConfig: Record<string, { label: string; className: string }> = {
  pending: { label: 'Pending', className: 'bg-muted text-foreground' },
  building: { label: 'Building', className: 'bg-status-info-muted text-status-info-foreground animate-pulse' },
  deploying: { label: 'Deploying', className: 'bg-status-info-muted text-status-info-foreground animate-pulse' },
  active: { label: 'Active', className: 'bg-status-success-muted text-status-success-foreground' },
  sleeping: { label: 'Sleeping', className: 'bg-status-warning-muted text-status-warning-foreground' },
  failed: { label: 'Failed', className: 'bg-status-error-muted text-status-error-foreground' },
  closed: { label: 'Closed', className: 'bg-muted text-muted-foreground' },
};

function formatTimeAgo(dateString: string): string {
  const date = new Date(dateString);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMins = Math.floor(diffMs / 60000);
  const diffHours = Math.floor(diffMins / 60);
  const diffDays = Math.floor(diffHours / 24);

  if (diffDays > 0) return `${diffDays}d ago`;
  if (diffHours > 0) return `${diffHours}h ago`;
  if (diffMins > 0) return `${diffMins}m ago`;
  return 'Just now';
}

export function RecentPreviews({ serviceId, limit = 3 }: RecentPreviewsProps) {
  const [previews, setPreviews] = useState<RecentPreview[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchPreviews = useCallback(async () => {
    try {
      setError(null);
      const data = await apiGet<{ previews: RecentPreview[] }>(`/v1/services/${serviceId}/previews`);
      // Filter to active previews and limit
      const recentActive = (data.previews || [])
        .filter(p => !['closed', 'failed'].includes(p.status))
        .slice(0, limit);
      setPreviews(recentActive);
    } catch (err) {
      console.error('Failed to fetch previews:', err);
      setError(err instanceof Error ? err.message : 'Failed to load');
    } finally {
      setLoading(false);
    }
  }, [serviceId, limit]);

  useEffect(() => {
    fetchPreviews();
  }, [fetchPreviews]);

  usePolling(fetchPreviews, POLLING_IDLE);

  if (loading) {
    return (
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-sm font-medium flex items-center gap-2">
            <Eye className="h-4 w-4" />
            Recent Previews
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-center py-4">
            <Spinner />
          </div>
        </CardContent>
      </Card>
    );
  }

  if (error) {
    return (
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-sm font-medium flex items-center gap-2">
            <Eye className="h-4 w-4" />
            Recent Previews
          </CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground">{error}</p>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <CardTitle className="text-sm font-medium flex items-center gap-2">
            <Eye className="h-4 w-4" />
            Recent Previews
          </CardTitle>
          {previews.length > 0 && (
            <Badge variant="outline" className="text-xs">
              {previews.length} active
            </Badge>
          )}
        </div>
        <CardDescription className="text-xs">
          PR preview deployments
        </CardDescription>
      </CardHeader>
      <CardContent>
        {previews.length === 0 ? (
          <div className="text-center py-4 text-muted-foreground">
            <p className="text-sm">No active previews</p>
            <p className="text-xs mt-1">Open a PR to create a preview</p>
          </div>
        ) : (
          <div className="space-y-3">
            {previews.map((preview) => {
              const config = statusConfig[preview.status] || statusConfig.pending;
              return (
                <div
                  key={preview.id}
                  className="flex items-start justify-between border-b border-border pb-3 last:border-0 last:pb-0"
                >
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <Badge className={`text-xs ${config.className}`}>
                        {config.label}
                      </Badge>
                      <a
                        href={preview.pr_url}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="text-sm font-medium hover:underline truncate flex items-center gap-1"
                      >
                        PR #{preview.pr_number}
                        <ExternalLink className="h-3 w-3" />
                      </a>
                    </div>
                    <p className="text-xs text-muted-foreground mt-1 truncate" title={preview.pr_title}>
                      {preview.pr_title || 'Untitled PR'}
                    </p>
                    <div className="flex items-center gap-3 mt-1 text-xs text-muted-foreground">
                      <span className="flex items-center gap-1">
                        <GitBranch className="h-3 w-3" />
                        {preview.pr_branch}
                      </span>
                      <span className="flex items-center gap-1">
                        <GitCommit className="h-3 w-3" />
                        {preview.commit_sha.substring(0, 7)}
                      </span>
                      <span className="flex items-center gap-1">
                        <Clock className="h-3 w-3" />
                        {formatTimeAgo(preview.updated_at)}
                      </span>
                    </div>
                  </div>
                  {preview.status === 'active' && (
                    <Button
                      size="sm"
                      variant="ghost"
                      className="h-7 px-2"
                      onClick={() => window.open(preview.preview_url, '_blank')}
                    >
                      <ExternalLink className="h-3 w-3" />
                    </Button>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
