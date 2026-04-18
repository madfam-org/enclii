'use client';

import { useState, useEffect, useCallback } from 'react';
import Link from 'next/link';
import { useParams } from 'next/navigation';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { apiGet, apiPost, apiPatch, apiDelete } from '@/lib/api';

// P2.3: outbound lifecycle webhook subscriptions.
// Mirrors the shape of packages/sdk-go/pkg/types/outbound_webhooks.go.
interface Subscription {
  id: string;
  project_id: string;
  name: string;
  url: string;
  secret_sha256_prefix: string;
  event_types: string[];
  active: boolean;
  created_by: string;
  created_at: string;
  updated_at: string;
  last_success_at?: string;
  last_failure_at?: string;
  consecutive_failures: number;
  auto_disabled_at?: string;
}

interface CreateResponse {
  subscription: Subscription;
  signing_secret: string;
  note: string;
}

interface Delivery {
  id: string;
  subscription_id: string;
  event_id: string;
  event_type: string;
  attempt_number: number;
  status: string;
  http_status?: number;
  response_snippet?: string;
  error_message?: string;
  attempted_at?: string;
  delivered_at?: string;
  duration_ms?: number;
  next_retry_at?: string;
  created_at: string;
}

interface EventTypeOption {
  type: string;
  description: string;
}

// ---------------------------------------------------------------------------
// Page component
// ---------------------------------------------------------------------------

