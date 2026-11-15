-- Schema version 19: the owner's ratings of bot replies (additive).
-- One row per rated bot message: thumbs up or down, a reason for a thumbs
-- down, an optional note, and copies of the customer's message and the bot's
-- reply as they were, so the ratings can later become test cases even after
-- the chat is gone from Chatwoot. Rating the same reply again replaces it.
-- Code older than v19 ignores the table, so rolling the code back is safe.
SET LOCAL lock_timeout = '5s';

CREATE TABLE IF NOT EXISTS reply_ratings (
    id              BIGSERIAL PRIMARY KEY,
    account_id      BIGINT NOT NULL CHECK (account_id > 0),
    conversation_id BIGINT NOT NULL CHECK (conversation_id > 0),
    message_id      BIGINT NOT NULL CHECK (message_id > 0),
    rating          SMALLINT NOT NULL CHECK (rating IN (-1, 1)),
    reason          TEXT NOT NULL DEFAULT ''
                    CHECK (reason IN ('', 'wrong_fact', 'missed_sale', 'handoff', 'tone', 'too_long', 'other')),
    note            TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 1000),
    customer_text   TEXT NOT NULL DEFAULT '' CHECK (length(customer_text) <= 2000),
    bot_text        TEXT NOT NULL DEFAULT '' CHECK (length(bot_text) <= 4000),
    rated_by        TEXT NOT NULL DEFAULT '' CHECK (length(rated_by) <= 200),
    replied_at      TIMESTAMPTZ,
    rated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (account_id, message_id)
);

CREATE INDEX IF NOT EXISTS reply_ratings_account_rated
    ON reply_ratings (account_id, rated_at DESC);

-- Supabase's API roles get every new table by default; ratings are the
-- owner's (/admin), so shut them out as earlier tables do.
ALTER TABLE reply_ratings ENABLE ROW LEVEL SECURITY;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') THEN
        REVOKE ALL ON reply_ratings FROM anon;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticated') THEN
        REVOKE ALL ON reply_ratings FROM authenticated;
    END IF;
END $$;
