'use client';

/**
 * Project billing page (P2.2).
 *
 * Renders:
 *   - Month-to-date spend vs. the active budget, with threshold markers
 *   - Daily spend time-series and top cost drivers for the selected window
 *   - Budget card with create / edit modals
 *   - Alert history table
 *   - Throttle banner when deploys are blocked in non-production
 *
 * Data comes from the Switchyard /billing/* proxy endpoints, which forward
 * to Waybill. Spend that metering did not measure reads as unknown, never as
 * $0.00 (see lib/billing-spend.ts). Each read fails on its own: one failed
 * endpoint shows a notice in its section, not a blank page.
 */

import { useCallback, useEffect, useState } from 'react';
import Link from 'next/link';
import { useParams } from 'next/navigation';
import { apiGet } from '@/lib/api';
import { compareToBudget } from '@/lib/billing-spend';
import type {
  BudgetAlertEvent,
  BudgetThrottle,
  ProjectBudget,
  ProjectCostResponse,
} from '@/types/billing';
import { BudgetModal } from './budget-modal';
import {
  AlertHistory,
  DailySpendChart,
  SectionNotice,
  SpendHero,
  TopDrivers,
} from './spend-panels';

type Range = '14d' | '30d' | '90d' | '1y';

const RANGE_LABEL: Record<Range, string> = {
  '14d': 'Last 14 days',
  '30d': 'Last 30 days',
  '90d': 'Last 90 days',
  '1y': 'Last year',
};

type Read<T> = { data: T; error: null } | { data: null; error: string };

function settle<T>(r: PromiseSettledResult<T>): Read<T> {
  if (r.status === 'fulfilled') return { data: r.value, error: null };
  return { data: null, error: r.reason instanceof Error ? r.reason.message : 'Request failed' };
}

interface BillingData {
  monthToDate: Read<ProjectCostResponse>;
  drivers: Read<ProjectCostResponse>;
  series: Read<ProjectCostResponse>;
  budgets: Read<ProjectBudget[]>;
  alerts: Read<BudgetAlertEvent[]>;
  throttles: Read<BudgetThrottle[]>;
}

