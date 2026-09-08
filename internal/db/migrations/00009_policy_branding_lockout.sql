-- +goose Up
-- Expands the singleton policies row with password/session/login controls,
-- adds account-lockout tracking to users, and a singleton branding row for
-- the (single-tenant-per-deployment) customizable login screen.

ALTER TABLE policies
    ADD COLUMN password_min_length integer NOT NULL DEFAULT 8,
    ADD COLUMN password_require_upper boolean NOT NULL DEFAULT false,
    ADD COLUMN password_require_lower boolean NOT NULL DEFAULT false,
    ADD COLUMN password_require_digit boolean NOT NULL DEFAULT false,
    ADD COLUMN password_require_symbol boolean NOT NULL DEFAULT false,
    ADD COLUMN password_max_age_days integer NOT NULL DEFAULT 0,
    ADD COLUMN max_failed_login_attempts integer NOT NULL DEFAULT 0,
    ADD COLUMN lockout_duration_minutes integer NOT NULL DEFAULT 15,
    ADD COLUMN session_idle_timeout_minutes integer NOT NULL DEFAULT 0,
    ADD COLUMN sso_only boolean NOT NULL DEFAULT false,
    ADD COLUMN login_ip_allowlist text NOT NULL DEFAULT '';

-- password_max_age_days = 0 means passwords never expire;
-- max_failed_login_attempts = 0 means lockout is disabled;
-- session_idle_timeout_minutes = 0 means no idle timeout (session-cookie TTL
-- alone governs expiry); login_ip_allowlist empty = unrestricted - all
-- following the existing "empty/zero = unrestricted" convention already
-- used by self_registration_allowed_domains.

ALTER TABLE users
    ADD COLUMN failed_login_count integer NOT NULL DEFAULT 0,
    ADD COLUMN locked_until timestamptz,
    ADD COLUMN password_changed_at timestamptz;

-- Singleton settings row for login-screen branding, kept separate from
-- policies since it's a distinct concern (cosmetic, not access control).
CREATE TABLE branding (
    id smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    product_name text NOT NULL DEFAULT '',
    logo_url text NOT NULL DEFAULT '',
    accent_color text NOT NULL DEFAULT '',
    background_image_url text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO branding (id) VALUES (1);

-- Grant the built-in Admin group access to the new Branding settings page.
UPDATE groups
SET permissions = permissions || '["branding"]'::jsonb
WHERE name = 'Admin' AND is_system AND NOT permissions @> '["branding"]'::jsonb;

-- +goose Down
UPDATE groups
SET permissions = permissions - 'branding'
WHERE name = 'Admin' AND is_system;

DROP TABLE branding;

ALTER TABLE users
    DROP COLUMN failed_login_count,
    DROP COLUMN locked_until,
    DROP COLUMN password_changed_at;

ALTER TABLE policies
    DROP COLUMN password_min_length,
    DROP COLUMN password_require_upper,
    DROP COLUMN password_require_lower,
    DROP COLUMN password_require_digit,
    DROP COLUMN password_require_symbol,
    DROP COLUMN password_max_age_days,
    DROP COLUMN max_failed_login_attempts,
    DROP COLUMN lockout_duration_minutes,
    DROP COLUMN session_idle_timeout_minutes,
    DROP COLUMN sso_only,
    DROP COLUMN login_ip_allowlist;
