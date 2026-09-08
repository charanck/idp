-- +goose Up
-- Introduces a three-tier hierarchy: User < Developer < Admin.
--   - Developer is new, seeded with exactly what the built-in User group
--     granted until now ("configs","flags"), plus "dashboard" - dashboard
--     access becomes a real module gate rather than "any logged-in user".
--   - Admin additionally gains "dashboard" (it already had every other
--     module).
--   - User is repurposed down to zero modules: an authenticated identity
--     with no web UI access beyond its own profile page. It remains the
--     default group for newly-created users.
-- Existing User-group members are moved to Developer so nobody already
-- relying on today's configs/flags access is locked out by this change;
-- only new signups from here on default into the now-empty User group.

INSERT INTO groups (id, name, is_system, permissions)
VALUES (gen_random_uuid(), 'Developer', true, '["dashboard","configs","flags"]');

UPDATE groups
SET permissions = permissions || '["dashboard"]'::jsonb
WHERE name = 'Admin' AND is_system AND NOT permissions @> '["dashboard"]'::jsonb;

INSERT INTO user_groups (user_id, group_id)
SELECT ug.user_id, dev.id
FROM user_groups ug
JOIN groups u ON u.id = ug.group_id AND u.name = 'User' AND u.is_system
JOIN groups dev ON dev.name = 'Developer' AND dev.is_system
ON CONFLICT DO NOTHING;

DELETE FROM user_groups
WHERE group_id = (SELECT id FROM groups WHERE name = 'User' AND is_system);

UPDATE groups SET permissions = '[]'
WHERE name = 'User' AND is_system;

-- +goose Down
UPDATE groups SET permissions = '["configs","flags"]'
WHERE name = 'User' AND is_system;

INSERT INTO user_groups (user_id, group_id)
SELECT ug.user_id, u.id
FROM user_groups ug
JOIN groups dev ON dev.id = ug.group_id AND dev.name = 'Developer' AND dev.is_system
JOIN groups u ON u.name = 'User' AND u.is_system
ON CONFLICT DO NOTHING;

UPDATE groups
SET permissions = permissions - 'dashboard'
WHERE name = 'Admin' AND is_system;

DELETE FROM groups WHERE name = 'Developer' AND is_system;
