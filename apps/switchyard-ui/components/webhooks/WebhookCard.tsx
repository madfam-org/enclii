'use client';

import { useState } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from "@enclii/ui-components/button";
import { Badge } from "@enclii/ui-components/badge";
import { ConfirmDialog } from '@/components/ui/confirm-dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@enclii/ui-components/dropdown-menu";
import { formatRelativeTime } from '@/lib/formatting';

export type WebhookType = 'slack' | 'discord' | 'telegram' | 'custom';
export type WebhookEventType =
  | 'deployment_succeeded'
  | 'deployment_failed'
  | 'build_succeeded'
  | 'build_failed'
  | 'service_created'
  | 'service_deleted'
  | 'database_created'
  | 'database_deleted';

export interface Webhook {
  id: string;
  project_id: string;
  name: string;
  type: WebhookType;
  webhook_url?: string;
  telegram_bot_token?: string;
  telegram_chat_id?: string;
  events: WebhookEventType[];
  enabled: boolean;
  last_delivery_at?: string;
  last_delivery_status?: string;
  failure_count: number;
  created_at: string;
  updated_at: string;
}

interface WebhookCardProps {
  webhook: Webhook;
  onEdit: (webhook: Webhook) => void;
  onDelete: (webhookId: string) => void;
  onTest: (webhookId: string) => void;
  onToggle: (webhookId: string, enabled: boolean) => void;
}

