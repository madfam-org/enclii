DROP INDEX IF EXISTS idx_outbound_webhook_deliveries_event;
DROP INDEX IF EXISTS idx_outbound_webhook_deliveries_pending;
DROP INDEX IF EXISTS idx_outbound_webhook_deliveries_subscription;
DROP TABLE IF EXISTS outbound_webhook_deliveries;

DROP INDEX IF EXISTS idx_outbound_webhook_subs_active;
DROP INDEX IF EXISTS idx_outbound_webhook_subs_project;
DROP TABLE IF EXISTS outbound_webhook_subscriptions;
