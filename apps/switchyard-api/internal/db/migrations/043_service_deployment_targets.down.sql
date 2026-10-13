DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM service_deployment_targets) OR EXISTS (SELECT 1 FROM service_deployment_target_events) THEN
        RAISE EXCEPTION 'cannot remove explicit deployment target history';
    END IF;
END;
$$;
DROP TABLE service_deployment_target_events;
DROP TABLE service_deployment_targets;
DROP FUNCTION refuse_service_deployment_target_rewrite();
