-- 038_ctm_team_crea DOWN
--
-- Reverse the CTM team backfill: drop admin's membership, NULL the two
-- projects' team_id, delete the team.
--
-- WARNING: destructive if data accumulated under the crea team since the up
-- migration (new team_members, other projects parented here). Down migrations
-- are for dev/test; production rollback restores from snapshot.

-- 1. Drop admin's membership of the CTM team.
DELETE FROM team_members
 WHERE user_id = (SELECT id FROM users WHERE email = 'admin@madfam.io')
   AND team_id IN (SELECT id FROM teams WHERE slug = 'crea');

-- 2. NULL the team_id on the two projects.
UPDATE projects SET team_id = NULL
 WHERE team_id IN (SELECT id FROM teams WHERE slug = 'crea')
   AND slug IN ('crea-map', 'crea-frontend');

-- 3. Delete the CTM team.
DELETE FROM teams WHERE slug = 'crea';
