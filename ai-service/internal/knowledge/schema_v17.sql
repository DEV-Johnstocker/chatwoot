-- Schema version 17: the images the bot may send (additive). The owner
-- uploads a few named pictures on /admin (Business profile): customer
-- reviews per product, examples, and so on, each with a line saying when to
-- send it. Where the bot writes [[IMAGE: <name>]] the system sends that
-- picture. The bytes live here, apart from business_profiles, so loading the
-- profile for every reply stays small. Code older than v17 ignores the
-- table, so rolling back is safe.
SET LOCAL lock_timeout = '5s';

CREATE TABLE IF NOT EXISTS bot_images (
    id           BIGSERIAL   PRIMARY KEY,
    account_id   BIGINT      NOT NULL CHECK (account_id > 0),
    name         TEXT        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 60),
    name_key     TEXT        NOT NULL,              -- lower-cased, single-spaced name, unique per account
    when_to_send TEXT        NOT NULL DEFAULT '' CHECK (char_length(when_to_send) <= 300),
    caption      TEXT        NOT NULL DEFAULT '' CHECK (char_length(caption) <= 1000),
    content_type TEXT        NOT NULL CHECK (content_type IN ('image/jpeg', 'image/png')),
    data         BYTEA       NOT NULL CHECK (octet_length(data) BETWEEN 1 AND 5242880),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (account_id, name_key)
);

-- Like every ai-service table: no access for Supabase's API roles, which get
-- new public tables and sequences by default. Review images can show
-- customers' names.
ALTER TABLE bot_images ENABLE ROW LEVEL SECURITY;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') THEN
        REVOKE ALL ON bot_images FROM anon;
        REVOKE ALL ON SEQUENCE bot_images_id_seq FROM anon;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticated') THEN
        REVOKE ALL ON bot_images FROM authenticated;
        REVOKE ALL ON SEQUENCE bot_images_id_seq FROM authenticated;
    END IF;
END
$$;
