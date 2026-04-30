-- Remove jobs column from services table
ALTER TABLE public.services DROP COLUMN IF EXISTS jobs;
