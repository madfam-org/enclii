'use client';

import { useState, useEffect, useRef, useCallback } from 'react';
import { usePolling } from '@/hooks/use-polling';
import { POLLING_NORMAL } from '@/lib/constants';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from "@enclii/ui-components/button";
import { Badge } from "@enclii/ui-components/badge";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@enclii/ui-components/dialog";
import { apiGet } from '@/lib/api';
import { Spinner } from '@/components/ui/spinner';
import { buildBuildLogStreamWsUrl } from '@/lib/ws-url';
import { formatTimestamp, formatRelativeTime } from '@/lib/formatting';
import type { Release } from './types';

interface BuildLogsViewerProps {
  serviceId: string;
  serviceName: string;
}

interface LogMessage {
  type: 'log' | 'error' | 'info' | 'connected' | 'disconnected';
  pod?: string;
  container?: string;
  timestamp: string;
  message: string;
}

interface LogLine {
  id: string;
  type: LogMessage['type'];
  pod?: string;
  container?: string;
  timestamp: Date;
  message: string;
}

export function BuildLogsViewer({ serviceId, serviceName }: BuildLogsViewerProps) {
  const [releases, setReleases] = useState<Release[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedRelease, setSelectedRelease] = useState<Release | null>(null);
  const [logsDialogOpen, setLogsDialogOpen] = useState(false);
  const [buildLogs, setBuildLogs] = useState<LogLine[]>([]);
  const [isStreaming, setIsStreaming] = useState(false);
  const [connectionStatus, setConnectionStatus] = useState<'disconnected' | 'connecting' | 'connected'>('disconnected');

  const wsRef = useRef<WebSocket | null>(null);
  const logContainerRef = useRef<HTMLDivElement>(null);
  const logIdCounter = useRef(0);

  // Fetch releases
  const fetchReleases = useCallback(async () => {
    try {
      setError(null);
      const data = await apiGet<{ releases: Release[] }>(`/v1/services/${serviceId}/releases`);
      setReleases(data.releases || []);
    } catch (err) {
      console.error('Failed to fetch releases:', err);
      setError(err instanceof Error ? err.message : 'Failed to fetch releases');
    } finally {
      setLoading(false);
    }
  }, [serviceId]);

  useEffect(() => {
    fetchReleases();
  }, [fetchReleases]);

  usePolling(fetchReleases, POLLING_NORMAL);

  // Auto-scroll to bottom when new logs arrive
  useEffect(() => {
    if (logContainerRef.current) {
      logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight;
    }
  }, [buildLogs]);

  // Clean up WebSocket on unmount
  useEffect(() => {
    return () => {
      if (wsRef.current) {
        wsRef.current.close();
      }
    };
  }, []);

  const connectWebSocket = useCallback((releaseId: string) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.close();
    }

    setConnectionStatus('connecting');
    setBuildLogs([]);

    const wsUrl = buildBuildLogStreamWsUrl(serviceId, releaseId);

    try {
      const ws = new WebSocket(wsUrl);
      wsRef.current = ws;

      ws.onopen = () => {
        setConnectionStatus('connected');
        setIsStreaming(true);
      };

      ws.onmessage = (event) => {
        try {
          const data: LogMessage = JSON.parse(event.data);
          const logLine: LogLine = {
            id: `log-${logIdCounter.current++}`,
            type: data.type,
            pod: data.pod,
            container: data.container,
            timestamp: new Date(data.timestamp),
            message: data.message,
          };
          setBuildLogs(prev => [...prev.slice(-999), logLine]);
        } catch (e) {
          console.error('Failed to parse log message:', e);
        }
      };

      ws.onerror = () => {
        setConnectionStatus('disconnected');
        setIsStreaming(false);
      };

      ws.onclose = () => {
        setConnectionStatus('disconnected');
        setIsStreaming(false);
      };
    } catch (e) {
      setConnectionStatus('disconnected');
    }
  }, [serviceId]);

  const disconnectWebSocket = useCallback(() => {
    if (wsRef.current) {
      wsRef.current.close();
      wsRef.current = null;
    }
    setIsStreaming(false);
    setConnectionStatus('disconnected');
  }, []);

  const openLogsDialog = (release: Release) => {
    setSelectedRelease(release);
    setBuildLogs([]);
    setLogsDialogOpen(true);

    // If build is in progress, start streaming
    if (release.status === 'building') {
      connectWebSocket(release.id);
    }
  };

  const closeLogsDialog = () => {
    disconnectWebSocket();
    setLogsDialogOpen(false);
    setSelectedRelease(null);
    setBuildLogs([]);
  };

  const getStatusBadge = (status: string) => {
    const variants: Record<string, 'default' | 'secondary' | 'destructive' | 'outline'> = {
      building: 'secondary',
      ready: 'default',
      failed: 'destructive',
    };
    return <Badge variant={variants[status] || 'outline'}>{status}</Badge>;
  };

  const getLogLineColor = (type: LogMessage['type'], message: string): string => {
    if (type === 'error') return 'text-status-error';
    if (type === 'info' || type === 'connected') return 'text-status-info';
    if (type === 'disconnected') return 'text-status-warning';

    const lowerMsg = message.toLowerCase();
    if (lowerMsg.includes('error') || lowerMsg.includes('failed')) return 'text-status-error';
    if (lowerMsg.includes('warning') || lowerMsg.includes('warn')) return 'text-status-warning';
    if (lowerMsg.includes('success') || lowerMsg.includes('completed')) return 'text-status-success';
    return 'text-gray-200';
  };

  if (loading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Builds</CardTitle>
          <CardDescription>Build history for {serviceName}</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-center py-8">
            <Spinner size="lg" />
            <span className="ml-3 text-muted-foreground">Loading builds...</span>
          </div>
        </CardContent>
      </Card>
    );
  }

  if (error) {
    return (
      <Card className="border-status-error/30">
        <CardHeader>
          <CardTitle>Builds</CardTitle>
          <CardDescription>Build history for {serviceName}</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="text-center py-8">
            <p className="text-status-error mb-4">{error}</p>
            <Button variant="outline" onClick={fetchReleases}>
              Try Again
            </Button>
          </div>
        </CardContent>
      </Card>
    );
  }

  return (
    <>
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <div>
            <CardTitle>Builds</CardTitle>
            <CardDescription>Build history for {serviceName}</CardDescription>
          </div>
          <Button variant="outline" size="sm" onClick={fetchReleases}>
            <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
            Refresh
          </Button>
        </CardHeader>
        <CardContent>
          {releases.length === 0 ? (
            <div className="text-center py-12 text-muted-foreground">
              <svg aria-hidden="true" className="w-12 h-12 mx-auto mb-4 text-muted-foreground/50" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M19.428 15.428a2 2 0 00-1.022-.547l-2.387-.477a6 6 0 00-3.86.517l-.318.158a6 6 0 01-3.86.517L6.05 15.21a2 2 0 00-1.806.547M8 4h8l-1 1v5.172a2 2 0 00.586 1.414l5 5c1.26 1.26.367 3.414-1.415 3.414H4.828c-1.782 0-2.674-2.154-1.414-3.414l5-5A2 2 0 009 10.172V5L8 4z" />
              </svg>
              <p className="text-lg font-medium">No builds yet</p>
              <p className="text-sm mt-1">Trigger a build to see build history here.</p>
            </div>
          ) : (
            <div className="space-y-3">
              {releases.map((release) => (
                <div
                  key={release.id}
                  className="flex items-center justify-between p-4 border rounded-lg hover:bg-accent"
                >
                  <div className="flex items-center gap-4">
                    <div>
                      {getStatusBadge(release.status)}
                    </div>
                    <div>
                      <p className="font-medium">{release.version}</p>
                      <p className="text-sm text-muted-foreground font-mono">
                        {release.git_sha?.substring(0, 8) || 'N/A'}
                      </p>
                    </div>
                  </div>
                  <div className="flex items-center gap-4">
                    <span className="text-sm text-muted-foreground">
                      {formatRelativeTime(release.created_at)}
                    </span>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => openLogsDialog(release)}
                    >
                      <svg aria-hidden="true" className="w-4 h-4 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
                      </svg>
                      View Logs
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Build Logs Dialog */}
      <Dialog open={logsDialogOpen} onOpenChange={(open) => !open && closeLogsDialog()}>
        <DialogContent className="max-w-4xl max-h-[80vh] flex flex-col">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              Build Logs - {selectedRelease?.version}
              {selectedRelease && getStatusBadge(selectedRelease.status)}
            </DialogTitle>
            <DialogDescription>
              {selectedRelease?.git_sha && (
                <span className="font-mono">Commit: {selectedRelease.git_sha.substring(0, 8)}</span>
              )}
              {selectedRelease?.status === 'building' && (
                <Badge className="ml-2" variant={
                  connectionStatus === 'connected' ? 'default' :
                  connectionStatus === 'connecting' ? 'secondary' : 'outline'
                }>
                  {connectionStatus === 'connected' && '● Live'}
                  {connectionStatus === 'connecting' && '○ Connecting...'}
                  {connectionStatus === 'disconnected' && '○ Disconnected'}
                </Badge>
              )}
            </DialogDescription>
          </DialogHeader>

          <div className="flex gap-2 my-2">
            {selectedRelease?.status === 'building' && (
              <Button
                size="sm"
                variant={isStreaming ? 'destructive' : 'default'}
                onClick={() => isStreaming ? disconnectWebSocket() : connectWebSocket(selectedRelease.id)}
              >
                {isStreaming ? 'Stop Streaming' : 'Start Streaming'}
              </Button>
            )}
            <Button size="sm" variant="outline" onClick={() => setBuildLogs([])}>
              Clear
            </Button>
          </div>

          <div
            ref={logContainerRef}
            className="flex-1 bg-gray-900 text-gray-100 font-mono text-xs overflow-auto p-4 rounded-md min-h-[300px]"
          >
            {buildLogs.length === 0 ? (
              <div className="text-center text-gray-500 py-8">
                {selectedRelease?.status === 'building' ? (
                  <>
                    <p>Waiting for build logs...</p>
                    <p className="mt-2 text-xs">
                      {isStreaming ? 'Connected to build stream' : 'Click "Start Streaming" to view live logs'}
                    </p>
                  </>
                ) : selectedRelease?.status === 'ready' ? (
                  <>
                    <svg aria-hidden="true" className="mx-auto h-12 w-12 mb-4 text-status-success" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
                    </svg>
                    <p>Build completed successfully</p>
                    <p className="mt-2 text-xs">Image: {selectedRelease?.image_uri}</p>
                  </>
                ) : selectedRelease?.status === 'failed' ? (
                  <>
                    <svg aria-hidden="true" className="mx-auto h-12 w-12 mb-4 text-status-error" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2m7-2a9 9 0 11-18 0 9 9 0 0118 0z" />
                    </svg>
                    <p className="text-status-error font-semibold">Build failed</p>
                    {selectedRelease?.error_message ? (
                      <div className="mt-4 text-left bg-gray-800 p-4 rounded-md">
                        <p className="text-xs text-gray-400 mb-2">Error details:</p>
                        <pre className="text-status-error text-xs whitespace-pre-wrap break-words">
                          {selectedRelease.error_message}
                        </pre>
                      </div>
                    ) : (
                      <p className="mt-2 text-xs text-gray-400">No error details available. Check build worker logs.</p>
                    )}
                  </>
                ) : (
                  <p>No logs available</p>
                )}
              </div>
            ) : (
              buildLogs.map((log) => (
                <div
                  key={log.id}
                  className={`py-0.5 hover:bg-gray-800 ${getLogLineColor(log.type, log.message)}`}
                >
                  <span className="text-gray-500 mr-2">
                    [{formatTimestamp(log.timestamp)}]
                  </span>
                  {log.pod && (
                    <span className="text-cyan-400 mr-2">[{log.pod}]</span>
                  )}
                  <span>{log.message}</span>
                </div>
              ))
            )}
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
