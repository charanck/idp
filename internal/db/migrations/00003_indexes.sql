-- +goose Up
-- Indexes for columns filtered directly by service-layer queries that
-- previously had no supporting index (see internal/auth, internal/config,
-- internal/notification service .go files for the corresponding queries).

CREATE INDEX IF NOT EXISTS users_is_staff_idx ON users (is_staff);
CREATE INDEX IF NOT EXISTS service_clients_is_active_idx ON service_clients (is_active);
CREATE INDEX IF NOT EXISTS config_entries_is_secret_idx ON config_entries (is_secret);
CREATE INDEX IF NOT EXISTS feature_flags_is_enabled_idx ON feature_flags (is_enabled);

-- Partial composite index matching ConsumeUnreadInAppForUser's actual
-- predicate (channel = 'inapp' AND recipient ->> 'user_id' = ? AND
-- read_at IS NULL, ordered by created_at DESC) more directly than the
-- existing notifications_recipient_user_id_idx expression index alone.
CREATE INDEX IF NOT EXISTS notifications_unread_recipient_idx ON notifications
    ((recipient ->> 'user_id'), created_at DESC)
    WHERE read_at IS NULL AND channel = 'inapp';

-- +goose Down
DROP INDEX IF EXISTS notifications_unread_recipient_idx;
DROP INDEX IF EXISTS feature_flags_is_enabled_idx;
DROP INDEX IF EXISTS config_entries_is_secret_idx;
DROP INDEX IF EXISTS service_clients_is_active_idx;
DROP INDEX IF EXISTS users_is_staff_idx;
