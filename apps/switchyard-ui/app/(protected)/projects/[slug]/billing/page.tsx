'use client';

/**
 * Project billing page (P2.2).
 *
 * Renders:
 *   - Hero spend bar vs. the active budget, with threshold markers
 *   - Per-service cost breakdown (top drivers)
 *   - Daily spend time-series (14-day default)
 *   - Budget card with create / edit modals
 *   - Alert history table
 *   - Throttle banner when deploys are blocked in non-production
 *
 * Data comes from the Switchyard /billing/* proxy endpoints, which forward
 * to Waybill. Projects without a budget see a CTA to create one.
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
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
import { apiDelete, apiGet, apiPatch, apiPost } from '@/lib/api';
import { cn } from '@/lib/utils';

// ---- Types ----

type Period = 'monthly' | 'weekly' | 'quarterly';

interface Budget {
  id: string;
  project_id: string;
  amount_cents: number;
  currency: string;
  period: Period;
  alert_thresholds: number[];
  hard_throttle: boolean;
  created_at: string;
  updated_at: string;
}

interface CostResponse {
  project_id: string;
  period_start: string;
  period_end: string;
  total_cents: number;
  group_by: string;
  series?: Array<{ bucket: string; cost_cents: number; by_metric?: Record<string, number> }>;
  breakdown?: Array<{ key: string; cost_cents: number }>;
}

interface AlertEvent {
  id: string;
  budget_id: string;
  project_id: string;
  period_start: string;
  period_end: string;
  threshold: number;
  actual_cents: number;
  budget_cents: number;
  dispatched_at?: string | null;
  dispatch_attempts: number;
  last_error?: string;
  created_at: string;
}

interface Throttle {
  id: string;
  project_id: string;
  reason: string;
  env_scope: string;
  activated_at: string;
  cleared_at?: string | null;
}

// ---- Helpers ----

function formatCurrencyCents(cents: number, currency = 'USD'): string {
  return new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency,
  }).format(cents / 100);
}

function percentConsumed(actualCents: number, budgetCents: number): number {
  if (budgetCents <= 0) return 0;
  return Math.min(Math.round((actualCents * 100) / budgetCents), 300);
}

function barColor(pct: number): string {
  if (pct >= 100) return 'bg-status-error';
  if (pct >= 80) return 'bg-status-warning';
  return 'bg-status-success';
}

// ---- Page ----

export default function ProjectBillingPage() {
  const params = useParams();
  const slug = (params?.slug as string) ?? '';

  const [period, setPeriod] = useState<'14d' | '30d' | '90d' | '1y'>('30d');
  const [cost, setCost] = useState<CostResponse | null>(null);
  const [series, setSeries] = useState<CostResponse | null>(null);
  const [budgets, setBudgets] = useState<Budget[]>([]);
  const [alerts, setAlerts] = useState<AlertEvent[]>([]);
  const [throttles, setThrottles] = useState<Throttle[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [showBudgetModal, setShowBudgetModal] = useState(false);
  const [editBudget, setEditBudget] = useState<Budget | null>(null);

  const refresh = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const [costRes, seriesRes, budgetsRes, alertsRes, throttleRes] = await Promise.all([
        apiGet<CostResponse>(`/v1/projects/${slug}/billing/cost?period=${period}&group_by=service`),
        apiGet<CostResponse>(`/v1/projects/${slug}/billing/cost?period=${period}&group_by=day`),
        apiGet<{ budgets: Budget[] }>(`/v1/projects/${slug}/billing/budgets`),
        apiGet<{ alerts: AlertEvent[] }>(`/v1/projects/${slug}/billing/budgets/alerts`),
        apiGet<{ throttles: Throttle[] }>(`/v1/projects/${slug}/billing/throttles`),
      ]);
      setCost(costRes);
      setSeries(seriesRes);
      setBudgets(budgetsRes.budgets ?? []);
      setAlerts(alertsRes.alerts ?? []);
      setThrottles(throttleRes.throttles ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load billing data');
    } finally {
      setLoading(false);
    }
  }, [slug, period]);

  useEffect(() => {
    if (slug) {
      void refresh();
    }
  }, [slug, refresh]);

  const primaryBudget = budgets[0] ?? null;
  const pct = primaryBudget
    ? percentConsumed(cost?.total_cents ?? 0, primaryBudget.amount_cents)
    : 0;

  const activeThrottle = throttles.find((t) => !t.cleared_at) ?? null;

  const maxSeries = useMemo(() => {
    const points = series?.series ?? [];
    return points.reduce((m, p) => Math.max(m, p.cost_cents), 1);
  }, [series]);

  if (loading && !cost) {
    return (
      <div className="max-w-7xl mx-auto py-6 sm:px-6 lg:px-8">
        <div className="animate-pulse space-y-4">
          <div className="h-8 bg-muted rounded w-1/3"></div>
          <div className="h-40 bg-muted rounded"></div>
          <div className="h-64 bg-muted rounded"></div>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="max-w-7xl mx-auto py-6 sm:px-6 lg:px-8">
        <div className="bg-status-error-muted border border-status-error/30 rounded-md p-4">
          <h3 className="text-sm font-medium text-status-error-foreground">
            Error loading billing data
          </h3>
          <p className="mt-2 text-sm text-status-error-foreground">{error}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-7xl mx-auto py-6 sm:px-6 lg:px-8">
      <nav className="flex mb-4" aria-label="Breadcrumb">
        <ol className="flex items-center space-x-4">
          <li>
            <Link href="/projects" className="text-muted-foreground hover:text-foreground">
              Projects
            </Link>
          </li>
          <li>
            <span className="text-muted-foreground/70">/</span>
          </li>
          <li>
            <Link
              href={`/projects/${slug}`}
              className="text-muted-foreground hover:text-foreground"
            >
              {slug}
            </Link>
          </li>
          <li>
            <span className="text-muted-foreground/70">/</span>
          </li>
          <li>
            <span className="text-sm font-medium">Billing</span>
          </li>
        </ol>
      </nav>

      {activeThrottle && (
        <div className="mb-6 rounded-md border border-status-error/50 bg-status-error-muted p-4">
          <div className="flex items-start justify-between">
            <div>
              <h3 className="text-sm font-semibold text-status-error-foreground">
                Deploys blocked in {activeThrottle.env_scope}
              </h3>
              <p className="mt-1 text-sm text-status-error-foreground">
                This project is over-budget. Reason: <code>{activeThrottle.reason}</code>. Raise
                the budget or clear the throttle to resume deploys.
              </p>
            </div>
            <a
              href="https://enclii.dev/docs/billing/throttle"
              target="_blank"
              rel="noopener noreferrer"
              className="text-sm underline text-status-error-foreground"
            >
              Runbook
            </a>
          </div>
        </div>
      )}

      <div className="flex items-center justify-between mb-6">
        <h1 className="text-3xl font-bold">Billing &amp; Usage</h1>
        <div className="flex items-center gap-2">
          <select
            className="bg-card border border-input rounded-md px-3 py-1.5 text-sm"
            value={period}
            onChange={(e) => setPeriod(e.target.value as typeof period)}
          >
            <option value="14d">Last 14 days</option>
            <option value="30d">Last 30 days</option>
            <option value="90d">Last 90 days</option>
            <option value="1y">Last year</option>
          </select>
          <button
            onClick={() => {
              setEditBudget(null);
              setShowBudgetModal(true);
            }}
            className="inline-flex items-center px-3 py-1.5 border border-transparent text-sm font-medium rounded-md shadow-sm text-white bg-enclii-blue hover:bg-enclii-blue-dark"
          >
            {primaryBudget ? 'Edit budget' : 'Create budget'}
          </button>
        </div>
      </div>

      {/* Hero: spend vs. budget */}
      <div className="bg-card shadow rounded-lg p-6 mb-6">
        <div className="flex items-center justify-between">
          <div>
            <p className="text-sm text-muted-foreground">Current-period spend</p>
            <p className="text-3xl font-bold">
              {formatCurrencyCents(cost?.total_cents ?? 0, primaryBudget?.currency ?? 'USD')}
            </p>
          </div>
          {primaryBudget && (
            <div className="text-right">
              <p className="text-sm text-muted-foreground">Budget ({primaryBudget.period})</p>
              <p className="text-xl font-semibold">
                {formatCurrencyCents(primaryBudget.amount_cents, primaryBudget.currency)}
              </p>
              <p className="text-sm text-muted-foreground mt-1">{pct}% consumed</p>
            </div>
          )}
        </div>
        {primaryBudget && (
          <div className="mt-6 relative">
            <div className="h-3 w-full bg-muted rounded-full overflow-hidden">
              <div
                className={cn('h-full transition-all', barColor(pct))}
                style={{ width: `${Math.min(pct, 100)}%` }}
              />
            </div>
            {/* Threshold markers */}
            {primaryBudget.alert_thresholds.map((t) => (
              <div
                key={t}
                className="absolute top-0 h-3 w-px bg-foreground/30"
                style={{ left: `${Math.min(t, 100)}%` }}
                aria-label={`${t}% threshold marker`}
                title={`${t}% alert threshold`}
              />
            ))}
          </div>
        )}
      </div>

      {/* Spend time-series */}
      <div className="bg-card shadow rounded-lg p-6 mb-6">
        <h2 className="text-sm font-medium text-muted-foreground mb-4">Daily spend</h2>
        <div className="flex items-end gap-1 h-40">
          {(series?.series ?? []).map((p) => {
            const height = Math.max(2, Math.round((p.cost_cents / maxSeries) * 100));
            return (
              <div
                key={p.bucket}
                className="flex-1 bg-enclii-blue/60 hover:bg-enclii-blue transition-colors rounded-sm"
                style={{ height: `${height}%` }}
                title={`${new Date(p.bucket).toLocaleDateString()}: ${formatCurrencyCents(p.cost_cents)}`}
              />
            );
          })}
        </div>
      </div>

      {/* Top drivers */}
      <div className="bg-card shadow rounded-lg p-6 mb-6">
        <h2 className="text-sm font-medium text-muted-foreground mb-4">Top cost drivers</h2>
        {(cost?.breakdown ?? []).length === 0 ? (
          <p className="text-sm text-muted-foreground">No usage recorded in this period.</p>
        ) : (
          <ul className="space-y-2">
            {(cost?.breakdown ?? []).slice(0, 5).map((entry) => (
              <li
                key={entry.key}
                className="flex items-center justify-between border-b border-border/50 pb-2 last:border-0"
              >
                <span className="font-mono text-sm">{entry.key}</span>
                <span className="font-medium">{formatCurrencyCents(entry.cost_cents)}</span>
              </li>
            ))}
          </ul>
        )}
      </div>

      {/* Alert history */}
      <div className="bg-card shadow rounded-lg p-6">
        <h2 className="text-sm font-medium text-muted-foreground mb-4">Recent alerts</h2>
        {alerts.length === 0 ? (
          <p className="text-sm text-muted-foreground">No threshold crossings recorded.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="min-w-full divide-y divide-border">
              <thead className="bg-muted/50">
                <tr>
                  <th className="px-3 py-2 text-left text-xs font-medium uppercase">When</th>
                  <th className="px-3 py-2 text-left text-xs font-medium uppercase">Threshold</th>
                  <th className="px-3 py-2 text-left text-xs font-medium uppercase">Actual</th>
                  <th className="px-3 py-2 text-left text-xs font-medium uppercase">Budget</th>
                  <th className="px-3 py-2 text-left text-xs font-medium uppercase">Status</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {alerts.map((a) => (
                  <tr key={a.id}>
                    <td className="px-3 py-2 text-sm text-muted-foreground whitespace-nowrap">
                      {new Date(a.created_at).toLocaleString()}
                    </td>
                    <td className="px-3 py-2 text-sm">{a.threshold}%</td>
                    <td className="px-3 py-2 text-sm font-mono">
                      {formatCurrencyCents(a.actual_cents)}
                    </td>
                    <td className="px-3 py-2 text-sm font-mono">
                      {formatCurrencyCents(a.budget_cents)}
                    </td>
                    <td className="px-3 py-2 text-sm">
                      {a.dispatched_at ? (
                        <span className="text-status-success-foreground">Delivered</span>
                      ) : (
                        <span className="text-status-warning-foreground">Pending</span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <BudgetModal
        open={showBudgetModal}
        onClose={() => setShowBudgetModal(false)}
        slug={slug}
        existing={primaryBudget}
        onSaved={() => {
          setShowBudgetModal(false);
          void refresh();
        }}
      />
    </div>
  );
}

// ---- Budget modal ----

function BudgetModal({
  open,
  onClose,
  slug,
  existing,
  onSaved,
}: {
  open: boolean;
  onClose: () => void;
  slug: string;
  existing: Budget | null;
  onSaved: () => void;
}) {
  const [amount, setAmount] = useState<string>(
    existing ? (existing.amount_cents / 100).toString() : ''
  );
  const [period, setPeriod] = useState<Period>(existing?.period ?? 'monthly');
  const [thresholds, setThresholds] = useState<string>(
    existing?.alert_thresholds.join(',') ?? '50,80,100'
  );
  const [hardThrottle, setHardThrottle] = useState<boolean>(existing?.hard_throttle ?? true);
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setAmount(existing ? (existing.amount_cents / 100).toString() : '');
    setPeriod(existing?.period ?? 'monthly');
    setThresholds(existing?.alert_thresholds.join(',') ?? '50,80,100');
    setHardThrottle(existing?.hard_throttle ?? true);
    setErr(null);
  }, [open, existing]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    setErr(null);
    try {
      const parsedAmount = parseFloat(amount);
      if (!Number.isFinite(parsedAmount) || parsedAmount <= 0) {
        throw new Error('Amount must be a positive number');
      }
      const parsedThresholds = thresholds
        .split(',')
        .map((t) => Number.parseInt(t.trim(), 10))
        .filter((n) => Number.isFinite(n) && n > 0);

      if (existing) {
        await apiPatch(`/v1/projects/${slug}/billing/budgets/${existing.id}`, {
          amount_cents: Math.round(parsedAmount * 100),
          alert_thresholds: parsedThresholds,
          hard_throttle: hardThrottle,
        });
      } else {
        await apiPost(`/v1/projects/${slug}/billing/budgets`, {
          amount_cents: Math.round(parsedAmount * 100),
          period,
          alert_thresholds: parsedThresholds,
          hard_throttle: hardThrottle,
        });
      }
      onSaved();
    } catch (error) {
      setErr(error instanceof Error ? error.message : 'Failed to save budget');
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async () => {
    if (!existing) return;
    if (!confirm('Delete this budget? Alerts and throttle will stop firing.')) return;
    setSubmitting(true);
    try {
      await apiDelete(`/v1/projects/${slug}/billing/budgets/${existing.id}`);
      onSaved();
    } catch (error) {
      setErr(error instanceof Error ? error.message : 'Failed to delete budget');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{existing ? 'Edit budget' : 'Create budget'}</DialogTitle>
          <DialogDescription>
            Alerts fire at each threshold percentage. 100% auto-throttles deploys in non-production
            environments (operator can clear the throttle).
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-sm font-medium mb-1">Amount (USD)</label>
            <input
              type="number"
              step="0.01"
              required
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              className="w-full px-3 py-2 border border-input rounded-md"
            />
          </div>

          {!existing && (
            <div>
              <label className="block text-sm font-medium mb-1">Period</label>
              <select
                value={period}
                onChange={(e) => setPeriod(e.target.value as Period)}
                className="w-full px-3 py-2 border border-input rounded-md"
              >
                <option value="monthly">Monthly</option>
                <option value="weekly">Weekly</option>
                <option value="quarterly">Quarterly</option>
              </select>
            </div>
          )}

          <div>
            <label className="block text-sm font-medium mb-1">Alert thresholds (%)</label>
            <input
              type="text"
              value={thresholds}
              onChange={(e) => setThresholds(e.target.value)}
              placeholder="50,80,100"
              className="w-full px-3 py-2 border border-input rounded-md"
            />
          </div>

          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={hardThrottle}
              onChange={(e) => setHardThrottle(e.target.checked)}
            />
            Auto-throttle non-production deploys at 100%
          </label>

          {err && (
            <p className="text-sm text-status-error-foreground bg-status-error-muted p-2 rounded">
              {err}
            </p>
          )}

          <DialogFooter className="flex items-center justify-between">
            {existing ? (
              <button
                type="button"
                onClick={handleDelete}
                disabled={submitting}
                className="text-sm text-status-error-foreground hover:underline"
              >
                Delete budget
              </button>
            ) : (
              <span />
            )}
            <div className="flex gap-2">
              <button
                type="button"
                onClick={onClose}
                className="px-3 py-1.5 text-sm bg-secondary text-secondary-foreground rounded-md"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={submitting}
                className="px-3 py-1.5 text-sm text-white bg-enclii-blue rounded-md disabled:opacity-50"
              >
                {submitting ? 'Saving…' : 'Save'}
              </button>
            </div>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
