-- Schema version 26: where a Messenger chat came from (additive). Facebook
-- sends a referral (a Click-to-Messenger ad, an m.me link with a ref) with
-- the customer's message; Chatwoot drops it, so ai-service saves it on the
-- way through (internal/referral). One row per referral event; the unique
-- key makes a retried delivery a no-op. psid is the customer's Page-scoped
-- Messenger id (personal data): rows are deleted after 90 days. Code older
-- than v26 never reads the table, so rolling it back is safe.
-- Issues: DEV-Johnstocker/chatwoot#1, #155.
SET LOCAL lock_timeout = '5s';

CREATE TABLE IF NOT EXISTS chat_referrals (
    id          BIGSERIAL PRIMARY KEY,
    page_id     TEXT NOT NULL,
    psid        TEXT NOT NULL,
    source      TEXT NOT NULL DEFAULT '',
    ref         TEXT NOT NULL DEFAULT '',
    ad_id       TEXT NOT NULL DEFAULT '',
    ad_title    TEXT NOT NULL DEFAULT '',
    referred_at TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chat_referrals_once UNIQUE (page_id, psid, source, ad_id, ref, referred_at)
);

CREATE INDEX IF NOT EXISTS chat_referrals_customer ON chat_referrals (page_id, psid, referred_at DESC);
CREATE INDEX IF NOT EXISTS chat_referrals_age ON chat_referrals (referred_at);

-- Supabase's API roles never read customer data: the rows are only for
-- ai-service and the owner's /admin, so shut them out (as schema_v23 does).
ALTER TABLE chat_referrals ENABLE ROW LEVEL SECURITY;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') THEN
        REVOKE ALL ON chat_referrals FROM anon;
        REVOKE ALL ON SEQUENCE chat_referrals_id_seq FROM anon;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticated') THEN
        REVOKE ALL ON chat_referrals FROM authenticated;
        REVOKE ALL ON SEQUENCE chat_referrals_id_seq FROM authenticated;
    END IF;
END $$;
