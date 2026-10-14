-- +goose NO TRANSACTION
-- +goose Up
-- Performance indexes (built CONCURRENTLY so writes aren't blocked, hence
-- NO TRANSACTION). Three groups: indexes backing foreign keys (cascade
-- deletes and reverse lookups otherwise seq-scan the child table), indexes
-- matching real query shapes (analytics counts, dashboard recency, retention
-- deletes, notification lists), and trigram indexes for ILIKE '%x%' search.

-- FK-supporting indexes.
CREATE INDEX CONCURRENTLY IF NOT EXISTS config_entries_environment_id_idx ON config_entries (environment_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS feature_flags_environment_id_idx ON feature_flags (environment_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS user_groups_group_id_idx ON user_groups (group_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS group_applications_application_id_idx ON group_applications (application_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS service_client_applications_application_id_idx ON service_client_applications (application_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS service_client_allowed_groups_group_id_idx ON service_client_allowed_groups (group_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS oidc_authorization_codes_service_client_id_idx ON oidc_authorization_codes (service_client_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS oidc_authorization_codes_user_id_idx ON oidc_authorization_codes (user_id);

-- Query-shaped indexes.
CREATE INDEX CONCURRENTLY IF NOT EXISTS activities_type_timestamp_idx ON activities (type, "timestamp" DESC);
CREATE INDEX CONCURRENTLY IF NOT EXISTS notifications_created_at_idx ON notifications (created_at DESC);
CREATE INDEX CONCURRENTLY IF NOT EXISTS notifications_status_updated_at_idx ON notifications (status, updated_at);
CREATE INDEX CONCURRENTLY IF NOT EXISTS notifications_application_created_at_idx ON notifications (application_id, created_at DESC);
CREATE INDEX CONCURRENTLY IF NOT EXISTS config_entries_updated_at_idx ON config_entries (updated_at DESC);

-- Soft-delete: every feature_flags read filters deleted_at IS NULL.
CREATE INDEX CONCURRENTLY IF NOT EXISTS feature_flags_active_scope_name_idx
    ON feature_flags (application_id, environment_id, name) WHERE deleted_at IS NULL;

-- Trigram indexes for ILIKE '%x%' searches on the large/growing tables.
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX CONCURRENTLY IF NOT EXISTS activities_user_email_trgm_idx ON activities USING gin (user_email gin_trgm_ops);
CREATE INDEX CONCURRENTLY IF NOT EXISTS users_email_trgm_idx ON users USING gin (email gin_trgm_ops);
CREATE INDEX CONCURRENTLY IF NOT EXISTS config_entries_key_trgm_idx ON config_entries USING gin (key gin_trgm_ops);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS config_entries_key_trgm_idx;
DROP INDEX CONCURRENTLY IF EXISTS users_email_trgm_idx;
DROP INDEX CONCURRENTLY IF EXISTS activities_user_email_trgm_idx;
DROP INDEX CONCURRENTLY IF EXISTS feature_flags_active_scope_name_idx;
DROP INDEX CONCURRENTLY IF EXISTS config_entries_updated_at_idx;
DROP INDEX CONCURRENTLY IF EXISTS notifications_application_created_at_idx;
DROP INDEX CONCURRENTLY IF EXISTS notifications_status_updated_at_idx;
DROP INDEX CONCURRENTLY IF EXISTS notifications_created_at_idx;
DROP INDEX CONCURRENTLY IF EXISTS activities_type_timestamp_idx;
DROP INDEX CONCURRENTLY IF EXISTS oidc_authorization_codes_user_id_idx;
DROP INDEX CONCURRENTLY IF EXISTS oidc_authorization_codes_service_client_id_idx;
DROP INDEX CONCURRENTLY IF EXISTS service_client_allowed_groups_group_id_idx;
DROP INDEX CONCURRENTLY IF EXISTS service_client_applications_application_id_idx;
DROP INDEX CONCURRENTLY IF EXISTS group_applications_application_id_idx;
DROP INDEX CONCURRENTLY IF EXISTS user_groups_group_id_idx;
DROP INDEX CONCURRENTLY IF EXISTS feature_flags_environment_id_idx;
DROP INDEX CONCURRENTLY IF EXISTS config_entries_environment_id_idx;
