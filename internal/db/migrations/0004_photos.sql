-- Card photos, kept in a private R2 bucket. Devices upload straight to the
-- bucket through short-lived signed links; this table tracks what exists.
CREATE TABLE sync_photos (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id    UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    card_id       UUID NOT NULL,
    side          TEXT NOT NULL CHECK (side IN ('front', 'back', 'front_thumb', 'back_thumb')),
    object_key    TEXT NOT NULL UNIQUE,
    bytes         BIGINT NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'ready', 'replaced')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    confirmed_at  TIMESTAMPTZ
);
CREATE INDEX sync_photos_card ON sync_photos (account_id, card_id) WHERE status = 'ready';
CREATE INDEX sync_photos_cleanup ON sync_photos (status, created_at) WHERE status <> 'ready';
