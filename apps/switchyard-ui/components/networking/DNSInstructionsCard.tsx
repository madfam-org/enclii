'use client';

import { useState } from 'react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@enclii/ui-components/dialog";
import { Button } from "@enclii/ui-components/button";
import { Badge } from "@enclii/ui-components/badge";
import { apiPost } from '@/lib/api';
import { Spinner } from '@/components/ui/spinner';
import { DomainInfo } from './types';

interface DNSInstructionsCardProps {
  domain: DomainInfo;
  onClose: () => void;
}

export function DNSInstructionsCard({ domain, onClose }: DNSInstructionsCardProps) {
  const [verifying, setVerifying] = useState(false);
  const [verificationResult, setVerificationResult] = useState<{
    success: boolean;
    message: string;
  } | null>(null);

  const handleVerify = async () => {
    setVerifying(true);
    setVerificationResult(null);

    try {
      // Call verify endpoint
      const response = await apiPost<{ verified: boolean; message: string }>(
        `/v1/services/${domain.id}/domains/${domain.id}/verify`,
        {}
      );

      setVerificationResult({
        success: response.verified,
        message: response.verified
          ? 'Domain verified successfully!'
          : 'DNS records not found. Please check your configuration.',
      });
    } catch (err) {
      setVerificationResult({
        success: false,
        message: err instanceof Error ? err.message : 'Verification failed',
      });
    } finally {
      setVerifying(false);
    }
  };

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
  };

  // Extract subdomain from domain name for DNS record name
  const getSubdomain = () => {
    const parts = domain.domain.split('.');
    return parts.length > 2 ? parts[0] : '@';
  };

  return (
    <Dialog open={true} onOpenChange={onClose}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            DNS Configuration
            <Badge className="bg-status-warning-muted text-status-warning-foreground border-status-warning/30">
              {domain.status}
            </Badge>
          </DialogTitle>
          <DialogDescription>
            Configure these DNS records to verify and activate <strong>{domain.domain}</strong>
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          {/* Step 1: TXT Verification Record */}
          <div className="rounded-lg border p-4">
            <div className="flex items-center justify-between mb-3">
              <div className="flex items-center gap-2">
                <span className="w-6 h-6 rounded-full bg-primary/10 text-primary flex items-center justify-center text-xs font-medium">
                  1
                </span>
                <span className="font-medium">Verification Record</span>
              </div>
              <Badge variant="outline">TXT</Badge>
            </div>

            <div className="space-y-3 text-sm">
              <div className="flex items-start justify-between">
                <div>
                  <span className="text-muted-foreground">Type:</span>
                  <span className="ml-2 font-medium">TXT</span>
                </div>
              </div>

              <div className="space-y-1">
                <span className="text-muted-foreground">Name:</span>
                <div className="flex items-center gap-2">
                  <code className="flex-1 bg-muted px-3 py-1.5 rounded text-xs font-mono">
                    _enclii-verification.{getSubdomain()}
                  </code>
                  <Button
                    variant="ghost"
                    size="sm"
                    aria-label="Copy to clipboard"
                    className="h-8 px-2"
                    onClick={() => copyToClipboard(`_enclii-verification.${getSubdomain()}`)}
                  >
                    <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
                    </svg>
                  </Button>
                </div>
              </div>

              <div className="space-y-1">
                <span className="text-muted-foreground">Value:</span>
                <div className="flex items-center gap-2">
                  <code className="flex-1 bg-muted px-3 py-1.5 rounded text-xs font-mono break-all">
                    {domain.verification_txt || `enclii-verification=${domain.id}`}
                  </code>
                  <Button
                    variant="ghost"
                    size="sm"
                    aria-label="Copy to clipboard"
                    className="h-8 px-2"
                    onClick={() => copyToClipboard(domain.verification_txt || `enclii-verification=${domain.id}`)}
                  >
                    <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
                    </svg>
                  </Button>
                </div>
              </div>
            </div>
          </div>

          {/* Step 2: CNAME Record */}
          <div className="rounded-lg border p-4">
            <div className="flex items-center justify-between mb-3">
              <div className="flex items-center gap-2">
                <span className="w-6 h-6 rounded-full bg-status-success-muted text-status-success-foreground flex items-center justify-center text-xs font-medium">
                  2
                </span>
                <span className="font-medium">Routing Record</span>
              </div>
              <Badge variant="outline">CNAME</Badge>
            </div>

            <div className="space-y-3 text-sm">
              <div className="space-y-1">
                <span className="text-muted-foreground">Name:</span>
                <div className="flex items-center gap-2">
                  <code className="flex-1 bg-muted px-3 py-1.5 rounded text-xs font-mono">
                    {getSubdomain()}
                  </code>
                  <Button
                    variant="ghost"
                    size="sm"
                    aria-label="Copy to clipboard"
                    className="h-8 px-2"
                    onClick={() => copyToClipboard(getSubdomain())}
                  >
                    <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
                    </svg>
                  </Button>
                </div>
              </div>

              <div className="space-y-1">
                <span className="text-muted-foreground">Value:</span>
                <div className="flex items-center gap-2">
                  <code className="flex-1 bg-muted px-3 py-1.5 rounded text-xs font-mono">
                    {domain.dns_cname || 'tunnel.enclii.dev'}
                  </code>
                  <Button
                    variant="ghost"
                    size="sm"
                    aria-label="Copy to clipboard"
                    className="h-8 px-2"
                    onClick={() => copyToClipboard(domain.dns_cname || 'tunnel.enclii.dev')}
                  >
                    <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
                    </svg>
                  </Button>
                </div>
              </div>
            </div>
          </div>

          {/* Verification Result */}
          {verificationResult && (
            <div
              className={`rounded-lg p-3 text-sm ${
                verificationResult.success
                  ? 'bg-status-success-muted border border-status-success/30 text-status-success-foreground'
                  : 'bg-status-error-muted border border-status-error/30 text-status-error-foreground'
              }`}
            >
              <div className="flex items-center gap-2">
                {verificationResult.success ? (
                  <svg aria-hidden="true" className="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
                    <path fillRule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clipRule="evenodd" />
                  </svg>
                ) : (
                  <svg aria-hidden="true" className="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
                    <path fillRule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z" clipRule="evenodd" />
                  </svg>
                )}
                {verificationResult.message}
              </div>
            </div>
          )}

          <p className="text-xs text-muted-foreground">
            DNS propagation can take up to 24 hours. Click &quot;Verify&quot; to check if your records are configured correctly.
          </p>
        </div>

        <DialogFooter className="gap-2">
          <Button variant="outline" onClick={onClose}>
            Close
          </Button>
          <Button onClick={handleVerify} disabled={verifying}>
            {verifying ? (
              <>
                <Spinner size="sm" className="text-white mr-2" />
                Verifying...
              </>
            ) : (
              <>
                <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                Verify DNS
              </>
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export default DNSInstructionsCard;
