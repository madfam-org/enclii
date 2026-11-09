import type { ProjectCostResponse } from '@/types/billing';

/**
 * Whether a project's spend figure is a measurement.
 *
 * Waybill sums `hourly_usage`, which only its aggregator writes, so an empty
 * table reads as `total_cents: 0` whether the project spent nothing or nothing
 * was metered. Since #774 the cost read says which: `spend_known` is false
 * unless `metering.status` is `ok`. The billing page must then show the spend
 * as unknown, not as `$0.00` (the CLI already does).
 *
 * Older Waybill builds send neither field; their figure is shown as before.
 */
export type SpendState =
  | { known: true }
  | { known: false; status: string; detail: string };

export function spendState(cost: ProjectCostResponse | null | undefined): SpendState {
  if (!cost) return { known: true };
  const status = cost.metering?.status;
  if (cost.spend_known === false || (status !== undefined && status !== 'ok')) {
    return {
      known: false,
      status: status ?? 'unknown',
      detail: cost.metering?.detail ?? '',
    };
  }
  return { known: true };
}

const STATUS_LABEL: Record<string, string> = {
  not_running: 'Metering is not running',
  stale: 'Metering is stale, so figures are a lower bound',
  unknown: 'Metering status could not be read',
};

/** One line for the page when the spend is not a measurement. */
export function spendUnknownLabel(state: SpendState): string | null {
  if (state.known) return null;
  return STATUS_LABEL[state.status] ?? `Metering status: ${state.status}`;
}

// ---- Money, budget comparison and coverage ----

/** Waybill prices usage in USD cents; the cost response carries no currency. */
export const SPEND_CURRENCY = 'USD';

export function formatCents(cents: number, currency: string = SPEND_CURRENCY): string {
  return new Intl.NumberFormat(undefined, { style: 'currency', currency }).format(cents / 100);
}

/** Highest percentage the page prints; anything above reads "over 300%". */
export const PERCENT_CAP = 300;

/**
 * How month-to-date spend relates to a budget.
 *
 * - `pct`: a measured comparison (only a monthly USD budget against the
 *   month-to-date read).
 * - `unknown`: spend is not a measurement.
 * - `currency`: the budget is not in Waybill's currency; no conversion exists
 *   here, so the two are not compared.
 * - `period`: Waybill has no read for a weekly or quarterly window yet.
 */
export type BudgetComparison =
  | { kind: 'pct'; pct: number }
  | { kind: 'unknown' }
  | { kind: 'currency'; budgetCurrency: string }
  | { kind: 'period'; period: string };

export function compareToBudget(
  budget: { amount_cents: number; currency: string; period: string },
  monthToDate: ProjectCostResponse | null | undefined,
): BudgetComparison {
  if (!spendState(monthToDate).known || !monthToDate) return { kind: 'unknown' };
  if (budget.currency.toUpperCase() !== SPEND_CURRENCY) {
    return { kind: 'currency', budgetCurrency: budget.currency.toUpperCase() };
  }
  if (budget.period !== 'monthly') return { kind: 'period', period: budget.period };
  if (budget.amount_cents <= 0) return { kind: 'pct', pct: 0 };
  const pct = Math.round((monthToDate.total_cents * 100) / budget.amount_cents);
  return { kind: 'pct', pct: Math.min(pct, PERCENT_CAP) };
}

/** The line under the budget figure. */
export function comparisonLabel(c: BudgetComparison): string {
  switch (c.kind) {
    case 'pct':
      return c.pct >= PERCENT_CAP ? `Over ${PERCENT_CAP}% used` : `${c.pct}% used this month`;
    case 'unknown':
      return 'Usage against budget unknown';
    case 'currency':
      return `Not compared: budget is in ${c.budgetCurrency}, spend is metered in ${SPEND_CURRENCY}`;
    case 'period':
      return `Not compared: spend is read month to date, this budget is ${c.period}`;
  }
}

function shortDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric', timeZone: 'UTC' });
}

/**
 * What a figure covers: the window it sums and, when Waybill reports it, the
 * last hour the aggregator closed. Empty when the response has no window.
 */
export function coverageLine(cost: ProjectCostResponse | null | undefined): string {
  if (!cost?.period_start || !cost?.period_end) return '';
  const parts = [`Source: Waybill`, `${shortDate(cost.period_start)} – ${shortDate(cost.period_end)} (UTC)`];
  const hour = cost.metering?.last_aggregated_hour;
  if (hour) {
    const at = new Date(hour).toLocaleString(undefined, {
      month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', timeZone: 'UTC',
    });
    parts.push(`aggregated through ${at} UTC`);
  }
  return parts.join(' · ');
}

// ---- Budget form ----

/** Split "50, 80,100" into percentages, keeping what did not parse. */
export function parseThresholds(text: string): { values: number[]; invalid: string[] } {
  const values: number[] = [];
  const invalid: string[] = [];
  for (const raw of text.split(',')) {
    const t = raw.trim();
    if (!t) continue;
    const n = Number(t);
    if (Number.isInteger(n) && n > 0 && n <= PERCENT_CAP) values.push(n);
    else invalid.push(t);
  }
  return { values: Array.from(new Set(values)).sort((a, b) => a - b), invalid };
}

// ---- Alert delivery ----

export function alertDelivery(a: {
  dispatched_at?: string | null;
  dispatch_attempts: number;
  last_error?: string;
}): { tone: 'success' | 'warning' | 'error'; label: string } {
  if (a.dispatched_at) return { tone: 'success', label: 'Delivered' };
  if (a.dispatch_attempts > 0 && a.last_error) {
    const n = a.dispatch_attempts;
    return { tone: 'error', label: `Not delivered (${n} attempt${n === 1 ? '' : 's'})` };
  }
  return { tone: 'warning', label: 'Pending' };
}
