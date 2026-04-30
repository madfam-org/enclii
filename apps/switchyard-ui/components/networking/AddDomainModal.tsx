'use client';

import { useState, useEffect } from 'react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@enclii/ui-components/dialog";
import { Button } from "@enclii/ui-components/button";
import { Input } from "@enclii/ui-components/input";
import { Label } from "@enclii/ui-components/label";
import { apiGet, apiPost } from '@/lib/api';
import { Spinner } from '@/components/ui/spinner';
import { Environment, AddDomainRequest, AddDomainResponse } from './types';

interface AddDomainModalProps {
  serviceId: string;
  serviceName: string;
  isOpen: boolean;
  onClose: () => void;
  onSuccess: () => void;
}

export function AddDomainModal({
  serviceId,
  serviceName,
  isOpen,
  onClose,
  onSuccess,
}: AddDomainModalProps) {
  const [domainType, setDomainType] = useState<'platform' | 'custom'>('platform');
  const [customDomain, setCustomDomain] = useState('');
  const [selectedEnvironment, setSelectedEnvironment] = useState('');
  const [zeroTrustEnabled, setZeroTrustEnabled] = useState(false);
  const [environments, setEnvironments] = useState<Environment[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadingEnvs, setLoadingEnvs] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [dnsInstructions, setDnsInstructions] = useState<AddDomainResponse['dns_instructions'] | null>(null);

  useEffect(() => {
    if (isOpen) {
      fetchEnvironments();
      // Reset form state
      setDomainType('platform');
      setCustomDomain('');
      setSelectedEnvironment('');
      setZeroTrustEnabled(false);
      setError(null);
      setDnsInstructions(null);
    }
  }, [isOpen]);

  const fetchEnvironments = async () => {
    try {
      setLoadingEnvs(true);
      const response = await apiGet<{ environments: Environment[] }>('/v1/environments');
      setEnvironments(response.environments || []);
      // Auto-select first environment
      if (response.environments && response.environments.length > 0) {
        setSelectedEnvironment(response.environments[0].id);
      }
    } catch (err) {
      console.error('Failed to fetch environments:', err);
      setError('Failed to load environments');
    } finally {
      setLoadingEnvs(false);
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setLoading(true);

    try {
      const request: AddDomainRequest = {
        environment_id: selectedEnvironment,
        is_platform_domain: domainType === 'platform',
        zero_trust_enabled: zeroTrustEnabled,
      };

      if (domainType === 'custom') {
        if (!customDomain) {
          setError('Please enter a domain name');
          setLoading(false);
          return;
        }
        request.domain = customDomain;
      }

      const response = await apiPost<AddDomainResponse>(
        `/v1/services/${serviceId}/domains`,
        request
      );

      // If custom domain, show DNS instructions
      if (domainType === 'custom' && response.dns_instructions) {
        setDnsInstructions(response.dns_instructions);
      } else {
        onSuccess();
      }
    } catch (err) {
      console.error('Failed to add domain:', err);
      setError(err instanceof Error ? err.message : 'Failed to add domain');
    } finally {
      setLoading(false);
    }
  };

  const handleComplete = () => {
    setDnsInstructions(null);
    onSuccess();
  };

  // Show DNS instructions after creating custom domain
  if (dnsInstructions) {
    return (
      <Dialog open={isOpen} onOpenChange={onClose}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>Configure DNS</DialogTitle>
            <DialogDescription>
              Add these DNS records to verify your domain and enable routing
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            {/* Verification TXT Record */}
            <div className="rounded-lg border p-4 bg-muted/50">
              <div className="flex items-center justify-between mb-2">
                <span className="text-sm font-medium">Step 1: Verification Record</span>
                <span className="text-xs bg-primary/10 text-primary px-2 py-0.5 rounded">TXT</span>
              </div>
              <div className="space-y-2 text-sm">
                <div>
                  <span className="text-muted-foreground">Name:</span>
                  <code className="ml-2 bg-background px-2 py-0.5 rounded text-xs">
                    {dnsInstructions.verification.name}
                  </code>
                </div>
                <div>
                  <span className="text-muted-foreground">Value:</span>
                  <code className="ml-2 bg-background px-2 py-0.5 rounded text-xs break-all">
                    {dnsInstructions.verification.value}
                  </code>
                </div>
              </div>
            </div>

            {/* CNAME Record */}
            <div className="rounded-lg border p-4 bg-muted/50">
              <div className="flex items-center justify-between mb-2">
                <span className="text-sm font-medium">Step 2: Routing Record</span>
                <span className="text-xs bg-status-success-muted text-status-success-foreground px-2 py-0.5 rounded">CNAME</span>
              </div>
              <div className="space-y-2 text-sm">
                <div>
                  <span className="text-muted-foreground">Name:</span>
                  <code className="ml-2 bg-background px-2 py-0.5 rounded text-xs">
                    {dnsInstructions.cname.name}
                  </code>
                </div>
                <div>
                  <span className="text-muted-foreground">Value:</span>
                  <code className="ml-2 bg-background px-2 py-0.5 rounded text-xs">
                    {dnsInstructions.cname.value}
                  </code>
                </div>
              </div>
            </div>

            <p className="text-xs text-muted-foreground">
              DNS changes can take up to 24 hours to propagate. Your domain will be verified automatically.
            </p>
          </div>

          <DialogFooter>
            <Button onClick={handleComplete}>
              Done
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    );
  }

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>Add Domain</DialogTitle>
            <DialogDescription>
              Add a domain to make {serviceName} accessible via HTTP/HTTPS
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4 py-4">
            {/* Domain Type Selection */}
            <div className="space-y-3">
              <Label>Domain Type</Label>
              <div className="grid grid-cols-2 gap-3">
                <button
                  type="button"
                  onClick={() => setDomainType('platform')}
                  className={`p-3 rounded-lg border text-left transition-colors ${
                    domainType === 'platform'
                      ? 'border-primary bg-primary/10 text-primary'
                      : 'border-border hover:border-border/80'
                  }`}
                >
                  <div className="font-medium text-sm">Platform Domain</div>
                  <div className="text-xs text-muted-foreground mt-1">
                    Auto-configured *.enclii.dev
                  </div>
                </button>
                <button
                  type="button"
                  onClick={() => setDomainType('custom')}
                  className={`p-3 rounded-lg border text-left transition-colors ${
                    domainType === 'custom'
                      ? 'border-primary bg-primary/10 text-primary'
                      : 'border-border hover:border-border/80'
                  }`}
                >
                  <div className="font-medium text-sm">Custom Domain</div>
                  <div className="text-xs text-muted-foreground mt-1">
                    Your own domain name
                  </div>
                </button>
              </div>
            </div>

            {/* Custom Domain Input */}
            {domainType === 'custom' && (
              <div className="space-y-2">
                <Label htmlFor="domain">Domain Name</Label>
                <Input
                  id="domain"
                  placeholder="app.example.com"
                  value={customDomain}
                  onChange={(e) => setCustomDomain(e.target.value)}
                />
                <p className="text-xs text-muted-foreground">
                  You'll need to configure DNS after adding the domain
                </p>
              </div>
            )}

            {/* Environment Selection */}
            <div className="space-y-2">
              <Label htmlFor="environment">Environment</Label>
              {loadingEnvs ? (
                <div className="h-10 flex items-center">
                  <Spinner size="sm" />
                  <span className="ml-2 text-sm text-muted-foreground">Loading environments...</span>
                </div>
              ) : (
                <select
                  id="environment"
                  value={selectedEnvironment}
                  onChange={(e) => setSelectedEnvironment(e.target.value)}
                  className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                >
                  {environments.map((env) => (
                    <option key={env.id} value={env.id}>
                      {env.name} {env.is_production ? '(Production)' : ''}
                    </option>
                  ))}
                </select>
              )}
            </div>

            {/* Zero Trust Protection */}
            <div className="flex items-center justify-between p-3 rounded-lg border bg-muted/30">
              <div>
                <div className="font-medium text-sm flex items-center gap-2">
                  <svg aria-hidden="true" className="w-4 h-4 text-purple-600" fill="currentColor" viewBox="0 0 20 20">
                    <path fillRule="evenodd" d="M2.166 4.999A11.954 11.954 0 0010 1.944 11.954 11.954 0 0017.834 5c.11.65.166 1.32.166 2.001 0 5.225-3.34 9.67-8 11.317C5.34 16.67 2 12.225 2 7c0-.682.057-1.35.166-2.001zm11.541 3.708a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clipRule="evenodd" />
                  </svg>
                  Zero Trust Protection
                </div>
                <p className="text-xs text-muted-foreground mt-0.5">
                  Require authentication via Cloudflare Access
                </p>
              </div>
              <button
                type="button"
                role="switch"
                aria-checked={zeroTrustEnabled}
                onClick={() => setZeroTrustEnabled(!zeroTrustEnabled)}
                className={`relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 ${
                  zeroTrustEnabled ? 'bg-primary' : 'bg-muted'
                }`}
              >
                <span
                  className={`pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out ${
                    zeroTrustEnabled ? 'translate-x-5' : 'translate-x-0'
                  }`}
                />
              </button>
            </div>

            {/* Error Display */}
            {error && (
              <div className="rounded-lg bg-status-error-muted border border-status-error/30 p-3 text-sm text-status-error">
                {error}
              </div>
            )}
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={loading || !selectedEnvironment}>
              {loading ? (
                <>
                  <Spinner size="sm" className="text-white mr-2" />
                  Adding...
                </>
              ) : (
                'Add Domain'
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export default AddDomainModal;
