-- Schema version 23: leads, one row per chat that showed buying interest
-- (additive). The bot adds a row when a customer asks about price, buying or
-- a promotion ("asked_price") and moves it to "quoted" when its reply gives a
-- price or a link. The /admin Leads page lists these rows next to the
-- Facebook comment leads in fb_comments; the sales table, when present, moves
-- a row on to "slip" or "paid".
-- contact_name and contact_id are personal data, kept only for the owner's
-- lead list; leads_account_contact finds one customer's rows for a PDPA
-- request. Code older than v23 ignores the table, so rolling the code back
-- is safe.
SET LOCAL lock_timeout = '5s';

CREATE TABLE IF NOT EXISTS leads (
    id              BIGSERIAL PRIMARY KEY,
    account_id      BIGINT NOT NULL CHECK (account_id > 0),
    conversation_id BIGINT NOT NULL CHECK (conversation_id > 0),
    inbox_id        BIGINT NOT NULL DEFAULT 0 CHECK (inbox_id >= 0),
    contact_id      BIGINT NOT NULL DEFAULT 0 CHECK (contact_id >= 0),
    contact_name    TEXT NOT NULL DEFAULT '' CHECK (char_length(contact_name) <= 256),
    interest        TEXT NOT NULL DEFAULT '' CHECK (char_length(interest) <= 200),
    stage           TEXT NOT NULL CHECK (stage IN ('asked_price', 'quoted', 'slip', 'paid')),
    first_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_contact_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (account_id, conversation_id)
);

CREATE INDEX IF NOT EXISTS leads_account_last
    ON leads (account_id, last_contact_at DESC);
CREATE INDEX IF NOT EXISTS leads_account_contact
    ON leads (account_id, contact_id);

-- Supabase's API roles get every new table and sequence by default; leads
-- are the owner's (/admin), so shut them out of both.
ALTER TABLE leads ENABLE ROW LEVEL SECURITY;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') THEN
        REVOKE ALL ON leads FROM anon;
        REVOKE ALL ON SEQUENCE leads_id_seq FROM anon;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticated') THEN
        REVOKE ALL ON leads FROM authenticated;
        REVOKE ALL ON SEQUENCE leads_id_seq FROM authenticated;
    END IF;
END $$;
