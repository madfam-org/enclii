'use client';

/**
 * Project addons page (P3.1 Sprint 1).
 *
 * Renders the managed-DB addons for the current project:
 *   - Table of addons with status badge, plan, created_at.
 *   - "Create addon" button → modal (name + engine + plan selector).
 *   - Row action: destroy (double-confirm modal).
 *
 * The backend is switchyard-api's /v1/projects/:slug/addons + /v1/addons/:id.
 * Plans catalog is fetched from /v1/addons/plans?engine=postgres so the
 * dropdown stays in sync with the server-side plan enum.
 *
 * Sprint 2 will extend with backup history, PITR, credential rotation.
 * Sprint 3 adds per-plan pricing and the billing hook.
 */

import { useCallback, useEffect, useState } from 'react';
import Link from 'next/link';
import { useParams } from 'next/navigation';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@enclii/ui-components/dialog";
import { apiDelete, apiGet, apiPost } from '@/lib/api';

// ---- Types ----

type AddonStatus = 'pending' | 'provisioning' | 'ready' | 'failed' | 'deleting' | 'deleted';

interface Addon {
  id: string;
  project_id: string;
  name: string;
  type: string;
  plan: string;
  status: AddonStatus;
  status_message?: string;
  host?: string;
  port?: number;
  database_name?: string;
  created_at: string;
  provisioned_at?: string | null;
}

interface Plan {
  code: string;
  engine: string;
  display_name: string;
  tier: string;
  storage_gb: number;
  cpu_request: string;
  memory_request: string;
  max_connections: number;
  ha_enabled: boolean;
  price_cents_month: number;
}

// ---- Status badge ----

function StatusBadge({ status }: { status: AddonStatus }) {
  const tone: Record<AddonStatus, string> = {
    pending: 'bg-slate-100 text-slate-700 border-slate-200',
    provisioning: 'bg-blue-50 text-blue-700 border-blue-200',
    ready: 'bg-green-50 text-green-700 border-green-200',
    failed: 'bg-red-50 text-red-700 border-red-200',
    deleting: 'bg-amber-50 text-amber-700 border-amber-200',
    deleted: 'bg-slate-100 text-slate-500 border-slate-200',
  };
  return (
    <span className={`inline-flex items-center rounded-md border px-2 py-0.5 text-xs font-medium ${tone[status] ?? tone.pending}`}>
      {status}
    </span>
  );
}

function formatDate(raw?: string | null) {
  if (!raw) return '—';
  try {
    return new Date(raw).toLocaleString();
  } catch {
    return raw;
  }
}

// ---- Page ----

