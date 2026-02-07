'use client';

import { useState, useEffect } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { apiGet } from "@/lib/api";
// Shared types
import type { Release, BuildStage, BuildStep } from "@/lib/types";

// Icons as SVG components
const CheckCircleIcon = () => (
  <svg aria-hidden="true" className="h-5 w-5 text-status-success" fill="none" stroke="currentColor" viewBox="0 0 24 24">
    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
  </svg>
);

const XCircleIcon = () => (
  <svg aria-hidden="true" className="h-5 w-5 text-status-error" fill="none" stroke="currentColor" viewBox="0 0 24 24">
    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2m7-2a9 9 0 11-18 0 9 9 0 0118 0z" />
  </svg>
);

const SpinnerIcon = () => (
  <svg aria-hidden="true" className="h-5 w-5 text-primary animate-spin" fill="none" viewBox="0 0 24 24">
    <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
    <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
  </svg>
);

const ClockIcon = () => (
  <svg aria-hidden="true" className="h-5 w-5 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
  </svg>
);

const TerminalIcon = () => (
  <svg aria-hidden="true" className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" />
  </svg>
);

interface BuildProgressProps {
  serviceId: string;
  releaseId?: string;
  onComplete?: (release: Release) => void;
  onError?: (error: string) => void;
}

// Build pipeline stages
const BUILD_STEPS: { name: string; key: string }[] = [
  { name: 'Clone Repository', key: 'clone' },
  { name: 'Detect Build Type', key: 'detect' },
  { name: 'Build Image', key: 'build' },
  { name: 'Generate SBOM', key: 'sbom' },
  { name: 'Push to Registry', key: 'push' },
];

