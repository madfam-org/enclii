'use client';

import { useState } from 'react';
import { toast } from 'sonner';
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { apiPost, apiDelete } from "@/lib/api";

export type FunctionRuntime = 'go' | 'python' | 'node' | 'rust';
export type FunctionStatus = 'pending' | 'building' | 'deploying' | 'ready' | 'failed' | 'deleting';

export interface FunctionConfig {
  runtime: FunctionRuntime;
  handler: string;
  memory: string;
  timeout: number;
  min_replicas: number;
  max_replicas: number;
  cooldown_period: number;
  concurrency: number;
  env_vars?: { name: string; value: string }[];
}

export interface Function {
  id: string;
  project_id: string;
  name: string;
  config: FunctionConfig;
  status: FunctionStatus;
  status_message?: string;
  endpoint?: string;
  image_uri?: string;
  available_replicas: number;
  invocation_count: number;
  avg_duration_ms: number;
  last_invoked_at?: string;
  created_at: string;
  updated_at: string;
  deployed_at?: string;
}

interface FunctionCardProps {
  fn: Function & { project_name?: string; project_slug?: string };
  onDelete: () => void;
  isDeleting: boolean;
}

const STATUS_COLORS: Record<FunctionStatus, string> = {
  pending: 'bg-muted text-muted-foreground',
  building: 'bg-status-info-muted text-status-info-foreground',
  deploying: 'bg-status-warning-muted text-status-warning-foreground',
  ready: 'bg-status-success-muted text-status-success-foreground',
  failed: 'bg-status-error-muted text-status-error-foreground',
  deleting: 'bg-status-warning-muted text-status-warning-foreground',
};

const RUNTIME_ICONS: Record<FunctionRuntime, { icon: JSX.Element; color: string; label: string }> = {
  go: {
    icon: (
      <svg aria-hidden="true" className="w-6 h-6" viewBox="0 0 24 24" fill="currentColor">
        <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-1 15l-4-4 1.41-1.41L11 14.17l6.59-6.59L19 9l-8 8z"/>
      </svg>
    ),
    color: 'text-primary bg-primary/10',
    label: 'Go',
  },
  python: {
    icon: (
      <svg aria-hidden="true" className="w-6 h-6" viewBox="0 0 24 24" fill="currentColor">
        <path d="M12 2c-1.66 0-3.06.53-3.94 1.42-.89.89-1.42 2.28-1.42 3.94v2h5v1H5.28c-1.66 0-3.28.9-3.28 3.28v4c0 1.66.53 3.06 1.42 3.94.89.89 2.28 1.42 3.94 1.42h1.64v-3.94c0-1.66.9-3.06 2.56-3.06h5c1.66 0 3-1.34 3-3v-4c0-1.66-1.34-3-3-3h-1V5.36c0-1.66-.53-3.06-1.42-3.94C14.06 2.53 12.66 2 11 2h1zm-1.5 2.5a1 1 0 110 2 1 1 0 010-2z"/>
      </svg>
    ),
    color: 'text-status-warning bg-status-warning-muted',
    label: 'Python',
  },
  node: {
    icon: (
      <svg aria-hidden="true" className="w-6 h-6" viewBox="0 0 24 24" fill="currentColor">
        <path d="M12 1.85c-.27 0-.55.07-.78.2L3.78 6.35c-.48.28-.78.8-.78 1.36v8.58c0 .56.3 1.08.78 1.36l7.44 4.3c.48.28 1.08.28 1.56 0l7.44-4.3c.48-.28.78-.8.78-1.36V7.71c0-.56-.3-1.08-.78-1.36l-7.44-4.3c-.23-.13-.51-.2-.78-.2z"/>
      </svg>
    ),
    color: 'text-status-success bg-status-success-muted',
    label: 'Node.js',
  },
  rust: {
    icon: (
      <svg aria-hidden="true" className="w-6 h-6" viewBox="0 0 24 24" fill="currentColor">
        <path d="M12 2a10 10 0 100 20 10 10 0 000-20zm0 2a8 8 0 110 16 8 8 0 010-16zm-1 3v2H9v2h2v4H9v2h6v-2h-2v-4h2V9h-2V7h-2z"/>
      </svg>
    ),
    color: 'text-destructive bg-destructive/10',
    label: 'Rust',
  },
};

