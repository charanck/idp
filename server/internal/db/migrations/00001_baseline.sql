-- +goose Up
-- Baseline schema matching the existing Django-managed Postgres database
-- (authentication + config_management apps). This does not replay Django's
-- migration history - it's a fresh baseline reflecting the current shape of
-- the schema as-is. On a real cutover, this file's DDL must be reconciled
-- against `pg_dump --schema-only` of the live database before being marked
-- applied (see cmd/migrate-cutover), rather than re-run against a DB that
-- already has these tables.

CREATE TABLE IF NOT EXISTS users (
    id                    uuid PRIMARY KEY,
    password              varchar(128) NOT NULL,
    last_login            timestamptz,
    is_superuser          boolean NOT NULL DEFAULT false,
    username              varchar(150) NOT NULL UNIQUE,
    first_name            varchar(150) NOT NULL DEFAULT '',
    last_name             varchar(150) NOT NULL DEFAULT '',
    email                 varchar(254) NOT NULL UNIQUE,
    is_staff              boolean NOT NULL DEFAULT false,
    is_active             boolean NOT NULL DEFAULT false,
    date_joined           timestamptz NOT NULL DEFAULT now(),
    force_password_reset  boolean NOT NULL DEFAULT false,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS users_created_at_idx ON users (created_at DESC);
CREATE INDEX IF NOT EXISTS users_is_active_created_at_idx ON users (is_active, created_at DESC);

CREATE TABLE IF NOT EXISTS service_clients (
    id              uuid PRIMARY KEY,
    name            varchar(120) NOT NULL UNIQUE,
    api_key_id      varchar(80) UNIQUE,
    api_key_hash    varchar(255) NOT NULL DEFAULT '',
    encryption_key  varchar(255) NOT NULL DEFAULT '',
    is_active       boolean NOT NULL DEFAULT true,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS service_clients_created_at_idx ON service_clients (created_at DESC);
CREATE INDEX IF NOT EXISTS service_clients_is_active_created_at_idx ON service_clients (is_active, created_at DESC);

CREATE TABLE IF NOT EXISTS oauth_providers (
    id                 uuid PRIMARY KEY,
    name               varchar(120) NOT NULL UNIQUE,
    client_id          varchar(255) NOT NULL,
    client_secret      varchar(255) NOT NULL,
    authorization_url  varchar(500) NOT NULL,
    token_url          varchar(500) NOT NULL,
    userinfo_url       varchar(500),
    scope              varchar(255) NOT NULL DEFAULT 'openid email profile',
    is_active          boolean NOT NULL DEFAULT true,
    auto_create_users  boolean NOT NULL DEFAULT true,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS oauth_providers_is_active_created_at_idx ON oauth_providers (is_active, created_at DESC);

CREATE TABLE IF NOT EXISTS oauth_user_tokens (
    id                    uuid PRIMARY KEY,
    user_id               uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider_id           uuid NOT NULL REFERENCES oauth_providers (id) ON DELETE CASCADE,
    access_token          text NOT NULL,
    refresh_token         text,
    token_type            varchar(50) NOT NULL DEFAULT 'Bearer',
    expires_at            timestamptz,
    provider_user_id      varchar(255) NOT NULL,
    provider_user_email   varchar(254),
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, provider_id)
);
CREATE INDEX IF NOT EXISTS oauth_user_tokens_provider_user_idx ON oauth_user_tokens (provider_id, provider_user_id);

CREATE TABLE IF NOT EXISTS applications (
    id          uuid PRIMARY KEY,
    name        varchar(120) NOT NULL UNIQUE,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS environments (
    id              uuid PRIMARY KEY,
    application_id  uuid NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    name            varchar(120) NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (application_id, name)
);
CREATE INDEX IF NOT EXISTS environments_application_name_idx ON environments (application_id, name);

CREATE TABLE IF NOT EXISTS config_entries (
    id              uuid PRIMARY KEY,
    application_id  uuid NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    environment_id  uuid NOT NULL REFERENCES environments (id) ON DELETE CASCADE,
    key             varchar(120) NOT NULL,
    value           text NOT NULL,
    type            varchar(20) NOT NULL DEFAULT 'string',
    is_secret       boolean NOT NULL DEFAULT false,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (application_id, environment_id, key)
);
CREATE INDEX IF NOT EXISTS config_entries_scope_idx ON config_entries (application_id, environment_id);

CREATE TABLE IF NOT EXISTS config_entry_versions (
    id               uuid PRIMARY KEY,
    config_entry_id  uuid NOT NULL REFERENCES config_entries (id) ON DELETE CASCADE,
    value            text NOT NULL,
    type             varchar(20) NOT NULL DEFAULT 'string',
    is_secret        boolean NOT NULL DEFAULT false,
    action           varchar(20) NOT NULL,
    version          integer NOT NULL CHECK (version > 0),
    changed_by       varchar(254),
    created_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (config_entry_id, version)
);
CREATE INDEX IF NOT EXISTS config_entry_versions_entry_version_idx ON config_entry_versions (config_entry_id, version DESC);

CREATE TABLE IF NOT EXISTS feature_flags (
    id              uuid PRIMARY KEY,
    application_id  uuid NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    environment_id  uuid NOT NULL REFERENCES environments (id) ON DELETE CASCADE,
    name            varchar(120) NOT NULL,
    description     text,
    is_enabled      boolean NOT NULL DEFAULT false,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    deleted_at      timestamptz,
    UNIQUE (application_id, environment_id, name)
);
CREATE INDEX IF NOT EXISTS feature_flags_scope_name_idx ON feature_flags (application_id, environment_id, name);
CREATE INDEX IF NOT EXISTS feature_flags_scope_deleted_idx ON feature_flags (application_id, environment_id, deleted_at);
CREATE INDEX IF NOT EXISTS feature_flags_deleted_at_idx ON feature_flags (deleted_at);

CREATE TABLE IF NOT EXISTS activities (
    id             uuid PRIMARY KEY,
    type           varchar(20) NOT NULL,
    resource       varchar(50) NOT NULL,
    resource_id    varchar(255) NOT NULL,
    resource_name  varchar(255),
    user_email     varchar(254),
    details        text,
    ip_address     inet,
    "timestamp"    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS activities_timestamp_resource_idx ON activities ("timestamp" DESC, resource);
CREATE INDEX IF NOT EXISTS activities_user_email_timestamp_idx ON activities (user_email, "timestamp" DESC);
CREATE INDEX IF NOT EXISTS activities_resource_type_timestamp_idx ON activities (resource, type, "timestamp" DESC);
CREATE INDEX IF NOT EXISTS activities_type_idx ON activities (type);
CREATE INDEX IF NOT EXISTS activities_resource_idx ON activities (resource);

-- +goose Down
DROP TABLE IF EXISTS activities;
DROP TABLE IF EXISTS feature_flags;
DROP TABLE IF EXISTS config_entry_versions;
DROP TABLE IF EXISTS config_entries;
DROP TABLE IF EXISTS environments;
DROP TABLE IF EXISTS applications;
DROP TABLE IF EXISTS oauth_user_tokens;
DROP TABLE IF EXISTS oauth_providers;
DROP TABLE IF EXISTS service_clients;
DROP TABLE IF EXISTS users;
