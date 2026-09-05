-- +goose Up
-- Extends service_clients into an OIDC Identity Provider: an "auth
-- application" (relying party) redirects its users here to log in, using the
-- same client_id/client_secret it already has for S2S config/flag reads
-- (api_key_id / the secret behind api_key_hash) - no new credentials.

ALTER TABLE service_clients
    ADD COLUMN is_auth_application boolean NOT NULL DEFAULT false,
    ADD COLUMN require_consent boolean NOT NULL DEFAULT false;

-- Config/flag S2S read scope for a service client; empty = unrestricted
-- (mirrors group_applications' "empty = unrestricted" convention).
CREATE TABLE service_client_applications (
    service_client_id uuid NOT NULL REFERENCES service_clients(id) ON DELETE CASCADE,
    application_id uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    PRIMARY KEY (service_client_id, application_id)
);

CREATE TABLE service_client_redirect_uris (
    id uuid PRIMARY KEY,
    service_client_id uuid NOT NULL REFERENCES service_clients(id) ON DELETE CASCADE,
    redirect_uri text NOT NULL
);
CREATE INDEX service_client_redirect_uris_client_idx ON service_client_redirect_uris (service_client_id);

-- Who may log into this auth application; empty = any directory user.
CREATE TABLE service_client_allowed_groups (
    service_client_id uuid NOT NULL REFERENCES service_clients(id) ON DELETE CASCADE,
    group_id uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    PRIMARY KEY (service_client_id, group_id)
);

-- Singleton RSA-2048 keypair used to sign every issued ID/access token,
-- generated once on first use. The private key is encrypted at rest via the
-- same Fernet MASTER_ENCRYPTION_KEY mechanism used for config secrets.
CREATE TABLE oidc_signing_keys (
    id smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    kid text NOT NULL,
    encrypted_private_key text NOT NULL,
    public_key_pem text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE oidc_authorization_codes (
    code text PRIMARY KEY,
    service_client_id uuid NOT NULL REFERENCES service_clients(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    redirect_uri text NOT NULL,
    scope text NOT NULL,
    nonce text,
    used boolean NOT NULL DEFAULT false,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX oidc_authorization_codes_expires_at_idx ON oidc_authorization_codes (expires_at);

-- +goose Down
DROP TABLE oidc_authorization_codes;
DROP TABLE oidc_signing_keys;
DROP TABLE service_client_allowed_groups;
DROP TABLE service_client_redirect_uris;
DROP TABLE service_client_applications;
ALTER TABLE service_clients
    DROP COLUMN require_consent,
    DROP COLUMN is_auth_application;
