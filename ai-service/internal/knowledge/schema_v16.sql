-- Schema version 16: the business profile gets a bank-transfer field (additive).
-- The owner writes the bank, account name and number once on /admin; the bot
-- may send it as written, and a reply with any other long number is stopped
-- (internal/bot/numbers.go). Existing rows get an empty value: the bot keeps
-- handing bank transfers to staff until the owner fills it in. Code older than
-- v16 ignores the column, so rolling the code back is safe.
SET LOCAL lock_timeout = '5s';

ALTER TABLE business_profiles ADD COLUMN IF NOT EXISTS bank_transfer TEXT NOT NULL DEFAULT '';
