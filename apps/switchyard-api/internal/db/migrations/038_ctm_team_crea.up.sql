-- 038_ctm_team_crea
--
-- Create the Crea Tu Mundo (CTM) tenant team and parent its two apps to it.
--
-- WHY. crea-frontend and crea-map are the CLIENT's property — CTM hired MADFAM
-- to migrate crea-frontend off Wix and to develop crea-map. As part of CTM's
-- Nauta vCTO engagement they get enclii as a client-facing tenant to provision
-- their own staging + prod frontend and map; MADFAM mounts and OPERATES it on
-- their behalf during vCTO (admin@madfam.io keeps operative control via the
-- master-admin act-as flow), and CTM would self-manage only if they downgrade
-- vCTO → ERP. Both projects were created (2026-08-17 / 2026-08-20) AFTER the
-- 2026-05-02 audit that migration 024 backfilled, so they are team_id IS NULL
-- ("personal") today. This gives them their tenant home.
--
-- This mirrors 024's shape exactly and is FULLY IDEMPOTENT (safe to re-run).
-- admin@madfam.io is made owner so the master admin can enter the tenant and
-- operate it; a CTM-side owner is added later (a team_members row) if/when the
-- client goes hands-on.

-- 1. The CTM team. tier=client, like the white-glove clients in 024.
INSERT INTO teams (id, name, slug, description, settings, created_at, updated_at) VALUES
    (gen_random_uuid(), 'Crea Tu Mundo', 'crea', 'Crea Tu Mundo Autismo — client site (crea-frontend) + clinical MAP (crea-map)', '{"tier":"client"}'::jsonb, now(), now())
ON CONFLICT (slug) DO NOTHING;

-- 2. Re-parent CTM's two projects. UPDATE WHERE team_id IS NULL keeps re-runs
--    safe and never stomps a project already parented elsewhere.
UPDATE projects SET team_id = (SELECT id FROM teams WHERE slug = 'crea')
 WHERE slug IN ('crea-map', 'crea-frontend') AND team_id IS NULL;

-- 3. admin@madfam.io is owner of the CTM team (operative control via act-as).
--    The user must already exist (Janua SSO on first login); a no-op otherwise.
INSERT INTO team_members (id, team_id, user_id, role, joined_at, accepted_at)
SELECT gen_random_uuid(), t.id, u.id, 'owner', now(), now()
  FROM teams t, users u
 WHERE t.slug = 'crea'
   AND u.email = 'admin@madfam.io'
   AND NOT EXISTS (
       SELECT 1 FROM team_members tm
        WHERE tm.team_id = t.id AND tm.user_id = u.id
   );

-- 4. Set teams.owner_id to admin@madfam.io for the CTM team if still NULL.
UPDATE teams t
   SET owner_id = (SELECT id FROM users WHERE email = 'admin@madfam.io')
 WHERE t.slug = 'crea'
   AND t.owner_id IS NULL
   AND EXISTS (SELECT 1 FROM users WHERE email = 'admin@madfam.io');