const WEBHOOK_TYPE_CONFIG: Record<WebhookType, { label: string; color: string; icon: JSX.Element }> = {
  slack: {
    label: 'Slack',
    color: 'bg-muted text-foreground',
    icon: (
      <svg aria-hidden="true" className="w-5 h-5" viewBox="0 0 24 24" fill="currentColor">
        <path d="M5.042 15.165a2.528 2.528 0 0 1-2.52 2.523A2.528 2.528 0 0 1 0 15.165a2.527 2.527 0 0 1 2.522-2.52h2.52v2.52zM6.313 15.165a2.527 2.527 0 0 1 2.521-2.52 2.527 2.527 0 0 1 2.521 2.52v6.313A2.528 2.528 0 0 1 8.834 24a2.528 2.528 0 0 1-2.521-2.522v-6.313zM8.834 5.042a2.528 2.528 0 0 1-2.521-2.52A2.528 2.528 0 0 1 8.834 0a2.528 2.528 0 0 1 2.521 2.522v2.52H8.834zM8.834 6.313a2.528 2.528 0 0 1 2.521 2.521 2.528 2.528 0 0 1-2.521 2.521H2.522A2.528 2.528 0 0 1 0 8.834a2.528 2.528 0 0 1 2.522-2.521h6.312zM18.956 8.834a2.528 2.528 0 0 1 2.522-2.521A2.528 2.528 0 0 1 24 8.834a2.528 2.528 0 0 1-2.522 2.521h-2.522V8.834zM17.688 8.834a2.528 2.528 0 0 1-2.523 2.521 2.527 2.527 0 0 1-2.52-2.521V2.522A2.527 2.527 0 0 1 15.165 0a2.528 2.528 0 0 1 2.523 2.522v6.312zM15.165 18.956a2.528 2.528 0 0 1 2.523 2.522A2.528 2.528 0 0 1 15.165 24a2.527 2.527 0 0 1-2.52-2.522v-2.522h2.52zM15.165 17.688a2.527 2.527 0 0 1-2.52-2.523 2.526 2.526 0 0 1 2.52-2.52h6.313A2.527 2.527 0 0 1 24 15.165a2.528 2.528 0 0 1-2.522 2.523h-6.313z"/>
      </svg>
    ),
  },
  discord: {
    label: 'Discord',
    color: 'bg-muted text-foreground',
    icon: (
      <svg aria-hidden="true" className="w-5 h-5" viewBox="0 0 24 24" fill="currentColor">
        <path d="M20.317 4.37a19.791 19.791 0 0 0-4.885-1.515.074.074 0 0 0-.079.037c-.21.375-.444.864-.608 1.25a18.27 18.27 0 0 0-5.487 0 12.64 12.64 0 0 0-.617-1.25.077.077 0 0 0-.079-.037A19.736 19.736 0 0 0 3.677 4.37a.07.07 0 0 0-.032.027C.533 9.046-.32 13.58.099 18.057a.082.082 0 0 0 .031.057 19.9 19.9 0 0 0 5.993 3.03.078.078 0 0 0 .084-.028 14.09 14.09 0 0 0 1.226-1.994.076.076 0 0 0-.041-.106 13.107 13.107 0 0 1-1.872-.892.077.077 0 0 1-.008-.128 10.2 10.2 0 0 0 .372-.292.074.074 0 0 1 .077-.01c3.928 1.793 8.18 1.793 12.062 0a.074.074 0 0 1 .078.01c.12.098.246.198.373.292a.077.077 0 0 1-.006.127 12.299 12.299 0 0 1-1.873.892.077.077 0 0 0-.041.107c.36.698.772 1.362 1.225 1.993a.076.076 0 0 0 .084.028 19.839 19.839 0 0 0 6.002-3.03.077.077 0 0 0 .032-.054c.5-5.177-.838-9.674-3.549-13.66a.061.061 0 0 0-.031-.03zM8.02 15.33c-1.183 0-2.157-1.085-2.157-2.419 0-1.333.956-2.419 2.157-2.419 1.21 0 2.176 1.096 2.157 2.42 0 1.333-.956 2.418-2.157 2.418zm7.975 0c-1.183 0-2.157-1.085-2.157-2.419 0-1.333.955-2.419 2.157-2.419 1.21 0 2.176 1.096 2.157 2.42 0 1.333-.946 2.418-2.157 2.418z"/>
      </svg>
    ),
  },
  telegram: {
    label: 'Telegram',
    color: 'bg-muted text-foreground',
    icon: (
      <svg aria-hidden="true" className="w-5 h-5" viewBox="0 0 24 24" fill="currentColor">
        <path d="M11.944 0A12 12 0 0 0 0 12a12 12 0 0 0 12 12 12 12 0 0 0 12-12A12 12 0 0 0 12 0a12 12 0 0 0-.056 0zm4.962 7.224c.1-.002.321.023.465.14a.506.506 0 0 1 .171.325c.016.093.036.306.02.472-.18 1.898-.962 6.502-1.36 8.627-.168.9-.499 1.201-.82 1.23-.696.065-1.225-.46-1.9-.902-1.056-.693-1.653-1.124-2.678-1.8-1.185-.78-.417-1.21.258-1.91.177-.184 3.247-2.977 3.307-3.23.007-.032.014-.15-.056-.212s-.174-.041-.249-.024c-.106.024-1.793 1.14-5.061 3.345-.48.33-.913.49-1.302.48-.428-.008-1.252-.241-1.865-.44-.752-.245-1.349-.374-1.297-.789.027-.216.325-.437.893-.663 3.498-1.524 5.83-2.529 6.998-3.014 3.332-1.386 4.025-1.627 4.476-1.635z"/>
      </svg>
    ),
  },
  custom: {
    label: 'Custom',
    color: 'bg-muted text-muted-foreground',
    icon: (
      <svg aria-hidden="true" className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1" />
      </svg>
    ),
  },
};

const EVENT_LABELS: Record<WebhookEventType, string> = {
  deployment_succeeded: 'Deployment Succeeded',
  deployment_failed: 'Deployment Failed',
  build_succeeded: 'Build Succeeded',
  build_failed: 'Build Failed',
  service_created: 'Service Created',
  service_deleted: 'Service Deleted',
  database_created: 'Database Created',
  database_deleted: 'Database Deleted',
};

