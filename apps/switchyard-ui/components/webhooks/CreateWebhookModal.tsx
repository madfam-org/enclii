'use client';

import { useState, useEffect } from 'react';
import { Button } from "@enclii/ui-components/button";
import { Input } from "@enclii/ui-components/input";
import { Label } from "@enclii/ui-components/label";
import { Checkbox } from "@/components/ui/checkbox";
import { Spinner } from '@/components/ui/spinner';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@enclii/ui-components/dialog";
import type { Webhook, WebhookType, WebhookEventType } from './WebhookCard';

interface CreateWebhookModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSubmit: (data: WebhookFormData) => Promise<void>;
  editingWebhook?: Webhook | null;
}

export interface WebhookFormData {
  name: string;
  type: WebhookType;
  webhook_url?: string;
  telegram_bot_token?: string;
  telegram_chat_id?: string;
  signing_secret?: string;
  custom_headers?: Record<string, string>;
  events: WebhookEventType[];
  enabled: boolean;
}

const WEBHOOK_TYPES: Array<{
  value: WebhookType;
  label: string;
  description: string;
  icon: JSX.Element;
}> = [
  {
    value: 'slack',
    label: 'Slack',
    description: 'Send notifications to Slack channels',
    icon: (
      <svg aria-hidden="true" className="w-8 h-8 text-purple-600" viewBox="0 0 24 24" fill="currentColor">
        <path d="M5.042 15.165a2.528 2.528 0 0 1-2.52 2.523A2.528 2.528 0 0 1 0 15.165a2.527 2.527 0 0 1 2.522-2.52h2.52v2.52zM6.313 15.165a2.527 2.527 0 0 1 2.521-2.52 2.527 2.527 0 0 1 2.521 2.52v6.313A2.528 2.528 0 0 1 8.834 24a2.528 2.528 0 0 1-2.521-2.522v-6.313zM8.834 5.042a2.528 2.528 0 0 1-2.521-2.52A2.528 2.528 0 0 1 8.834 0a2.528 2.528 0 0 1 2.521 2.522v2.52H8.834zM8.834 6.313a2.528 2.528 0 0 1 2.521 2.521 2.528 2.528 0 0 1-2.521 2.521H2.522A2.528 2.528 0 0 1 0 8.834a2.528 2.528 0 0 1 2.522-2.521h6.312zM18.956 8.834a2.528 2.528 0 0 1 2.522-2.521A2.528 2.528 0 0 1 24 8.834a2.528 2.528 0 0 1-2.522 2.521h-2.522V8.834zM17.688 8.834a2.528 2.528 0 0 1-2.523 2.521 2.527 2.527 0 0 1-2.52-2.521V2.522A2.527 2.527 0 0 1 15.165 0a2.528 2.528 0 0 1 2.523 2.522v6.312zM15.165 18.956a2.528 2.528 0 0 1 2.523 2.522A2.528 2.528 0 0 1 15.165 24a2.527 2.527 0 0 1-2.52-2.522v-2.522h2.52zM15.165 17.688a2.527 2.527 0 0 1-2.52-2.523 2.526 2.526 0 0 1 2.52-2.52h6.313A2.527 2.527 0 0 1 24 15.165a2.528 2.528 0 0 1-2.522 2.523h-6.313z"/>
      </svg>
    ),
  },
  {
    value: 'discord',
    label: 'Discord',
    description: 'Send notifications to Discord channels',
    icon: (
      <svg aria-hidden="true" className="w-8 h-8 text-indigo-600" viewBox="0 0 24 24" fill="currentColor">
        <path d="M20.317 4.37a19.791 19.791 0 0 0-4.885-1.515.074.074 0 0 0-.079.037c-.21.375-.444.864-.608 1.25a18.27 18.27 0 0 0-5.487 0 12.64 12.64 0 0 0-.617-1.25.077.077 0 0 0-.079-.037A19.736 19.736 0 0 0 3.677 4.37a.07.07 0 0 0-.032.027C.533 9.046-.32 13.58.099 18.057a.082.082 0 0 0 .031.057 19.9 19.9 0 0 0 5.993 3.03.078.078 0 0 0 .084-.028 14.09 14.09 0 0 0 1.226-1.994.076.076 0 0 0-.041-.106 13.107 13.107 0 0 1-1.872-.892.077.077 0 0 1-.008-.128 10.2 10.2 0 0 0 .372-.292.074.074 0 0 1 .077-.01c3.928 1.793 8.18 1.793 12.062 0a.074.074 0 0 1 .078.01c.12.098.246.198.373.292a.077.077 0 0 1-.006.127 12.299 12.299 0 0 1-1.873.892.077.077 0 0 0-.041.107c.36.698.772 1.362 1.225 1.993a.076.076 0 0 0 .084.028 19.839 19.839 0 0 0 6.002-3.03.077.077 0 0 0 .032-.054c.5-5.177-.838-9.674-3.549-13.66a.061.061 0 0 0-.031-.03zM8.02 15.33c-1.183 0-2.157-1.085-2.157-2.419 0-1.333.956-2.419 2.157-2.419 1.21 0 2.176 1.096 2.157 2.42 0 1.333-.956 2.418-2.157 2.418zm7.975 0c-1.183 0-2.157-1.085-2.157-2.419 0-1.333.955-2.419 2.157-2.419 1.21 0 2.176 1.096 2.157 2.42 0 1.333-.946 2.418-2.157 2.418z"/>
      </svg>
    ),
  },
  {
    value: 'telegram',
    label: 'Telegram',
    description: 'Send notifications via Telegram Bot',
    icon: (
      <svg aria-hidden="true" className="w-8 h-8 text-sky-600" viewBox="0 0 24 24" fill="currentColor">
        <path d="M11.944 0A12 12 0 0 0 0 12a12 12 0 0 0 12 12 12 12 0 0 0 12-12A12 12 0 0 0 12 0a12 12 0 0 0-.056 0zm4.962 7.224c.1-.002.321.023.465.14a.506.506 0 0 1 .171.325c.016.093.036.306.02.472-.18 1.898-.962 6.502-1.36 8.627-.168.9-.499 1.201-.82 1.23-.696.065-1.225-.46-1.9-.902-1.056-.693-1.653-1.124-2.678-1.8-1.185-.78-.417-1.21.258-1.91.177-.184 3.247-2.977 3.307-3.23.007-.032.014-.15-.056-.212s-.174-.041-.249-.024c-.106.024-1.793 1.14-5.061 3.345-.48.33-.913.49-1.302.48-.428-.008-1.252-.241-1.865-.44-.752-.245-1.349-.374-1.297-.789.027-.216.325-.437.893-.663 3.498-1.524 5.83-2.529 6.998-3.014 3.332-1.386 4.025-1.627 4.476-1.635z"/>
      </svg>
    ),
  },
  {
    value: 'custom',
    label: 'Custom Webhook',
    description: 'Send to your own HTTP endpoint',
    icon: (
      <svg aria-hidden="true" className="w-8 h-8 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1" />
      </svg>
    ),
  },
];

