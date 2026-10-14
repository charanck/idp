-- +goose NO TRANSACTION
-- +goose Up
-- Drop indexes that exactly duplicate, or are a leading prefix of, another
-- index/unique constraint on the same table. They only add write cost.
DROP INDEX CONCURRENTLY IF EXISTS environments_application_name_idx;          -- = UNIQUE(application_id, name)
DROP INDEX CONCURRENTLY IF EXISTS config_entries_scope_idx;                   -- prefix of UNIQUE(application_id, environment_id, key)
DROP INDEX CONCURRENTLY IF EXISTS config_entry_versions_entry_version_idx;    -- = UNIQUE(config_entry_id, version), btree scans backward
DROP INDEX CONCURRENTLY IF EXISTS feature_flags_scope_name_idx;               -- = UNIQUE(application_id, environment_id, name)
DROP INDEX CONCURRENTLY IF EXISTS feature_flags_deleted_at_idx;               -- mostly-NULL btree; replaced by partial index in 00013
DROP INDEX CONCURRENTLY IF EXISTS activities_resource_idx;                    -- prefix of (resource, type, timestamp)
DROP INDEX CONCURRENTLY IF EXISTS activities_type_idx;                        -- prefix of (type, timestamp) from 00013
DROP INDEX CONCURRENTLY IF EXISTS service_clients_is_active_idx;              -- prefix of (is_active, created_at)

-- +goose Down
CREATE INDEX CONCURRENTLY IF NOT EXISTS environments_application_name_idx ON environments (application_id, name);
CREATE INDEX CONCURRENTLY IF NOT EXISTS config_entries_scope_idx ON config_entries (application_id, environment_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS config_entry_versions_entry_version_idx ON config_entry_versions (config_entry_id, version DESC);
CREATE INDEX CONCURRENTLY IF NOT EXISTS feature_flags_scope_name_idx ON feature_flags (application_id, environment_id, name);
CREATE INDEX CONCURRENTLY IF NOT EXISTS feature_flags_deleted_at_idx ON feature_flags (deleted_at);
CREATE INDEX CONCURRENTLY IF NOT EXISTS activities_resource_idx ON activities (resource);
CREATE INDEX CONCURRENTLY IF NOT EXISTS activities_type_idx ON activities (type);
CREATE INDEX CONCURRENTLY IF NOT EXISTS service_clients_is_active_idx ON service_clients (is_active);
