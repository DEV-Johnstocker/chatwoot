-- Schema version 18: the bot's main switch and weekly hours (additive).
-- One row per account: mode on | off | scheduled, the time zone the hours are
-- read in, and seven days (Sunday first) of {on, from, to} in minutes after
-- midnight. An account without a row keeps the bot on, as before v18, so
-- existing accounts change nothing until the owner saves the /admin page.
-- Code older than v18 ignores the table, so rolling the code back is safe
-- (the bot is then on at all times again, whatever the owner saved).
SET LOCAL lock_timeout = '5s';

CREATE TABLE IF NOT EXISTS bot_schedules (
    account_id BIGINT PRIMARY KEY CHECK (account_id > 0),
    mode       TEXT NOT NULL DEFAULT 'on' CHECK (mode IN ('on', 'off', 'scheduled')),
    time_zone  TEXT NOT NULL DEFAULT 'Asia/Bangkok' CHECK (length(time_zone) BETWEEN 1 AND 64),
    days       JSONB NOT NULL
               CHECK (jsonb_typeof(days) = 'array' AND jsonb_array_length(days) = 7),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Supabase's API roles get every new table by default; switching a bot off
-- is the owner's call alone (/admin), so shut them out as earlier tables do.
ALTER TABLE bot_schedules ENABLE ROW LEVEL SECURITY;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') THEN
        REVOKE ALL ON bot_schedules FROM anon;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticated') THEN
        REVOKE ALL ON bot_schedules FROM authenticated;
    END IF;
END $$;
