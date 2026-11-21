-- Schema version 25: a bot picture can be ticked "use in follow-ups"
-- (additive). A follow-up then sends at most one ticked picture the customer
-- has not had yet, after its text. The removed copy keeps the tick, so
-- putting a picture back from History restores it as it was. A constant
-- default is metadata-only in Postgres 11+, so no table rewrite. Code older
-- than v25 ignores the columns, so rolling it back is safe.
SET LOCAL lock_timeout = '5s';

ALTER TABLE bot_images
    ADD COLUMN IF NOT EXISTS use_in_followup BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE bot_images_removed
    ADD COLUMN IF NOT EXISTS use_in_followup BOOLEAN NOT NULL DEFAULT false;
