-- Schema version 22: AI spend (additive).
-- ai_usage: one row per answered model call (tokens, model, purpose). No
-- message text and nothing about the customer; costs are worked out from a
-- dated price table in code when the page is read.
-- ai_spend_caps: the owner's daily and monthly caps, per business or for
-- the whole service (account_id 0). Caps only alert; the bot keeps answering.
-- ai_spend_alerts: which 80% / 100% alerts were already sent, so each one
-- goes out once per day or month, also across restarts and replicas.
-- Code older than v22 ignores these tables, so rolling the code back is safe.
SET LOCAL lock_timeout = '5s';

CREATE TABLE IF NOT EXISTS ai_usage (
    id            BIGSERIAL PRIMARY KEY,
    account_id    BIGINT NOT NULL DEFAULT 0 CHECK (account_id >= 0),
    purpose       TEXT NOT NULL CHECK (length(purpose) BETWEEN 1 AND 40),
    model         TEXT NOT NULL DEFAULT '' CHECK (length(model) <= 200),
    input_tokens  INT NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    cached_tokens INT NOT NULL DEFAULT 0 CHECK (cached_tokens >= 0),
    output_tokens INT NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    audio_seconds REAL NOT NULL DEFAULT 0 CHECK (audio_seconds >= 0),
    at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS ai_usage_at ON ai_usage (at);

CREATE TABLE IF NOT EXISTS ai_spend_caps (
    account_id     BIGINT PRIMARY KEY CHECK (account_id >= 0),
    daily_micros   BIGINT CHECK (daily_micros > 0),
    monthly_micros BIGINT CHECK (monthly_micros > 0),
    updated_by     TEXT NOT NULL DEFAULT '' CHECK (length(updated_by) <= 200),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS ai_spend_alerts (
    account_id   BIGINT NOT NULL CHECK (account_id >= 0),
    period       TEXT NOT NULL CHECK (period IN ('day', 'month')),
    period_start DATE NOT NULL,
    level        SMALLINT NOT NULL CHECK (level IN (80, 100)),
    cap_micros   BIGINT NOT NULL CHECK (cap_micros > 0),
    spent_micros BIGINT NOT NULL CHECK (spent_micros >= 0),
    sent_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, period, period_start, level, cap_micros)
);

-- Supabase's API roles get every new table and sequence by default; these
-- are the owner's (/admin), so shut them out.
ALTER TABLE ai_usage ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_spend_caps ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_spend_alerts ENABLE ROW LEVEL SECURITY;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') THEN
        REVOKE ALL ON ai_usage, ai_spend_caps, ai_spend_alerts FROM anon;
        REVOKE ALL ON SEQUENCE ai_usage_id_seq FROM anon;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticated') THEN
        REVOKE ALL ON ai_usage, ai_spend_caps, ai_spend_alerts FROM authenticated;
        REVOKE ALL ON SEQUENCE ai_usage_id_seq FROM authenticated;
    END IF;
END $$;