export function BuildProgress({ serviceId, releaseId, onComplete, onError }: BuildProgressProps) {
  const [steps, setSteps] = useState<BuildStep[]>(
    BUILD_STEPS.map(s => ({ name: s.name, status: 'pending' as BuildStage }))
  );
  const [currentStep, setCurrentStep] = useState(0);
  const [buildLogs, setBuildLogs] = useState<string[]>([]);
  const [release, setRelease] = useState<Release | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [showLogs, setShowLogs] = useState(false);

  // Poll for build status
  useEffect(() => {
    if (!releaseId) return;

    const pollInterval = setInterval(async () => {
      try {
        const releases = await apiGet<Release[]>(`/v1/services/${serviceId}/releases`);
        const currentRelease = releases.find(r => r.id === releaseId);

        if (currentRelease) {
          setRelease(currentRelease);

          // Update steps based on release status
          if (currentRelease.status === 'building') {
            // Simulate progress through steps
            const elapsed = Date.now() - new Date(currentRelease.created_at).getTime();
            const stepDuration = 20000; // 20 seconds per step estimate
            const estimatedStep = Math.min(
              Math.floor(elapsed / stepDuration),
              BUILD_STEPS.length - 1
            );

            setCurrentStep(estimatedStep);
            setSteps(prevSteps =>
              prevSteps.map((step, idx) => ({
                ...step,
                status: idx < estimatedStep ? 'completed' :
                       idx === estimatedStep ? 'running' : 'pending',
              }))
            );

            // Add simulated log entries
            if (estimatedStep > 0 && buildLogs.length < estimatedStep * 3) {
              const newLogs = [
                `[${new Date().toLocaleTimeString()}] ${BUILD_STEPS[estimatedStep - 1].name} completed`,
                `[${new Date().toLocaleTimeString()}] Starting ${BUILD_STEPS[estimatedStep].name}...`,
              ];
              setBuildLogs(prev => [...prev, ...newLogs]);
            }
          } else if (currentRelease.status === 'ready') {
            // Build completed successfully
            setSteps(BUILD_STEPS.map(s => ({
              name: s.name,
              status: 'completed' as BuildStage,
            })));
            setCurrentStep(BUILD_STEPS.length);
            setBuildLogs(prev => [
              ...prev,
              `[${new Date().toLocaleTimeString()}] Build completed successfully!`,
              `[${new Date().toLocaleTimeString()}] Image: ${currentRelease.image_tag || 'generated'}`,
            ]);

            clearInterval(pollInterval);
            onComplete?.(currentRelease);
          } else if (currentRelease.status === 'failed') {
            // Build failed
            setSteps(prevSteps =>
              prevSteps.map((step, idx) => ({
                ...step,
                status: idx < currentStep ? 'completed' :
                       idx === currentStep ? 'failed' : 'pending',
              }))
            );
            setError('Build failed. Check logs for details.');
            setBuildLogs(prev => [
              ...prev,
              `[${new Date().toLocaleTimeString()}] ERROR: Build failed`,
            ]);

            clearInterval(pollInterval);
            onError?.('Build failed');
          }
        }
      } catch (err) {
        console.error('Failed to poll build status:', err);
      }
    }, 3000); // Poll every 3 seconds

    return () => clearInterval(pollInterval);
  }, [serviceId, releaseId, currentStep, onComplete, onError, buildLogs.length]);

  const getStepIcon = (status: BuildStage) => {
    switch (status) {
      case 'completed':
        return <CheckCircleIcon />;
      case 'failed':
        return <XCircleIcon />;
      case 'running':
        return <SpinnerIcon />;
      default:
        return <ClockIcon />;
    }
  };

  const getStatusBadge = () => {
    if (error) {
      return <Badge variant="destructive">Failed</Badge>;
    }
    if (release?.status === 'ready') {
      return <Badge className="bg-status-success-muted text-status-success-foreground">Completed</Badge>;
    }
    if (release?.status === 'building') {
      return <Badge className="bg-status-info-muted text-status-info-foreground">Building</Badge>;
    }
    return <Badge variant="secondary">Pending</Badge>;
  };

  const getElapsedTime = () => {
    if (!release) return null;

    const start = new Date(release.created_at);
    const end = release.completed_at ? new Date(release.completed_at) : new Date();
    const elapsed = Math.floor((end.getTime() - start.getTime()) / 1000);

    if (elapsed < 60) return `${elapsed}s`;
    const minutes = Math.floor(elapsed / 60);
    const seconds = elapsed % 60;
    return `${minutes}m ${seconds}s`;
  };

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <CardTitle className="text-lg flex items-center gap-2">
            <SpinnerIcon />
            Build Progress
          </CardTitle>
          <div className="flex items-center gap-2">
            {getStatusBadge()}
            {getElapsedTime() && (
              <span className="text-sm text-muted-foreground">{getElapsedTime()}</span>
            )}
          </div>
        </div>
      </CardHeader>
      <CardContent>
        {/* Progress Steps */}
        <div className="space-y-4 mb-6">
          {steps.map((step, index) => (
            <div key={step.name} className="flex items-center gap-3">
              <div className="flex-shrink-0">
                {getStepIcon(step.status)}
              </div>
              <div className="flex-1">
                <div className="flex items-center justify-between">
                  <span className={`text-sm font-medium ${
                    step.status === 'completed' ? 'text-status-success' :
                    step.status === 'failed' ? 'text-status-error' :
                    step.status === 'running' ? 'text-status-info' :
                    'text-muted-foreground'
                  }`}>
                    {step.name}
                  </span>
                  {step.duration && (
                    <span className="text-xs text-muted-foreground">{step.duration}</span>
                  )}
                </div>
                {step.message && (
                  <p className="text-xs text-muted-foreground mt-1">{step.message}</p>
                )}
              </div>
            </div>
          ))}
        </div>

        {/* Error Message */}
        {error && (
          <div className="mb-4 p-3 bg-status-error-muted border border-status-error/30 rounded-md text-status-error-foreground text-sm">
            {error}
          </div>
        )}

        {/* Build Logs Toggle */}
        <div className="border-t pt-4">
          <button
            onClick={() => setShowLogs(!showLogs)}
            className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"
          >
            <TerminalIcon />
            {showLogs ? 'Hide' : 'Show'} Build Logs
          </button>

          {showLogs && (
            <div className="mt-3 bg-gray-900 text-gray-100 rounded-md p-4 font-mono text-xs overflow-x-auto max-h-64 overflow-y-auto">
              {buildLogs.length === 0 ? (
                <p className="text-gray-500">Waiting for build logs...</p>
              ) : (
                buildLogs.map((log, idx) => (
                  <div key={idx} className="whitespace-pre-wrap">
                    {log}
                  </div>
                ))
              )}
            </div>
          )}
        </div>

        {/* Release Info */}
        {release && release.status === 'ready' && (
          <div className="mt-4 p-3 bg-status-success-muted border border-status-success/30 rounded-md">
            <p className="text-sm text-status-success-foreground font-medium">Build Successful!</p>
            <div className="mt-2 space-y-1 text-xs text-status-success">
              <p>Version: {release.version}</p>
              {release.git_sha && <p>Commit: {release.git_sha.substring(0, 8)}</p>}
              {release.image_tag && <p>Image: {release.image_tag}</p>}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

export default BuildProgress;
