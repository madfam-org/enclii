'use client';

import { useState, useEffect, useCallback } from 'react';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import { apiGet, apiPost } from '@/lib/api';
import { Spinner } from '@/components/ui/spinner';
import Link from 'next/link';

interface CustomDomain {
  id: string;
  service_id: string;
  environment_id: string;
  domain: string;
  verified: boolean;
  tls_enabled: boolean;
  tls_issuer: string;
  created_at: string;
  updated_at: string;
  verified_at?: string;
  is_platform_domain: boolean;
  zero_trust_enabled: boolean;
  status: string;
  dns_cname?: string;
  service_name: string;
  environment_name: string;
  project_slug?: string;
}

interface DomainsResponse {
  domains: CustomDomain[];
  total: number;
  limit: number;
  offset: number;
}

interface DomainStats {
  total_domains: number;
  verified_domains: number;
  pending_domains: number;
  tls_enabled: number;
  platform_domains: number;
  custom_domains: number;
}

export default function DomainsPage() {
  const [domains, setDomains] = useState<CustomDomain[]>([]);
  const [stats, setStats] = useState<DomainStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [syncing, setSyncing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [verifiedFilter, setVerifiedFilter] = useState<string>('');
  const [offset, setOffset] = useState(0);
  const [total, setTotal] = useState(0);
  const limit = 50;

  const fetchDomains = useCallback(async (reset = false) => {
    try {
      setError(null);
      const currentOffset = reset ? 0 : offset;

      const params = new URLSearchParams({
        limit: limit.toString(),
        offset: currentOffset.toString(),
      });

      if (verifiedFilter === 'verified') {
        params.append('verified', 'true');
      } else if (verifiedFilter === 'pending') {
        params.append('verified', 'false');
      }

      const data = await apiGet<DomainsResponse>(`/v1/domains?${params.toString()}`);

      if (reset) {
        setDomains(data.domains || []);
        setOffset(limit);
      } else {
        setDomains(prev => [...prev, ...(data.domains || [])]);
        setOffset(currentOffset + limit);
      }

      setTotal(data.total);
    } catch (err) {
      console.error('Failed to fetch domains:', err);
      setError(err instanceof Error ? err.message : 'Failed to fetch domains');
    } finally {
      setLoading(false);
    }
  }, [offset, verifiedFilter]);

  const fetchStats = useCallback(async () => {
    try {
      const data = await apiGet<DomainStats>('/v1/domains/stats');
      setStats(data);
    } catch (err) {
      console.error('Failed to fetch domain stats:', err);
    }
  }, []);

  const syncFromCloudflare = useCallback(async () => {
    try {
      setSyncing(true);
      setError(null);
      await apiPost('/v1/domains/sync', {});
      // Refresh data after sync
      await fetchDomains(true);
      await fetchStats();
    } catch (err) {
      console.error('Failed to sync from Cloudflare:', err);
      setError(err instanceof Error ? err.message : 'Failed to sync from Cloudflare');
    } finally {
      setSyncing(false);
    }
  }, [fetchDomains, fetchStats]);

  useEffect(() => {
    fetchDomains(true);
    fetchStats();
  }, [verifiedFilter]);

  const handleLoadMore = () => {
    fetchDomains(false);
  };

  const getStatusBadge = (domain: CustomDomain) => {
    if (domain.verified) {
      return <Badge variant="default" className="bg-status-success-muted text-status-success-foreground">Verified</Badge>;
    }
    return <Badge variant="secondary" className="bg-status-warning-muted text-status-warning-foreground">Pending</Badge>;
  };

  const getTLSBadge = (domain: CustomDomain) => {
    if (domain.tls_enabled) {
      return (
        <Badge variant="outline" className="text-status-success border-status-success">
          <svg aria-hidden="true" className="w-3 h-3 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
          </svg>
          TLS
        </Badge>
      );
    }
    return (
      <Badge variant="outline" className="text-muted-foreground border-border">
        No TLS
      </Badge>
    );
  };

  const filteredDomains = searchQuery
    ? domains.filter(d =>
        d.domain.toLowerCase().includes(searchQuery.toLowerCase()) ||
        d.service_name?.toLowerCase().includes(searchQuery.toLowerCase()) ||
        d.environment_name?.toLowerCase().includes(searchQuery.toLowerCase())
      )
    : domains;

  const hasMore = domains.length < total;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Domains</h1>
          <p className="text-muted-foreground">
            Manage all custom domains across your services
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={() => { fetchDomains(true); fetchStats(); }} disabled={loading}>
            <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
            Refresh
          </Button>
          <Button onClick={syncFromCloudflare} disabled={syncing}>
            {syncing ? (
              <>
                <Spinner size="sm" className="mr-2" />
                Syncing...
              </>
            ) : (
              <>
                <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M7 16a4 4 0 01-.88-7.903A5 5 0 1115.9 6L16 6a5 5 0 011 9.9M9 19l3 3m0 0l3-3m-3 3V10" />
                </svg>
                Sync from Cloudflare
              </>
            )}
          </Button>
        </div>
      </div>

      {/* Stats Cards */}
      {stats && (
        <div className="grid gap-4 md:grid-cols-4">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Total Domains</CardTitle>
              <svg aria-hidden="true" className="w-4 h-4 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9" />
              </svg>
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{stats.total_domains}</div>
              <p className="text-xs text-muted-foreground">
                {stats.platform_domains} platform, {stats.custom_domains} custom
              </p>
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Verified</CardTitle>
              <svg aria-hidden="true" className="w-4 h-4 text-status-success" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold text-status-success">{stats.verified_domains}</div>
              <p className="text-xs text-muted-foreground">Active and serving traffic</p>
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Pending</CardTitle>
              <svg aria-hidden="true" className="w-4 h-4 text-status-warning" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold text-status-warning">{stats.pending_domains}</div>
              <p className="text-xs text-muted-foreground">Awaiting DNS verification</p>
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">TLS Enabled</CardTitle>
              <svg aria-hidden="true" className="w-4 h-4 text-status-info" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
              </svg>
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold text-status-info">{stats.tls_enabled}</div>
              <p className="text-xs text-muted-foreground">HTTPS secured</p>
            </CardContent>
          </Card>
        </div>
      )}

      {/* Filters */}
      <Card>
        <CardContent className="pt-6">
          <div className="flex flex-wrap gap-4">
            <div className="flex-1 min-w-[200px]">
              <Input
                type="text"
                placeholder="Search domains, services..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
              />
            </div>
            <select
              value={verifiedFilter}
              onChange={(e) => setVerifiedFilter(e.target.value)}
              className="px-3 py-2 border rounded-md bg-card"
            >
              <option value="">All Status</option>
              <option value="verified">Verified</option>
              <option value="pending">Pending</option>
            </select>
          </div>
        </CardContent>
      </Card>

      {/* Domains List */}
      <Card>
        <CardHeader>
          <CardTitle>All Domains</CardTitle>
          <CardDescription>
            {filteredDomains.length} of {total} domains shown
          </CardDescription>
        </CardHeader>
        <CardContent>
          {loading && domains.length === 0 ? (
            <div className="flex items-center justify-center py-12">
              <Spinner size="lg" />
              <span className="ml-3 text-muted-foreground">Loading domains...</span>
            </div>
          ) : error ? (
            <div className="text-center py-12">
              <p className="text-status-error mb-4">{error}</p>
              <Button variant="outline" onClick={() => fetchDomains(true)}>
                Try Again
              </Button>
            </div>
          ) : filteredDomains.length === 0 ? (
            <div className="text-center py-12 text-muted-foreground">
              <svg className="w-12 h-12 mx-auto mb-4 text-muted-foreground/50" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9" />
              </svg>
              <p className="text-lg font-medium">No domains found</p>
              <p className="text-sm mt-1">Add domains to your services to see them here.</p>
            </div>
          ) : (
            <div className="space-y-4">
              <div className="overflow-x-auto">
                <table className="w-full">
                  <thead>
                    <tr className="border-b">
                      <th className="text-left py-3 px-4 font-medium text-muted-foreground">Domain</th>
                      <th className="text-left py-3 px-4 font-medium text-muted-foreground">Service</th>
                      <th className="text-left py-3 px-4 font-medium text-muted-foreground">Environment</th>
                      <th className="text-left py-3 px-4 font-medium text-muted-foreground">Status</th>
                      <th className="text-left py-3 px-4 font-medium text-muted-foreground">TLS</th>
                      <th className="text-left py-3 px-4 font-medium text-muted-foreground">Added</th>
                      <th className="text-left py-3 px-4 font-medium text-muted-foreground">Actions</th>
                    </tr>
                  </thead>
                  <tbody>
                    {filteredDomains.map((domain) => (
                      <tr key={domain.id} className="border-b hover:bg-accent">
                        <td className="py-3 px-4">
                          <div className="flex items-center gap-2">
                            <svg aria-hidden="true" className="w-4 h-4 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9" />
                            </svg>
                            <a
                              href={`https://${domain.domain}`}
                              target="_blank"
                              rel="noopener noreferrer"
                              className="font-mono text-sm text-primary hover:underline"
                            >
                              {domain.domain}
                            </a>
                            {domain.is_platform_domain && (
                              <Badge variant="outline" className="text-xs">Platform</Badge>
                            )}
                            {domain.zero_trust_enabled && (
                              <Badge variant="outline" className="text-purple-600 border-purple-600 text-xs">
                                <svg aria-hidden="true" className="w-3 h-3 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" />
                                </svg>
                                Zero Trust
                              </Badge>
                            )}
                          </div>
                        </td>
                        <td className="py-3 px-4">
                          <Link
                            href={`/services/${domain.service_id}`}
                            className="text-sm hover:underline"
                          >
                            {domain.service_name || 'Unknown'}
                          </Link>
                        </td>
                        <td className="py-3 px-4">
                          <span className="text-sm text-muted-foreground">
                            {domain.environment_name || 'Unknown'}
                          </span>
                        </td>
                        <td className="py-3 px-4">
                          {getStatusBadge(domain)}
                        </td>
                        <td className="py-3 px-4">
                          {getTLSBadge(domain)}
                        </td>
                        <td className="py-3 px-4 text-sm text-muted-foreground">
                          {new Date(domain.created_at).toLocaleDateString()}
                        </td>
                        <td className="py-3 px-4">
                          <div className="flex items-center gap-2">
                            <Link href={`/services/${domain.service_id}`}>
                              <Button variant="ghost" size="sm" aria-label="Service settings">
                                <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
                                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
                                </svg>
                              </Button>
                            </Link>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

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
