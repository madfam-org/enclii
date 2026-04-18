'use client';

/**
 * Project exports page (P3.6).
 *
 * Lets project admins request a tenant data export (manifests +
 * pg_dump + blob manifest + audit timeline + secret references with
 * values redacted) and download ready exports.
 *
 * Production requests land in status=pending and require a second
 * project admin's approval before the pipeline runs — the page
 * surfaces both the requester-side view ("awaiting approval") and
 * the approver-side prompt.
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { useParams } from 'next/navigation';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { apiDelete, apiGet, apiPost } from '@/lib/api';

type ExportStatus =
  | 'pending'
  | 'running'
  | 'ready'
  | 'failed'
  | 'expired'
  | 'deleted';

interface TenantExport {
  id: string;
  project_id: string;
  status: ExportStatus;
  requested_by: string;
  requested_at: string;
  approved_by?: string | null;
  approved_at?: string | null;
  tarball_r2_key?: string | null;
  tarball_size_bytes?: number | null;
  sha256?: string | null;
  part_count: number;
  error_message?: string | null;
  expires_at?: string | null;
  created_at: string;
  updated_at: string;
}

interface ExportDownloadResponse {
  export: TenantExport;
  download_url?: string;
  expires_in_seconds?: number;
}

export default function TenantExportsPage() {
  const params = useParams<{ slug: string }>();
  const slug = params?.slug ?? '';

  const [exports, setExports] = useState<TenantExport[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    if (!slug) return;
    setLoading(true);
    setError(null);
    try {
      const resp = await apiGet<{ exports: TenantExport[] }>(
        `/v1/projects/${slug}/exports`,
      );
      setExports(resp.exports ?? []);
    } catch (err: unknown) {
      setError(errMessage(err));
    } finally {
      setLoading(false);
    }
  }, [slug]);

  useEffect(() => {
    void refresh();
    // Poll while anything is in-flight (pending/running).
    const t = setInterval(() => {
      if (exports.some((e) => e.status === 'pending' || e.status === 'running')) {
        void refresh();
      }
    }, 15_000);
    return () => clearInterval(t);
  }, [refresh, exports]);

  const startExport = useCallback(async () => {
    setCreating(true);
    setError(null);
    try {
      await apiPost(`/v1/projects/${slug}/exports`, {});
      setConfirmOpen(false);
      await refresh();
    } catch (err: unknown) {
      setError(errMessage(err));
    } finally {
      setCreating(false);
    }
  }, [slug, refresh]);

  const downloadExport = useCallback(async (id: string) => {
    setError(null);
    try {
      const resp = await apiGet<ExportDownloadResponse>(`/v1/exports/${id}`);
      if (!resp.download_url) {
        setError('Export is not ready for download yet.');
        return;
      }
      // Open a fresh pre-signed URL in a new tab so we never cache it
      // on the client side. URL expires in ~15 minutes.
      window.open(resp.download_url, '_blank', 'noopener');
    } catch (err: unknown) {
      setError(errMessage(err));
    }
  }, []);

  const approveExport = useCallback(
    async (id: string) => {
      setError(null);
      try {
        await apiPost(`/v1/exports/${id}/approve`, {});
        await refresh();
      } catch (err: unknown) {
        setError(errMessage(err));
      }
    },
    [refresh],
  );

  const deleteExport = useCallback(
    async (id: string) => {
      if (!confirm('Purge this export from R2 immediately?')) return;
      setError(null);
      try {
        await apiDelete(`/v1/exports/${id}`);
        await refresh();
      } catch (err: unknown) {
        setError(errMessage(err));
      }
    },
    [refresh],
  );

  const hasPending = useMemo(
    () => exports.some((e) => e.status === 'pending'),
    [exports],
  );

  return (
    <div className="p-6 space-y-6">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Tenant exports</h1>
          <p className="text-muted-foreground text-sm mt-1 max-w-2xl">
            Download everything Enclii holds about this project: K8s
            manifests, database dumps, blob inventories, audit timeline,
            and secret references. Secret <strong>values</strong> are
            never included — rotate post-leave via Vault.
          </p>
        </div>
        <Button onClick={() => setConfirmOpen(true)} disabled={creating}>
          New export
        </Button>
      </header>

      {error && (
        <div className="rounded-md border border-red-300 bg-red-50 p-3 text-sm text-red-900">
          {error}
        </div>
      )}

      {hasPending && (
        <div className="rounded-md border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900">
          At least one export is awaiting a second project admin&apos;s
          approval before the pipeline runs.
        </div>
      )}

      <div className="rounded-lg border">
        <table className="w-full text-sm">
          <thead className="bg-muted/50 text-left">
            <tr>
              <th className="p-3 font-medium">Status</th>
              <th className="p-3 font-medium">Requested</th>
              <th className="p-3 font-medium">Requester</th>
              <th className="p-3 font-medium">Size</th>
              <th className="p-3 font-medium">Expires</th>
              <th className="p-3 font-medium">sha256</th>
              <th className="p-3 font-medium text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            {loading && (
              <tr>
                <td colSpan={7} className="p-6 text-center text-muted-foreground">
                  Loading…
                </td>
              </tr>
            )}
            {!loading && exports.length === 0 && (
              <tr>
                <td colSpan={7} className="p-6 text-center text-muted-foreground">
                  No exports yet. Click &quot;New export&quot; to request one.
                </td>
              </tr>
            )}
            {exports.map((row) => (
              <tr key={row.id} className="border-t">
                <td className="p-3">
                  <StatusBadge status={row.status} />
                </td>
                <td className="p-3">{fmtDate(row.requested_at)}</td>
                <td className="p-3">{row.requested_by}</td>
                <td className="p-3">{fmtSize(row.tarball_size_bytes)}</td>
                <td className="p-3">
                  {row.expires_at ? fmtDate(row.expires_at) : '—'}
                </td>
                <td className="p-3 font-mono text-xs text-muted-foreground">
                  {row.sha256 ? row.sha256.slice(0, 20) + '…' : '—'}
                </td>
                <td className="p-3 text-right space-x-2">
                  {row.status === 'pending' && (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => approveExport(row.id)}
                    >
                      Approve
                    </Button>
                  )}
                  {row.status === 'ready' && (
                    <Button size="sm" onClick={() => downloadExport(row.id)}>
                      Download
                    </Button>
                  )}
                  {(row.status === 'ready' || row.status === 'failed') && (
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => deleteExport(row.id)}
                    >
                      Delete
                    </Button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Request a new tenant export</DialogTitle>
            <DialogDescription>
              We&apos;ll assemble K8s manifests, pg_dump of each bound
              database addon, R2 blob inventory, secret references
              (names only), and the project audit timeline.
              <br />
              <br />
              In production this request enters an approval queue — a
              second project admin must approve before the pipeline
              runs. Typical completion: within 24 hours for projects
              under 5&nbsp;GB.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmOpen(false)}>
              Cancel
            </Button>
            <Button onClick={startExport} disabled={creating}>
              {creating ? 'Requesting…' : 'Request export'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function StatusBadge({ status }: { status: ExportStatus }) {
  const color: Record<ExportStatus, string> = {
    pending: 'bg-amber-100 text-amber-900',
    running: 'bg-blue-100 text-blue-900',
    ready: 'bg-emerald-100 text-emerald-900',
    failed: 'bg-red-100 text-red-900',
    expired: 'bg-gray-100 text-gray-800',
    deleted: 'bg-gray-100 text-gray-500',
  };
  return (
    <span
      className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${color[status]}`}
    >
      {status}
    </span>
  );
}

function fmtDate(iso: string): string {
  try {
    return new Date(iso).toLocaleString();
  } catch {
    return iso;
  }
}

function fmtSize(n?: number | null): string {
  if (!n || n === 0) return '—';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i += 1;
  }
  return `${v.toFixed(1)} ${units[i]}`;
}

function errMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}
