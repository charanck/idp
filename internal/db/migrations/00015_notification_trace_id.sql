-- +goose Up
-- Trace ID of the API request that queued the notification (nullable: rows
-- created before this migration, or without a request context, have none).
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS trace_id text;

-- +goose Down
ALTER TABLE notifications DROP COLUMN IF EXISTS trace_id;
