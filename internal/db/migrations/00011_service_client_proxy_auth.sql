-- +goose Up
-- Moves reverse-proxy host mapping ("proxy auth") from Application onto
-- ServiceClient, mirroring how IsAuthApplication+service_client_allowed_groups
-- already gate OIDC login: a proxied app now registers as a ServiceClient
-- with its own hostnames and an is_proxy_auth_enabled toggle, reusing that
-- same service_client_allowed_groups list to decide who's let through
-- forward-auth. application_domains had no reliable 1:1 Application->
-- ServiceClient mapping to migrate through, so it's dropped as-is.

ALTER TABLE service_clients
    ADD COLUMN is_proxy_auth_enabled boolean NOT NULL DEFAULT false;

CREATE TABLE service_client_domains (
    service_client_id uuid NOT NULL REFERENCES service_clients(id) ON DELETE CASCADE,
    host text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (service_client_id, host)
);

DROP TABLE application_domains;

-- +goose Down
CREATE TABLE application_domains (
    application_id uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    host text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (application_id, host)
);

DROP TABLE service_client_domains;

ALTER TABLE service_clients
    DROP COLUMN is_proxy_auth_enabled;
