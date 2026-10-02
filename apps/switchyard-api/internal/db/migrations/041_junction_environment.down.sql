DROP INDEX IF EXISTS public.idx_junctions_environment_id;
ALTER TABLE public.junctions DROP COLUMN IF EXISTS environment_id;
