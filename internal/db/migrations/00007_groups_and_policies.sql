-- +goose Up
-- Groups are the sole access-control primitive: a user's effective admin
-- permissions are the union of their groups' "permissions" module lists, and
-- (optionally) a group can restrict members to a subset of Applications.
-- Admin/User are built-in, non-deletable groups seeded below to reproduce
-- today's exact is_staff-based access split.

CREATE TABLE groups (
    id uuid PRIMARY KEY,
    name varchar(255) NOT NULL UNIQUE,
    is_system boolean NOT NULL DEFAULT false,
    permissions jsonb NOT NULL DEFAULT '[]',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_groups (
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, group_id)
);

-- Empty for a group = unrestricted (all applications), matching today's
-- behavior where any logged-in user can reach every application's
-- configs/flags.
CREATE TABLE group_applications (
    group_id uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    application_id uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, application_id)
);

-- Singleton settings row, extensible with more policy columns later.
CREATE TABLE policies (
    id smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    self_registration_allowed_domains text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO policies (id) VALUES (1);

INSERT INTO groups (id, name, is_system, permissions) VALUES
    (gen_random_uuid(), 'Admin', true,
     '["applications","environments","configs","flags","service_clients","users","groups","oauth_providers","policies","notification_settings","activity_log"]'),
    (gen_random_uuid(), 'User', true, '["configs","flags"]');

INSERT INTO user_groups (user_id, group_id)
SELECT u.id, g.id FROM users u JOIN groups g ON g.name = 'Admin' AND g.is_system WHERE u.is_staff = true;
INSERT INTO user_groups (user_id, group_id)
SELECT u.id, g.id FROM users u JOIN groups g ON g.name = 'User' AND g.is_system WHERE u.is_staff = false;

-- +goose Down
DROP TABLE policies;
DROP TABLE group_applications;
DROP TABLE user_groups;
DROP TABLE groups;
