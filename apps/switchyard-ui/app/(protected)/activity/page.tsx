'use client';

import { useState, useEffect, useCallback } from 'react';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import { apiGet } from '@/lib/api';
import { Spinner } from '@/components/ui/spinner';

interface AuditLog {
  id: string;
  timestamp: string;
  actor_id: string;
  actor_email: string;
  actor_role: string;
  action: string;
  resource_type: string;
  resource_id: string;
  resource_name: string;
  project_id?: string;
  environment_id?: string;
  ip_address: string;
  outcome: string;
  context: Record<string, unknown>;
  metadata: Record<string, unknown>;
}

interface ActivityResponse {
  activities: AuditLog[];
  count: number;
  limit: number;
  offset: number;
}

export default function ActivityPage() {
  const [activities, setActivities] = useState<AuditLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [actionFilter, setActionFilter] = useState('');
  const [resourceTypeFilter, setResourceTypeFilter] = useState('');
  const [searchQuery, setSearchQuery] = useState('');
  const [offset, setOffset] = useState(0);
  const [hasMore, setHasMore] = useState(true);
  const limit = 50;

  const fetchActivities = useCallback(async (reset = false) => {
    try {
      setError(null);
      const currentOffset = reset ? 0 : offset;

      const params = new URLSearchParams({
        limit: limit.toString(),
        offset: currentOffset.toString(),
      });

      if (actionFilter) {
        params.append('action', actionFilter);
      }
      if (resourceTypeFilter) {
        params.append('resource_type', resourceTypeFilter);
      }

      const data = await apiGet<ActivityResponse>(`/v1/activity?${params.toString()}`);

      if (reset) {
        setActivities(data.activities);
        setOffset(limit);
      } else {
        setActivities(prev => [...prev, ...data.activities]);
        setOffset(currentOffset + limit);
      }

      setHasMore(data.activities.length === limit);
    } catch (err) {
      console.error('Failed to fetch activities:', err);
      setError(err instanceof Error ? err.message : 'Failed to fetch activities');
    } finally {
      setLoading(false);
    }
  }, [offset, actionFilter, resourceTypeFilter]);

  useEffect(() => {
    fetchActivities(true);
  }, [actionFilter, resourceTypeFilter]);

  const handleLoadMore = () => {
    fetchActivities(false);
  };

  const getActionBadge = (action: string) => {
    const colors: Record<string, string> = {
      create: 'bg-status-success-muted text-status-success-foreground',
      update: 'bg-status-info-muted text-status-info-foreground',
      delete: 'bg-status-error-muted text-status-error-foreground',
      deploy: 'bg-purple-100 text-purple-800',
      rollback: 'bg-status-warning-muted text-status-warning-foreground',
      build: 'bg-indigo-100 text-indigo-800',
      login: 'bg-muted text-foreground',
      logout: 'bg-muted text-foreground',
    };
    return (
      <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${colors[action] || 'bg-muted text-foreground'}`}>
        {action}
      </span>
    );
  };

  const getResourceIcon = (resourceType: string) => {
    const icons: Record<string, React.ReactNode> = {
      project: (
        <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" />
        </svg>
      ),
      service: (
        <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2m-2-4h.01M17 16h.01" />
        </svg>
      ),
      deployment: (
        <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M7 16a4 4 0 01-.88-7.903A5 5 0 1115.9 6L16 6a5 5 0 011 9.9M15 13l-3-3m0 0l-3 3m3-3v12" />
        </svg>
      ),
      team: (
        <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M17 20h5v-2a3 3 0 00-5.356-1.857M17 20H7m10 0v-2c0-.656-.126-1.283-.356-1.857M7 20H2v-2a3 3 0 015.356-1.857M7 20v-2c0-.656.126-1.283.356-1.857m0 0a5.002 5.002 0 019.288 0M15 7a3 3 0 11-6 0 3 3 0 016 0zm6 3a2 2 0 11-4 0 2 2 0 014 0zM7 10a2 2 0 11-4 0 2 2 0 014 0z" />
        </svg>
      ),
    };
    return icons[resourceType] || (
      <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
      </svg>
    );
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
    if (days < 7) return `${days}d ago`;
    return date.toLocaleDateString();
  };

  const filteredActivities = searchQuery
    ? activities.filter(a =>
        a.actor_email.toLowerCase().includes(searchQuery.toLowerCase()) ||
        a.resource_name.toLowerCase().includes(searchQuery.toLowerCase()) ||
        a.action.toLowerCase().includes(searchQuery.toLowerCase())
      )
    : activities;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Activity</h1>
          <p className="text-muted-foreground">
            View all activity across your projects and services
          </p>
        </div>
        <Button variant="outline" onClick={() => fetchActivities(true)}>
          <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          Refresh
        </Button>
      </div>

      {/* Filters */}
      <Card>
        <CardContent className="pt-6">
          <div className="flex flex-wrap gap-4">
            <div className="flex-1 min-w-[200px]">
              <Input
                type="text"
                placeholder="Search by email, resource, or action..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
              />
            </div>
            <select
              value={actionFilter}
              onChange={(e) => setActionFilter(e.target.value)}
              className="px-3 py-2 border rounded-md bg-card"
            >
              <option value="">All Actions</option>
              <option value="create">Create</option>
              <option value="update">Update</option>
              <option value="delete">Delete</option>
              <option value="deploy">Deploy</option>
              <option value="rollback">Rollback</option>
              <option value="build">Build</option>
              <option value="login">Login</option>
              <option value="logout">Logout</option>
            </select>
            <select
              value={resourceTypeFilter}
              onChange={(e) => setResourceTypeFilter(e.target.value)}
              className="px-3 py-2 border rounded-md bg-card"
            >
              <option value="">All Resources</option>
              <option value="project">Project</option>
              <option value="service">Service</option>
              <option value="deployment">Deployment</option>
              <option value="release">Release</option>
              <option value="team">Team</option>
              <option value="user">User</option>
              <option value="env_var">Environment Variable</option>
              <option value="domain">Domain</option>
              <option value="preview">Preview</option>
            </select>
          </div>
        </CardContent>
      </Card>

      {/* Activity List */}
      <Card>
        <CardHeader>
          <CardTitle>Recent Activity</CardTitle>
          <CardDescription>
            {filteredActivities.length} activities shown
          </CardDescription>
        </CardHeader>
        <CardContent>
          {loading && activities.length === 0 ? (
            <div className="flex items-center justify-center py-12">
              <Spinner size="lg" />
              <span className="ml-3 text-muted-foreground">Loading activity...</span>
            </div>
          ) : error ? (
            <div className="text-center py-12">
              <p className="text-status-error mb-4">{error}</p>
              <Button variant="outline" onClick={() => fetchActivities(true)}>
                Try Again
              </Button>
            </div>
          ) : filteredActivities.length === 0 ? (
            <div className="text-center py-12 text-muted-foreground">
              <svg aria-hidden="true" className="w-12 h-12 mx-auto mb-4 text-muted-foreground/50" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
              <p className="text-lg font-medium">No activity found</p>
              <p className="text-sm mt-1">Activity will appear here as you use the platform.</p>
            </div>
          ) : (
            <div className="space-y-4">
              {filteredActivities.map((activity) => (
                <div
                  key={activity.id}
                  className="flex items-start gap-4 p-4 border rounded-lg hover:bg-accent"
                >
                  <div className="flex-shrink-0 p-2 bg-muted rounded-lg">
                    {getResourceIcon(activity.resource_type)}
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="font-medium text-sm">{activity.actor_email}</span>
                      {getActionBadge(activity.action)}
                      <span className="text-sm text-muted-foreground">{activity.resource_type}</span>
                      <span className="font-medium text-sm">{activity.resource_name}</span>
                    </div>
                    <div className="flex items-center gap-2 mt-1 text-xs text-muted-foreground">
                      <span>{formatRelativeTime(activity.timestamp)}</span>
                      <span>•</span>
                      <Badge variant={activity.outcome === 'success' ? 'default' : 'destructive'} className="text-xs">
                        {activity.outcome}
                      </Badge>
                      {activity.ip_address && (
                        <>
                          <span>•</span>
                          <span>{activity.ip_address}</span>
                        </>
                      )}
                    </div>
                  </div>
                </div>
              ))}

              {hasMore && (
                <div className="text-center pt-4">
                  <Button variant="outline" onClick={handleLoadMore} disabled={loading}>
                    {loading ? 'Loading...' : 'Load More'}
                  </Button>
                </div>
              )}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
