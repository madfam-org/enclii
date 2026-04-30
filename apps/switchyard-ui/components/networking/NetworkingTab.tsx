'use client';

import { useState, useEffect } from 'react';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from "@enclii/ui-components/button";
import { Badge } from "@enclii/ui-components/badge";
import { apiGet } from '@/lib/api';
import { Spinner } from '@/components/ui/spinner';
import { ServiceNetworking, DomainInfo, TunnelStatusInfo, InternalRoute } from './types';
import { AddDomainModal } from './AddDomainModal';
import { DNSInstructionsCard } from './DNSInstructionsCard';

interface NetworkingTabProps {
  serviceId: string;
  serviceName: string;
}

export function NetworkingTab({ serviceId, serviceName }: NetworkingTabProps) {
  const [networking, setNetworking] = useState<ServiceNetworking | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [isAddDomainOpen, setIsAddDomainOpen] = useState(false);
  const [selectedDomain, setSelectedDomain] = useState<DomainInfo | null>(null);

  const fetchNetworking = async () => {
    try {
      setError(null);
      const data = await apiGet<ServiceNetworking>(`/v1/services/${serviceId}/networking`);
      setNetworking(data);
    } catch (err) {
      console.error('Failed to fetch networking info:', err);
      setError(err instanceof Error ? err.message : 'Failed to load networking data');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchNetworking();
  }, [serviceId]);

  const handleDomainAdded = () => {
    setIsAddDomainOpen(false);
    fetchNetworking();
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Spinner size="lg" />
        <span className="ml-3 text-muted-foreground">Loading networking configuration...</span>
      </div>
    );
  }

  if (error) {
    return (
      <Card className="border-status-error/30 bg-status-error-muted">
        <CardContent className="py-8">
          <div className="text-center">
            <p className="text-status-error font-medium mb-4">{error}</p>
            <Button variant="outline" onClick={fetchNetworking}>
              Try Again
            </Button>
          </div>
        </CardContent>
      </Card>
    );
  }

  return (
    <div className="space-y-6">
      {/* Tunnel Status Card */}
      {networking?.tunnel_status && (
        <TunnelStatusCard status={networking.tunnel_status} />
      )}

      {/* Domains Section */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-4">
          <div>
            <CardTitle className="text-lg">Domains</CardTitle>
            <CardDescription>
              Manage custom and platform domains for this service
            </CardDescription>
          </div>
          <Button onClick={() => setIsAddDomainOpen(true)}>
            <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
            </svg>
            Add Domain
          </Button>
        </CardHeader>
        <CardContent>
          {networking?.domains && networking.domains.length > 0 ? (
            <DomainsList
              domains={networking.domains}
              onViewInstructions={setSelectedDomain}
              onRefresh={fetchNetworking}
            />
          ) : (
            <div className="text-center py-8 text-muted-foreground">
              <svg aria-hidden="true" className="w-12 h-12 mx-auto mb-4 text-muted-foreground/50" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9" />
              </svg>
              <p>No domains configured</p>
              <p className="text-sm mt-1">Add a domain to make your service accessible</p>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Internal Routes Section */}
      {networking?.internal_routes && networking.internal_routes.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-lg">Internal Routes</CardTitle>
            <CardDescription>
              Service mesh routing configuration
            </CardDescription>
          </CardHeader>
          <CardContent>
            <InternalRoutesTable routes={networking.internal_routes} />
          </CardContent>
        </Card>
      )}

      {/* DNS Instructions Modal */}
      {selectedDomain && !selectedDomain.is_platform_domain && (
        <DNSInstructionsCard
          domain={selectedDomain}
          onClose={() => setSelectedDomain(null)}
        />
      )}

      {/* Add Domain Modal */}
      <AddDomainModal
        serviceId={serviceId}
        serviceName={serviceName}
        isOpen={isAddDomainOpen}
        onClose={() => setIsAddDomainOpen(false)}
        onSuccess={handleDomainAdded}
      />
    </div>
  );
}

// Tunnel Status Card Component
function TunnelStatusCard({ status }: { status: TunnelStatusInfo }) {
  const getStatusColor = (tunnelStatus: string) => {
    switch (tunnelStatus) {
      case 'active':
        return 'bg-status-success-muted text-status-success-foreground border-status-success/30';
      case 'degraded':
        return 'bg-status-warning-muted text-status-warning-foreground border-status-warning/30';
      default:
        return 'bg-status-error-muted text-status-error-foreground border-status-error/30';
    }
  };

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <CardTitle className="text-lg flex items-center gap-2">
            <svg aria-hidden="true" className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" />
            </svg>
            Cloudflare Tunnel
          </CardTitle>
          <Badge className={getStatusColor(status.status)}>
            {status.status}
          </Badge>
        </div>
      </CardHeader>
      <CardContent>
        <dl className="grid grid-cols-2 gap-4 text-sm">
          <div>
            <dt className="text-muted-foreground">Tunnel Name</dt>
            <dd className="font-medium">{status.tunnel_name}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Connectors</dt>
            <dd className="font-medium">{status.connectors} active</dd>
          </div>
          <div className="col-span-2">
            <dt className="text-muted-foreground">CNAME Target</dt>
            <dd className="font-mono text-xs bg-muted px-2 py-1 rounded mt-1">
              {status.cname}
            </dd>
          </div>
        </dl>
      </CardContent>
    </Card>
  );
}

