'use client';

import { useState, useEffect, useCallback } from 'react';
import Link from 'next/link';
import { useParams } from 'next/navigation';
import { toast } from 'sonner';
import { Button } from "@enclii/ui-components/button";
import { Card, CardContent } from '@/components/ui/card';
import { WebhookCard, Webhook, WebhookType, WebhookEventType } from '@/components/webhooks/WebhookCard';
import { CreateWebhookModal, WebhookFormData } from '@/components/webhooks/CreateWebhookModal';
import { apiGet, apiPost, apiPatch, apiDelete } from '@/lib/api';

interface Project {
  id: string;
  name: string;
  slug: string;
}

interface WebhooksResponse {
  webhooks: Webhook[];
}

interface EventTypesResponse {
  event_types: Array<{
    type: WebhookEventType;
    description: string;
  }>;
}

export default function ProjectWebhooksPage() {
  const params = useParams();
  const slug = params?.slug as string;

  const [project, setProject] = useState<Project | null>(null);
  const [webhooks, setWebhooks] = useState<Webhook[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [editingWebhook, setEditingWebhook] = useState<Webhook | null>(null);

  const fetchProject = useCallback(async () => {
    try {
      const projectData = await apiGet<Project>(`/v1/projects/${slug}`);
      setProject(projectData);
    } catch (err) {
      console.error('Failed to fetch project:', err);
    }
  }, [slug]);

  const fetchWebhooks = useCallback(async () => {
    try {
      setError(null);
      const data = await apiGet<WebhooksResponse>(`/v1/projects/${slug}/webhooks`);
      setWebhooks(data.webhooks || []);
    } catch (err) {
      console.error('Failed to fetch webhooks:', err);
      setError(err instanceof Error ? err.message : 'Failed to load webhooks');
    } finally {
      setLoading(false);
    }
  }, [slug]);

  useEffect(() => {
    if (slug) {
      fetchProject();
      fetchWebhooks();
    }
  }, [slug, fetchProject, fetchWebhooks]);

  const handleCreateWebhook = async (data: WebhookFormData) => {
    await apiPost(`/v1/projects/${slug}/webhooks`, data);
    setShowCreateModal(false);
    await fetchWebhooks();
  };

  const handleUpdateWebhook = async (data: WebhookFormData) => {
    if (!editingWebhook) return;
    await apiPatch(`/v1/webhooks/${editingWebhook.id}`, data);
    setEditingWebhook(null);
    await fetchWebhooks();
  };

  const handleDeleteWebhook = async (webhookId: string) => {
    await apiDelete(`/v1/webhooks/${webhookId}`);
    await fetchWebhooks();
  };

  const handleTestWebhook = async (webhookId: string) => {
    try {
      await apiPost(`/v1/webhooks/${webhookId}/test`, { event_type: 'deployment_succeeded' });
      toast.success('Test notification sent successfully');
    } catch (err) {
      toast.error('Failed to send test notification: ' + (err instanceof Error ? err.message : 'Unknown error'));
    }
  };

  const handleToggleWebhook = async (webhookId: string, enabled: boolean) => {
    await apiPatch(`/v1/webhooks/${webhookId}`, { enabled });
    await fetchWebhooks();
  };

  if (loading) {
    return (
      <div className="max-w-5xl mx-auto py-6 px-4 sm:px-6 lg:px-8">
        <div className="animate-pulse space-y-6">
          <div className="h-8 bg-muted rounded w-1/3"></div>
          <div className="h-4 bg-muted rounded w-2/3"></div>
          <div className="grid gap-4 md:grid-cols-2">
            {[1, 2].map((i) => (
              <div key={i} className="h-48 bg-muted rounded-lg"></div>
            ))}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-5xl mx-auto py-6 px-4 sm:px-6 lg:px-8">
      {/* Breadcrumb */}
      <nav className="flex mb-6" aria-label="Breadcrumb">
        <ol className="flex items-center space-x-4">
          <li>
            <Link href="/projects" className="text-muted-foreground hover:text-foreground">
              Projects
            </Link>
          </li>
          <li>
            <div className="flex items-center">
              <svg aria-hidden="true" className="flex-shrink-0 h-5 w-5 text-muted-foreground/50" fill="currentColor" viewBox="0 0 20 20">
                <path fillRule="evenodd" d="M7.293 14.707a1 1 0 010-1.414L10.586 10 7.293 6.707a1 1 0 011.414-1.414l4 4a1 1 0 010 1.414l-4 4a1 1 0 01-1.414 0z" clipRule="evenodd" />
              </svg>
              <Link href={`/projects/${slug}`} className="ml-4 text-muted-foreground hover:text-foreground">
                {project?.name || slug}
              </Link>
            </div>
          </li>
          <li>
            <div className="flex items-center">
              <svg aria-hidden="true" className="flex-shrink-0 h-5 w-5 text-muted-foreground/50" fill="currentColor" viewBox="0 0 20 20">
                <path fillRule="evenodd" d="M7.293 14.707a1 1 0 010-1.414L10.586 10 7.293 6.707a1 1 0 011.414-1.414l4 4a1 1 0 010 1.414l-4 4a1 1 0 01-1.414 0z" clipRule="evenodd" />
              </svg>
              <span className="ml-4 text-sm font-medium text-muted-foreground">Webhooks</span>
            </div>
          </li>
        </ol>
      </nav>

      {/* Header */}
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-2xl font-bold text-foreground">Notification Webhooks</h1>
          <p className="text-muted-foreground mt-1">
            Receive real-time notifications about deployments, builds, and more
          </p>
        </div>
        <Button onClick={() => setShowCreateModal(true)}>
          <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
          </svg>
          Add Webhook
        </Button>
      </div>

      {error && (
        <Card className="border-status-error/30 bg-status-error-muted mb-6">
          <CardContent className="py-4">
            <div className="flex items-center gap-3">
              <svg aria-hidden="true" className="w-5 h-5 text-status-error" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
              <p className="text-status-error">{error}</p>
              <Button variant="outline" size="sm" onClick={fetchWebhooks} className="ml-auto">
                Try Again
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Webhooks List */}
      {webhooks.length === 0 ? (
        <Card>
          <CardContent className="py-12">
            <div className="text-center">
              <svg
                aria-hidden="true"
                className="mx-auto h-12 w-12 text-muted-foreground"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={1.5}
                  d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9"
                />
              </svg>
              <h3 className="mt-4 text-lg font-medium text-foreground">No webhooks configured</h3>
              <p className="mt-2 text-sm text-muted-foreground max-w-md mx-auto">
                Set up webhooks to receive notifications on Slack, Discord, Telegram, or your own endpoints
                when deployments succeed, builds fail, and more.
              </p>
              <Button onClick={() => setShowCreateModal(true)} className="mt-6">
                <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
                </svg>
                Create Your First Webhook
              </Button>
            </div>
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {webhooks.map((webhook) => (
            <WebhookCard
              key={webhook.id}
              webhook={webhook}
              onEdit={setEditingWebhook}
              onDelete={handleDeleteWebhook}
              onTest={handleTestWebhook}
              onToggle={handleToggleWebhook}
            />
          ))}
        </div>
      )}

      {/* Info section */}
      <Card className="mt-8 bg-status-info-muted border-status-info/30">
        <CardContent className="py-4">
          <div className="flex gap-3">
            <svg aria-hidden="true" className="w-5 h-5 text-status-info mt-0.5 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <div>
              <p className="text-sm text-status-info-foreground font-medium">How webhooks work</p>
              <p className="text-sm text-status-info mt-1">
                When events occur in your project (like a deployment succeeding or failing),
                we&apos;ll send a notification to your configured webhooks. Webhooks are automatically
                disabled after 5 consecutive failures to prevent spam.
              </p>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Create/Edit Modal */}
      <CreateWebhookModal
        isOpen={showCreateModal || !!editingWebhook}
        onClose={() => {
          setShowCreateModal(false);
          setEditingWebhook(null);
        }}
        onSubmit={editingWebhook ? handleUpdateWebhook : handleCreateWebhook}
        editingWebhook={editingWebhook}
      />
    </div>
  );
}
