'use client';

import { useState } from 'react';
import { toast } from 'sonner';
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { apiGet } from "@/lib/api";
import { Spinner } from '@/components/ui/spinner';
import type { DatabaseAddon, DatabaseAddonStatus, DatabaseAddonType } from "@/app/(protected)/databases/page";

interface DatabaseCardProps {
  database: DatabaseAddon & { project_name?: string; project_slug?: string };
  onDelete: () => void;
  isDeleting: boolean;
}

const STATUS_COLORS: Record<DatabaseAddonStatus, string> = {
  pending: 'bg-muted text-foreground',
  provisioning: 'bg-status-warning-muted text-status-warning-foreground',
  ready: 'bg-status-success-muted text-status-success-foreground',
  failed: 'bg-status-error-muted text-status-error-foreground',
  deleting: 'bg-status-warning-muted text-status-warning-foreground',
  deleted: 'bg-muted text-muted-foreground',
};

const TYPE_ICONS: Record<DatabaseAddonType, { icon: JSX.Element; color: string }> = {
  postgres: {
    icon: (
      <svg aria-hidden="true" className="w-6 h-6" viewBox="0 0 24 24" fill="currentColor">
        <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 18c-4.41 0-8-3.59-8-8s3.59-8 8-8 8 3.59 8 8-3.59 8-8 8z"/>
        <path d="M12 6c-3.31 0-6 2.69-6 6s2.69 6 6 6 6-2.69 6-6-2.69-6-6-6zm0 10c-2.21 0-4-1.79-4-4s1.79-4 4-4 4 1.79 4 4-1.79 4-4 4z"/>
      </svg>
    ),
    color: 'text-primary bg-primary/10',
  },
  redis: {
    icon: (
      <svg aria-hidden="true" className="w-6 h-6" viewBox="0 0 24 24" fill="currentColor">
        <path d="M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5"/>
      </svg>
    ),
    color: 'text-destructive bg-destructive/10',
  },
  mysql: {
    icon: (
      <svg aria-hidden="true" className="w-6 h-6" viewBox="0 0 24 24" fill="currentColor">
        <path d="M12 3C7.58 3 4 4.79 4 7v10c0 2.21 3.58 4 8 4s8-1.79 8-4V7c0-2.21-3.58-4-8-4zm0 2c3.87 0 6 1.5 6 2s-2.13 2-6 2-6-1.5-6-2 2.13-2 6-2zM6 17v-2.34c1.35.85 3.56 1.34 6 1.34s4.65-.49 6-1.34V17c0 .5-2.13 2-6 2s-6-1.5-6-2z"/>
      </svg>
    ),
    color: 'text-status-warning bg-status-warning-muted',
  },
};

export function DatabaseCard({ database, onDelete, isDeleting }: DatabaseCardProps) {
  const [showCredentials, setShowCredentials] = useState(false);
  const [credentials, setCredentials] = useState<{ connection_uri?: string } | null>(null);
  const [loadingCreds, setLoadingCreds] = useState(false);

  const typeInfo = TYPE_ICONS[database.type];
  const statusColor = STATUS_COLORS[database.status];

  const handleShowCredentials = async () => {
    if (showCredentials) {
      setShowCredentials(false);
      return;
    }

    if (database.status !== 'ready') {
      toast.error('Database must be ready to view credentials');
      return;
    }

    setLoadingCreds(true);
    try {
      const creds = await apiGet<{ connection_uri?: string }>(`/v1/addons/${database.id}/credentials`);
      setCredentials(creds);
      setShowCredentials(true);
    } catch (err) {
      console.error('Failed to fetch credentials:', err);
      toast.error(err instanceof Error ? err.message : 'Failed to fetch credentials');
    } finally {
      setLoadingCreds(false);
    }
  };

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    // Could add a toast notification here
  };

  return (
    <Card className="hover:shadow-md transition-shadow">
      <CardHeader className="pb-3">
        <div className="flex items-start justify-between">
          <div className="flex items-center gap-3">
            <div className={`p-2 rounded-lg ${typeInfo.color}`}>
              {typeInfo.icon}
            </div>
            <div>
              <CardTitle className="text-lg">{database.name}</CardTitle>
              <p className="text-sm text-muted-foreground capitalize">{database.type}</p>
            </div>
          </div>
          <span className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${statusColor}`}>
            {database.status === 'provisioning' && (
              <Spinner size="sm" className="mr-1" />
            )}
            {database.status}
          </span>
        </div>
      </CardHeader>
      <CardContent>
        <div className="space-y-3">
          {/* Project info */}
          {database.project_name && (
            <div className="flex items-center text-sm">
              <span className="text-muted-foreground w-20">Project:</span>
              <span className="font-medium">{database.project_name}</span>
            </div>
          )}

          {/* Config info */}
          {database.config && (
            <>
              {database.config.version && (
                <div className="flex items-center text-sm">
                  <span className="text-muted-foreground w-20">Version:</span>
                  <span>{database.config.version}</span>
                </div>
              )}
              {database.config.storage_gb && (
                <div className="flex items-center text-sm">
                  <span className="text-muted-foreground w-20">Storage:</span>
                  <span>{database.config.storage_gb} GB</span>
                </div>
              )}
              {database.config.memory && (
                <div className="flex items-center text-sm">
                  <span className="text-muted-foreground w-20">Memory:</span>
                  <span>{database.config.memory}</span>
                </div>
              )}
            </>
          )}

          {/* Status message */}
          {database.status_message && (
            <div className="text-sm text-muted-foreground italic">
              {database.status_message}
            </div>
          )}

          {/* Credentials section */}
          {showCredentials && credentials && (
            <div className="mt-4 p-3 bg-muted/50 rounded-lg">
              <div className="flex items-center justify-between mb-2">
                <span className="text-sm font-medium">Connection URI</span>
                <Button
                  variant="ghost"
                  size="sm"
                  aria-label="Copy connection URI"
                  onClick={() => copyToClipboard(credentials.connection_uri || '')}
                >
                  <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
                  </svg>
                </Button>
              </div>
              <code className="text-xs break-all bg-muted p-2 rounded block">
                {credentials.connection_uri || 'N/A'}
              </code>
            </div>
          )}

          {/* Actions */}
          <div className="flex items-center gap-2 pt-3 border-t">
            <Button
              variant="outline"
              size="sm"
              onClick={handleShowCredentials}
              disabled={database.status !== 'ready' || loadingCreds}
            >
              {loadingCreds ? (
                <Spinner size="sm" className="mr-1" />
              ) : (
                <svg aria-hidden="true" className="w-4 h-4 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z" />
                </svg>
              )}
              {showCredentials ? 'Hide' : 'Credentials'}
            </Button>
            <Button
              variant="destructive"
              size="sm"
              onClick={onDelete}
              disabled={isDeleting || database.status === 'deleting'}
            >
              {isDeleting ? (
                <Spinner size="sm" className="mr-1" />
              ) : (
                <svg aria-hidden="true" className="w-4 h-4 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                </svg>
              )}
              Delete
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