export default function ProjectBillingPage() {
  const params = useParams();
  const slug = (params?.slug as string) ?? '';

  const [range, setRange] = useState<Range>('30d');
  const [data, setData] = useState<BillingData | null>(null);
  const [loading, setLoading] = useState(true);

  const [showBudgetModal, setShowBudgetModal] = useState(false);

  const refresh = useCallback(async () => {
    setLoading(true);
    const base = `/v1/projects/${slug}/billing`;
    const [mtd, drivers, series, budgets, alerts, throttles] = await Promise.allSettled([
      apiGet<ProjectCostResponse>(`${base}/cost?period=mtd`),
      apiGet<ProjectCostResponse>(`${base}/cost?period=${range}&group_by=service`),
      apiGet<ProjectCostResponse>(`${base}/cost?period=${range}&group_by=day`),
      apiGet<{ budgets: ProjectBudget[] }>(`${base}/budgets`).then((r) => r.budgets ?? []),
      apiGet<{ alerts: BudgetAlertEvent[] }>(`${base}/budgets/alerts`).then((r) => r.alerts ?? []),
      apiGet<{ throttles: BudgetThrottle[] }>(`${base}/throttles`).then((r) => r.throttles ?? []),
    ]);
    setData({
      monthToDate: settle(mtd),
      drivers: settle(drivers),
      series: settle(series),
      budgets: settle(budgets),
      alerts: settle(alerts),
      throttles: settle(throttles),
    });
    setLoading(false);
  }, [slug, range]);

  useEffect(() => {
    if (slug) {
      void refresh();
    }
  }, [slug, refresh]);

  if (loading && !data) {
    return (
      <div className="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8" aria-busy="true">
        <p className="sr-only" role="status">
          Loading billing data…
        </p>
        <div className="animate-pulse space-y-4 motion-reduce:animate-none">
          <div className="bg-muted h-8 w-1/3 rounded"></div>
          <div className="bg-muted h-40 rounded"></div>
          <div className="bg-muted h-64 rounded"></div>
        </div>
      </div>
    );
  }

  const allFailed = data && Object.values(data).every((r) => r.error);
  if (data && allFailed) {
    return (
      <div className="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
        <div
          role="alert"
          className="bg-status-error-muted border-status-error/30 rounded-md border p-4"
        >
          <h3 className="text-status-error-muted-foreground text-sm font-medium">
            Error loading billing data
          </h3>
          <p className="text-status-error-muted-foreground mt-2 text-sm">{data.monthToDate.error}</p>
          <button
            type="button"
            onClick={() => void refresh()}
            className="text-status-error-muted-foreground mt-3 text-sm font-medium underline underline-offset-2"
          >
            Retry
          </button>
        </div>
      </div>
    );
  }

  const budgets = data?.budgets.data ?? [];
  const primaryBudget = budgets[0] ?? null;
  const monthToDate = data?.monthToDate.data ?? null;
  const comparison = primaryBudget ? compareToBudget(primaryBudget, monthToDate) : null;
  const activeThrottle = (data?.throttles.data ?? []).find((t) => !t.cleared_at) ?? null;
  const rangeLabel = RANGE_LABEL[range];
  const retry = () => void refresh();

  return (
    <div className="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8" aria-busy={loading}>
      <nav className="mb-4 flex text-sm" aria-label="Breadcrumb">
        <ol className="flex flex-wrap items-center gap-x-4 gap-y-1">
          <li>
            <Link href="/projects" className="text-muted-foreground hover:text-foreground">
              Projects
            </Link>
          </li>
          <li aria-hidden="true">
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
          <li aria-hidden="true">
            <span className="text-muted-foreground/70">/</span>
          </li>
          <li>
            <span className="font-medium" aria-current="page">
              Billing
            </span>
          </li>
        </ol>
      </nav>

      {activeThrottle && (
        <div
          role="alert"
          className="border-status-error/50 bg-status-error-muted mb-6 rounded-md border p-4"
        >
          <div className="flex flex-wrap items-start justify-between gap-2">
            <div>
              <h3 className="text-status-error-muted-foreground text-sm font-semibold">
                Deploys blocked in {activeThrottle.env_scope}
              </h3>
              <p className="text-status-error-muted-foreground mt-1 text-sm">
                This project is over-budget. Reason: <code>{activeThrottle.reason}</code>. Raise
                the budget or clear the throttle to resume deploys.
              </p>
            </div>
            <a
              href="https://enclii.dev/docs/billing/throttle"
              target="_blank"
              rel="noopener noreferrer"
              className="text-status-error-muted-foreground text-sm underline"
            >
              Runbook
            </a>
          </div>
        </div>
      )}

      <div className="mb-6 flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-3xl font-bold">Billing &amp; Usage</h1>
        <div className="flex flex-wrap items-center gap-2">
          {loading && (
            <span role="status" className="text-muted-foreground text-xs">
              Updating…
            </span>
          )}
          <label htmlFor="billing-range" className="sr-only">
            Time range for daily spend and cost drivers
          </label>
          <select
            id="billing-range"
            className="bg-card border-input rounded-md border px-3 py-1.5 text-sm"
            value={range}
            onChange={(e) => setRange(e.target.value as Range)}
          >
            {(Object.keys(RANGE_LABEL) as Range[]).map((r) => (
              <option key={r} value={r}>
                {RANGE_LABEL[r]}
              </option>
            ))}
          </select>
          <button
            type="button"
            onClick={() => setShowBudgetModal(true)}
            disabled={!!data?.budgets.error}
            className="bg-enclii-blue hover:bg-enclii-blue-dark inline-flex items-center rounded-md border border-transparent px-3 py-1.5 text-sm font-medium text-white shadow-sm disabled:opacity-50"
          >
            {primaryBudget ? 'Edit budget' : 'Create budget'}
          </button>
        </div>
      </div>

      {data?.monthToDate.error || data?.budgets.error ? (
        <div className="mb-6">
          <SectionNotice
            message={
              data.monthToDate.error
                ? `Couldn't load this month's spend: ${data.monthToDate.error}`
                : `Couldn't load budgets: ${data.budgets.error}`
            }
            onRetry={retry}
          />
        </div>
      ) : null}
      {!data?.monthToDate.error && (
        <SpendHero monthToDate={monthToDate} budget={primaryBudget} comparison={comparison} />
      )}

      {data?.series.error ? (
        <div className="mb-6">
          <SectionNotice message={`Couldn't load daily spend: ${data.series.error}`} onRetry={retry} />
        </div>
      ) : (
        <DailySpendChart series={data?.series.data ?? null} rangeLabel={rangeLabel} />
      )}

      {data?.drivers.error ? (
        <div className="mb-6">
          <SectionNotice message={`Couldn't load cost drivers: ${data.drivers.error}`} onRetry={retry} />
        </div>
      ) : (
        <TopDrivers cost={data?.drivers.data ?? null} rangeLabel={rangeLabel} />
      )}

      {data?.alerts.error ? (
        <SectionNotice message={`Couldn't load alert history: ${data.alerts.error}`} onRetry={retry} />
      ) : (
        <AlertHistory alerts={data?.alerts.data ?? []} budgets={budgets} />
      )}

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
