-- Schema version 27: which Messenger customer each chat is with (additive).
-- Chatwoot's webhook carries the customer's Page-scoped id (contact_inbox
-- source_id); keeping it per chat lets /admin match a chat to the ad or m.me
-- link the customer came from (chat_referrals, v26). An account only ever
-- sees referrals of customers who chatted in its own inboxes. psid is
-- personal data: rows not seen for 90 days are deleted. Code older than v27
-- never reads the table, so rolling it back is safe.
-- Issues: DEV-Johnstocker/chatwoot#1, #156.
SET LOCAL lock_timeout = '5s';

CREATE TABLE IF NOT EXISTS chat_customers (
    account_id      BIGINT      NOT NULL CHECK (account_id > 0),
    conversation_id BIGINT      NOT NULL CHECK (conversation_id > 0),
    inbox_id        BIGINT      NOT NULL DEFAULT 0 CHECK (inbox_id >= 0),
    psid            TEXT        NOT NULL CHECK (char_length(psid) BETWEEN 1 AND 64),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, conversation_id)
);

CREATE INDEX IF NOT EXISTS chat_customers_psid ON chat_customers (account_id, psid);
CREATE INDEX IF NOT EXISTS chat_customers_age ON chat_customers (updated_at);

-- Supabase's API roles never read customer data (as schema_v23 does).
ALTER TABLE chat_customers ENABLE ROW LEVEL SECURITY;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') THEN
        REVOKE ALL ON chat_customers FROM anon;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticated') THEN
        REVOKE ALL ON chat_customers FROM authenticated;
    END IF;
END $$;
