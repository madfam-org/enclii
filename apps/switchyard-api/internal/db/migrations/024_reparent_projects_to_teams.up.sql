-- 024_reparent_projects_to_teams
--
-- Backfill the master-admin tenant model. Today every project has
-- team_id IS NULL, so the XC-2 acting-as filter (Round 4-6) has nothing
-- to filter on. This migration creates a teams row per known tenant
-- (white-glove client OR MADFAM platform group), parents each existing
-- project to its team, and backfills team_members so admin@madfam.io is
-- owner on every team.
--
-- See claudedocs/master-admin-tenant-switching.md "Phasing" section.
--
-- The migration is FULLY IDEMPOTENT — safe to re-run. New projects added
-- to the cluster after this migration ships should be onboarded with an
-- explicit team_id from the onboarding handler; this migration only
-- backfills the existing 25 projects observed in the 2026-05-02 audit.

-- 1. Insert team rows. ON CONFLICT (slug) DO NOTHING keeps re-runs safe.
--    Each tenant gets a stable slug + display name + description so the
--    /v1/admin/tenants list reads cleanly in the master-admin switcher.
INSERT INTO teams (id, name, slug, description, settings, created_at, updated_at) VALUES
    -- White-glove clients (one team per business)
    (gen_random_uuid(), 'Dhanam',                  'dhanam',                  'Financial compliance + KYC platform',                          '{"tier":"client"}'::jsonb, now(), now()),
    (gen_random_uuid(), 'factlas',                 'factlas',                 'Geospatial intelligence (atlas + tiles)',                      '{"tier":"client"}'::jsonb, now(), now()),
    (gen_random_uuid(), 'Karafiel',                'karafiel',                'Legal compliance + audit trail platform',                      '{"tier":"client"}'::jsonb, now(), now()),
    (gen_random_uuid(), 'Pravara MES',             'pravara-mes',             'Manufacturing execution system',                                '{"tier":"client"}'::jsonb, now(), now()),
    (gen_random_uuid(), 'Routecraft',              'routecraft',              'Routing + dispatch logistics',                                  '{"tier":"client"}'::jsonb, now(), now()),
    (gen_random_uuid(), 'Tezca',                   'tezca',                   'Mexican law intelligence platform',                             '{"tier":"client"}'::jsonb, now(), now()),
    (gen_random_uuid(), 'Forgesight',              'forgesight',              'Customer-facing CRM + admin tooling',                           '{"tier":"client"}'::jsonb, now(), now()),
    (gen_random_uuid(), 'Yantra4D',                'yantra4d',                'Simulation + studio platform',                                  '{"tier":"client"}'::jsonb, now(), now()),
    (gen_random_uuid(), 'Symbiosis HCM',           'symbiosis-hcm',           'HR + payroll platform',                                         '{"tier":"client"}'::jsonb, now(), now()),
    (gen_random_uuid(), 'Nuit One',                'nuit-one',                'Nuit One product line',                                         '{"tier":"client"}'::jsonb, now(), now()),
    (gen_random_uuid(), 'Forj',                    'forj',                    'Forj product line',                                             '{"tier":"client"}'::jsonb, now(), now()),
    (gen_random_uuid(), 'Coforma Studio',          'coforma-studio',          'Coforma collaborative architecture studio',                     '{"tier":"client"}'::jsonb, now(), now()),
    (gen_random_uuid(), 'Blueprint Harvester',     'blueprint-harvester',     'Blueprint harvester product',                                   '{"tier":"client"}'::jsonb, now(), now()),
    (gen_random_uuid(), 'Bloom Scroll',            'bloom-scroll',            'Bloom Scroll product',                                          '{"tier":"client"}'::jsonb, now(), now()),
    (gen_random_uuid(), 'CEQ',                     'ceq',                     'CEQ product',                                                   '{"tier":"client"}'::jsonb, now(), now()),
    (gen_random_uuid(), 'Digifab Quoting',         'digifab-quoting',         'Digifab quoting product',                                       '{"tier":"client"}'::jsonb, now(), now()),
    (gen_random_uuid(), 'Primavera3D',             'primavera3d',             'Primavera3D product',                                           '{"tier":"client"}'::jsonb, now(), now()),
    (gen_random_uuid(), 'Fortuna',                 'fortuna',                 'Fortuna product',                                               '{"tier":"client"}'::jsonb, now(), now()),
    (gen_random_uuid(), 'Avala',                   'avala',                   'Avala learning-verification platform',                          '{"tier":"client"}'::jsonb, now(), now()),

    -- MADFAM internal platform group — every internal infra/site project
    -- lives under this team so master-admin's "Acting as MADFAM Platform"
    -- view shows the platform's own moving parts together.
    (gen_random_uuid(), 'MADFAM Platform',         'madfam-platform',         'Internal MADFAM infra, sites, and platform services',           '{"tier":"internal"}'::jsonb, now(), now())

ON CONFLICT (slug) DO NOTHING;

