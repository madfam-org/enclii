'use client';

/**
 * Presentational panels for the project billing page. Each panel renders a
 * figure only when it is a measurement: unmetered spend reads as unknown,
 * stale spend as a lower bound, and a budget that cannot be compared says why
 * instead of showing a percentage.
 */

import {
  comparisonLabel,
  coverageLine,
  alertDelivery,
  formatCents,
  spendState,
  spendUnknownLabel,
  type BudgetComparison,
} from '@/lib/billing-spend';
import { cn } from '@/lib/utils';
import type { BudgetAlertEvent, ProjectBudget, ProjectCostResponse } from '@/types/billing';

const CARD = 'bg-card shadow rounded-lg p-6 mb-6';
const CARD_TITLE = 'text-sm font-medium text-muted-foreground mb-4';

function barColor(pct: number): string {
  if (pct >= 100) return 'bg-status-error';
  if (pct >= 80) return 'bg-status-warning';
  return 'bg-status-success';
}

/** An inline notice for a section whose read failed; the rest of the page stays usable. */
export function SectionNotice({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div
      role="alert"
      className="bg-status-error-muted text-status-error-muted-foreground flex flex-wrap items-center justify-between gap-2 rounded-md px-3 py-2 text-sm"
    >
      <span>{message}</span>
      {onRetry && (
        <button type="button" onClick={onRetry} className="font-medium underline underline-offset-2">
          Retry
        </button>
      )}
    </div>
  );
}

// ---- Hero: month-to-date spend vs. budget ----

export function SpendHero({
  monthToDate,
  budget,
  comparison,
}: {
  monthToDate: ProjectCostResponse | null;
  budget: ProjectBudget | null;
  comparison: BudgetComparison | null;
}) {
  const state = spendState(monthToDate);
  const unknown = spendUnknownLabel(state);
  const total = monthToDate?.total_cents ?? 0;
  const floor = !state.known && state.status === 'stale' && total > 0;
  const coverage = coverageLine(monthToDate);

  return (
    <section className={CARD} aria-labelledby="spend-hero-title">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h2 id="spend-hero-title" className="text-muted-foreground text-sm">
            Spend this month
          </h2>
          {state.known ? (
            <p className="text-3xl font-bold tabular-nums">{formatCents(total)}</p>
          ) : (
            <>
              <p className="text-3xl font-bold tabular-nums" data-testid="spend-unknown">
                {floor ? `At least ${formatCents(total)}` : 'Unknown'}
              </p>
              <p className="text-status-warning-muted-foreground mt-1 text-sm">{unknown}</p>
            </>
          )}
          {coverage && <p className="text-muted-foreground mt-2 text-xs">{coverage}</p>}
        </div>
        {budget && comparison && (
          <div className="sm:text-right">
            <p className="text-muted-foreground text-sm">Budget ({budget.period})</p>
            <p className="text-xl font-semibold tabular-nums">
              {formatCents(budget.amount_cents, budget.currency)}
            </p>
            <p className="text-muted-foreground mt-1 text-sm" data-testid="budget-comparison">
              {comparisonLabel(comparison)}
            </p>
          </div>
        )}
      </div>
      {budget && comparison && <BudgetBar budget={budget} comparison={comparison} />}
    </section>
  );
}

function BudgetBar({ budget, comparison }: { budget: ProjectBudget; comparison: BudgetComparison }) {
  if (comparison.kind !== 'pct') {
    // Not a measurement: an empty, dashed track, never a 0% fill.
    return (
      <div
        aria-hidden="true"
        data-testid="budget-bar-unmeasured"
        className="border-border mt-6 h-3 w-full rounded-full border border-dashed"
      />
    );
  }
  const pct = comparison.pct;
  return (
    <div className="relative mt-6">
      <div
        role="progressbar"
        aria-label="Budget used this month"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.min(pct, 100)}
        aria-valuetext={comparisonLabel(comparison)}
        className="bg-muted h-3 w-full overflow-hidden rounded-full"
      >
        <div
          className={cn('h-full transition-[width] duration-300 ease-out motion-reduce:transition-none', barColor(pct))}
          style={{ width: `${Math.min(pct, 100)}%` }}
        />
      </div>
      {budget.alert_thresholds.map((t) => (
        <div
          key={t}
          aria-hidden="true"
          className="bg-foreground/30 absolute top-0 h-3 w-px"
          style={{ left: `${Math.min(t, 100)}%` }}
          title={`${t}% alert threshold`}
        />
      ))}
      {budget.alert_thresholds.length > 0 && (
        <p className="text-muted-foreground mt-2 text-xs">
          Alerts at {budget.alert_thresholds.map((t) => `${t}%`).join(', ')}
        </p>
      )}
    </div>
  );
}

// ---- Daily spend ----

