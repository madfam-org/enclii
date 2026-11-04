-- 046_platform_ci_project
--
-- A project for the shared CI runner pool, so its minutes have somewhere to
-- land (RFC 0049 Phase 1).
--
-- Weighbridge (apps/waybill/cmd/weighbridge) meters every finished ARC runner
-- pod as a build.completed event. The shared org-wide pool carries no
-- per-tenant label, and usage_events.project_id is a foreign key to
-- projects(id), so without a project to file them under every runner's
-- minutes were counted in weighbridge_runners_unattributed_total and dropped.
--
-- The pool IS MADFAM's own platform CI, so filing it under one internal
-- project is true, not a placeholder: no tenant is invoiced for it. When
-- per-tenant scale sets stamp enclii.dev/project-id on their runner pods,
-- those pods are attributed to their own project and only the shared pool
-- stays here.
--
-- The id is fixed because infra/k8s/production/monitoring/weighbridge.yaml
-- names it in WEIGHBRIDGE_DEFAULT_PROJECT_ID. Parented to the MADFAM Platform
-- team (024) so it shows in that team's view and nowhere else; if that team
-- row is missing the project is created unparented rather than failing.
--
-- Idempotent: ON CONFLICT DO NOTHING covers both the id and the slug, so a
-- rerun, or a project someone already created by hand, is left untouched.
INSERT INTO projects (id, name, slug, description, team_id, created_at, updated_at)
VALUES (
    '02799103-7f4d-443a-af62-b4ba6015a92c',
    'Platform CI',
    'platform-ci',
    'Shared GitHub Actions runner pool (ARC). Runner minutes metered by Weighbridge.',
    (SELECT id FROM teams WHERE slug = 'madfam-platform'),
    now(),
    now()
)
ON CONFLICT DO NOTHING;