export default function ProjectAddonsPage() {
  const params = useParams();
  const slug = (params?.slug as string) ?? '';

  const [addons, setAddons] = useState<Addon[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Create modal
  const [createOpen, setCreateOpen] = useState(false);
  const [createName, setCreateName] = useState('');
  const [createEngine, setCreateEngine] = useState('postgres');
  const [createPlan, setCreatePlan] = useState('standard-0');
  const [creating, setCreating] = useState(false);
  const [plans, setPlans] = useState<Plan[]>([]);

  // Destroy modal
  const [destroyTarget, setDestroyTarget] = useState<Addon | null>(null);
  const [destroyConfirm, setDestroyConfirm] = useState('');
  const [destroying, setDestroying] = useState(false);

  const loadAddons = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const resp = await apiGet<{ addons: Addon[]; count: number }>(
        `/v1/projects/${encodeURIComponent(slug)}/addons`,
      );
      setAddons(resp.addons ?? []);
    } catch (err: any) {
      setError(err?.message ?? 'Failed to load addons');
    } finally {
      setLoading(false);
    }
  }, [slug]);

  const loadPlans = useCallback(async (engine: string) => {
    try {
      const resp = await apiGet<{ plans: Plan[] }>(
        `/v1/addons/plans?engine=${encodeURIComponent(engine)}`,
      );
      setPlans(resp.plans ?? []);
    } catch {
      // Plans are non-critical for page load; the modal surfaces the error.
      setPlans([]);
    }
  }, []);

  useEffect(() => {
    if (slug) {
      void loadAddons();
    }
  }, [slug, loadAddons]);

  // Auto-refresh while any addon is non-terminal (provisioning/deleting) so
  // status badges advance without a manual reload.
  useEffect(() => {
    const hasInflight = addons.some(
      (a) => a.status === 'pending' || a.status === 'provisioning' || a.status === 'deleting',
    );
    if (!hasInflight) return;
    const t = setInterval(() => {
      void loadAddons();
    }, 5000);
    return () => clearInterval(t);
  }, [addons, loadAddons]);

  useEffect(() => {
    if (createOpen) {
      void loadPlans(createEngine);
    }
  }, [createOpen, createEngine, loadPlans]);

  async function handleCreate() {
    if (!createName.trim() || !createPlan) return;
    setCreating(true);
    try {
      await apiPost(`/v1/projects/${encodeURIComponent(slug)}/addons`, {
        name: createName.trim(),
        type: createEngine,
        plan: createPlan,
      });
      setCreateOpen(false);
      setCreateName('');
      await loadAddons();
    } catch (err: any) {
      setError(err?.message ?? 'Failed to create addon');
    } finally {
      setCreating(false);
    }
  }

  async function handleDestroy() {
    if (!destroyTarget) return;
    // Double-confirm: user must re-type the addon name.
    if (destroyConfirm !== destroyTarget.name) return;
    setDestroying(true);
    try {
      await apiDelete(`/v1/addons/${destroyTarget.id}`);
      setDestroyTarget(null);
      setDestroyConfirm('');
      await loadAddons();
    } catch (err: any) {
      setError(err?.message ?? 'Failed to destroy addon');
    } finally {
      setDestroying(false);
    }
  }

  return (
    <div className="p-6 max-w-6xl mx-auto">
      <div className="flex items-center justify-between mb-6">
        <div>
          <Link href={`/projects/${slug}`} className="text-sm text-slate-500 hover:underline">
            ← {slug}
          </Link>
          <h1 className="text-2xl font-semibold mt-1">Database Addons</h1>
          <p className="text-sm text-slate-500 mt-1">
            Managed Postgres instances scoped to this project. Credentials inject as{' '}
            <code className="bg-slate-100 px-1 rounded">DATABASE_URL</code> into any bound service.
          </p>
        </div>
        <button
          onClick={() => setCreateOpen(true)}
          className="inline-flex items-center px-4 py-2 bg-slate-900 text-white rounded-md text-sm font-medium hover:bg-slate-800"
        >
          + Create addon
        </button>
      </div>

      {error && (
        <div className="mb-4 border border-red-200 bg-red-50 text-red-700 rounded-md px-4 py-2 text-sm">
          {error}
        </div>
      )}

      <div className="border border-slate-200 rounded-lg overflow-hidden">
        <table className="w-full">
          <thead className="bg-slate-50">
            <tr className="text-left text-xs uppercase tracking-wide text-slate-500">
              <th className="px-4 py-3">Name</th>
              <th className="px-4 py-3">Engine</th>
              <th className="px-4 py-3">Plan</th>
              <th className="px-4 py-3">Status</th>
              <th className="px-4 py-3">Created</th>
              <th className="px-4 py-3 text-right">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100">
            {loading && (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center text-slate-500 text-sm">
                  Loading…
                </td>
              </tr>
            )}
            {!loading && addons.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-12 text-center text-slate-500 text-sm">
                  No addons yet. Click <strong>Create addon</strong> to provision one.
                </td>
              </tr>
            )}
            {!loading &&
              addons.map((a) => (
                <tr key={a.id} className="text-sm">
                  <td className="px-4 py-3 font-medium text-slate-900">
                    <Link href={`/addons/${a.id}`} className="hover:underline">
                      {a.name}
                    </Link>
                  </td>
                  <td className="px-4 py-3 text-slate-600">{a.type}</td>
                  <td className="px-4 py-3 text-slate-600">
                    <code className="bg-slate-100 rounded px-1">{a.plan}</code>
                  </td>
                  <td className="px-4 py-3">
                    <StatusBadge status={a.status} />
                    {a.status_message && a.status !== 'ready' && (
                      <div className="text-xs text-slate-400 mt-1">{a.status_message}</div>
                    )}
                  </td>
                  <td className="px-4 py-3 text-slate-500">{formatDate(a.created_at)}</td>
                  <td className="px-4 py-3 text-right">
                    <button
                      onClick={() => setDestroyTarget(a)}
                      disabled={a.status === 'deleting' || a.status === 'deleted'}
                      className="text-red-600 hover:text-red-800 text-xs font-medium disabled:text-slate-300 disabled:cursor-not-allowed"
                    >
                      Destroy
                    </button>
                  </td>
                </tr>
              ))}
          </tbody>
        </table>
      </div>

      {/* ---- Create modal ---- */}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Create database addon</DialogTitle>
            <DialogDescription>
              Provisions a fresh isolated Postgres cluster scoped to{' '}
              <strong>{slug}</strong>. Typically takes 1–2 minutes.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4 py-2">
            <div>
              <label className="text-sm font-medium block mb-1">Name</label>
              <input
                type="text"
                value={createName}
                onChange={(e) => setCreateName(e.target.value)}
                className="w-full border border-slate-300 rounded-md px-3 py-2 text-sm"
                placeholder="my-db"
              />
              <p className="text-xs text-slate-500 mt-1">
                Must be unique within the project.
              </p>
            </div>

            <div>
              <label className="text-sm font-medium block mb-1">Engine</label>
              <select
                value={createEngine}
                onChange={(e) => setCreateEngine(e.target.value)}
                className="w-full border border-slate-300 rounded-md px-3 py-2 text-sm"
              >
                <option value="postgres">Postgres</option>
                {/* Redis / MySQL are scaffolded but not GA in Sprint 1. */}
              </select>
            </div>

            <div>
              <label className="text-sm font-medium block mb-1">Plan</label>
              <select
                value={createPlan}
                onChange={(e) => setCreatePlan(e.target.value)}
                className="w-full border border-slate-300 rounded-md px-3 py-2 text-sm"
              >
                {plans.length === 0 && <option value="standard-0">standard-0 (default)</option>}
                {plans.map((p) => (
                  <option key={p.code} value={p.code}>
                    {p.display_name} ({p.cpu_request} CPU / {p.memory_request} / {p.max_connections} conns)
                  </option>
                ))}
              </select>
              <p className="text-xs text-slate-500 mt-1">
                Resource preset is determined by the plan; larger plans coming in Sprint 2.
              </p>
            </div>
          </div>

          <DialogFooter>
            <button
              onClick={() => setCreateOpen(false)}
              className="px-4 py-2 text-sm font-medium text-slate-700 hover:text-slate-900"
            >
              Cancel
            </button>
            <button
              onClick={handleCreate}
              disabled={creating || !createName.trim()}
              className="px-4 py-2 bg-slate-900 text-white rounded-md text-sm font-medium disabled:opacity-50"
            >
              {creating ? 'Creating…' : 'Create'}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* ---- Destroy modal ---- */}
      <Dialog
        open={!!destroyTarget}
        onOpenChange={(open) => {
          if (!open) {
            setDestroyTarget(null);
            setDestroyConfirm('');
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Destroy addon</DialogTitle>
            <DialogDescription>
              This will permanently delete the Postgres cluster and its data.{' '}
              <strong>There is no rollback.</strong>{' '}
              Type the addon name to confirm.
            </DialogDescription>
          </DialogHeader>

          <div className="py-2 space-y-2">
            <div className="text-sm">
              Name: <code className="bg-slate-100 px-1 rounded">{destroyTarget?.name}</code>
            </div>
            <input
              type="text"
              value={destroyConfirm}
              onChange={(e) => setDestroyConfirm(e.target.value)}
              className="w-full border border-slate-300 rounded-md px-3 py-2 text-sm"
              placeholder="Re-type the addon name"
            />
          </div>

          <DialogFooter>
            <button
              onClick={() => {
                setDestroyTarget(null);
                setDestroyConfirm('');
              }}
              className="px-4 py-2 text-sm font-medium text-slate-700 hover:text-slate-900"
            >
              Cancel
            </button>
            <button
              onClick={handleDestroy}
              disabled={destroying || destroyConfirm !== destroyTarget?.name}
              className="px-4 py-2 bg-red-600 text-white rounded-md text-sm font-medium disabled:opacity-50"
            >
              {destroying ? 'Destroying…' : 'Destroy'}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
