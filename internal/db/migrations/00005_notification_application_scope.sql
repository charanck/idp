-- +goose Up
-- Notifications become application-scoped, mirroring ConfigEntry/FeatureFlag
-- (application_id only - no environment_id, a narrower scope than
-- config/flags). 00002_notifications.sql's own "no tenant isolation" comment
-- confirms no production tenant-scoped rows exist yet, so existing rows are
-- backfilled to a single synthesized "legacy" Application rather than
-- requiring a real migration strategy.
-- +goose StatementBegin
DO $$
DECLARE
    legacy_app_id uuid;
BEGIN
    IF EXISTS (SELECT 1 FROM notifications LIMIT 1) THEN
        INSERT INTO applications (id, name, created_at, updated_at)
        VALUES (gen_random_uuid(), 'legacy', now(), now())
        ON CONFLICT (name) DO NOTHING;

        SELECT id INTO legacy_app_id FROM applications WHERE name = 'legacy';

        UPDATE notifications SET application_id = legacy_app_id WHERE application_id IS NULL;
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE notifications ADD COLUMN IF NOT EXISTS application_id uuid REFERENCES applications (id) ON DELETE CASCADE;
ALTER TABLE notifications ALTER COLUMN application_id SET NOT NULL;
CREATE INDEX IF NOT EXISTS notifications_application_id_idx ON notifications (application_id);

-- +goose Down
DROP INDEX IF EXISTS notifications_application_id_idx;
ALTER TABLE notifications DROP COLUMN IF EXISTS application_id;
