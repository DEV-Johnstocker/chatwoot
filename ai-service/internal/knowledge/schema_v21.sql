-- Schema version 21: removed bot pictures are kept so the owner can put one
-- back from the History page (additive; QA F-C-03). Removing a picture on
-- /admin moves its row here; only the newest few per account are kept
-- (images.MaxRemovedKept). Code older than v21 ignores the table and deletes
-- pictures outright, so rolling the code back is safe.
SET LOCAL lock_timeout = '5s';

CREATE TABLE IF NOT EXISTS bot_images_removed (
    id           BIGSERIAL   PRIMARY KEY,
    account_id   BIGINT      NOT NULL CHECK (account_id > 0),
    image_id     BIGINT      NOT NULL,              -- its id in bot_images before removal
    name         TEXT        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 60),
    when_to_send TEXT        NOT NULL DEFAULT '' CHECK (char_length(when_to_send) <= 300),
    caption      TEXT        NOT NULL DEFAULT '' CHECK (char_length(caption) <= 1000),
    content_type TEXT        NOT NULL CHECK (content_type IN ('image/jpeg', 'image/png')),
    data         BYTEA       NOT NULL CHECK (octet_length(data) BETWEEN 1 AND 5242880),
    removed_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS bot_images_removed_account_idx ON bot_images_removed (account_id, removed_at DESC);

-- Like bot_images: no access for Supabase's API roles (review pictures can
-- show customers' names), on the table and its id sequence.
ALTER TABLE bot_images_removed ENABLE ROW LEVEL SECURITY;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') THEN
        REVOKE ALL ON bot_images_removed FROM anon;
        REVOKE ALL ON SEQUENCE bot_images_removed_id_seq FROM anon;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticated') THEN
        REVOKE ALL ON bot_images_removed FROM authenticated;
        REVOKE ALL ON SEQUENCE bot_images_removed_id_seq FROM authenticated;
    END IF;
END $$;
