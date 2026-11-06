-- 047_resource_daily (down)
--
-- Drops the daily resource ledger only while it is empty. Prometheus keeps
-- days, not months: a captured day that is dropped here cannot be captured
-- again, so refuse instead of erasing history.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM resource_daily_captures)
    OR EXISTS (SELECT 1 FROM resource_daily)
    OR EXISTS (SELECT 1 FROM node_daily) THEN
  RAISE EXCEPTION 'resource_daily holds captured days; refusing to drop it';
 END IF;
END; $$;

DROP TABLE IF EXISTS resource_daily_captures;
DROP TABLE IF EXISTS node_daily;
DROP TABLE IF EXISTS resource_daily;