export function WebhookCard({ webhook, onEdit, onDelete, onTest, onToggle }: WebhookCardProps) {
  const [isDeleting, setIsDeleting] = useState(false);
  const [isTesting, setIsTesting] = useState(false);
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
  const typeConfig = WEBHOOK_TYPE_CONFIG[webhook.type];

  const handleDelete = () => {
    setShowDeleteConfirm(true);
  };

  const confirmDelete = async () => {
    setShowDeleteConfirm(false);
    setIsDeleting(true);
    try {
      await onDelete(webhook.id);
    } finally {
      setIsDeleting(false);
    }
  };

  const handleTest = async () => {
    setIsTesting(true);
    try {
      await onTest(webhook.id);
    } finally {
      setIsTesting(false);
    }
  };

  return (
    <Card className={!webhook.enabled ? 'opacity-60' : ''}>
      <CardHeader className="pb-3">
        <div className="flex items-start justify-between">
          <div className="flex items-center gap-3">
            <div className={`p-2 rounded-lg ${typeConfig.color}`}>
              {typeConfig.icon}
            </div>
            <div>
              <CardTitle className="text-lg flex items-center gap-2">
                {webhook.name}
                {!webhook.enabled && (
                  <Badge variant="outline" className="text-xs">Disabled</Badge>
                )}
                {webhook.failure_count >= 5 && (
                  <Badge variant="destructive" className="text-xs">Auto-disabled</Badge>
                )}
              </CardTitle>
              <p className="text-sm text-muted-foreground">{typeConfig.label}</p>
            </div>
          </div>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="sm" aria-label="More options">
                <svg aria-hidden="true" className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 5v.01M12 12v.01M12 19v.01M12 6a1 1 0 110-2 1 1 0 010 2zm0 7a1 1 0 110-2 1 1 0 010 2zm0 7a1 1 0 110-2 1 1 0 010 2z" />
                </svg>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onClick={() => onEdit(webhook)}>
                <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
                </svg>
                Edit
              </DropdownMenuItem>
              <DropdownMenuItem onClick={handleTest} disabled={isTesting}>
                <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z" />
                </svg>
                {isTesting ? 'Testing...' : 'Send Test'}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => onToggle(webhook.id, !webhook.enabled)}>
                <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d={webhook.enabled ? "M18.364 18.364A9 9 0 005.636 5.636m12.728 12.728A9 9 0 015.636 5.636m12.728 12.728L5.636 5.636" : "M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"} />
                </svg>
                {webhook.enabled ? 'Disable' : 'Enable'}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={handleDelete} disabled={isDeleting} className="text-destructive">
                <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                </svg>
                {isDeleting ? 'Deleting...' : 'Delete'}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Events */}
        <div>
          <p className="text-sm font-medium text-muted-foreground mb-2">Events</p>
          <div className="flex flex-wrap gap-1.5">
            {webhook.events.map((event) => (
              <Badge key={event} variant="secondary" className="text-xs">
                {EVENT_LABELS[event] || event}
              </Badge>
            ))}
          </div>
        </div>

        {/* Last delivery */}
        <div className="flex items-center justify-between text-sm">
          <span className="text-muted-foreground">Last delivery</span>
          {webhook.last_delivery_at ? (
            <div className="flex items-center gap-2">
              <span className={webhook.last_delivery_status === 'success' ? 'text-status-success' : 'text-destructive'}>
                {webhook.last_delivery_status === 'success' ? '✓' : '✗'}
              </span>
              <span>{formatRelativeTime(webhook.last_delivery_at)}</span>
            </div>
          ) : (
            <span className="text-muted-foreground">Never</span>
          )}
        </div>

        {/* Failure count */}
        {webhook.failure_count > 0 && (
          <div className="flex items-center justify-between text-sm">
            <span className="text-muted-foreground">Consecutive failures</span>
            <Badge variant={webhook.failure_count >= 5 ? 'destructive' : 'outline'}>
              {webhook.failure_count}
            </Badge>
          </div>
        )}
      </CardContent>

      <ConfirmDialog
        open={showDeleteConfirm}
        title="Delete Webhook"
        description={`Are you sure you want to delete the webhook "${webhook.name}"? This action cannot be undone.`}
        variant="destructive"
        confirmLabel="Delete"
        onConfirm={confirmDelete}
        onCancel={() => setShowDeleteConfirm(false)}
      />
    </Card>
  );
}
