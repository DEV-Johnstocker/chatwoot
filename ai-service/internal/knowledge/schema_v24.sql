-- Schema version 24: a per-business switch for human-like typing time
-- (additive). On: the bot keeps "typing…" on for about as long as a person
-- would take to type its reply (TYPING_SPEED, TYPING_MAX). Off (the default):
-- the reply goes out once the customer has been quiet for REPLY_DELAY (4s),
-- as before. A constant default is metadata-only in Postgres 11+, so no table
-- rewrite. Code older than v24 ignores the column, so rolling it back is safe.
SET LOCAL lock_timeout = '5s';

ALTER TABLE business_profiles
    ADD COLUMN IF NOT EXISTS typing_enabled BOOLEAN NOT NULL DEFAULT false;
