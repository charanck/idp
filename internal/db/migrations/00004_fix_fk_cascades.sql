-- +goose Up
-- Fixes FK constraints inherited from the Django-managed schema that predate
-- the Go baseline. 00001_baseline.sql declares ON DELETE CASCADE on every FK,
-- but it uses CREATE TABLE IF NOT EXISTS, so it never actually runs against
-- a database whose tables already exist (e.g. production, cut over from
-- Django) - those tables kept whatever on_delete behavior Django originally
-- set, which isn't always CASCADE (e.g. deleting a user was failing with
-- "update or delete on table users violates foreign key constraint
-- oauth_user_tokens_user_id_bd03e5c2_fk_users_id"). Constraint names are
-- Django-generated hashes that can differ per environment, so this discovers
-- every non-CASCADE FK in the schema by its actual table/column pairing
-- (rather than a hardcoded name) and rewrites it with ON DELETE CASCADE.
-- +goose StatementBegin
DO $$
DECLARE
    fk RECORD;
BEGIN
    FOR fk IN
        SELECT
            tc.constraint_name,
            tc.table_name,
            kcu.column_name,
            ccu.table_name AS foreign_table_name,
            ccu.column_name AS foreign_column_name,
            rc.delete_rule
        FROM information_schema.table_constraints tc
        JOIN information_schema.key_column_usage kcu
            ON tc.constraint_name = kcu.constraint_name
            AND tc.table_schema = kcu.table_schema
        JOIN information_schema.constraint_column_usage ccu
            ON tc.constraint_name = ccu.constraint_name
            AND tc.table_schema = ccu.table_schema
        JOIN information_schema.referential_constraints rc
            ON tc.constraint_name = rc.constraint_name
            AND tc.table_schema = rc.constraint_schema
        WHERE tc.constraint_type = 'FOREIGN KEY'
          AND tc.table_schema = 'public'
          AND rc.delete_rule <> 'CASCADE'
    LOOP
        EXECUTE format(
            'ALTER TABLE %I DROP CONSTRAINT %I',
            fk.table_name, fk.constraint_name
        );
        EXECUTE format(
            'ALTER TABLE %I ADD CONSTRAINT %I FOREIGN KEY (%I) REFERENCES %I (%I) ON DELETE CASCADE',
            fk.table_name, fk.constraint_name, fk.column_name,
            fk.foreign_table_name, fk.foreign_column_name
        );
        RAISE NOTICE 'fixed cascade on %.% (constraint %, was %)', fk.table_name, fk.column_name, fk.constraint_name, fk.delete_rule;
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- Not reversible: each FK's original on_delete rule (set by Django,
-- per-model) isn't recorded anywhere, so there's nothing safe to restore.
SELECT 1;
