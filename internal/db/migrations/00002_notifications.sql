-- +goose Up
-- Notification module: S2S-created notifications delivered asynchronously
-- via ASYNQ over skeleton Email/SMS/WhatsApp channels. No tenant isolation
-- (see plan) - idempotency_key is a plain unique column, not scoped further.

CREATE TABLE IF NOT EXISTS notifications (
    id                   uuid PRIMARY KEY,
    channel              varchar(20) NOT NULL,
    recipient            jsonb NOT NULL,
    content              jsonb NOT NULL,
    status               varchar(20) NOT NULL DEFAULT 'queued',
    provider             varchar(50),
    provider_message_id  varchar(255),
    attempt              integer NOT NULL DEFAULT 0,
    idempotency_key      varchar(255) UNIQUE,
    error                text,
    read_at              timestamptz,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS notifications_status_created_idx ON notifications (status, created_at DESC);
CREATE INDEX IF NOT EXISTS notifications_channel_created_idx ON notifications (channel, created_at DESC);
-- Recipient stays arbitrary per-channel jsonb (see model doc comment); this
-- expression index supports "unread notifications for this external
-- recipient user_id" lookups without adding a dedicated column.
CREATE INDEX IF NOT EXISTS notifications_recipient_user_id_idx ON notifications ((recipient ->> 'user_id'));

CREATE TABLE IF NOT EXISTS notification_provider_settings (
    id           uuid PRIMARY KEY,
    channel      varchar(20) NOT NULL UNIQUE,
    config       jsonb NOT NULL DEFAULT '{}'::jsonb,
    credentials  text NOT NULL DEFAULT '',
    is_active    boolean NOT NULL DEFAULT true,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS notification_provider_settings;
DROP TABLE IF EXISTS notifications;