const EVENT_OPTIONS: Array<{ value: WebhookEventType; label: string; category: string }> = [
  { value: 'deployment_succeeded', label: 'Deployment Succeeded', category: 'Deployments' },
  { value: 'deployment_failed', label: 'Deployment Failed', category: 'Deployments' },
  { value: 'build_succeeded', label: 'Build Succeeded', category: 'Builds' },
  { value: 'build_failed', label: 'Build Failed', category: 'Builds' },
  { value: 'service_created', label: 'Service Created', category: 'Services' },
  { value: 'service_deleted', label: 'Service Deleted', category: 'Services' },
  { value: 'database_created', label: 'Database Created', category: 'Databases' },
  { value: 'database_deleted', label: 'Database Deleted', category: 'Databases' },
];

export function CreateWebhookModal({ isOpen, onClose, onSubmit, editingWebhook }: CreateWebhookModalProps) {
  const [step, setStep] = useState<'type' | 'config'>('type');
  const [selectedType, setSelectedType] = useState<WebhookType | null>(null);
  const [name, setName] = useState('');
  const [webhookUrl, setWebhookUrl] = useState('');
  const [telegramBotToken, setTelegramBotToken] = useState('');
  const [telegramChatId, setTelegramChatId] = useState('');
  const [signingSecret, setSigningSecret] = useState('');
  const [selectedEvents, setSelectedEvents] = useState<WebhookEventType[]>([]);
  const [enabled, setEnabled] = useState(true);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Reset/populate form when editing
  useEffect(() => {
    if (editingWebhook) {
      setSelectedType(editingWebhook.type);
      setName(editingWebhook.name);
      setWebhookUrl(editingWebhook.webhook_url || '');
      setTelegramBotToken(editingWebhook.telegram_bot_token || '');
      setTelegramChatId(editingWebhook.telegram_chat_id || '');
      setSelectedEvents(editingWebhook.events);
      setEnabled(editingWebhook.enabled);
      setStep('config');
    } else {
      resetForm();
    }
  }, [editingWebhook, isOpen]);

  const resetForm = () => {
    setStep('type');
    setSelectedType(null);
    setName('');
    setWebhookUrl('');
    setTelegramBotToken('');
    setTelegramChatId('');
    setSigningSecret('');
    setSelectedEvents([]);
    setEnabled(true);
    setError(null);
  };

  const handleTypeSelect = (type: WebhookType) => {
    setSelectedType(type);
    setStep('config');
  };

  const handleEventToggle = (event: WebhookEventType) => {
    setSelectedEvents((prev) =>
      prev.includes(event)
        ? prev.filter((e) => e !== event)
        : [...prev, event]
    );
  };

  const handleSelectAllEvents = () => {
    if (selectedEvents.length === EVENT_OPTIONS.length) {
      setSelectedEvents([]);
    } else {
      setSelectedEvents(EVENT_OPTIONS.map((e) => e.value));
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    if (!selectedType || !name || selectedEvents.length === 0) {
      setError('Please fill in all required fields and select at least one event');
      return;
    }

    // Validate based on type
    if (selectedType === 'telegram') {
      if (!telegramBotToken || !telegramChatId) {
        setError('Telegram Bot Token and Chat ID are required');
        return;
      }
    } else if (!webhookUrl) {
      setError('Webhook URL is required');
      return;
    }

    setIsSubmitting(true);
    setError(null);

    try {
      await onSubmit({
        name,
        type: selectedType,
        webhook_url: selectedType !== 'telegram' ? webhookUrl : undefined,
        telegram_bot_token: selectedType === 'telegram' ? telegramBotToken : undefined,
        telegram_chat_id: selectedType === 'telegram' ? telegramChatId : undefined,
        signing_secret: selectedType === 'custom' ? signingSecret || undefined : undefined,
        events: selectedEvents,
        enabled,
      });
      resetForm();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save webhook');
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleClose = () => {
    resetForm();
    onClose();
  };

  const isEditing = !!editingWebhook;

  return (
    <Dialog open={isOpen} onOpenChange={(open) => { if (!open) handleClose(); }}>
      <DialogContent className="sm:max-w-lg max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>
            {isEditing ? 'Edit Webhook' : 'Create Webhook'}
          </DialogTitle>
          <DialogDescription>
            {isEditing
              ? 'Update your webhook configuration.'
              : 'Configure a new webhook to receive notifications.'}
          </DialogDescription>
        </DialogHeader>

        {error && (
          <div className="mb-4 p-3 bg-destructive/10 border border-destructive/30 rounded-lg text-destructive text-sm">
            {error}
          </div>
        )}

        {/* Step 1: Select Type */}
        {step === 'type' && !isEditing && (
          <div className="space-y-4">
            <p className="text-muted-foreground text-sm">
              Choose where you want to receive notifications
            </p>
            <div className="grid grid-cols-2 gap-3">
              {WEBHOOK_TYPES.map((type) => (
                <button
                  key={type.value}
                  type="button"
                  onClick={() => handleTypeSelect(type.value)}
                  className="p-4 border rounded-lg text-left transition-all hover:border-primary hover:bg-primary/10"
                >
                  <div className="flex items-center gap-3 mb-2">
                    {type.icon}
                  </div>
                  <span className="font-medium block">{type.label}</span>
                  <p className="text-xs text-muted-foreground mt-1">{type.description}</p>
                </button>
              ))}
            </div>
          </div>
        )}

        {/* Step 2: Configure */}
        {step === 'config' && selectedType && (
          <form onSubmit={handleSubmit} className="space-y-6">
            {/* Back button (only when creating) */}
            {!isEditing && (
              <button
                type="button"
                onClick={() => setStep('type')}
                className="text-sm text-muted-foreground hover:text-foreground flex items-center gap-1"
              >
                <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
                </svg>
                Back
              </button>
            )}

            {/* Selected type indicator */}
            <div className="flex items-center gap-3 p-3 bg-muted/50 rounded-lg">
              {WEBHOOK_TYPES.find((t) => t.value === selectedType)?.icon}
              <div>
                <p className="font-medium">
                  {WEBHOOK_TYPES.find((t) => t.value === selectedType)?.label}
                </p>
                <p className="text-xs text-muted-foreground">
                  {WEBHOOK_TYPES.find((t) => t.value === selectedType)?.description}
                </p>
              </div>
            </div>

            {/* Name */}
            <div className="space-y-2">
              <Label htmlFor="name">Webhook Name</Label>
              <Input
                id="name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g., Production Alerts"
                required
              />
            </div>

            {/* Type-specific fields */}
            {selectedType === 'telegram' ? (
              <>
                <div className="space-y-2">
                  <Label htmlFor="bot_token">Bot Token</Label>
                  <Input
                    id="bot_token"
                    type="password"
                    value={telegramBotToken}
                    onChange={(e) => setTelegramBotToken(e.target.value)}
                    placeholder="123456789:ABCdefGHIjklMNOpqrSTUVwxyz"
                    required
                  />
                  <p className="text-xs text-muted-foreground">
                    Get this from @BotFather on Telegram
                  </p>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="chat_id">Chat ID</Label>
                  <Input
                    id="chat_id"
                    value={telegramChatId}
                    onChange={(e) => setTelegramChatId(e.target.value)}
                    placeholder="-1001234567890 or @channelname"
                    required
                  />
                  <p className="text-xs text-muted-foreground">
                    Channel ID (with -100 prefix) or @username
                  </p>
                </div>
              </>
            ) : (
              <>
                <div className="space-y-2">
                  <Label htmlFor="webhook_url">
                    {selectedType === 'slack' && 'Slack Webhook URL'}
                    {selectedType === 'discord' && 'Discord Webhook URL'}
                    {selectedType === 'custom' && 'Webhook URL'}
                  </Label>
                  <Input
                    id="webhook_url"
                    type="url"
                    value={webhookUrl}
                    onChange={(e) => setWebhookUrl(e.target.value)}
                    placeholder={
                      selectedType === 'slack'
                        ? 'https://hooks.slack.com/services/...'
                        : selectedType === 'discord'
                        ? 'https://discord.com/api/webhooks/...'
                        : 'https://api.example.com/webhook'
                    }
                    required
                  />
                </div>
                {selectedType === 'custom' && (
                  <div className="space-y-2">
                    <Label htmlFor="signing_secret">Signing Secret (Optional)</Label>
                    <Input
                      id="signing_secret"
                      type="password"
                      value={signingSecret}
                      onChange={(e) => setSigningSecret(e.target.value)}
                      placeholder="Optional secret for HMAC signature"
                    />
                    <p className="text-xs text-muted-foreground">
                      Used to sign payloads with HMAC-SHA256. Check X-Enclii-Signature header.
                    </p>
                  </div>
                )}
              </>
            )}

            {/* Events */}
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <Label>Events to Subscribe</Label>
                <button
                  type="button"
                  onClick={handleSelectAllEvents}
                  className="text-xs text-primary hover:text-primary/80"
                >
                  {selectedEvents.length === EVENT_OPTIONS.length ? 'Deselect All' : 'Select All'}
                </button>
              </div>
              <div className="border rounded-lg divide-y">
                {['Deployments', 'Builds', 'Services', 'Databases'].map((category) => (
                  <div key={category} className="p-3">
                    <p className="text-xs font-medium text-muted-foreground mb-2">{category}</p>
                    <div className="space-y-2">
                      {EVENT_OPTIONS.filter((e) => e.category === category).map((event) => (
                        <label key={event.value} className="flex items-center gap-2 cursor-pointer">
                          <Checkbox
                            checked={selectedEvents.includes(event.value)}
                            onCheckedChange={() => handleEventToggle(event.value)}
                          />
                          <span className="text-sm">{event.label}</span>
                        </label>
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            </div>

            {/* Enabled toggle */}
            <label className="flex items-center gap-2 cursor-pointer">
              <Checkbox
                checked={enabled}
                onCheckedChange={(checked) => setEnabled(!!checked)}
              />
              <span className="text-sm">Enable this webhook</span>
            </label>

            {/* Actions */}
            <DialogFooter className="pt-4 border-t">
              <Button type="button" variant="outline" onClick={handleClose} disabled={isSubmitting}>
                Cancel
              </Button>
              <Button type="submit" disabled={isSubmitting || selectedEvents.length === 0}>
                {isSubmitting ? (
                  <>
                    <Spinner size="sm" className="mr-2" />
                    {isEditing ? 'Saving...' : 'Creating...'}
                  </>
                ) : (
                  isEditing ? 'Save Changes' : 'Create Webhook'
                )}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
