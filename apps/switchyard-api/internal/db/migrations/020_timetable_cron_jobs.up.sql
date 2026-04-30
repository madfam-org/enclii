-- Add jobs column to services table for Timetable (Cron Jobs)
ALTER TABLE public.services ADD COLUMN jobs jsonb DEFAULT '[]'::jsonb NOT NULL;
COMMENT ON COLUMN public.services.jobs IS 'Array of JobSpec definitions for scheduled cron tasks';
