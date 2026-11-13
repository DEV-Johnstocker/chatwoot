-- Schema version 17: one review image per account (additive). The owner
-- uploads it on /admin (Business profile); where the bot writes [[REVIEWS]]
-- the system sends this image to the customer. The bytes live here, apart
-- from business_profiles, so loading the profile for every reply stays
-- small. Code older than v17 ignores the table, so rolling back is safe.
SET LOCAL lock_timeout = '5s';

CREATE TABLE IF NOT EXISTS review_images (
    account_id   BIGINT      PRIMARY KEY,
    caption      TEXT        NOT NULL DEFAULT '',
    content_type TEXT        NOT NULL CHECK (content_type IN ('image/jpeg', 'image/png')),
    data         BYTEA       NOT NULL CHECK (octet_length(data) BETWEEN 1 AND 5242880),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
