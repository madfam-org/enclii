-- 046_platform_ci_project (down)
--
-- Removes the platform-ci project only while nothing has been metered or
-- deployed under it. usage_events, hourly_usage and daily_usage cascade on
-- project delete, so deleting a project that holds metered minutes would
-- silently erase that history; refuse instead.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM usage_events WHERE project_id = '02799103-7f4d-443a-af62-b4ba6015a92c')
    OR EXISTS (SELECT 1 FROM services WHERE project_id = '02799103-7f4d-443a-af62-b4ba6015a92c') THEN
  RAISE EXCEPTION 'platform-ci holds metered usage or services; refusing to delete it';
 END IF;
 DELETE FROM projects WHERE id = '02799103-7f4d-443a-af62-b4ba6015a92c' AND slug = 'platform-ci';
END; $$;
