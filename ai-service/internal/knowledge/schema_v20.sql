-- Schema version 20: sales, one row per paying chat (additive).
-- A payment slip the bot reads marks the chat "slip" (not yet checked);
-- staff confirm it in Chatwoot with the label "paid" and the conversation
-- attribute "paid_amount", which turns the row to "paid" with the amount.
-- Removing the label marks it "cancelled". credit says who closed the sale:
-- the bot (no hand-off to staff before the payment) or staff.
-- Amounts are in satang (1/100 baht). Code older than v20 ignores the
-- table, so rolling the code back is safe.
SET LOCAL lock_timeout = '5s';

CREATE TABLE IF NOT EXISTS sales (
    id                 BIGSERIAL PRIMARY KEY,
    account_id         BIGINT NOT NULL CHECK (account_id > 0),
    conversation_id    BIGINT NOT NULL CHECK (conversation_id > 0),
    inbox_id           BIGINT NOT NULL DEFAULT 0 CHECK (inbox_id >= 0),
    status             TEXT NOT NULL CHECK (status IN ('slip', 'paid', 'cancelled')),
    slip_amount_satang BIGINT CHECK (slip_amount_satang >= 0 AND slip_amount_satang < 100000000000),
    amount_satang      BIGINT CHECK (amount_satang >= 0 AND amount_satang < 100000000000),
    credit             TEXT NOT NULL DEFAULT '' CHECK (credit IN ('', 'bot', 'staff')),
    slip_at            TIMESTAMPTZ,
    paid_at            TIMESTAMPTZ,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (account_id, conversation_id)
);

CREATE INDEX IF NOT EXISTS sales_account_paid
    ON sales (account_id, paid_at DESC) WHERE status = 'paid';
CREATE INDEX IF NOT EXISTS sales_account_slip
    ON sales (account_id, slip_at DESC) WHERE status = 'slip';

-- Supabase's API roles get every new table and sequence by default; sales
-- are the owner's (/admin), so shut them out of both.
ALTER TABLE sales ENABLE ROW LEVEL SECURITY;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') THEN
        REVOKE ALL ON sales FROM anon;
        REVOKE ALL ON SEQUENCE sales_id_seq FROM anon;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticated') THEN
        REVOKE ALL ON sales FROM authenticated;
        REVOKE ALL ON SEQUENCE sales_id_seq FROM authenticated;
    END IF;
END $$;