export default function LifecycleWebhooksPage() {
  const params = useParams();
  const slug = params?.slug as string;

  const [subscriptions, setSubscriptions] = useState<Subscription[]>([]);
  const [eventTypes, setEventTypes] = useState<EventTypeOption[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [createdSecret, setCreatedSecret] = useState<CreateResponse | null>(null);
  const [activeDeliveries, setActiveDeliveries] = useState<{ sub: Subscription; list: Delivery[] } | null>(null);

  const fetchSubs = useCallback(async () => {
    try {
      setError(null);
      const data = await apiGet<{ subscriptions: Subscription[] }>(
        `/v1/projects/${slug}/lifecycle-webhooks`,
      );
      setSubscriptions(data.subscriptions || []);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load subscriptions');
    } finally {
      setLoading(false);
    }
  }, [slug]);

  const fetchEventTypes = useCallback(async () => {
    try {
      const data = await apiGet<{ event_types: EventTypeOption[] }>(
        `/v1/lifecycle-webhooks/event-types`,
      );
      setEventTypes(data.event_types || []);
    } catch (err) {
      console.error('Failed to load event types', err);
    }
  }, []);

  useEffect(() => {
    if (slug) {
      fetchSubs();
      fetchEventTypes();
    }
  }, [slug, fetchSubs, fetchEventTypes]);

  const handleCreate = async (data: { name: string; url: string; event_types: string[] }) => {
    try {
      const resp = await apiPost<CreateResponse>(
        `/v1/projects/${slug}/lifecycle-webhooks`,
        data,
      );
      setCreatedSecret(resp);
      setShowCreate(false);
      await fetchSubs();
    } catch (err) {
      toast.error('Failed to create webhook: ' + (err instanceof Error ? err.message : 'error'));
    }
  };

  const handleToggleActive = async (sub: Subscription) => {
    try {
      await apiPatch(`/v1/lifecycle-webhooks/${sub.id}`, { active: !sub.active });
      await fetchSubs();
    } catch (err) {
      toast.error('Toggle failed');
    }
  };

  const handleDelete = async (sub: Subscription) => {
    if (!confirm(`Delete webhook "${sub.name}"? This cannot be undone.`)) return;
    try {
      await apiDelete(`/v1/lifecycle-webhooks/${sub.id}`);
      await fetchSubs();
      toast.success('Webhook deleted');
    } catch (err) {
      toast.error('Delete failed');
    }
  };

  const handleRotate = async (sub: Subscription) => {
    if (!confirm(`Rotate signing secret for "${sub.name}"? Existing receivers will reject deliveries until they\'re updated with the new secret.`)) return;
    try {
      const resp = await apiPost<CreateResponse>(`/v1/lifecycle-webhooks/${sub.id}/rotate-secret`, {});
      setCreatedSecret(resp);
      await fetchSubs();
    } catch (err) {
      toast.error('Rotation failed');
    }
  };

  const handleTest = async (sub: Subscription) => {
    try {
      await apiPost(`/v1/lifecycle-webhooks/${sub.id}/test`, {});
      toast.success('Test ping enqueued — open deliveries to see the result');
    } catch (err) {
      toast.error('Test failed');
    }
  };

  const openDeliveries = async (sub: Subscription) => {
    try {
      const data = await apiGet<{ deliveries: Delivery[] }>(
        `/v1/lifecycle-webhooks/${sub.id}/deliveries?limit=50`,
      );
      setActiveDeliveries({ sub, list: data.deliveries || [] });
    } catch (err) {
      toast.error('Failed to load deliveries');
    }
  };

  const handleRedeliver = async (sub: Subscription, deliveryId: string) => {
    try {
      await apiPost(`/v1/lifecycle-webhooks/${sub.id}/deliveries/${deliveryId}/redeliver`, {});
      toast.success('Redelivery enqueued');
      await openDeliveries(sub);
    } catch (err) {
      toast.error('Redelivery failed');
    }
  };

  if (loading) {
    return (
      <div className="max-w-5xl mx-auto py-6 px-4">
        <div className="animate-pulse space-y-4">
          <div className="h-8 bg-muted rounded w-1/3" />
          <div className="h-48 bg-muted rounded-lg" />
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-5xl mx-auto py-6 px-4">
      {/* Breadcrumb */}
      <nav className="flex mb-6 text-sm" aria-label="Breadcrumb">
        <Link href="/projects" className="text-muted-foreground hover:text-foreground">Projects</Link>
        <span className="mx-2 text-muted-foreground/50">/</span>
        <Link href={`/projects/${slug}`} className="text-muted-foreground hover:text-foreground">{slug}</Link>
        <span className="mx-2 text-muted-foreground/50">/</span>
        <span className="text-muted-foreground">Lifecycle Webhooks</span>
      </nav>

      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold text-foreground">Lifecycle Webhooks</h1>
          <p className="text-muted-foreground mt-1 text-sm">
            Send signed <code className="text-xs">deploy / rollback / scale</code> events to your own HTTPS endpoints.
          </p>
        </div>
        <Button onClick={() => setShowCreate(true)}>+ Add Webhook</Button>
      </div>

      {error && (
        <Card className="mb-6 border-status-error/30">
          <CardContent className="py-4 text-status-error">{error}</CardContent>
        </Card>
      )}

      {subscriptions.length === 0 ? (
        <Card>
          <CardContent className="py-12 text-center text-muted-foreground">
            No lifecycle webhooks configured yet.
            <div className="mt-4">
              <Button onClick={() => setShowCreate(true)}>Create your first</Button>
            </div>
          </CardContent>
        </Card>
      ) : (
        <div className="space-y-4">
          {subscriptions.map((sub) => (
            <Card key={sub.id}>
              <CardContent className="py-4">
                <div className="flex items-start justify-between gap-4">
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <h3 className="font-semibold truncate">{sub.name}</h3>
                      {sub.active ? (
                        <Badge variant="default">active</Badge>
                      ) : (
                        <Badge variant="secondary">paused</Badge>
                      )}
                      {sub.auto_disabled_at && <Badge variant="destructive">auto-disabled</Badge>}
                    </div>
                    <p className="text-xs text-muted-foreground mt-1 truncate">{sub.url}</p>
                    <div className="flex flex-wrap gap-1 mt-2">
                      {(sub.event_types.length === 0 ? ['all events'] : sub.event_types).map((e) => (
                        <Badge key={e} variant="outline" className="text-xs">{e}</Badge>
                      ))}
                    </div>
                    <div className="text-xs text-muted-foreground mt-2 space-x-3">
                      <span>secret: <code>{sub.secret_sha256_prefix}…</code></span>
                      {sub.last_success_at && <span>last OK: {new Date(sub.last_success_at).toLocaleString()}</span>}
                      {sub.consecutive_failures > 0 && (
                        <span className="text-status-error">failures: {sub.consecutive_failures}</span>
                      )}
                    </div>
                  </div>
                  <div className="flex flex-col gap-2 shrink-0">
                    <Button size="sm" variant="outline" onClick={() => openDeliveries(sub)}>Deliveries</Button>
                    <Button size="sm" variant="outline" onClick={() => handleTest(sub)}>Test</Button>
                    <Button size="sm" variant="outline" onClick={() => handleToggleActive(sub)}>
                      {sub.active ? 'Pause' : 'Resume'}
                    </Button>
                    <Button size="sm" variant="outline" onClick={() => handleRotate(sub)}>Rotate secret</Button>
                    <Button size="sm" variant="destructive" onClick={() => handleDelete(sub)}>Delete</Button>
                  </div>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      {showCreate && (
        <CreateModal
          eventTypes={eventTypes}
          onCancel={() => setShowCreate(false)}
          onSubmit={handleCreate}
        />
      )}

      {createdSecret && (
        <SecretRevealModal
          response={createdSecret}
          onClose={() => setCreatedSecret(null)}
        />
      )}

      {activeDeliveries && (
        <DeliveriesDrawer
          subscription={activeDeliveries.sub}
          deliveries={activeDeliveries.list}
          onRedeliver={(id) => handleRedeliver(activeDeliveries.sub, id)}
          onClose={() => setActiveDeliveries(null)}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Modals — kept inline to avoid new component files for this one-pager.
// Split out if this grows past a few hundred lines.
// ---------------------------------------------------------------------------

function CreateModal(props: {
  eventTypes: EventTypeOption[];
  onCancel: () => void;
  onSubmit: (data: { name: string; url: string; event_types: string[] }) => void;
}) {
  const [name, setName] = useState('');
  const [url, setUrl] = useState('https://');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [subscribeAll, setSubscribeAll] = useState(true);

  const toggle = (t: string) => {
    const next = new Set(selected);
    if (next.has(t)) {
      next.delete(t);
    } else {
      next.add(t);
    }
    setSelected(next);
  };

  const submit = () => {
    if (!url.startsWith('https://')) {
      toast.error('URL must start with https://');
      return;
    }
    props.onSubmit({
      name: name || new URL(url).host,
      url,
      event_types: subscribeAll ? [] : Array.from(selected),
    });
  };

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <Card className="w-full max-w-lg mx-4">
        <CardContent className="py-6">
          <h2 className="text-lg font-bold mb-4">New lifecycle webhook</h2>
          <label className="block text-sm mb-1">Friendly name</label>
          <input
            className="w-full px-3 py-2 rounded border bg-background mb-4"
            placeholder="e.g. PagerDuty integration"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <label className="block text-sm mb-1">HTTPS URL</label>
          <input
            className="w-full px-3 py-2 rounded border bg-background mb-4 font-mono text-sm"
            placeholder="https://hooks.example.com/enclii"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
          />
          <label className="flex items-center gap-2 text-sm mb-3">
            <input
              type="checkbox"
              checked={subscribeAll}
              onChange={(e) => setSubscribeAll(e.target.checked)}
            />
            Subscribe to all events
          </label>
          {!subscribeAll && (
            <div className="border rounded p-3 mb-4 max-h-48 overflow-auto">
              {props.eventTypes.map((et) => (
                <label key={et.type} className="flex items-start gap-2 py-1 text-sm">
                  <input
                    type="checkbox"
                    className="mt-1"
                    checked={selected.has(et.type)}
                    onChange={() => toggle(et.type)}
                  />
                  <div>
                    <code className="text-xs">{et.type}</code>
                    <p className="text-muted-foreground text-xs">{et.description}</p>
                  </div>
                </label>
              ))}
            </div>
          )}
          <div className="flex gap-2 justify-end">
            <Button variant="outline" onClick={props.onCancel}>Cancel</Button>
            <Button onClick={submit}>Create</Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

function SecretRevealModal(props: { response: CreateResponse; onClose: () => void }) {
  const [ack, setAck] = useState(false);
  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <Card className="w-full max-w-lg mx-4">
        <CardContent className="py-6">
          <h2 className="text-lg font-bold mb-2">Signing secret</h2>
          <p className="text-sm text-muted-foreground mb-3">{props.response.note}</p>
          <div className="border rounded p-3 bg-muted font-mono text-sm break-all mb-3">
            {props.response.signing_secret}
          </div>
          <div className="flex gap-2 mb-4">
            <Button
              size="sm"
              variant="outline"
              onClick={() => {
                navigator.clipboard.writeText(props.response.signing_secret);
                toast.success('Copied to clipboard');
              }}
            >
              Copy
            </Button>
          </div>
          <label className="flex items-center gap-2 text-sm mb-4">
            <input type="checkbox" checked={ack} onChange={(e) => setAck(e.target.checked)} />
            I&apos;ve saved this secret somewhere safe.
          </label>
          <div className="flex justify-end">
            <Button disabled={!ack} onClick={props.onClose}>Done</Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

function DeliveriesDrawer(props: {
  subscription: Subscription;
  deliveries: Delivery[];
  onRedeliver: (id: string) => void;
  onClose: () => void;
}) {
  const successRate = (() => {
    if (props.deliveries.length === 0) return '—';
    const ok = props.deliveries.filter((d) => d.status === 'delivered').length;
    return `${((ok / props.deliveries.length) * 100).toFixed(0)}%`;
  })();

  return (
    <div className="fixed inset-0 bg-black/50 flex items-end md:items-center justify-center z-50">
      <Card className="w-full max-w-3xl mx-4 max-h-[85vh] flex flex-col">
        <CardContent className="py-6 flex-1 overflow-auto">
          <div className="flex items-center justify-between mb-4">
            <div>
              <h2 className="text-lg font-bold">Deliveries — {props.subscription.name}</h2>
              <p className="text-xs text-muted-foreground">
                Success rate (last {props.deliveries.length}): {successRate}
              </p>
            </div>
            <Button size="sm" variant="outline" onClick={props.onClose}>Close</Button>
          </div>
          {props.deliveries.length === 0 ? (
            <p className="text-sm text-muted-foreground">No deliveries yet.</p>
          ) : (
            <table className="w-full text-xs">
              <thead>
                <tr className="text-left text-muted-foreground">
                  <th className="py-1">Created</th>
                  <th>Event</th>
                  <th>Attempt</th>
                  <th>Status</th>
                  <th>HTTP</th>
                  <th>Duration</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {props.deliveries.map((d) => (
                  <tr key={d.id} className="border-t">
                    <td className="py-1">{new Date(d.created_at).toLocaleString()}</td>
                    <td><code>{d.event_type}</code></td>
                    <td>{d.attempt_number}</td>
                    <td>
                      <Badge variant={d.status === 'delivered' ? 'default' : d.status === 'dlq' ? 'destructive' : 'secondary'}>
                        {d.status}
                      </Badge>
                    </td>
                    <td>{d.http_status ?? '—'}</td>
                    <td>{d.duration_ms ? `${d.duration_ms} ms` : '—'}</td>
                    <td>
                      {(d.status === 'failed' || d.status === 'dlq') && (
                        <Button size="sm" variant="ghost" onClick={() => props.onRedeliver(d.id)}>
                          Redeliver
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
