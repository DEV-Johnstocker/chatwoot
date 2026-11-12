-- Schema version 15: AI apps may propose a new source (additive).
-- add_source over MCP no longer adds a page or data file at once: it waits in
-- ai_pending_changes as kind 'source' (target = the address) until the owner
-- approves it on /admin. This only widens the allowed kinds; existing rows
-- are untouched. Code older than v15 does not know a 'source' row: its
-- /admin shows it as a blank card, and approving it there drops the
-- proposal (nothing else changes). See deploy/README.md before rolling back.
--
-- v12 has two checks that mention kind: the list of kinds (named
-- ai_pending_changes_kind_check by Postgres) and "one profile row per
-- account". The old list is dropped only after checking that the name points
-- at it, so the profile rule can never be dropped by mistake.
SET LOCAL lock_timeout = '5s';

DO $$
DECLARE
    def TEXT;
BEGIN
    SELECT pg_get_constraintdef(oid) INTO def FROM pg_constraint
     WHERE conrelid = 'ai_pending_changes'::regclass AND conname = 'ai_pending_changes_kind_check';
    IF def IS NULL THEN
        RAISE EXCEPTION 'schema v15: ai_pending_changes_kind_check not found';
    END IF;
    IF def NOT LIKE '%''document''%' OR def LIKE '%target%' THEN
        RAISE EXCEPTION 'schema v15: ai_pending_changes_kind_check is not the list of kinds: %', def;
    END IF;
    ALTER TABLE ai_pending_changes DROP CONSTRAINT ai_pending_changes_kind_check;
    ALTER TABLE ai_pending_changes ADD CONSTRAINT ai_pending_changes_kind_check
        CHECK (kind IN ('profile', 'document', 'source'));
END $$;