export function DailySpendChart({
  series,
  rangeLabel,
}: {
  series: ProjectCostResponse | null;
  rangeLabel: string;
}) {
  const state = spendState(series);
  const points = series?.series ?? [];
  const notRunning = !state.known && state.status !== 'stale';
  const max = points.reduce((m, p) => Math.max(m, p.cost_cents), 0);
  const total = points.reduce((s, p) => s + p.cost_cents, 0);
  const peak = points.find((p) => p.cost_cents === max);

  let body: React.ReactNode;
  if (notRunning) {
    body = (
      <p className="text-muted-foreground text-sm">
        Daily spend can&apos;t be shown: {spendUnknownLabel(state)?.toLowerCase()}. Figures appear here
        once metering runs.
      </p>
    );
  } else if (points.length === 0) {
    body = <p className="text-muted-foreground text-sm">No spend recorded in this period.</p>;
  } else {
    const summary = `Daily spend, ${rangeLabel.toLowerCase()}: ${formatCents(total)} over ${points.length} days${
      peak && max > 0 ? `, highest ${formatCents(max)} on ${new Date(peak.bucket).toLocaleDateString()}` : ''
    }.`;
    body = (
      <>
        <p className="text-muted-foreground mb-1 text-xs tabular-nums">Peak {formatCents(max)}</p>
        <div role="img" aria-label={summary} className="border-border flex h-40 items-end gap-1 border-b">
          {points.map((p) => {
            const height = max > 0 ? Math.max(2, Math.round((p.cost_cents / max) * 100)) : 2;
            return (
              <div
                key={p.bucket}
                className="bg-enclii-blue/60 hover:bg-enclii-blue flex-1 rounded-sm transition-colors"
                style={{ height: `${height}%` }}
                title={`${new Date(p.bucket).toLocaleDateString()}: ${formatCents(p.cost_cents)}`}
              />
            );
          })}
        </div>
        <div className="text-muted-foreground mt-1 flex justify-between text-xs tabular-nums">
          <span>{new Date(points[0].bucket).toLocaleDateString()}</span>
          <span>{new Date(points[points.length - 1].bucket).toLocaleDateString()}</span>
        </div>
        {!state.known && (
          <p className="text-status-warning-muted-foreground mt-2 text-xs">
            {spendUnknownLabel(state)}; recent days may be missing.
          </p>
        )}
      </>
    );
  }

  return (
    <section className={CARD} aria-labelledby="daily-spend-title">
      <h2 id="daily-spend-title" className={CARD_TITLE}>
        Daily spend · {rangeLabel}
      </h2>
      {body}
    </section>
  );
}

// ---- Top cost drivers ----

export function TopDrivers({ cost, rangeLabel }: { cost: ProjectCostResponse | null; rangeLabel: string }) {
  const state = spendState(cost);
  const entries = (cost?.breakdown ?? []).slice(0, 5);
  const notRunning = !state.known && state.status !== 'stale';

  return (
    <section className={CARD} aria-labelledby="drivers-title">
      <h2 id="drivers-title" className={CARD_TITLE}>
        Top cost drivers · {rangeLabel}
      </h2>
      {notRunning ? (
        <p className="text-muted-foreground text-sm">
          Cost drivers can&apos;t be shown: {spendUnknownLabel(state)?.toLowerCase()}.
        </p>
      ) : entries.length === 0 ? (
        <p className="text-muted-foreground text-sm">No usage recorded in this period.</p>
      ) : (
        <>
          <ul className="space-y-2">
            {entries.map((entry) => (
              <li
                key={entry.key}
                className="border-border/50 flex items-center justify-between gap-4 border-b pb-2 last:border-0"
              >
                <span className="break-all font-mono text-sm">{entry.key}</span>
                <span className="font-medium tabular-nums">{formatCents(entry.cost_cents)}</span>
              </li>
            ))}
          </ul>
          {!state.known && (
            <p className="text-status-warning-muted-foreground mt-2 text-xs">
              {spendUnknownLabel(state)}.
            </p>
          )}
        </>
      )}
    </section>
  );
}

// ---- Alert history ----

const TONE_TEXT = {
  success: 'text-status-success-muted-foreground',
  warning: 'text-status-warning-muted-foreground',
  error: 'text-status-error-muted-foreground',
} as const;

export function AlertHistory({ alerts, budgets }: { alerts: BudgetAlertEvent[]; budgets: ProjectBudget[] }) {
  const currencyOf = (budgetId: string) => budgets.find((b) => b.id === budgetId)?.currency ?? 'USD';
  return (
    <section className="bg-card rounded-lg p-6 shadow" aria-labelledby="alerts-title">
      <h2 id="alerts-title" className={CARD_TITLE}>
        Recent alerts
      </h2>
      {alerts.length === 0 ? (
        <p className="text-muted-foreground text-sm">No threshold crossings recorded.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="divide-border min-w-full divide-y">
            <thead className="bg-muted/50">
              <tr>
                <th scope="col" className="px-3 py-2 text-left text-xs font-medium">When</th>
                <th scope="col" className="px-3 py-2 text-left text-xs font-medium">Threshold</th>
                <th scope="col" className="px-3 py-2 text-right text-xs font-medium">Spend</th>
                <th scope="col" className="px-3 py-2 text-right text-xs font-medium">Budget</th>
                <th scope="col" className="px-3 py-2 text-left text-xs font-medium">Delivery</th>
              </tr>
            </thead>
            <tbody className="divide-border divide-y">
              {alerts.map((a) => {
                const delivery = alertDelivery(a);
                return (
                  <tr key={a.id}>
                    <td className="text-muted-foreground whitespace-nowrap px-3 py-2 text-sm">
                      {new Date(a.created_at).toLocaleString()}
                    </td>
                    <td className="px-3 py-2 text-sm tabular-nums">{a.threshold}%</td>
                    <td className="px-3 py-2 text-right font-mono text-sm tabular-nums">
                      {formatCents(a.actual_cents)}
                    </td>
                    <td className="px-3 py-2 text-right font-mono text-sm tabular-nums">
                      {formatCents(a.budget_cents, currencyOf(a.budget_id))}
                    </td>
                    <td className={cn('whitespace-nowrap px-3 py-2 text-sm', TONE_TEXT[delivery.tone])}>
                      {delivery.label}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