export function FunctionCard({ fn, onDelete, isDeleting }: FunctionCardProps) {
  const [invoking, setInvoking] = useState(false);
  const [invokeResult, setInvokeResult] = useState<string | null>(null);

  const runtimeInfo = RUNTIME_ICONS[fn.config.runtime];
  const statusColor = STATUS_COLORS[fn.status];

  const handleInvoke = async () => {
    if (fn.status !== 'ready') {
      toast.error('Function must be ready to invoke');
      return;
    }

    setInvoking(true);
    setInvokeResult(null);
    try {
      const result = await apiPost<{ body: string; status_code: number }>(`/v1/functions/${fn.id}/invoke`, {});
      setInvokeResult(`Status: ${result.status_code}\n${result.body}`);
    } catch (err) {
      console.error('Failed to invoke function:', err);
      setInvokeResult(err instanceof Error ? err.message : 'Failed to invoke');
    } finally {
      setInvoking(false);
    }
  };

  const formatDuration = (ms: number): string => {
    if (ms < 1) return '<1ms';
    if (ms < 1000) return `${Math.round(ms)}ms`;
    return `${(ms / 1000).toFixed(2)}s`;
  };

  const formatLastInvoked = (dateStr?: string): string => {
    if (!dateStr) return 'Never';
    const date = new Date(dateStr);
    const now = new Date();
    const diff = now.getTime() - date.getTime();
    const minutes = Math.floor(diff / 60000);
    if (minutes < 1) return 'Just now';
    if (minutes < 60) return `${minutes}m ago`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours}h ago`;
    const days = Math.floor(hours / 24);
    return `${days}d ago`;
  };

  return (
    <Card className="hover:shadow-md transition-shadow">
      <CardHeader className="pb-3">
        <div className="flex items-start justify-between">
          <div className="flex items-center gap-3">
            <div className={`p-2 rounded-lg ${runtimeInfo.color}`}>
              {runtimeInfo.icon}
            </div>
            <div>
              <CardTitle className="text-lg">{fn.name}</CardTitle>
              <p className="text-sm text-muted-foreground">{runtimeInfo.label}</p>
            </div>
          </div>
          <Badge className={statusColor}>{fn.status}</Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Project info */}
        {fn.project_name && (
          <div className="text-sm text-muted-foreground">
            Project: {fn.project_name}
          </div>
        )}

        {/* Metrics */}
        <div className="grid grid-cols-3 gap-4 text-center">
          <div className="space-y-1">
            <p className="text-2xl font-semibold">{fn.invocation_count.toLocaleString()}</p>
            <p className="text-xs text-muted-foreground">Invocations</p>
          </div>
          <div className="space-y-1">
            <p className="text-2xl font-semibold">{formatDuration(fn.avg_duration_ms)}</p>
            <p className="text-xs text-muted-foreground">Avg Duration</p>
          </div>
          <div className="space-y-1">
            <p className="text-2xl font-semibold">{fn.available_replicas}</p>
            <p className="text-xs text-muted-foreground">Replicas</p>
          </div>
        </div>

        {/* Last invoked */}
        <div className="flex justify-between text-sm">
          <span className="text-muted-foreground">Last invoked</span>
          <span>{formatLastInvoked(fn.last_invoked_at)}</span>
        </div>

        {/* Endpoint */}
        {fn.endpoint && (
          <div className="flex items-center justify-between text-sm">
            <span className="text-muted-foreground">Endpoint</span>
            <code className="text-xs bg-muted px-2 py-1 rounded truncate max-w-[200px]">
              {fn.endpoint}
            </code>
          </div>
        )}

        {/* Invoke result */}
        {invokeResult && (
          <div className="p-3 bg-muted rounded-md text-sm">
            <pre className="whitespace-pre-wrap overflow-auto max-h-32">{invokeResult}</pre>
          </div>
        )}

        {/* Actions */}
        <div className="flex gap-2 pt-2">
          <Button
            variant="outline"
            size="sm"
            className="flex-1"
            onClick={handleInvoke}
            disabled={invoking || fn.status !== 'ready'}
          >
            {invoking ? 'Invoking...' : 'Invoke'}
          </Button>
          <Button
            variant="outline"
            size="sm"
            className="flex-1 text-destructive hover:text-destructive"
            onClick={onDelete}
            disabled={isDeleting}
          >
            {isDeleting ? 'Deleting...' : 'Delete'}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
