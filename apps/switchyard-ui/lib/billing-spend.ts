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
  stale: 'Metering is stale; the figure is at least this much',
  unknown: 'Metering status could not be read',
};

/** One line for the page when the spend is not a measurement. */
export function spendUnknownLabel(state: SpendState): string | null {
  if (state.known) return null;
  return STATUS_LABEL[state.status] ?? `Metering status: ${state.status}`;
}
