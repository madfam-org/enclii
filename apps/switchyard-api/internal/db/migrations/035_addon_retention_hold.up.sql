-- Retention hold on managed database deletion (2026-08-17 audit #10).
--
-- Deleting a managed-Postgres addon used to immediately tear down the
-- CloudNativePG Cluster, and CNPG's default reclaim behavior destroys the
-- cluster PVCs — a single delete irreversibly nuked the client's production
-- data with no grace window. This migration adds the storage for a
-- retention-hold state: a delete marks the addon `pending_deletion` and
-- stamps `deletion_scheduled_at`, and the data-bearing K8s resources are kept
-- until that time elapses (or an explicit force-delete). See
-- AddonService.DeleteAddonBy and the addon reconciler's retention sweep.

ALTER TABLE database_addons
    ADD COLUMN IF NOT EXISTS deletion_scheduled_at timestamp with time zone;

COMMENT ON COLUMN database_addons.deletion_scheduled_at IS
    'When a pending_deletion addon becomes eligible for hard teardown. NON-NULL only while the addon is in the retention-hold window; the CNPG Cluster + PVCs are retained until this time passes. NULL otherwise.';

-- Admit the new pending_deletion status. Rebuild the CHECK constraint because
-- the genesis constraint enumerates the allowed set and would reject the new
-- value. Drop-if-exists then re-add keeps this idempotent and re-runnable.
ALTER TABLE database_addons
    DROP CONSTRAINT IF EXISTS valid_addon_status;

ALTER TABLE database_addons
    ADD CONSTRAINT valid_addon_status CHECK (
        (status)::text = ANY ((ARRAY[
            'pending'::character varying,
            'provisioning'::character varying,
            'ready'::character varying,
            'failed'::character varying,
            'pending_deletion'::character varying,
            'deleting'::character varying,
            'deleted'::character varying
        ])::text[])
    );

-- The reconciler sweeps for retention-hold addons whose window has elapsed.
-- Partial index keeps that scan cheap — only rows actually in the hold state
-- carry a non-null deletion_scheduled_at.
CREATE INDEX IF NOT EXISTS idx_database_addons_deletion_scheduled_at
    ON database_addons (deletion_scheduled_at)
    WHERE deletion_scheduled_at IS NOT NULL AND deleted_at IS NULL;
