-- Synced inventory. Each table mirrors a desktop table. IDs are made by the
-- desktop (or the website) when a row is first synced and never change.
-- `data` holds the row's fields as the desktop knows them; `field_meta`
-- records, per field, the version and time it last changed, so two edits to
-- the same card on different devices can be merged field by field.

CREATE TABLE sync_boxes (
    id           UUID PRIMARY KEY,
    account_id   UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    edition      TEXT NOT NULL CHECK (edition IN ('sports', 'tcg')),
    version      INT NOT NULL DEFAULT 1,
    data         JSONB NOT NULL DEFAULT '{}',
    field_meta   JSONB NOT NULL DEFAULT '{}',
    deleted_at   TIMESTAMPTZ,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by   UUID REFERENCES devices(id) ON DELETE SET NULL
);
CREATE INDEX sync_boxes_account ON sync_boxes (account_id, edition);

CREATE TABLE sync_cards (
    id           UUID PRIMARY KEY,
    account_id   UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    edition      TEXT NOT NULL CHECK (edition IN ('sports', 'tcg')),
    version      INT NOT NULL DEFAULT 1,
    data         JSONB NOT NULL DEFAULT '{}',
    field_meta   JSONB NOT NULL DEFAULT '{}',
    -- Copied out of data for the 10,000-card limit and the website's lists.
    box_id       UUID,
    status       TEXT,
    deleted_at   TIMESTAMPTZ,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by   UUID REFERENCES devices(id) ON DELETE SET NULL
);
CREATE INDEX sync_cards_account ON sync_cards (account_id, edition) WHERE deleted_at IS NULL;
CREATE INDEX sync_cards_box ON sync_cards (account_id, box_id) WHERE deleted_at IS NULL;

CREATE TABLE sync_sold (
    id           UUID PRIMARY KEY,
    account_id   UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    edition      TEXT NOT NULL CHECK (edition IN ('sports', 'tcg')),
    version      INT NOT NULL DEFAULT 1,
    data         JSONB NOT NULL DEFAULT '{}',
    field_meta   JSONB NOT NULL DEFAULT '{}',
    deleted_at   TIMESTAMPTZ,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by   UUID REFERENCES devices(id) ON DELETE SET NULL
);
CREATE INDEX sync_sold_account ON sync_sold (account_id, edition);

-- The change feed. Every accepted change gets the next number; a device
-- pulls everything after the last number it has seen.
CREATE TABLE sync_changes (
    seq          BIGSERIAL PRIMARY KEY,
    account_id   UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    entity       TEXT NOT NULL CHECK (entity IN ('box', 'card', 'sold')),
    entity_id    UUID NOT NULL,
    version      INT NOT NULL,
    device_id    UUID REFERENCES devices(id) ON DELETE SET NULL,
    changed_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX sync_changes_account ON sync_changes (account_id, seq);

-- An edit that lost to a newer edit of the same field on another device.
CREATE TABLE sync_conflicts (
    id            BIGSERIAL PRIMARY KEY,
    account_id    UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    entity        TEXT NOT NULL,
    entity_id     UUID NOT NULL,
    field         TEXT NOT NULL,
    kept_value    JSONB,
    lost_value    JSONB,
    lost_device   UUID REFERENCES devices(id) ON DELETE SET NULL,
    lost_edited_at TIMESTAMPTZ,
    resolved_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX sync_conflicts_open ON sync_conflicts (account_id) WHERE resolved_at IS NULL;
