'use client';

import { useEffect, useState } from 'react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@enclii/ui-components/dialog';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';
import { apiDelete, apiPatch, apiPost } from '@/lib/api';
import { parseThresholds, SPEND_CURRENCY } from '@/lib/billing-spend';
import type { BudgetPeriod, ProjectBudget } from '@/types/billing';

const INPUT = 'w-full px-3 py-2 border border-input rounded-md bg-background';

export function BudgetModal({
  open,
  onClose,
  slug,
  existing,
  onSaved,
}: {
  open: boolean;
  onClose: () => void;
  slug: string;
  existing: ProjectBudget | null;
  onSaved: () => void;
}) {
  const [amount, setAmount] = useState<string>('');
  const [period, setPeriod] = useState<BudgetPeriod>('monthly');
  const [thresholds, setThresholds] = useState<string>('50,80,100');
  const [hardThrottle, setHardThrottle] = useState<boolean>(true);
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);

  useEffect(() => {
    if (!open) return;
    setAmount(existing ? (existing.amount_cents / 100).toString() : '');
    setPeriod(existing?.period ?? 'monthly');
    setThresholds(existing?.alert_thresholds.join(',') ?? '50,80,100');
    setHardThrottle(existing?.hard_throttle ?? true);
    setErr(null);
    setConfirmDelete(false);
  }, [open, existing]);

  // A new budget is created in Waybill's default currency; an existing one
  // keeps its own.
  const currency = (existing?.currency ?? SPEND_CURRENCY).toUpperCase();

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr(null);
    const parsedAmount = parseFloat(amount);
    if (!Number.isFinite(parsedAmount) || parsedAmount <= 0) {
      setErr('Enter an amount greater than zero.');
      return;
    }
    const parsed = parseThresholds(thresholds);
    if (parsed.invalid.length > 0) {
      setErr(
        `These thresholds aren't whole percentages between 1 and 300: ${parsed.invalid.join(', ')}.`,
      );
      return;
    }
    setSubmitting(true);
    try {
      if (existing) {
        await apiPatch(`/v1/projects/${slug}/billing/budgets/${existing.id}`, {
          amount_cents: Math.round(parsedAmount * 100),
          alert_thresholds: parsed.values,
          hard_throttle: hardThrottle,
        });
      } else {
        await apiPost(`/v1/projects/${slug}/billing/budgets`, {
          amount_cents: Math.round(parsedAmount * 100),
          period,
          alert_thresholds: parsed.values,
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
    setSubmitting(true);
    try {
      await apiDelete(`/v1/projects/${slug}/billing/budgets/${existing.id}`);
      setConfirmDelete(false);
      onSaved();
    } catch (error) {
      setConfirmDelete(false);
      setErr(error instanceof Error ? error.message : 'Failed to delete budget');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <>
      <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{existing ? 'Edit budget' : 'Create budget'}</DialogTitle>
            <DialogDescription>
              Alerts fire at each threshold percentage. 100% auto-throttles deploys in
              non-production environments (operator can clear the throttle).
            </DialogDescription>
          </DialogHeader>
          <form onSubmit={handleSubmit} className="space-y-4" noValidate>
            <div>
              <label htmlFor="budget-amount" className="mb-1 block text-sm font-medium">
                Amount ({currency})
              </label>
              <input
                id="budget-amount"
                type="number"
                inputMode="decimal"
                step="0.01"
                min="0.01"
                required
                value={amount}
                onChange={(e) => setAmount(e.target.value)}
                className={INPUT}
              />
            </div>

            {!existing && (
              <div>
                <label htmlFor="budget-period" className="mb-1 block text-sm font-medium">
                  Period
                </label>
                <select
                  id="budget-period"
                  value={period}
                  onChange={(e) => setPeriod(e.target.value as BudgetPeriod)}
                  className={INPUT}
                >
                  <option value="monthly">Monthly</option>
                  <option value="weekly">Weekly</option>
                  <option value="quarterly">Quarterly</option>
                </select>
              </div>
            )}

            <div>
              <label htmlFor="budget-thresholds" className="mb-1 block text-sm font-medium">
                Alert thresholds (%)
              </label>
              <input
                id="budget-thresholds"
                type="text"
                inputMode="numeric"
                value={thresholds}
                onChange={(e) => setThresholds(e.target.value)}
                placeholder="50,80,100"
                aria-describedby="budget-thresholds-hint"
                className={INPUT}
              />
              <p id="budget-thresholds-hint" className="text-muted-foreground mt-1 text-xs">
                Whole percentages, separated by commas.
              </p>
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
              <p
                role="alert"
                className="text-status-error-muted-foreground bg-status-error-muted rounded p-2 text-sm"
              >
                {err}
              </p>
            )}

            <DialogFooter className="flex items-center justify-between">
              {existing ? (
                <button
                  type="button"
                  onClick={() => setConfirmDelete(true)}
                  disabled={submitting}
                  className="text-status-error-muted-foreground text-sm hover:underline disabled:opacity-50"
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
                  className="bg-secondary text-secondary-foreground rounded-md px-3 py-1.5 text-sm"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={submitting}
                  className="bg-enclii-blue rounded-md px-3 py-1.5 text-sm text-white disabled:opacity-50"
                >
                  {submitting ? 'Saving…' : 'Save'}
                </button>
              </div>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
      <ConfirmDialog
        open={confirmDelete}
        title="Delete this budget?"
        description="Alerts and throttle will stop firing."
        confirmLabel="Delete budget"
        variant="destructive"
        loading={submitting}
        onConfirm={() => void handleDelete()}
        onCancel={() => setConfirmDelete(false)}
      />
    </>
  );
}
