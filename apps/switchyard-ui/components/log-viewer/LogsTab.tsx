'use client';

import { useState, useEffect, useRef, useCallback } from 'react';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import { apiGet, apiPost } from '@/lib/api';
import { API_BASE_URL } from '@/lib/constants';
import { formatTimestamp } from '@/lib/formatting';
import { LogMessage, LogLine, LogLevel, LogHistoryResponse, LogSearchResponse } from './types';

interface LogsTabProps {
  serviceId: string;
  serviceName: string;
  deploymentId?: string;
  env?: string;
}

function getWebSocketUrl(): string {
  const wsProtocol = API_BASE_URL.startsWith('https') ? 'wss' : 'ws';
  const baseUrl = API_BASE_URL.replace(/^https?:\/\//, '');
  return `${wsProtocol}://${baseUrl}`;
}

function getLogLevelFromMessage(message: string): LogLevel {
  const lowerMsg = message.toLowerCase();
  if (lowerMsg.includes('error') || lowerMsg.includes('fatal') || lowerMsg.includes('panic')) return 'error';
  if (lowerMsg.includes('warn')) return 'warn';
  if (lowerMsg.includes('debug') || lowerMsg.includes('trace')) return 'debug';
  return 'info';
}

function getLogLevelColor(type: LogMessage['type'], message: string): string {
  if (type === 'error') return 'text-status-error';
  if (type === 'info' || type === 'connected') return 'text-status-info';
  if (type === 'disconnected') return 'text-status-warning';

  const level = getLogLevelFromMessage(message);
  switch (level) {
    case 'error': return 'text-status-error';
    case 'warn': return 'text-status-warning';
    case 'debug': return 'text-gray-400';
    default: return 'text-gray-200';
  }
}

export function LogsTab({ serviceId, serviceName, deploymentId, env = 'development' }: LogsTabProps) {
  const [logs, setLogs] = useState<LogLine[]>([]);
  const [isStreaming, setIsStreaming] = useState(false);
  const [connectionStatus, setConnectionStatus] = useState<'disconnected' | 'connecting' | 'connected'>('disconnected');
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState('');
  const [levelFilter, setLevelFilter] = useState<LogLevel>('all');
  const [autoScroll, setAutoScroll] = useState(true);
  const [showTimestamps, setShowTimestamps] = useState(true);
  const [showPodInfo, setShowPodInfo] = useState(true);
  const [tailLines, setTailLines] = useState(100);
  const [searchQuery, setSearchQuery] = useState('');
  const [searchResults, setSearchResults] = useState<string[]>([]);
  const [isSearching, setIsSearching] = useState(false);

  const wsRef = useRef<WebSocket | null>(null);
  const logContainerRef = useRef<HTMLDivElement>(null);
  const logIdCounter = useRef(0);

  // Auto-scroll to bottom when new logs arrive
  useEffect(() => {
    if (autoScroll && logContainerRef.current) {
      logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight;
    }
  }, [logs, autoScroll]);

  // Clean up WebSocket on unmount
  useEffect(() => {
    return () => {
      if (wsRef.current) {
        wsRef.current.close();
      }
    };
  }, []);

  const connectWebSocket = useCallback(() => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      return;
    }

    setConnectionStatus('connecting');
    setError(null);

    // Get auth token for WebSocket
    let token = '';
    if (typeof window !== 'undefined') {
      const storedTokens = localStorage.getItem('enclii_tokens');
      if (storedTokens) {
        try {
          const tokens = JSON.parse(storedTokens);
          token = tokens.accessToken || '';
        } catch {
          // Invalid JSON
        }
      }
    }

    const wsBaseUrl = getWebSocketUrl();
    const endpoint = deploymentId
      ? `/v1/deployments/${deploymentId}/logs/stream`
      : `/v1/services/${serviceId}/logs/stream`;

    const params = new URLSearchParams({
      env,
      lines: tailLines.toString(),
      timestamps: showTimestamps.toString(),
    });

    // Include token in query params for WebSocket auth
    if (token) {
      params.append('token', token);
    }

    const wsUrl = `${wsBaseUrl}${endpoint}?${params.toString()}`;

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
          setLogs(prev => [...prev.slice(-999), logLine]); // Keep last 1000 logs
        } catch (e) {
          console.error('Failed to parse log message:', e);
        }
      };

      ws.onerror = (event) => {
        console.error('WebSocket error:', event);
        setError('WebSocket connection error. Check your network connection.');
      };

      ws.onclose = (event) => {
        setConnectionStatus('disconnected');
        setIsStreaming(false);

        if (!event.wasClean) {
          setError(`Connection closed unexpectedly (code: ${event.code})`);
        }
      };
    } catch (e) {
      setError('Failed to create WebSocket connection');
      setConnectionStatus('disconnected');
    }
  }, [serviceId, deploymentId, env, tailLines, showTimestamps]);

  const disconnectWebSocket = useCallback(() => {
    if (wsRef.current) {
      wsRef.current.close();
      wsRef.current = null;
    }
    setIsStreaming(false);
    setConnectionStatus('disconnected');
  }, []);

  const toggleStreaming = () => {
    if (isStreaming) {
      disconnectWebSocket();
    } else {
      connectWebSocket();
    }
  };

  const clearLogs = () => {
    setLogs([]);
  };

  const loadHistoricalLogs = async () => {
    try {
      setError(null);
      const data = await apiGet<LogHistoryResponse>(
        `/v1/services/${serviceId}/logs/history?env=${env}&lines=${tailLines}`
      );

      // Parse the logs string into individual lines
      const logLines = data.logs.split('\n').filter(line => line.trim());
      const parsedLogs: LogLine[] = logLines.map((line, idx) => ({
        id: `history-${idx}`,
        type: 'log' as const,
        timestamp: new Date(),
        message: line,
      }));

      setLogs(parsedLogs);
    } catch (err) {
      console.error('Failed to load historical logs:', err);
      setError(err instanceof Error ? err.message : 'Failed to load logs');
    }
  };

  const searchLogs = async () => {
    if (!searchQuery.trim()) return;

    setIsSearching(true);
    setError(null);

    try {
      const data = await apiPost<LogSearchResponse>(
        `/v1/services/${serviceId}/logs/search?env=${env}`,
        { query: searchQuery, limit: 500 }
      );
      setSearchResults(data.logs);
    } catch (err) {
      console.error('Failed to search logs:', err);
      setError(err instanceof Error ? err.message : 'Failed to search logs');
    } finally {
      setIsSearching(false);
    }
  };

  const downloadLogs = () => {
    const content = logs
      .map(log => {
        const ts = showTimestamps ? `[${formatTimestamp(log.timestamp)}] ` : '';
        const pod = showPodInfo && log.pod ? `[${log.pod}] ` : '';
        return `${ts}${pod}${log.message}`;
      })
      .join('\n');

    const blob = new Blob([content], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${serviceName}-logs-${new Date().toISOString().slice(0, 10)}.txt`;
    a.click();
    URL.revokeObjectURL(url);
  };

  // Filter logs
  const filteredLogs = logs.filter(log => {
    // Text filter
    if (filter && !log.message.toLowerCase().includes(filter.toLowerCase())) {
      return false;
    }
    // Level filter
    if (levelFilter !== 'all') {
      const logLevel = getLogLevelFromMessage(log.message);
      if (logLevel !== levelFilter) return false;
    }
    return true;
  });

  return (
    <div className="space-y-4">
      {/* Controls Header */}
      <Card>
        <CardHeader className="pb-2">
          <div className="flex items-center justify-between">
            <div>
              <CardTitle className="flex items-center gap-2">
                <svg aria-hidden="true" className="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                  <path d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
                </svg>
                Real-time Logs
              </CardTitle>
              <CardDescription>
                Stream logs from {serviceName} in {env}
              </CardDescription>
            </div>
            <div className="flex items-center gap-2">
              <Badge
                className={
                  connectionStatus === 'connected'
                    ? 'bg-status-success-muted text-status-success-foreground'
                    : connectionStatus === 'connecting'
                    ? 'bg-status-warning-muted text-status-warning-foreground animate-pulse'
                    : 'bg-muted text-foreground'
                }
              >
                {connectionStatus === 'connected' && '● Connected'}
                {connectionStatus === 'connecting' && '○ Connecting...'}
                {connectionStatus === 'disconnected' && '○ Disconnected'}
              </Badge>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <div className="flex flex-wrap gap-2">
            {/* Stream control */}
            <Button
              onClick={toggleStreaming}
              variant={isStreaming ? 'destructive' : 'default'}
              size="sm"
            >
              {isStreaming ? (
                <>
                  <svg aria-hidden="true" className="h-4 w-4 mr-2" viewBox="0 0 24 24" fill="currentColor">
                    <rect x="6" y="6" width="12" height="12" />
                  </svg>
                  Stop Streaming
                </>
              ) : (
                <>
                  <svg aria-hidden="true" className="h-4 w-4 mr-2" viewBox="0 0 24 24" fill="currentColor">
                    <polygon points="5 3 19 12 5 21" />
                  </svg>
                  Start Streaming
                </>
              )}
            </Button>

            {/* Load historical logs */}
            <Button onClick={loadHistoricalLogs} variant="outline" size="sm">
              <svg aria-hidden="true" className="h-4 w-4 mr-2" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <circle cx="12" cy="12" r="10" />
                <polyline points="12 6 12 12 16 14" />
              </svg>
              Load History
            </Button>

            {/* Clear logs */}
            <Button onClick={clearLogs} variant="outline" size="sm">
              <svg aria-hidden="true" className="h-4 w-4 mr-2" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <path d="M3 6h18M8 6V4h8v2m2 0v14a2 2 0 01-2 2H8a2 2 0 01-2-2V6h12z" />
              </svg>
              Clear
            </Button>

            {/* Download logs */}
            <Button onClick={downloadLogs} variant="outline" size="sm" disabled={logs.length === 0}>
              <svg aria-hidden="true" className="h-4 w-4 mr-2" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4M7 10l5 5 5-5M12 15V3" />
              </svg>
              Download
            </Button>

            {/* Auto-scroll toggle */}
            <Button
              onClick={() => setAutoScroll(!autoScroll)}
              variant={autoScroll ? 'secondary' : 'outline'}
              size="sm"
            >
              <svg aria-hidden="true" className="h-4 w-4 mr-2" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <path d="M12 5v14M19 12l-7 7-7-7" />
              </svg>
              Auto-scroll {autoScroll ? 'ON' : 'OFF'}
            </Button>
          </div>

          {/* Filters Row */}
          <div className="flex flex-wrap gap-2 mt-4">
            {/* Text filter */}
            <div className="flex-1 min-w-[200px]">
              <Input
                type="text"
                placeholder="Filter logs..."
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                className="h-8 text-sm"
              />
            </div>

            {/* Level filter */}
            <select
              value={levelFilter}
              onChange={(e) => setLevelFilter(e.target.value as LogLevel)}
              className="h-8 px-2 text-sm border rounded-md bg-card"
            >
              <option value="all">All Levels</option>
              <option value="error">Error</option>
              <option value="warn">Warning</option>
              <option value="info">Info</option>
              <option value="debug">Debug</option>
            </select>

            {/* Lines to fetch */}
            <select
              value={tailLines}
              onChange={(e) => setTailLines(parseInt(e.target.value))}
              className="h-8 px-2 text-sm border rounded-md bg-card"
            >
              <option value="50">50 lines</option>
              <option value="100">100 lines</option>
              <option value="500">500 lines</option>
              <option value="1000">1000 lines</option>
            </select>

            {/* Display options */}
            <label className="flex items-center gap-1 text-sm">
              <input
                type="checkbox"
                checked={showTimestamps}
                onChange={(e) => setShowTimestamps(e.target.checked)}
                className="rounded"
              />
              Timestamps
            </label>
            <label className="flex items-center gap-1 text-sm">
              <input
                type="checkbox"
                checked={showPodInfo}
                onChange={(e) => setShowPodInfo(e.target.checked)}
                className="rounded"
              />
              Pod Info
            </label>
          </div>
        </CardContent>
      </Card>

      {/* Search Card */}
      <Card>
        <CardContent className="py-3">
          <div className="flex gap-2">
            <Input
              type="text"
              placeholder="Search logs (e.g., 'error', 'timeout')..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && searchLogs()}
              className="flex-1"
            />
            <Button onClick={searchLogs} disabled={isSearching || !searchQuery.trim()}>
              {isSearching ? 'Searching...' : 'Search'}
            </Button>
          </div>
          {searchResults.length > 0 && (
            <div className="mt-3">
              <div className="flex items-center justify-between mb-2">
                <span className="text-sm text-muted-foreground">
                  Found {searchResults.length} matching lines
                </span>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => setSearchResults([])}
                >
                  Clear Results
                </Button>
              </div>
              <div className="max-h-40 overflow-auto bg-gray-900 rounded p-2 font-mono text-xs">
                {searchResults.map((line, idx) => (
                  <div key={idx} className="text-status-warning py-0.5">
                    {line}
                  </div>
                ))}
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Error display */}
      {error && (
        <div className="bg-status-error-muted border border-status-error/30 rounded-md p-4 text-status-error-foreground">
          <div className="flex items-center gap-2">
            <svg aria-hidden="true" className="h-5 w-5" viewBox="0 0 24 24" fill="currentColor">
              <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm1 15h-2v-2h2v2zm0-4h-2V7h2v6z" />
            </svg>
            <span>{error}</span>
          </div>
        </div>
      )}

      {/* Log viewer */}
      <Card className="overflow-hidden">
        <div
          ref={logContainerRef}
          className="bg-gray-900 text-gray-100 font-mono text-xs overflow-auto h-[500px] p-4"
        >
          {filteredLogs.length === 0 ? (
            <div className="text-center text-gray-500 py-8">
              {logs.length === 0 ? (
                <>
                  <svg aria-hidden="true" className="mx-auto h-12 w-12 mb-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1">
                    <path d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
                  </svg>
                  <p>No logs yet</p>
                  <p className="mt-2">Click "Start Streaming" or "Load History" to view logs</p>
                </>
              ) : (
                <p>No logs match your filter</p>
              )}
            </div>
          ) : (
            filteredLogs.map((log) => (
              <div
                key={log.id}
                className={`py-0.5 hover:bg-gray-800 ${getLogLevelColor(log.type, log.message)}`}
              >
                {showTimestamps && (
                  <span className="text-gray-500 mr-2">
                    [{formatTimestamp(log.timestamp)}]
                  </span>
                )}
                {showPodInfo && log.pod && (
                  <span className="text-cyan-400 mr-2">
                    [{log.pod}]
                  </span>
                )}
                {log.container && (
                  <span className="text-purple-400 mr-2">
                    [{log.container}]
                  </span>
                )}
                <span>{log.message}</span>
              </div>
            ))
          )}
        </div>

        {/* Status bar */}
        <div className="bg-gray-800 text-gray-400 text-xs px-4 py-2 flex items-center justify-between border-t border-gray-700">
          <span>
            {filteredLogs.length} / {logs.length} lines
            {filter && ` (filtered)`}
          </span>
          <span>
            {isStreaming ? '● Live' : '○ Paused'}
          </span>
        </div>
      </Card>
    </div>
  );
}
