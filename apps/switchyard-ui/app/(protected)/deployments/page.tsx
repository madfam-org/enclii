'use client';

import { useState, useEffect, useCallback } from "react";
import { usePolling } from "@/hooks/use-polling";
import { POLLING_SLOW } from "@/lib/constants";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { RefreshCw } from "lucide-react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Spinner } from "@/components/ui/spinner";
import { Badge } from "@/components/ui/badge";
import { DeploymentProgress, DeploymentProgressSkeleton, type DeploymentStage } from "@/components/dashboard/deployment-progress";
import { AuthorAvatar, CommitLink } from "@/components/git";
import { GitBranch } from "lucide-react";
import { apiGet } from "@/lib/api";
import type { Deployment, DeploymentsListResponse } from "@/components/deployments/types";

interface ServiceListResponse {
  services: Array<{ id: string; name: string }>;
}

export default function DeploymentsPage() {
  const router = useRouter();
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [activeDeployments, setActiveDeployments] = useState<Deployment[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchDeployments = async (isManualRefresh = false) => {
    try {
      setError(null);
      if (isManualRefresh) setRefreshing(true);

      // Try the cross-service deployments endpoint first
      let allDeployments: Deployment[] = [];

      try {
        const data = await apiGet<{ deployments: Deployment[]; count: number }>('/v1/deployments');
        allDeployments = data.deployments || [];
      } catch {
        // Fallback: aggregate from services
        const dashData = await apiGet<{ services: Array<{ id: string; name: string }> }>('/v1/dashboard/stats');
        const services = dashData.services || [];

        const results = await Promise.allSettled(
          services.map(async (svc) => {
            const data = await apiGet<DeploymentsListResponse>(`/v1/services/${svc.id}/deployments`);
            return (data.deployments || []).map((d) => ({ ...d, service_name: svc.name }));
          })
        );

        for (const result of results) {
          if (result.status === 'fulfilled') {
            allDeployments.push(...result.value);
          }
        }

        // Sort by created_at desc
        allDeployments.sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime());
      }

      const active = allDeployments.filter((d) => d.status === 'deploying' || d.status === 'pending');
      const history = allDeployments.filter((d) => d.status !== 'deploying' && d.status !== 'pending');

      setActiveDeployments(active);
      setDeployments(history);
      setLoading(false);
      setRefreshing(false);
    } catch (err) {
      console.error("Failed to fetch deployments:", err);
      setError(err instanceof Error ? err.message : "Failed to fetch deployments");
      setLoading(false);
      setRefreshing(false);
    }
  };

  const getDeploymentStage = (deployment: Deployment): DeploymentStage => {
    if (deployment.status === 'deploying') return 'deploying';
    if (deployment.status === 'pending') return 'building';
    if (deployment.status === 'failed') return 'failed';
    if (deployment.status === 'running') return 'completed';
    return 'deploying';
  };

  useEffect(() => {
    fetchDeployments();
  }, []);

  usePolling(fetchDeployments, POLLING_SLOW);

  const formatTimeAgo = useCallback((timestamp: string) => {
    const now = new Date();
    const diff = now.getTime() - new Date(timestamp).getTime();
    const seconds = Math.floor(diff / 1000);
    if (seconds < 60) return `${seconds} seconds ago`;
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return `${minutes} minute${minutes > 1 ? "s" : ""} ago`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours} hour${hours > 1 ? "s" : ""} ago`;
    const days = Math.floor(hours / 24);
    return `${days} day${days > 1 ? "s" : ""} ago`;
  }, []);

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

  const getStatusDotClass = (status: string) => {
    switch (status) {
      case "running": return "bg-status-success";
      case "deploying": case "pending": return "bg-status-info animate-pulse";
      case "failed": return "bg-status-error";
      default: return "bg-status-warning";
    }
  };

  if (loading) {
    return (
      <div className="container mx-auto py-8">
        <div className="mb-8">
          <h1 className="text-3xl font-bold">Deployments</h1>
          <p className="text-muted-foreground mt-2">Track and manage your deployment history</p>
        </div>
        <Card>
          <CardContent className="py-12">
            <div className="flex items-center justify-center">
              <Spinner size="lg" />
              <span className="ml-3 text-muted-foreground">Loading deployments...</span>
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
          <h1 className="text-3xl font-bold">Deployments</h1>
          <p className="text-muted-foreground mt-2">Track and manage your deployment history</p>
        </div>
        <Card className="border-status-error/30 bg-status-error-muted">
          <CardContent className="py-8">
            <div className="text-center">
              <p className="text-status-error font-medium mb-4">{error}</p>
              <button
                onClick={() => fetchDeployments()}
                className="inline-flex items-center px-4 py-2 border border-status-error/30 rounded-md shadow-sm text-sm font-medium text-status-error-foreground bg-background hover:bg-status-error-muted"
              >
                Try Again
              </button>
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
          <h1 className="text-3xl font-bold">Deployments</h1>
          <p className="text-muted-foreground mt-2">Track and manage your deployment history</p>
        </div>
        <button
          onClick={() => fetchDeployments(true)}
          disabled={refreshing}
          className="inline-flex items-center px-4 py-2 border border-input rounded-md shadow-sm text-sm font-medium text-foreground bg-background hover:bg-accent disabled:opacity-50 disabled:cursor-not-allowed"
        >
          <RefreshCw className={`w-4 h-4 mr-2 ${refreshing ? 'animate-spin' : ''}`} />
          {refreshing ? 'Refreshing...' : 'Refresh'}
        </button>
      </div>

      {/* Active Deployments */}
      {activeDeployments.length > 0 && (
        <div className="mb-8 space-y-4">
          <h2 className="text-lg font-semibold text-foreground">Active Deployments</h2>
          {activeDeployments.map((deployment) => (
            <div
              key={deployment.id}
              className="cursor-pointer"
              onClick={() => router.push(`/deployments/${deployment.id}`)}
            >
              <DeploymentProgress
                releaseId={deployment.id}
                serviceName={deployment.service_name || "Unknown Service"}
                currentStage={getDeploymentStage(deployment)}
                startedAt={deployment.created_at}
                onComplete={fetchDeployments}
              />
            </div>
          ))}
        </div>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Deployment History</CardTitle>
          <CardDescription>
            View all deployments across your services ({deployments.length} total)
          </CardDescription>
        </CardHeader>
        <CardContent>
          {deployments.length === 0 ? (
            <div className="text-center py-12">
              <div className="mx-auto w-12 h-12 rounded-full bg-muted flex items-center justify-center mb-4">
                <svg aria-hidden="true" className="w-6 h-6 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4" />
                </svg>
              </div>
              <p className="text-lg font-medium text-foreground">No deployments found</p>
              <p className="text-sm text-muted-foreground mt-2 max-w-md mx-auto">
                Once you deploy a service, your deployment history will appear here.
              </p>
              <div className="mt-6 space-y-2 text-left max-w-md mx-auto bg-muted/50 rounded-lg p-4 border border-border">
                <p className="text-sm font-medium text-foreground">Possible reasons:</p>
                <ul className="text-sm text-muted-foreground space-y-1 list-disc list-inside">
                  <li>No services have been registered yet</li>
                  <li>Services exist but have no deployments</li>
                  <li>Webhook hasn&apos;t triggered a build yet</li>
                </ul>
                <p className="text-sm text-muted-foreground mt-3">
                  Check the{" "}
                  <Link href="/services" className="text-primary hover:underline">Services page</Link>{" "}
                  to verify your services are registered.
                </p>
              </div>
            </div>
          ) : (
            <div className="space-y-3">
              {deployments.map((deployment) => (
                <div
                  key={deployment.id}
                  onClick={() => router.push(`/deployments/${deployment.id}`)}
                  className="flex items-center justify-between p-4 bg-muted/50 rounded-lg hover:bg-muted transition-colors border border-border cursor-pointer"
                >
                  <div className="flex items-center space-x-4 min-w-0">
                    <div className={`w-3 h-3 rounded-full flex-shrink-0 ${getStatusDotClass(deployment.status)}`} />
                    <div className="min-w-0">
                      <div className="flex items-center gap-2 flex-wrap">
                        {deployment.service_name && (
                          <span className="font-medium text-foreground">{deployment.service_name}</span>
                        )}
                        {deployment.git_branch && (
                          <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                            <GitBranch className="h-3 w-3" />
                            <span className="truncate max-w-[120px]">{deployment.git_branch}</span>
                          </span>
                        )}
                        {deployment.git_sha && (
                          <span className="text-xs font-mono text-muted-foreground">
                            {deployment.git_sha.slice(0, 7)}
                          </span>
                        )}
                      </div>
                      <div className="flex items-center gap-2 text-sm text-muted-foreground mt-1">
                        {deployment.pr_number && (
                          <span>PR #{deployment.pr_number}</span>
                        )}
                        {deployment.commit_message && (
                          <span className="truncate max-w-[300px]">{deployment.commit_message}</span>
                        )}
                        <span>•</span>
                        <span>{formatTimeAgo(deployment.created_at)}</span>
                        {deployment.commit_author && (
                          <>
                            <span>•</span>
                            <AuthorAvatar
                              name={deployment.commit_author}
                              username={deployment.commit_author_username}
                              email={deployment.commit_author_email}
                              avatarUrl={deployment.commit_author_avatar_url}
                              size="xs"
                              showName
                            />
                          </>
                        )}
                      </div>
                    </div>
                  </div>
                  <Badge variant={getStatusVariant(deployment.status)} className="flex-shrink-0 ml-4">
                    {deployment.status}
                  </Badge>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