// Domains List Component
interface DomainsListProps {
  domains: DomainInfo[];
  onViewInstructions: (domain: DomainInfo) => void;
  onRefresh: () => void;
}

function DomainsList({ domains, onViewInstructions, onRefresh }: DomainsListProps) {
  const getStatusBadge = (status: string) => {
    switch (status) {
      case 'active':
        return <Badge className="bg-status-success-muted text-status-success-foreground border-status-success/30">Active</Badge>;
      case 'pending':
        return <Badge className="bg-status-warning-muted text-status-warning-foreground border-status-warning/30">Pending</Badge>;
      case 'verifying':
        return <Badge className="bg-status-info-muted text-status-info-foreground border-status-info/30">Verifying</Badge>;
      case 'failed':
        return <Badge className="bg-status-error-muted text-status-error-foreground border-status-error/30">Failed</Badge>;
      default:
        return <Badge variant="outline">{status}</Badge>;
    }
  };

  const getTLSBadge = (tlsStatus: string) => {
    switch (tlsStatus) {
      case 'active':
        return (
          <span className="flex items-center text-status-success text-xs">
            <svg aria-hidden="true" className="w-3 h-3 mr-1" fill="currentColor" viewBox="0 0 20 20">
              <path fillRule="evenodd" d="M5 9V7a5 5 0 0110 0v2a2 2 0 012 2v5a2 2 0 01-2 2H5a2 2 0 01-2-2v-5a2 2 0 012-2zm8-2v2H7V7a3 3 0 016 0z" clipRule="evenodd" />
            </svg>
            TLS Active
          </span>
        );
      case 'provisioning':
        return (
          <span className="flex items-center text-status-warning text-xs">
            <Spinner size="sm" className="mr-1" />
            Provisioning
          </span>
        );
      default:
        return (
          <span className="flex items-center text-muted-foreground text-xs">
            <svg aria-hidden="true" className="w-3 h-3 mr-1" fill="currentColor" viewBox="0 0 20 20">
              <path fillRule="evenodd" d="M10 1a4.5 4.5 0 00-4.5 4.5V9H5a2 2 0 00-2 2v6a2 2 0 002 2h10a2 2 0 002-2v-6a2 2 0 00-2-2h-.5V5.5A4.5 4.5 0 0010 1zm3 8V5.5a3 3 0 10-6 0V9h6z" clipRule="evenodd" />
            </svg>
            Pending
          </span>
        );
    }
  };

  return (
    <div className="divide-y">
      {domains.map((domain) => (
        <div key={domain.id} className="py-4 first:pt-0 last:pb-0">
          <div className="flex items-center justify-between">
            <div className="flex-1">
              <div className="flex items-center gap-2">
                <a
                  href={`https://${domain.domain}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="font-medium text-primary hover:text-primary/80 hover:underline"
                >
                  {domain.domain}
                </a>
                {domain.is_platform_domain && (
                  <Badge variant="outline" className="text-xs">Platform</Badge>
                )}
                {domain.zero_trust_enabled && (
                  <Badge className="bg-muted text-foreground border-border text-xs">
                    <svg aria-hidden="true" className="w-3 h-3 mr-1" fill="currentColor" viewBox="0 0 20 20">
                      <path fillRule="evenodd" d="M2.166 4.999A11.954 11.954 0 0010 1.944 11.954 11.954 0 0017.834 5c.11.65.166 1.32.166 2.001 0 5.225-3.34 9.67-8 11.317C5.34 16.67 2 12.225 2 7c0-.682.057-1.35.166-2.001zm11.541 3.708a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clipRule="evenodd" />
                    </svg>
                    Protected
                  </Badge>
                )}
              </div>
              <div className="flex items-center gap-4 mt-1 text-sm text-muted-foreground">
                <span>{domain.environment}</span>
                {getTLSBadge(domain.tls_status)}
              </div>
            </div>
            <div className="flex items-center gap-3">
              {getStatusBadge(domain.status)}
              {!domain.is_platform_domain && domain.status === 'pending' && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => onViewInstructions(domain)}
                >
                  DNS Setup
                </Button>
              )}
              <Button variant="ghost" size="icon" className="h-8 w-8" aria-label="More options">
                <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 5v.01M12 12v.01M12 19v.01M12 6a1 1 0 110-2 1 1 0 010 2zm0 7a1 1 0 110-2 1 1 0 010 2zm0 7a1 1 0 110-2 1 1 0 010 2z" />
                </svg>
              </Button>
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}

// Internal Routes Table Component
function InternalRoutesTable({ routes }: { routes: InternalRoute[] }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b">
            <th className="text-left py-2 px-4 font-medium text-muted-foreground">Path</th>
            <th className="text-left py-2 px-4 font-medium text-muted-foreground">Target Service</th>
            <th className="text-left py-2 px-4 font-medium text-muted-foreground">Port</th>
          </tr>
        </thead>
        <tbody>
          {routes.map((route, index) => (
            <tr key={index} className="border-b last:border-0">
              <td className="py-2 px-4 font-mono text-xs">{route.path}</td>
              <td className="py-2 px-4 font-mono text-xs">{route.target_service}</td>
              <td className="py-2 px-4">{route.target_port}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export default NetworkingTab;
