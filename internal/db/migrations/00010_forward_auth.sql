-- +goose Up
-- Maps hostnames a reverse proxy forwards (X-Forwarded-Host) to the
-- Application whose Group allow-list gates access, for the forward-auth
-- verify endpoint (GET /forward-auth/verify). One Application may have
-- several hosts (e.g. a staging + prod domain), mirroring the
-- group_applications join-table pattern.
CREATE TABLE application_domains (
    application_id uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    host text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (application_id, host)
);

-- +goose Down
DROP TABLE application_domains;
