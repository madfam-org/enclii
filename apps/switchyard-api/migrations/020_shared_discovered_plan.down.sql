-- Reverse of 020_shared_discovered_plan.up.sql.
--
-- Safe to run only if no `database_addons` rows reference this plan.
-- The fk_database_addons_plan FK has ON DELETE RESTRICT, so PostgreSQL
-- will refuse the DELETE if any addon still points at this code.

DELETE FROM public.managed_db_plans WHERE code = 'shared-discovered';
