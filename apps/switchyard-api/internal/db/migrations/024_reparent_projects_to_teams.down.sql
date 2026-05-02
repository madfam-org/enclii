-- 024_reparent_projects_to_teams DOWN
--
-- Reverse the backfill. Drops the team_members rows that linked
-- admin@madfam.io to each backfilled team, NULLs out projects.team_id
-- for the matching slugs, then deletes the teams rows.
--
-- WARNING: this is destructive if other data has accumulated under these
-- team_ids since the up migration ran (new team_members, new projects
-- explicitly parented under one of these teams). Down migrations are
-- typically only run in dev/test environments — production rollback
-- should restore from snapshot.

-- 1. Drop team_members for admin on the backfilled teams.
DELETE FROM team_members
 WHERE user_id = (SELECT id FROM users WHERE email = 'admin@madfam.io')
   AND team_id IN (SELECT id FROM teams WHERE slug IN (
       'dhanam', 'factlas', 'karafiel', 'pravara-mes', 'routecraft',
       'tezca', 'forgesight', 'yantra4d', 'symbiosis-hcm', 'nuit-one',
       'forj', 'coforma-studio', 'blueprint-harvester', 'bloom-scroll',
       'ceq', 'digifab-quoting', 'primavera3d', 'fortuna', 'avala',
       'madfam-platform'
   ));

-- 2. NULL the team_id on projects we re-parented.
UPDATE projects SET team_id = NULL WHERE team_id IN (
    SELECT id FROM teams WHERE slug IN (
        'dhanam', 'factlas', 'karafiel', 'pravara-mes', 'routecraft',
        'tezca', 'forgesight', 'yantra4d', 'symbiosis-hcm', 'nuit-one',
        'forj', 'coforma-studio', 'blueprint-harvester', 'bloom-scroll',
        'ceq', 'digifab-quoting', 'primavera3d', 'fortuna', 'avala',
        'madfam-platform'
    )
);

-- 3. Drop the teams rows we created.
DELETE FROM teams WHERE slug IN (
    'dhanam', 'factlas', 'karafiel', 'pravara-mes', 'routecraft',
    'tezca', 'forgesight', 'yantra4d', 'symbiosis-hcm', 'nuit-one',
    'forj', 'coforma-studio', 'blueprint-harvester', 'bloom-scroll',
    'ceq', 'digifab-quoting', 'primavera3d', 'fortuna', 'avala',
    'madfam-platform'
);
