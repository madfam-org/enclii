-- P2.2 Spend visibility + budget enforcement
--
-- Adds:
--   budgets                    project-scoped spend budgets with threshold alerts
--   budget_alert_events        per-threshold cross log (idempotency source of truth)
--   waybill_throttles          active throttle records the switchyard reconciler reads
--
-- All amounts stored as minor units (bigint cents) to avoid float math.

CREATE TABLE IF NOT EXISTS public.budgets (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id         uuid NOT NULL REFERENCES public.projects(id) ON DELETE CASCADE,
    amount_cents       bigint NOT NULL CHECK (amount_cents > 0),
    currency           varchar(3) NOT NULL DEFAULT 'USD',
    period             varchar(16) NOT NULL DEFAULT 'monthly'
                         CHECK (period IN ('monthly','weekly','quarterly')),
    alert_thresholds   integer[] NOT NULL DEFAULT '{50,80,100}'::integer[],
    hard_throttle      boolean NOT NULL DEFAULT true,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

-- One active budget per (project, period) keeps the evaluator's job simple.
CREATE UNIQUE INDEX IF NOT EXISTS idx_budgets_project_period
    ON public.budgets(project_id, period);

CREATE INDEX IF NOT EXISTS idx_budgets_project
    ON public.budgets(project_id);

COMMENT ON TABLE public.budgets IS
  'Project-scoped spend budgets consumed by waybill alert evaluator (P2.2).';
COMMENT ON COLUMN public.budgets.alert_thresholds IS
  'Percent thresholds (0-500) that emit alerts; 100 triggers non-prod throttle.';
COMMENT ON COLUMN public.budgets.hard_throttle IS
  'When true, 100% crossing in non-production writes a waybill_throttles row.';

-- Idempotent alert ledger. The tuple (budget_id, period_start, threshold)
-- is unique; re-crossings after a new period start create new rows.
CREATE TABLE IF NOT EXISTS public.budget_alert_events (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    budget_id          uuid NOT NULL REFERENCES public.budgets(id) ON DELETE CASCADE,
    project_id         uuid NOT NULL REFERENCES public.projects(id) ON DELETE CASCADE,
    period_start       timestamptz NOT NULL,
    period_end         timestamptz NOT NULL,
    threshold          integer NOT NULL CHECK (threshold > 0 AND threshold <= 500),
    actual_cents       bigint NOT NULL,
    budget_cents       bigint NOT NULL,
    dispatched_at      timestamptz,
    dispatch_attempts  integer NOT NULL DEFAULT 0,
    last_error         text,
    created_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (budget_id, period_start, threshold)
);

CREATE INDEX IF NOT EXISTS idx_budget_alert_events_project
    ON public.budget_alert_events(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_budget_alert_events_pending
    ON public.budget_alert_events(dispatched_at) WHERE dispatched_at IS NULL;

COMMENT ON TABLE public.budget_alert_events IS
  'Append-only log of budget threshold crossings. Dhanam is notified exactly once per row.';

-- Active throttle records. A row here tells the switchyard reconciler to
-- block future deploys / scale-ups for the project. Production is never
-- auto-throttled; operators clear rows manually.
CREATE TABLE IF NOT EXISTS public.waybill_throttles (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id         uuid NOT NULL REFERENCES public.projects(id) ON DELETE CASCADE,
    reason             varchar(64) NOT NULL,
    budget_id          uuid REFERENCES public.budgets(id) ON DELETE SET NULL,
    env_scope          varchar(32) NOT NULL DEFAULT 'non-production',
    activated_at       timestamptz NOT NULL DEFAULT now(),
    cleared_at         timestamptz,
    cleared_by         uuid REFERENCES public.users(id) ON DELETE SET NULL,
    metadata           jsonb NOT NULL DEFAULT '{}'::jsonb
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_waybill_throttles_active
    ON public.waybill_throttles(project_id, env_scope)
    WHERE cleared_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_waybill_throttles_project
    ON public.waybill_throttles(project_id);

COMMENT ON TABLE public.waybill_throttles IS
  'Active deploy blocks. Rows with cleared_at IS NULL halt reconciler work.';

-- Bookkeeping: track when the evaluator last ran so operators can see staleness.
CREATE TABLE IF NOT EXISTS public.budget_evaluator_runs (
    id           bigserial PRIMARY KEY,
    started_at   timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz,
    projects_evaluated integer NOT NULL DEFAULT 0,
    alerts_fired integer NOT NULL DEFAULT 0,
    errors       integer NOT NULL DEFAULT 0,
    notes        text
);