-- 2. Re-parent projects. Each UPDATE is keyed on the project slug we know
--    exists in the cluster as of 2026-05-02. UPDATE WHERE team_id IS NULL
--    keeps re-runs safe (already-parented projects are not stomped).
UPDATE projects SET team_id = (SELECT id FROM teams WHERE slug = 'dhanam')              WHERE slug = 'dhanam'              AND team_id IS NULL;
UPDATE projects SET team_id = (SELECT id FROM teams WHERE slug = 'factlas')             WHERE slug = 'factlas'             AND team_id IS NULL;
UPDATE projects SET team_id = (SELECT id FROM teams WHERE slug = 'karafiel')            WHERE slug = 'karafiel'            AND team_id IS NULL;
UPDATE projects SET team_id = (SELECT id FROM teams WHERE slug = 'pravara-mes')         WHERE slug = 'pravara-mes'         AND team_id IS NULL;
UPDATE projects SET team_id = (SELECT id FROM teams WHERE slug = 'routecraft')          WHERE slug = 'routecraft'          AND team_id IS NULL;
UPDATE projects SET team_id = (SELECT id FROM teams WHERE slug = 'tezca')               WHERE slug = 'tezca'               AND team_id IS NULL;
UPDATE projects SET team_id = (SELECT id FROM teams WHERE slug = 'forgesight')          WHERE slug = 'forgesight'          AND team_id IS NULL;
UPDATE projects SET team_id = (SELECT id FROM teams WHERE slug = 'yantra4d')            WHERE slug = 'yantra4d'            AND team_id IS NULL;
UPDATE projects SET team_id = (SELECT id FROM teams WHERE slug = 'symbiosis-hcm')       WHERE slug = 'symbiosis-hcm'       AND team_id IS NULL;
UPDATE projects SET team_id = (SELECT id FROM teams WHERE slug = 'nuit-one')            WHERE slug = 'nuit-one'            AND team_id IS NULL;
UPDATE projects SET team_id = (SELECT id FROM teams WHERE slug = 'forj')                WHERE slug = 'forj'                AND team_id IS NULL;
UPDATE projects SET team_id = (SELECT id FROM teams WHERE slug = 'coforma-studio')      WHERE slug = 'coforma-studio'      AND team_id IS NULL;
UPDATE projects SET team_id = (SELECT id FROM teams WHERE slug = 'blueprint-harvester') WHERE slug = 'blueprint-harvester' AND team_id IS NULL;
UPDATE projects SET team_id = (SELECT id FROM teams WHERE slug = 'bloom-scroll')        WHERE slug = 'bloom-scroll'        AND team_id IS NULL;
UPDATE projects SET team_id = (SELECT id FROM teams WHERE slug = 'ceq')                 WHERE slug = 'ceq'                 AND team_id IS NULL;
UPDATE projects SET team_id = (SELECT id FROM teams WHERE slug = 'digifab-quoting')     WHERE slug = 'digifab-quoting'     AND team_id IS NULL;
UPDATE projects SET team_id = (SELECT id FROM teams WHERE slug = 'primavera3d')         WHERE slug = 'primavera3d'         AND team_id IS NULL;
UPDATE projects SET team_id = (SELECT id FROM teams WHERE slug = 'fortuna')             WHERE slug = 'fortuna'             AND team_id IS NULL;
UPDATE projects SET team_id = (SELECT id FROM teams WHERE slug = 'avala')               WHERE slug = 'avala'               AND team_id IS NULL;

-- MADFAM-internal projects all live under one team. Slugs match the
-- audit's project list (Enclii, Janua, NPM Registry, Platform
-- Infrastructure, accionables-madlab, madfam-site).
UPDATE projects SET team_id = (SELECT id FROM teams WHERE slug = 'madfam-platform') WHERE slug IN (
    'enclii', 'janua', 'npm-registry', 'platform-infrastructure',
    'accionables-madlab', 'madfam-site'
) AND team_id IS NULL;

-- 3. Backfill team_members. admin@madfam.io is owner of every team so
--    the master admin can switch into any of them. The user must already
--    exist (created via Janua SSO on first login); if they don't, this
--    is a no-op (no rows match).
INSERT INTO team_members (id, team_id, user_id, role, joined_at, accepted_at)
SELECT gen_random_uuid(), t.id, u.id, 'owner', now(), now()
  FROM teams t, users u
 WHERE u.email = 'admin@madfam.io'
   AND NOT EXISTS (
       SELECT 1 FROM team_members tm
        WHERE tm.team_id = t.id AND tm.user_id = u.id
   );

-- 4. Set teams.owner_id to admin@madfam.io for the rows we just created
--    (where the column is still NULL after the bulk insert).
UPDATE teams t
   SET owner_id = (SELECT id FROM users WHERE email = 'admin@madfam.io')
 WHERE t.owner_id IS NULL
   AND EXISTS (SELECT 1 FROM users WHERE email = 'admin@madfam.io');
