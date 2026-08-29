-- +goose Up
-- Hourly analytics snapshots powering the dashboard's trend charts. Rows are
-- written by internal/analytics's DBOS-scheduled workflow and pruned to a
-- rolling 7-day window on every run, so this table is a sliding window, not
-- an ever-growing history.

CREATE TABLE IF NOT EXISTS analytics_snapshots (
    id                           uuid PRIMARY KEY,
    captured_at                  timestamptz NOT NULL,
    application_count            bigint NOT NULL DEFAULT 0,
    environment_count            bigint NOT NULL DEFAULT 0,
    config_count                 bigint NOT NULL DEFAULT 0,
    secret_count                 bigint NOT NULL DEFAULT 0,
    flag_count                   bigint NOT NULL DEFAULT 0,
    client_count                 bigint NOT NULL DEFAULT 0,
    activity_create_count        bigint NOT NULL DEFAULT 0,
    activity_update_count        bigint NOT NULL DEFAULT 0,
    activity_delete_count        bigint NOT NULL DEFAULT 0,
    activity_login_count         bigint NOT NULL DEFAULT 0,
    activity_login_failed_count  bigint NOT NULL DEFAULT 0,
    notification_sent_count      bigint NOT NULL DEFAULT 0,
    notification_failed_count    bigint NOT NULL DEFAULT 0,
    s2s_request_count            bigint NOT NULL DEFAULT 0,
    created_at                   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS analytics_snapshots_captured_at_idx ON analytics_snapshots (captured_at DESC);

-- +goose Down
DROP TABLE IF EXISTS analytics_snapshots;
